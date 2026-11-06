package main

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestSlackFailuresUseDocumentedClassesAcrossCommandFamilies(t *testing.T) {
	t.Setenv("SLICK_CONFIG", filepath.Join(t.TempDir(), "unused.toml"))
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	reaction := []string{"react", "add", "--channel=C123", "--timestamp=1746284582.123456", "--emoji=rocket"}
	history := []string{"history", "list", "--channel=C123"}
	for _, tc := range []struct {
		code, method, kind string
		exit               int
		args               []string
	}{
		{"bad_timestamp", "reactions.add", "validation_error", 4, reaction},
		{"too_many_emoji", "reactions.add", "validation_error", 4, reaction},
		{"thread_locked", "reactions.add", "validation_error", 4, reaction},
		{"no_access", "reactions.add", "auth_failure", 1, reaction},
		{"access_denied", "reactions.add", "auth_failure", 1, reaction},
		{"accesslimited", "reactions.add", "auth_failure", 1, reaction},
		{"token_expired", "users.profile.set", "auth_failure", 1, []string{"status", "set", "--text=Reviewing changes"}},
		{"permission_denied", "users.profile.set", "auth_failure", 1, []string{"status", "set", "--text=Reviewing changes"}},
		{"too_long", "users.profile.set", "validation_error", 4, []string{"status", "set", "--text=Reviewing changes"}},
		{"invalid_emoji_not_allowed", "users.profile.set", "validation_error", 4, []string{"status", "set", "--text=Reviewing changes"}},
		{"rate_limited", "chat.postMessage", "rate_limit", 3, []string{"message", "send", "--channel=C123", "--message=Review complete"}},
		{"invalid_cursor", "conversations.history", "validation_error", 4, history},
		{"invalid_ts_oldest", "conversations.history", "validation_error", 4, history},
		{"invalid_ts_latest", "conversations.history", "validation_error", 4, history},
		{"restricted_action", "conversations.history", "auth_failure", 1, history},
		{"thread_not_found", "conversations.replies", "not_found", 2, []string{"history", "list", "--channel=C123", "--thread=1746284582.123456"}},
		{"file_not_found", "chat.update", "not_found", 2, []string{"message", "edit", "--channel=C123", "--timestamp=1746284582.123456", "--message=Review complete"}},
		{"invalid_blocks", "chat.update", "validation_error", 4, []string{"message", "edit", "--channel=C123", "--timestamp=1746284582.123456", "--message=Review complete"}},
		{"invalid_scheduled_message_id", "chat.deleteScheduledMessage", "not_found", 2, []string{"message", "scheduled", "delete", "--channel=C123", "--scheduled-id=Q1"}},
		{"internal_error", "reactions.add", "server_error", 5, reaction},
		{"future_slack_error", "reactions.add", "server_error", 5, reaction},
	} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(tc.code+"/"+mode, func(t *testing.T) {
				server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
					"auth.test": func(testutil.SlackRequest) testutil.SlackResponse {
						response := testutil.JSONResponse(`{"ok":true,"user_id":"U1"}`)
						response.Header.Set("X-OAuth-Scopes", "reactions:write,channels:history,chat:write,users.profile:write")
						return response
					},
					tc.method: func(req testutil.SlackRequest) testutil.SlackResponse {
						if req.Method != "POST" || req.Path != "/api/"+tc.method || req.Form.Get("token") != "xox-test" || !strings.HasPrefix(req.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
							t.Error("wrong SDK method, path, encoding or synthetic authentication")
						}
						return testutil.JSONResponse(`{"ok":false,"error":"` + tc.code + `"}`)
					},
				})
				args := append(append([]string{}, tc.args...), "--output="+mode, "--no-throttle")
				stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(), "", args)
				cliErr, ok := errors.AsType[clioutput.CommandError](err)
				if !ok || cliErr.CLIError.ExitCode != tc.exit || cliErr.CLIError.Type != tc.kind {
					t.Fatalf("wrong recovery classification for %s: err=%v stdout=%s stderr=%s", tc.code, err, stdout, stderr)
				}
				if strings.Contains(stdout+stderr, "xox-test") || len(server.Requests(tc.method)) != 1 {
					t.Fatal("credential leaked or failed operation replayed")
				}
				if mode == "human" {
					if stdout != "" || !strings.Contains(stderr, tc.code) {
						t.Fatal("human error used stdout or lost its safe Slack code")
					}
					return
				}
				var fields map[string]json.RawMessage
				if err := json.Unmarshal([]byte(stdout), &fields); err != nil {
					t.Fatal(err)
				}
				if len(fields) != 4 || string(fields["ok"]) != "false" || string(fields["data"]) != "null" {
					t.Fatal("failure changed the four-key machine envelope")
				}
				var frame struct {
					OK   bool `json:"ok"`
					Meta struct {
						Exit int `json:"exit_code"`
					} `json:"meta"`
					Data   any `json:"data"`
					Errors []struct {
						Type string `json:"type"`
						Exit int    `json:"exit_code"`
					} `json:"errors"`
				}
				if err := json.Unmarshal([]byte(stdout), &frame); err != nil {
					t.Fatal(err)
				}
				if frame.OK || frame.Meta.Exit != tc.exit || frame.Data != nil || len(frame.Errors) != 1 || frame.Errors[0].Type != tc.kind || frame.Errors[0].Exit != tc.exit || stderr != "" {
					t.Fatal("failure classification broke the common machine envelope")
				}
			})
		}
	}
}
