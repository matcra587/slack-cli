package output

import (
	"context"
	"errors"
	"math"
	"regexp"
	"strings"

	slackgo "github.com/slack-go/slack"
)

var missingScopeMessageRE = regexp.MustCompile(`missing required scope: ([A-Za-z0-9:._-]+)`)

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
	if ctx.Err() == context.Canceled || errors.Is(err, context.Cause(ctx)) {
		return CLIError{Type: ErrorTypeCanceled, Message: "canceled", ExitCode: ExitCodeCanceled}
	}
	return CLIError{Type: ErrorTypeServer, Message: err.Error(), ExitCode: ExitCodeServer}
}

func mapSlackError(err error, kind string) (CLIError, bool) {
	var scopeErr MissingScopeError
	if errors.As(err, &scopeErr) {
		cliErr := CLIError{Type: ErrorTypeAuth, Message: scopeErr.Error(), ExitCode: ExitCodeAuthFailure}
		if needed := missingScopeDetails(scopeErr); len(needed) > 0 {
			cliErr = cliErr.WithDetails(map[string]any{"needed": needed})
		}
		return cliErr, true
	}

	var rateErr *slackgo.RateLimitedError
	if errors.As(err, &rateErr) {
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

	switch slackErr.Err {
	case "channel_not_found", "user_not_found", "message_not_found", "not_in_channel", "scheduled_message_not_found":
		return CLIError{Type: ErrorTypeNotFound, Message: slackErr.Err, ExitCode: ExitCodeNotFound}, true
	case "not_allowed_token_type", "invalid_arguments", "cant_update_message", "cant_delete_message", "time_in_past", "time_too_far":
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
		return CLIError{Type: ErrorTypeValidation, Message: "too_many_reactions: Slack message reaction limit reached", ExitCode: ExitCodeValidation}, true
	case "invalid_auth", "not_authed", "account_inactive", "token_revoked", "missing_scope", "no_permission":
		cliErr := CLIError{Type: ErrorTypeAuth, Message: slackScopeMessage(slackErr), ExitCode: ExitCodeAuthFailure}
		if needed := neededScopeFromSlackError(slackErr); needed != "" {
			cliErr = cliErr.WithDetails(map[string]any{"needed": needed})
		}
		return cliErr, true
	case "ratelimited":
		return CLIError{Type: ErrorTypeRateLimit, Message: slackErr.Err, ExitCode: ExitCodeRateLimit}, true
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
	for _, message := range slackErr.ResponseMetadata.Messages {
		matches := missingScopeMessageRE.FindStringSubmatch(message)
		if len(matches) == 2 {
			return matches[1]
		}
	}
	return ""
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
