package output

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"unicode"

	"github.com/matcra587/slack-cli/internal/slackcatalog"
	slackgo "github.com/slack-go/slack"
)

var (
	missingScopeMessageRE = regexp.MustCompile(`missing required scope: ([A-Za-z0-9:._-]+(?:\s*,\s*[A-Za-z0-9:._-]+)*)`)
	slackErrorCodeRE      = regexp.MustCompile(`^[a-z][a-z0-9_]{0,127}$`)
)

var knownSlackScopes = sync.OnceValue(func() map[string]bool {
	names := make(map[string]bool)
	for _, method := range slackcatalog.All() {
		for _, scopes := range method.Scopes {
			for _, scope := range scopes {
				names[scope] = true
			}
		}
	}
	for _, scope := range []string{"admin", "bot", "client", "commands", "email", "identify", "incoming-webhook", "openid", "post", "profile", "read", "tokens.basic", "identity.basic", "identity.email", "identity.avatar", "identity.team"} {
		names[scope] = true
	}
	return names
})

// CliErrorFromSlack maps a slack-go error to a structured CLIError.
// The kind hint refines messages for codes whose meaning depends on
// what was being named, such as invalid_name for emoji vs. channel input.
func CliErrorFromSlack(ctx context.Context, err error, kind string) CLIError {
	if cliErr, ok := mapSlackError(err, kind); ok {
		return cliErr
	}
	if errors.Is(err, context.DeadlineExceeded) || ctx.Err() == context.DeadlineExceeded {
		return CLIError{Type: ErrorTypeTimeout, Message: "timeout", ExitCode: ExitCodeTimeout}
	}
	// context.Canceled covers both explicit cancel and signal cancellation;
	// errors.Is against context.Cause catches signal.signalError wrapped by url.Error.
	if errors.Is(err, context.Canceled) || ctx.Err() == context.Canceled || errors.Is(err, context.Cause(ctx)) {
		return CLIError{Type: ErrorTypeCanceled, Message: "canceled", ExitCode: ExitCodeCanceled}
	}
	if status, ok := errors.AsType[slackgo.StatusCodeError](err); ok {
		if status.Code == http.StatusTooManyRequests {
			return CLIError{Type: ErrorTypeRateLimit, Message: "ratelimited", ExitCode: ExitCodeRateLimit}
		}
		// The numeric code is useful; a server-controlled reason phrase can
		// contain credentials or signed URLs and is not a safe diagnostic.
		return RuntimeCLIError(strings.TrimSpace(fmt.Sprintf("slack server error: %d %s", status.Code, http.StatusText(status.Code))))
	}
	if _, ok := errors.AsType[*json.SyntaxError](err); ok {
		return RuntimeCLIError("Slack returned malformed JSON")
	}
	if _, ok := errors.AsType[*json.UnmarshalTypeError](err); ok {
		return RuntimeCLIError("Slack returned unexpected response data")
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return RuntimeCLIError("Slack returned an incomplete response")
	}
	// Transport and provider error strings can contain request URLs or
	// credentials. Classification is retained without publishing that text.
	return RuntimeCLIError("Slack API request failed")
}

func mapSlackError(err error, kind string) (CLIError, bool) {
	if scopeErr, ok := errors.AsType[MissingScopeError](err); ok {
		cliErr := CLIError{Type: ErrorTypeAuth, Message: scopeErr.Error(), ExitCode: ExitCodeAuthFailure}
		if needed := missingScopeDetails(scopeErr); len(needed) > 0 {
			match := "all"
			if len(scopeErr.All) == 0 {
				match = "any"
			}
			cliErr = cliErr.WithDetails(map[string]any{"needed": needed, "needed_match": match})
		}
		return cliErr, true
	}

	if rateErr, ok := errors.AsType[*slackgo.RateLimitedError](err); ok {
		seconds := max(int(math.Ceil(rateErr.RetryAfter.Seconds())), 0)
		return CLIError{
			Type:              ErrorTypeRateLimit,
			Message:           "ratelimited",
			RetryAfterSeconds: &seconds,
			ExitCode:          ExitCodeRateLimit,
		}, true
	}

	var slackErr slackgo.SlackErrorResponse
	if !errors.As(err, &slackErr) {
		return CLIError{}, false
	}
	// Error and scope fields are remote data. Keep diagnostics to their defined
	// grammar so a malformed response cannot echo a credential into CLI output.
	if !slackErrorCodeRE.MatchString(slackErr.Err) || (!strings.Contains(slackErr.Err, "_") && slackErr.Err != "ratelimited" && slackErr.Err != "accesslimited") {
		return RuntimeCLIError("Slack returned an invalid error response"), true
	}

	switch slackErr.Err {
	case "channel_not_found", "user_not_found", "message_not_found", "not_in_channel", "scheduled_message_not_found",
		"file_not_found", "file_comment_not_found", "thread_not_found", "invalid_scheduled_message_id", "users_not_found", "invalid_channel":
		return CLIError{Type: ErrorTypeNotFound, Message: slackErr.Err, ExitCode: ExitCodeNotFound}, true
	case "not_allowed_token_type", "invalid_arguments", "cant_update_message", "cant_delete_message", "time_in_past", "time_too_far",
		"bad_timestamp", "invalid_ts_latest", "invalid_ts_oldest", "invalid_cursor", "invalid_blocks", "invalid_attachments",
		"msg_too_long", "no_text", "no_item_specified", "not_reactable", "is_archived", "thread_locked",
		"too_many_attachments", "too_many_contact_cards", "invalid_arg_name", "invalid_array_arg", "invalid_form_data",
		"invalid_post_type", "invalid_charset", "missing_argument", "missing_post_type", "invalid_limit", "no_query",
		"invalid_channel_id", "invalid_profile", "invalid_emoji_not_allowed", "invalid_ooo_message", "too_long",
		"must_clear_both_status_text_and_status_emoji", "file_type_not_allowed", "file_upload_size_restricted",
		"file_uploads_except_images_disabled", "snippet_too_large", "storage_limit_reached", "channels_limit_exceeded",
		"alt_txt_too_large", "unknown_snippet_type", "unknown_subtype", "request_timeout",
		"too_many_users", "not_enough_users", "users_list_not_supplied":
		return CLIError{Type: ErrorTypeValidation, Message: slackErr.Err, ExitCode: ExitCodeValidation}, true
	case "invalid_name":
		return CLIError{
			Type:     ErrorTypeValidation,
			Message:  invalidNameMessage(kind),
			ExitCode: ExitCodeValidation,
		}, true
	case "already_reacted":
		return CLIError{Type: ErrorTypeValidation, Message: "already_reacted: this emoji is already on the message", ExitCode: ExitCodeValidation}, true
	case "no_reaction":
		return CLIError{Type: ErrorTypeNotFound, Message: "no_reaction: no such reaction on this message to remove", ExitCode: ExitCodeNotFound}, true
	case "too_many_reactions":
		return CLIError{Type: ErrorTypeValidation, Message: "too_many_reactions: Slack limit for reactions from one person reached", ExitCode: ExitCodeValidation}, true
	case "too_many_emoji":
		return CLIError{Type: ErrorTypeValidation, Message: "too_many_emoji: Slack limit for distinct emoji reactions reached", ExitCode: ExitCodeValidation}, true
	case "invalid_auth", "not_authed", "account_inactive", "token_revoked", "missing_scope", "no_permission",
		"token_expired", "access_denied", "accesslimited", "no_access", "ekm_access_denied", "restricted_action",
		"team_access_not_granted", "two_factor_setup_required", "bad_token", "invalid_token", "permission_denied",
		"file_uploads_disabled", "enterprise_is_restricted", "user_is_external_guest", "posting_to_channel_denied":
		cliErr := CLIError{Type: ErrorTypeAuth, Message: slackScopeMessage(slackErr), ExitCode: ExitCodeAuthFailure}
		if needed := neededScopeFromSlackError(slackErr); needed != "" {
			cliErr = cliErr.WithDetails(map[string]any{"needed": strings.Split(needed, ","), "needed_match": "reported"})
		}
		return cliErr, true
	case "ratelimited", "rate_limited":
		cliErr := CLIError{Type: ErrorTypeRateLimit, Message: slackErr.Err, ExitCode: ExitCodeRateLimit}
		var timing interface{ RetryAfterSeconds() *int }
		if errors.As(err, &timing) {
			cliErr.RetryAfterSeconds = timing.RetryAfterSeconds()
		}
		return cliErr, true
	default:
		return CLIError{Type: ErrorTypeServer, Message: slackErr.Err, ExitCode: ExitCodeServer}, true
	}
}

func slackScopeMessage(slackErr slackgo.SlackErrorResponse) string {
	needed := neededScopeFromSlackError(slackErr)
	if slackErr.Err == "missing_scope" && needed != "" {
		return "missing_scope: missing required Slack scope: " + needed
	}
	return slackErr.Err
}

func neededScopeFromSlackError(slackErr slackgo.SlackErrorResponse) string {
	if needed := validatedScopeList(slackErr.Needed); needed != "" {
		return needed
	}
	for _, message := range slackErr.ResponseMetadata.Messages {
		matches := missingScopeMessageRE.FindStringSubmatch(message)
		if len(matches) == 2 {
			if needed := validatedScopeList(matches[1]); needed != "" {
				return needed
			}
		}
	}
	return ""
}

func validatedScopeList(value string) string {
	scopes := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
	for _, scope := range scopes {
		if !knownSlackScopes()[scope] {
			return ""
		}
	}
	return strings.Join(scopes, ",")
}

func missingScopeDetails(err MissingScopeError) []string {
	if len(err.All) > 0 {
		return append([]string(nil), err.All...)
	}
	if len(err.Any) > 0 {
		return append([]string(nil), err.Any...)
	}
	return nil
}

// MissingScopeError is returned by scope-checking helpers when required Slack
// OAuth scopes are absent. Defined here so CliErrorFromSlack can match it via
// errors.As across packages.
type MissingScopeError struct {
	All []string
	Any []string
}

func (e MissingScopeError) Error() string {
	if len(e.All) == 1 {
		return "missing required Slack scope: " + e.All[0]
	}
	if len(e.All) > 1 {
		return "missing required Slack scopes: " + strings.Join(e.All, ",")
	}
	return "missing one of required Slack scopes: " + strings.Join(e.Any, ",")
}
