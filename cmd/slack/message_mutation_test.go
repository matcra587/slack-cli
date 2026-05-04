package main

import (
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestMessageEditCommandUpdatesOwnedMessage(t *testing.T) {
	server := ownedMessageMutationServer(t, "chat.update")
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"message", "edit", "--channel", "C123", "--timestamp", "1746284582.123456", "--message", "new"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"message"`) {
		t.Fatalf("stdout = %s, want edited message", stdout)
	}
}

func TestMessageDeleteCommandRequiresForce(t *testing.T) {
	server := testutil.NewSlackServer(t, nil)
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"message", "delete", "--channel", "C123", "--timestamp", "1746284582.123456"},
	)
	if err == nil {
		t.Fatal("Execute returned nil error, want force validation error")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, `"type":"validation_error"`) {
		t.Fatalf("stderr = %s, want validation_error", stderr)
	}
}

func TestMessageDeleteCommandDeletesOwnedMessageWithForce(t *testing.T) {
	server := ownedMessageMutationServer(t, "chat.delete")
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"message", "delete", "--channel", "C123", "--timestamp", "1746284582.123456", "--force"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"deleted":true`) {
		t.Fatalf("stdout = %s, want deleted true", stdout)
	}
}

func ownedMessageMutationServer(t *testing.T, mutation string) *testutil.SlackServer {
	t.Helper()
	return testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"auth.test": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"user_id":"U123"}`)
		},
		"conversations.replies": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"messages":[{"type":"message","user":"U123","text":"old","ts":"1746284582.123456"}]}`)
		},
		mutation: func(testutil.SlackRequest) testutil.SlackResponse {
			if mutation == "chat.delete" {
				return testutil.JSONResponse(`{"ok":true,"channel":"C123","ts":"1746284582.123456"}`)
			}
			return testutil.JSONResponse(`{"ok":true,"channel":"C123","ts":"1746284582.123456","message":{"type":"message","user":"U123","text":"new","ts":"1746284582.123456"}}`)
		},
	})
}
