package main

import (
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestDMListCommandWritesConversationList(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"conversations.list": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("types"); got != "im" {
				t.Fatalf("types = %q, want im", got)
			}
			return testutil.JSONResponse(`{"ok":true,"channels":[{"id":"D123","is_im":true,"user":"U123"}]}`)
		},
	})
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"dm", "list", "--max-items", "1"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "D123") {
		t.Fatalf("stdout = %s, want DM ID", stdout)
	}
}
