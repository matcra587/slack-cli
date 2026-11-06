package message_test

import (
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestRecipientPreflightUsesConversationSpecificScopes(t *testing.T) {
	for _, tc := range []struct {
		name, scopes, recipients, channel, users string
		scheduled                                bool
	}{
		{name: "direct", scopes: "chat:write,im:write", recipients: "U1", channel: "D123", users: "U1"},
		{name: "group", scopes: "chat:write,mpim:write", recipients: "U1,U2", channel: "G123", users: "U1,U2"},
		{name: "group email", scopes: "chat:write,mpim:write,users:read.email", recipients: "dev@example.test,U2", channel: "G123", users: "U1,U2"},
		{name: "scheduled group", scopes: "chat:write,mpim:write", recipients: "U1,U2", channel: "G123", users: "U1,U2", scheduled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
				"auth.test": func(testutil.SlackRequest) testutil.SlackResponse {
					response := testutil.JSONResponse(`{"ok":true,"user_id":"USELF"}`)
					response.Header.Set("X-OAuth-Scopes", tc.scopes)
					return response
				},
				"users.lookupByEmail": func(req testutil.SlackRequest) testutil.SlackResponse {
					if req.Form.Get("email") != "dev@example.test" {
						t.Error("email lookup argument changed")
					}
					return testutil.JSONResponse(`{"ok":true,"user":{"id":"U1"}}`)
				},
				"conversations.open": func(req testutil.SlackRequest) testutil.SlackResponse {
					if req.Method != "POST" || req.Form.Get("users") != tc.users || req.Form.Get("return_im") != "true" {
						t.Error("DM opening request did not match the recipient contract")
					}
					return testutil.JSONResponse(`{"ok":true,"channel":{"id":"` + tc.channel + `"}}`)
				},
				"chat.postMessage": func(req testutil.SlackRequest) testutil.SlackResponse {
					if req.Form.Get("channel") != tc.channel {
						t.Error("message used the wrong conversation")
					}
					return testutil.JSONResponse(`{"ok":true,"channel":"` + tc.channel + `","ts":"1746284582.123456","message":{"text":"Release review"}}`)
				},
				"chat.scheduleMessage": func(req testutil.SlackRequest) testutil.SlackResponse {
					if req.Form.Get("channel") != tc.channel || req.Form.Get("post_at") == "" {
						t.Error("scheduled message used the wrong target or omitted its time")
					}
					return testutil.JSONResponse(`{"ok":true,"channel":"` + tc.channel + `","scheduled_message_id":"Q123"}`)
				},
			})
			args := []string{"message", "send", "--user", tc.recipients, "--message", "Release review", "--output=json", "--no-throttle"}
			if tc.scheduled {
				args = append(args, "--schedule", "1h")
			}
			stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(), "", args)
			if err != nil || stderr != "" {
				t.Fatalf("valid minimum scopes were rejected: err=%v stdout=%s stderr=%s", err, stdout, stderr)
			}
			wantMethod, forbiddenMethod := "chat.postMessage", "chat.scheduleMessage"
			if tc.scheduled {
				wantMethod, forbiddenMethod = forbiddenMethod, wantMethod
			}
			if len(server.Requests("conversations.open")) != 1 || len(server.Requests(wantMethod)) != 1 || len(server.Requests(forbiddenMethod)) != 0 {
				t.Fatal("recipient send did not perform exactly the intended mutations")
			}
			wantLookups := 0
			if strings.Contains(tc.recipients, "@") {
				wantLookups = 1
			}
			if len(server.Requests("users.lookupByEmail")) != wantLookups {
				t.Fatal("recipient lookup count changed")
			}
		})
	}
}

func TestRecipientMissingScopesStopBeforeOpeningOrSending(t *testing.T) {
	for _, tc := range []struct{ name, recipients, scopes, missing string }{
		{"group requires group scope", "U1,U2", "chat:write,im:write", "mpim:write"},
		{"email requires email lookup scope", "dev@example.test,U2", "chat:write,mpim:write", "users:read.email"},
		{"direct requires direct scope", "U1", "chat:write,mpim:write", "im:write"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
				"auth.test": func(testutil.SlackRequest) testutil.SlackResponse {
					response := testutil.JSONResponse(`{"ok":true,"user_id":"USELF"}`)
					response.Header.Set("X-OAuth-Scopes", tc.scopes)
					return response
				},
				"users.lookupByEmail": func(testutil.SlackRequest) testutil.SlackResponse {
					return testutil.JSONResponse(`{"ok":true,"user":{"id":"U1"}}`)
				},
				"conversations.open": func(testutil.SlackRequest) testutil.SlackResponse {
					return testutil.JSONResponse(`{"ok":true,"channel":{"id":"D123"}}`)
				},
				"chat.postMessage": func(testutil.SlackRequest) testutil.SlackResponse {
					return testutil.JSONResponse(`{"ok":true,"channel":"D123","ts":"1746284582.123456"}`)
				},
			})
			stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(), "", []string{"message", "send", "--user", tc.recipients, "--message", "Release review", "--output=json", "--no-throttle"})
			if err == nil || stderr != "" || !strings.Contains(stdout, `"type":"auth_failure"`) || !strings.Contains(stdout, tc.missing) {
				t.Fatalf("missing scope was not reported correctly: err=%v stdout=%s stderr=%s", err, stdout, stderr)
			}
			for _, method := range []string{"users.lookupByEmail", "conversations.open", "chat.postMessage"} {
				if len(server.Requests(method)) != 0 {
					t.Errorf("missing scope reached %s", method)
				}
			}
		})
	}
}
