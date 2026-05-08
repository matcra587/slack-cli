package main

import (
	"encoding/json"
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

func TestMessageEditCommandSupportsBlockInput(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"auth.test": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"user_id":"U123"}`)
		},
		"conversations.history": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"messages":[{"type":"message","user":"U123","text":"old","ts":"1746284582.123456"}]}`)
		},
		"chat.update": func(req testutil.SlackRequest) testutil.SlackResponse {
			var blocks []map[string]any
			if err := json.Unmarshal([]byte(req.Form.Get("blocks")), &blocks); err != nil {
				t.Fatalf("blocks form value is not JSON: %v", err)
			}
			text := blocks[0]["text"].(map[string]any)
			if text["text"] != "edit block" {
				t.Fatalf("block text = %q, want edit block", text["text"])
			}
			return testutil.JSONResponse(`{"ok":true,"channel":"C123","ts":"1746284582.123456","message":{"type":"message","user":"U123","text":"edit block","ts":"1746284582.123456"}}`)
		},
	})
	defer server.Close()

	_, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"message", "edit", "--channel", "C123", "--timestamp", "1746284582.123456", "--blocks", "--message", `[{"type":"section","text":{"type":"mrkdwn","text":"edit block"}}]`},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
}

func TestMessageEditCommandRejectsMalformedBlockInput(t *testing.T) {
	server := testutil.NewSlackServer(t, nil)
	defer server.Close()

	_, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"message", "edit", "--channel", "C123", "--timestamp", "1746284582.123456", "--blocks", "--message", `not-json`},
	)
	if err == nil {
		t.Fatal("Execute returned nil error, want malformed block validation error")
	}
	if got := len(server.Requests("chat.update")); got != 0 {
		t.Fatalf("chat.update requests = %d, want 0", got)
	}
	if !strings.Contains(stderr, `"type":"validation_error"`) {
		t.Fatalf("stderr = %s, want validation_error", stderr)
	}
}

func TestMessageEditCommandPreservesUnsupportedMarkdownSourceFallback(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"auth.test": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"user_id":"U123"}`)
		},
		"conversations.history": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"messages":[{"type":"message","user":"U123","text":"old","ts":"1746284582.123456"}]}`)
		},
		"chat.update": func(req testutil.SlackRequest) testutil.SlackResponse {
			var blocks []map[string]any
			if err := json.Unmarshal([]byte(req.Form.Get("blocks")), &blocks); err != nil {
				t.Fatalf("blocks form value is not JSON: %v", err)
			}
			if got := rawSectionText(t, blocks[0]); got != "```text\nfixed\n```" {
				t.Fatalf("block text = %q, want source-preserving fenced code", got)
			}
			return testutil.JSONResponse(`{"ok":true,"channel":"C123","ts":"1746284582.123456","message":{"type":"message","user":"U123","text":"fixed","ts":"1746284582.123456"}}`)
		},
	})
	defer server.Close()

	_, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"message", "edit", "--channel", "C123", "--timestamp", "1746284582.123456", "--message", "```text\nfixed\n```\n"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
}

func TestMessageEditCommandRejectsInvalidRawBlockRequiredFieldsBeforeSlackRequest(t *testing.T) {
	server := testutil.NewSlackServer(t, nil)
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"message", "edit", "--channel", "C123", "--timestamp", "1746284582.123456", "--blocks", "--message", `[{"type":"image","alt_text":"missing-url"}]`},
	)
	if err == nil {
		t.Fatal("Execute returned nil error, want raw block validation error")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	for _, method := range []string{"auth.test", "conversations.replies", "chat.update"} {
		if got := len(server.Requests(method)); got != 0 {
			t.Fatalf("%s requests = %d, want 0", method, got)
		}
	}
	if !strings.Contains(stderr, `"type":"validation_error"`) || !strings.Contains(stderr, "image_url or slack_file is required") {
		t.Fatalf("stderr = %s, want image validation error", stderr)
	}
}

func TestMessageEditCommandDryRunSkipsOwnershipAndMutation(t *testing.T) {
	server := testutil.NewSlackServer(t, nil)
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"message", "edit", "--channel", "C123", "--timestamp", "1746284582.123456", "--message", "preview", "--dry-run"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	for _, method := range []string{"auth.test", "conversations.replies", "chat.update"} {
		if got := len(server.Requests(method)); got != 0 {
			t.Fatalf("%s requests = %d, want 0", method, got)
		}
	}
	if !strings.Contains(stdout, `"dry_run":true`) {
		t.Fatalf("stdout = %s, want dry_run true", stdout)
	}
}

func TestMessageEditCommandMapsCantUpdateMessageToValidationError(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"chat.update": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":false,"error":"cant_update_message"}`)
		},
	})
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"message", "edit", "--channel", "C123", "--timestamp", "1746284582.123456", "--message", "new"},
	)
	if err == nil {
		t.Fatal("Execute returned nil error, want validation_error for cant_update_message")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, `"type":"validation_error"`) || !strings.Contains(stderr, "cant_update_message") {
		t.Fatalf("stderr = %s, want validation_error containing cant_update_message", stderr)
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

func TestMessageDeleteCommandDryRunSkipsOwnershipAndMutation(t *testing.T) {
	server := testutil.NewSlackServer(t, nil)
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"message", "delete", "--channel", "C123", "--timestamp", "1746284582.123456", "--dry-run"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	for _, method := range []string{"auth.test", "conversations.replies", "chat.delete"} {
		if got := len(server.Requests(method)); got != 0 {
			t.Fatalf("%s requests = %d, want 0", method, got)
		}
	}
	if !strings.Contains(stdout, `"dry_run":true`) {
		t.Fatalf("stdout = %s, want dry_run true", stdout)
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

func TestMessageDeleteCommandDeletesThreadReplyWithForce(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"chat.delete": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"channel":"C123","ts":"1746284582.999999"}`)
		},
	})
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"message", "delete", "--channel", "C123", "--timestamp", "1746284582.999999", "--force"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"deleted":true`) {
		t.Fatalf("stdout = %s, want deleted true", stdout)
	}
}

func TestMessageDeleteCommandMapsCantDeleteMessageToValidationError(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"chat.delete": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":false,"error":"cant_delete_message"}`)
		},
	})
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"message", "delete", "--channel", "C123", "--timestamp", "1746284582.123456", "--force"},
	)
	if err == nil {
		t.Fatal("Execute returned nil error, want validation_error for cant_delete_message")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, `"type":"validation_error"`) || !strings.Contains(stderr, "cant_delete_message") {
		t.Fatalf("stderr = %s, want validation_error containing cant_delete_message", stderr)
	}
}

func ownedMessageMutationServer(t *testing.T, mutation string) *testutil.SlackServer {
	t.Helper()
	handlers := map[string]testutil.SlackHandler{
		mutation: func(testutil.SlackRequest) testutil.SlackResponse {
			if mutation == "chat.delete" {
				return testutil.JSONResponse(`{"ok":true,"channel":"C123","ts":"1746284582.123456"}`)
			}
			return testutil.JSONResponse(`{"ok":true,"channel":"C123","ts":"1746284582.123456","message":{"type":"message","user":"U123","text":"new","ts":"1746284582.123456"}}`)
		},
	}
	return testutil.NewSlackServer(t, handlers)
}
