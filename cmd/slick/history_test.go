package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestHistoryListCommandWritesPaginatedEnvelope(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"conversations.history": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("cursor"); got != "cursor-1" {
				t.Fatalf("cursor = %q, want cursor-1", got)
			}
			return testutil.JSONResponse(`{"ok":true,"messages":[{"type":"message","user":"U1","text":"hello","ts":"1746284582.123456"}],"response_metadata":{"next_cursor":"cursor-2"}}`)
		},
	})
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"history", "list", "--channel", "C123", "--max-items", "1", "--since", "1746280000.000000", "--until", "1746290000.000000", "--user", "U1", "--cursor", "cursor-1"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}

	var envelope map[string]any
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	meta := envelope["meta"].(map[string]any)
	if meta["command"] != "history.list" {
		t.Fatalf("meta.command = %q, want history.list", meta["command"])
	}
	pagination := meta["pagination"].(map[string]any)
	if pagination["next_cursor"] != "cursor-2" {
		t.Fatalf("pagination.next_cursor = %q, want cursor-2", pagination["next_cursor"])
	}
}

func TestHistoryListCommandReadsThreadWhenThreadFlagIsSet(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"conversations.replies": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("ts"); got != "1746284582.123456" {
				t.Fatalf("ts = %q, want thread ts", got)
			}
			return testutil.JSONResponse(`{"ok":true,"messages":[{"type":"message","user":"U1","text":"parent","ts":"1746284582.123456"}]}`)
		},
	})
	defer server.Close()

	_, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"history", "list", "--channel", "C123", "--thread", "1746284582.123456", "--max-items", "10"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
}

func TestHistoryListCommandCanIncludeBoundedReplies(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"conversations.history": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"messages":[{"type":"message","user":"U1","text":"parent","ts":"1746284582.123456","reply_count":2}]}`)
		},
		"conversations.replies": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("limit"); got != "2" {
				t.Fatalf("reply limit = %q, want parent plus one reply", got)
			}
			return testutil.JSONResponse(`{"ok":true,"messages":[{"type":"message","user":"U1","text":"parent","ts":"1746284582.123456"},{"type":"message","user":"U2","text":"reply","ts":"1746284584.123456"}]}`)
		},
	})
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"history", "list", "--channel", "C123", "--include-replies", "--reply-limit", "1"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"replies":[`) {
		t.Fatalf("stdout = %s, want inline replies", stdout)
	}
}

func TestHistoryListCommandMapsMissingChannelToNotFound(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"conversations.history": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":false,"error":"channel_not_found"}`)
		},
	})
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"history", "list", "--channel", "C404"},
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
