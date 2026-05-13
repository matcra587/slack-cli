package output

import (
	"context"
	"strings"
	"testing"

	slackgo "github.com/slack-go/slack"
)

func TestCliErrorFromSlackMissingScopeIncludesNeeded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		err         error
		wantMessage string
		wantNeeded  any
	}{
		{
			name: "slack response metadata message",
			err: slackgo.SlackErrorResponse{
				Err: "missing_scope",
				ResponseMetadata: slackgo.ResponseMetadata{
					Messages: []string{"[ERROR] missing required scope: chat:write"},
				},
			},
			wantMessage: "missing_scope: missing required Slack scope: chat:write",
			wantNeeded:  "chat:write",
		},
		{
			name:        "local scope preflight",
			err:         MissingScopeError{All: []string{"files:write"}},
			wantMessage: "missing required Slack scope: files:write",
			wantNeeded:  []string{"files:write"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := CliErrorFromSlack(context.Background(), tt.err, "")
			if got.Type != ErrorTypeAuth {
				t.Fatalf("type = %q, want %q", got.Type, ErrorTypeAuth)
			}
			if got.Message != tt.wantMessage {
				t.Fatalf("message = %q, want %q", got.Message, tt.wantMessage)
			}
			if got.Details["needed"] == nil {
				t.Fatalf("details = %#v, want needed=%#v", got.Details, tt.wantNeeded)
			}
			if !strings.Contains(got.Message, "scope") {
				t.Fatalf("message = %q, want actionable scope text", got.Message)
			}
		})
	}
}
