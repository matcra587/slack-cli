package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestDMSendCommandOpensConversationThenSendsMessage(t *testing.T) {
	t.Setenv("CLAUDE_CODE", "1")

	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"conversations.open": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("users"); got != "U123" {
				t.Fatalf("users = %q, want U123", got)
			}
			return testutil.JSONResponse(`{"ok":true,"channel":{"id":"D123"}}`)
		},
		"chat.postMessage": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("channel"); got != "D123" {
				t.Fatalf("channel = %q, want D123", got)
			}
			return testutil.JSONResponse(`{"ok":true,"channel":"D123","ts":"1746284582.123456","message":{"type":"message","text":"hello","ts":"1746284582.123456"}}`)
		},
		"chat.getPermalink": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"permalink":"https://example.slack.com/archives/D123/p1746284582123456"}`)
		},
	})
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(),
		"",
		[]string{"dm", "send", "--user", "U123", "--message", "hello"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}

	var envelope map[string]any
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	data := envelope["data"].(map[string]any)
	message := data["message"].(map[string]any)
	if message["channel"] != "D123" {
		t.Fatalf("message.channel = %q, want D123", message["channel"])
	}
	if data["attribution"] != true {
		t.Fatalf("attribution = %#v, want true", data["attribution"])
	}
}

func TestDMSendCommandRejectsBotTokenBeforeSlackRequest(t *testing.T) {
	server := testutil.NewSlackServer(t, nil)
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"dm", "send", "--user", "U123", "--message", "hello"},
	)
	if err == nil {
		t.Fatal("Execute returned nil error, want bot-token validation error")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, `"type":"validation_error"`) {
		t.Fatalf("stderr = %s, want validation_error", stderr)
	}
	if got := len(server.Requests("conversations.open")); got != 0 {
		t.Fatalf("conversations.open requests = %d, want 0", got)
	}
}
