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

func TestLookupUserHidesEmptyPresence(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"users.list": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"members":[{"id":"U123","name":"matt","tz":"America/Toronto"}]}`)
		},
		"users.info": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"user":{"id":"U123","name":"matt","tz":"America/Toronto"}}`)
		},
		"users.getPresence": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"presence":""}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"--plain", "lookup", "user", "--max-items", "1", "--presence"},
	)
	if err != nil {
		t.Fatalf("lookup user returned error: %v\nstderr=%s", err, stderr)
	}
	if strings.Contains(stdout, "PRESENCE") {
		t.Fatalf("stdout = %s, should hide all-empty presence column", stdout)
	}

	stdout, stderr, err = executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"lookup", "user", "--user", "U123", "--presence"},
	)
	if err != nil {
		t.Fatalf("lookup user info returned error: %v\nstderr=%s", err, stderr)
	}
	if strings.Contains(stdout, `"presence"`) {
		t.Fatalf("stdout = %s, should omit empty presence field", stdout)
	}
}

func TestLookupUserShowsPresenceColumnWhenAnyUserHasPresence(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"users.list": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"members":[{"id":"U123","name":"matt","presence":"active"},{"id":"U456","name":"deploy"}]}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"--plain", "lookup", "user", "--max-items", "2", "--presence"},
	)
	if err != nil {
		t.Fatalf("lookup user returned error: %v\nstderr=%s", err, stderr)
	}
	for _, want := range []string{"PRESENCE", "active"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout = %s, want %q", stdout, want)
		}
	}
}

func TestLookupUserListExcludesDeletedUsersByDefault(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"users.list": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"members":[{"id":"UACTIVE","name":"active","deleted":false},{"id":"UDELETED","name":"deleted","deleted":true}]}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"lookup", "user", "--max-items", "2"},
	)
	if err != nil {
		t.Fatalf("lookup user returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "UACTIVE") {
		t.Fatalf("stdout = %s, want active user", stdout)
	}
	if strings.Contains(stdout, "UDELETED") {
		t.Fatalf("stdout = %s, did not want deleted user by default", stdout)
	}
}

func TestLookupUserListCanIncludeDeletedUsers(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"users.list": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"members":[{"id":"UACTIVE","name":"active","deleted":false},{"id":"UDELETED","name":"deleted","deleted":true}]}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"lookup", "user", "--max-items", "2", "--include-deleted"},
	)
	if err != nil {
		t.Fatalf("lookup user --include-deleted returned error: %v\nstderr=%s", err, stderr)
	}
	for _, want := range []string{"UACTIVE", "UDELETED"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout = %s, want %s", stdout, want)
		}
	}
}

func TestLookupUserMapsMissingUserToNotFound(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"users.info": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":false,"error":"user_not_found"}`)
		},
	})

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
