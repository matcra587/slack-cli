package main

import (
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestLookupUserListsAndShowsInfo(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"users.list": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"members":[{"id":"U123","name":"matt","tz":"America/Toronto","profile":{"status_text":"Deploying"}}]}`)
		},
		"users.info": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"user":{"id":"U123","name":"matt","tz":"America/Toronto","profile":{"status_text":"Deploying"}}}`)
		},
		"users.getPresence": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"presence":"active"}`)
		},
	})
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"lookup", "user", "--max-items", "1", "--presence"},
	)
	if err != nil {
		t.Fatalf("lookup user returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "Deploying") {
		t.Fatalf("stdout = %s, want status text", stdout)
	}

	stdout, stderr, err = executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"lookup", "user", "--user", "U123", "--presence"},
	)
	if err != nil {
		t.Fatalf("lookup user info returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"presence":"active"`) {
		t.Fatalf("stdout = %s, want presence", stdout)
	}
}

func TestLookupUserMapsMissingUserToNotFound(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"users.info": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":false,"error":"user_not_found"}`)
		},
	})
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"lookup", "user", "--user", "U404"},
	)
	if err == nil {
		t.Fatal("Execute returned nil error, want not-found")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, `"type":"not_found"`) {
		t.Fatalf("stderr = %s, want not_found", stderr)
	}
}
