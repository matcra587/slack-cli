package output_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	slackgo "github.com/slack-go/slack"
)

func TestDocumentedUploadFailuresKeepActionableErrorClasses(t *testing.T) {
	t.Parallel()
	for _, group := range []struct {
		name  string
		codes []string
		kind  string
		exit  int
	}{
		{"workspace and account restrictions", []string{"file_uploads_disabled", "enterprise_is_restricted", "user_is_external_guest", "posting_to_channel_denied"}, "auth_failure", 1},
		{"missing channel", []string{"invalid_channel"}, "not_found", 2},
		{"file and request restrictions", []string{"file_type_not_allowed", "file_upload_size_restricted", "file_uploads_except_images_disabled", "snippet_too_large", "storage_limit_reached", "channels_limit_exceeded", "alt_txt_too_large", "unknown_snippet_type", "unknown_subtype", "request_timeout"}, "validation_error", 4},
		{"service or unknown failures", []string{"file_update_failed", "fatal_error", "internal_error", "service_unavailable", "org_login_required", "team_added_to_org", "future_file_error"}, "server_error", 5},
	} {
		for _, code := range group.codes {
			t.Run(group.name+"/"+code, func(t *testing.T) {
				t.Parallel()
				got := clioutput.CliErrorFromSlack(t.Context(), fmt.Errorf("synthetic wrapped failure: %w", slackgo.SlackErrorResponse{Err: code}), "")
				if got.Type != group.kind || got.ExitCode != group.exit || got.Message != code || got.RetryAfterSeconds != nil {
					t.Fatalf("%s returned %s/%d, want %s/%d", code, got.Type, got.ExitCode, group.kind, group.exit)
				}
			})
		}
	}
}

func TestUploadRequestTimeoutCodeIsDifferentFromCallerDeadline(t *testing.T) {
	t.Parallel()
	remote := clioutput.CliErrorFromSlack(t.Context(), slackgo.SlackErrorResponse{Err: "request_timeout"}, "")
	deadline := clioutput.CliErrorFromSlack(t.Context(), fmt.Errorf("synthetic wrapped deadline: %w", context.DeadlineExceeded), "")
	canceled := clioutput.CliErrorFromSlack(t.Context(), errors.Join(errors.New("synthetic wrapped cancel"), context.Canceled), "")
	if remote.ExitCode != 4 || remote.Type != "validation_error" || deadline.ExitCode != 7 || deadline.Type != "timeout" || canceled.ExitCode != 6 || canceled.Type != "canceled" {
		t.Fatal("Slack's incomplete POST body error was confused with caller cancellation or deadline")
	}
}
