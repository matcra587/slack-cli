package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestSearchMessagesCommandWritesPaginatedEnvelope(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"search.messages": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"messages":{"matches":[{"channel":{"id":"C123","name":"alerts"},"user":"U1","text":"deploy failed in prod","ts":"1746284582.123456","permalink":"https://example.slack.com/archives/C123/p1746284582123456","snippet":"deploy failed"}],"pagination":{"page":1,"page_count":2}}}`)
		},
	})
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(),
		"",
		[]string{"lookup", "messages", "--query", "deploy failed", "--max-items", "1", "--cursor", "1"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if envelope["meta"].(map[string]any)["command"] != "search.messages" {
		t.Fatalf("meta.command = %q, want search.messages", envelope["meta"].(map[string]any)["command"])
	}
	if !strings.Contains(stdout, "deploy failed in prod") {
		t.Fatalf("stdout = %s, want full message text", stdout)
	}
}

func TestSearchMessagesCommandReturnsEmptyMatchesForNoResults(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"search.messages": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"messages":{"matches":[],"pagination":{"page":1,"page_count":1}}}`)
		},
	})
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(),
		"",
		[]string{"lookup", "messages", "--query", "no such message", "--max-items", "10"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	var envelope struct {
		Data struct {
			Matches []cliSearchMessage `json:"matches"`
		} `json:"data"`
		Errors []CLIError `json:"errors"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if len(envelope.Data.Matches) != 0 {
		t.Fatalf("matches = %#v, want empty", envelope.Data.Matches)
	}
	if len(envelope.Errors) != 0 {
		t.Fatalf("errors = %#v, want empty", envelope.Errors)
	}
}

func TestSearchMessagesCommandRejectsMissingQuery(t *testing.T) {
	server := testutil.NewSlackServer(t, nil)
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(),
		"",
		[]string{"lookup", "messages"},
	)
	if err == nil {
		t.Fatal("Execute returned nil error, want validation error")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, `"type":"validation_error"`) {
		t.Fatalf("stderr = %s, want validation_error", stderr)
	}
}

func TestSearchMessagesRequiresUserToken(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"search.messages": func(testutil.SlackRequest) testutil.SlackResponse {
			t.Fatal("search.messages should not be called for bot-token profiles")
			return testutil.JSONResponse(`{"ok":true}`)
		},
	})
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"lookup", "messages", "--query", "deploy failed"},
	)
	if err == nil {
		t.Fatal("Execute returned nil error, want auth failure")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, `"type":"auth_failure"`) || !strings.Contains(stderr, "user token") || !strings.Contains(stderr, "search:read") {
		t.Fatalf("stderr = %s, want user-token search scope auth failure", stderr)
	}
}
