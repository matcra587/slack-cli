package output_test

import (
	"context"
	"strings"
	"testing"

	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	slackgo "github.com/slack-go/slack"
)

func TestDocumentedSlackErrorCategoriesPreserveRecoverySignals(t *testing.T) {
	for _, group := range []struct {
		name, codes, kind string
		exit              int
	}{
		{"credentials and access", `token_expired access_denied accesslimited no_access ekm_access_denied restricted_action team_access_not_granted two_factor_setup_required bad_token invalid_token permission_denied`, "auth_failure", 1},
		{"missing targets", `file_not_found file_comment_not_found thread_not_found invalid_scheduled_message_id users_not_found`, "not_found", 2},
		{"rate limiting", `rate_limited ratelimited`, "rate_limit", 3},
		{"invalid input and unsupported target state", `bad_timestamp invalid_ts_latest invalid_ts_oldest invalid_cursor invalid_blocks invalid_attachments msg_too_long no_text no_item_specified not_reactable is_archived thread_locked too_many_emoji too_many_reactions too_many_attachments too_many_contact_cards invalid_arg_name invalid_array_arg invalid_form_data invalid_post_type invalid_charset missing_argument missing_post_type invalid_limit no_query invalid_channel_id invalid_profile invalid_emoji_not_allowed invalid_ooo_message too_long must_clear_both_status_text_and_status_emoji`, "validation_error", 4},
		{"service failures and unknown codes", `service_unavailable internal_error fatal_error future_slack_error`, "server_error", 5},
	} {
		for code := range strings.FieldsSeq(group.codes) {
			t.Run(group.name+"/"+code, func(t *testing.T) {
				t.Parallel()
				got := clioutput.CliErrorFromSlack(context.Background(), slackgo.SlackErrorResponse{Err: code}, "")
				if got.Type != group.kind || got.ExitCode != group.exit || !strings.Contains(got.Message, code) {
					t.Fatalf("%s classified as %s/%d (%s), want %s/%d", code, got.Type, got.ExitCode, got.Message, group.kind, group.exit)
				}
			})
		}
	}
}
