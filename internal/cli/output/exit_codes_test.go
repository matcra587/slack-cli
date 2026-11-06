package output

import (
	"context"
	"errors"
	"net/url"
	"reflect"
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
			name:        "structured scope without metadata",
			err:         slackgo.SlackErrorResponse{Err: "missing_scope", Needed: "chat:write,users:read"},
			wantMessage: "missing_scope: missing required Slack scope: chat:write,users:read",
			wantNeeded:  []string{"chat:write", "users:read"},
		},
		{
			name:        "structured scope takes precedence",
			err:         slackgo.SlackErrorResponse{Err: "missing_scope", Needed: " files:write ", ResponseMetadata: slackgo.ResponseMetadata{Messages: []string{"[ERROR] missing required scope: chat:write"}}},
			wantMessage: "missing_scope: missing required Slack scope: files:write",
			wantNeeded:  []string{"files:write"},
		},
		{
			name:        "known legacy standalone scopes",
			err:         slackgo.SlackErrorResponse{Err: "missing_scope", Needed: "bot, incoming-webhook"},
			wantMessage: "missing_scope: missing required Slack scope: bot,incoming-webhook",
			wantNeeded:  []string{"bot", "incoming-webhook"},
		},
		{
			name: "slack response metadata message",
			err: slackgo.SlackErrorResponse{
				Err: "missing_scope",
				ResponseMetadata: slackgo.ResponseMetadata{
					Messages: []string{"[ERROR] missing required scope: chat:write"},
				},
			},
			wantMessage: "missing_scope: missing required Slack scope: chat:write",
			wantNeeded:  []string{"chat:write"},
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
			if !reflect.DeepEqual(got.Details["needed"], tt.wantNeeded) {
				t.Fatalf("details = %#v, want needed=%#v", got.Details, tt.wantNeeded)
			}
			if !strings.Contains(got.Message, "scope") {
				t.Fatalf("message = %q, want actionable scope text", got.Message)
			}
		})
	}
}

func TestSlackTransportDiagnosticsHideUnreviewedProviderText(t *testing.T) {
	for _, err := range []error{
		&url.Error{Op: "Post", URL: "https://example.invalid/?signature=synthetic-private-link", Err: errors.New("provider xox-test")},
		errors.New("refusing to send token to https://example.invalid/?signature=synthetic-private-link"),
	} {
		got := CliErrorFromSlack(context.Background(), err, "")
		if got.Type != ErrorTypeServer || got.ExitCode != ExitCodeServer || strings.Contains(got.Message, "synthetic-private-link") || strings.Contains(got.Message, "xox-test") {
			t.Fatal("transport/provider failure leaked an unreviewed credential or changed class")
		}
	}
}
