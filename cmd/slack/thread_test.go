package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestThreadReplyCommandPostsNestedReply(t *testing.T) {
	t.Setenv("CLAUDE_CODE", "1")

	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"chat.postMessage": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("thread_ts"); got != "1746284582.123456" {
				t.Fatalf("thread_ts = %q, want parent timestamp", got)
			}
			return testutil.JSONResponse(`{"ok":true,"channel":"C123","ts":"1746284599.123456","message":{"type":"message","text":"reply","ts":"1746284599.123456","thread_ts":"1746284582.123456"}}`)
		},
		"chat.getPermalink": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"permalink":"https://example.slack.com/archives/C123/p1746284599123456"}`)
		},
	})
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"thread", "reply", "--channel", "C123", "--parent", "1746284582.123456", "--message", "reply"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}

	var envelope map[string]any
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if envelope["meta"].(map[string]any)["command"] != "thread.reply" {
		t.Fatalf("meta.command = %q, want thread.reply", envelope["meta"].(map[string]any)["command"])
	}
	data := envelope["data"].(map[string]any)
	if data["attribution"] != true {
		t.Fatalf("attribution = %#v, want true", data["attribution"])
	}
}

func TestThreadReplyCommandMapsInvalidParentToNotFound(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"chat.postMessage": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":false,"error":"message_not_found"}`)
		},
	})
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"thread", "reply", "--channel", "C123", "--parent", "1746284582.123456", "--message", "reply"},
	)
	if err == nil {
		t.Fatal("Execute returned nil error, want not-found error")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, `"type":"not_found"`) {
		t.Fatalf("stderr = %s, want not_found", stderr)
	}
}
