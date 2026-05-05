package main

import (
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestReactionCommandAddRemoveAndList(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"reactions.add": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("name"); got != "thumbsup" {
				t.Fatalf("add name = %q, want thumbsup", got)
			}
			return testutil.JSONResponse(`{"ok":true}`)
		},
		"reactions.remove": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("name"); got != "thumbsup" {
				t.Fatalf("remove name = %q, want thumbsup", got)
			}
			return testutil.JSONResponse(`{"ok":true}`)
		},
		"reactions.get": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"message":{"reactions":[{"name":"thumbsup","count":1,"users":["U1"]}]}}`)
		},
	})
	defer server.Close()

	for _, args := range [][]string{
		{"reaction", "add", "--channel", "C123", "--timestamp", "1746284582.123456", "--emoji", ":thumbsup:"},
		{"reaction", "remove", "--channel", "C123", "--timestamp", "1746284582.123456", "--emoji", "thumbsup"},
		{"reaction", "list", "--channel", "C123", "--timestamp", "1746284582.123456"},
	} {
		stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "", args)
		if err != nil {
			t.Fatalf("Execute %v returned error: %v\nstderr=%s", args, err, stderr)
		}
		if !strings.Contains(stdout, `"reaction`) {
			t.Fatalf("stdout for %v = %s, want reaction data", args, stdout)
		}
	}
}

func TestReactionCommandDryRunSkipsMutation(t *testing.T) {
	for _, tt := range []struct {
		name   string
		action string
		method string
		want   string
	}{
		{name: "add", action: "add", method: "reactions.add", want: `"dry_run":true`},
		{name: "remove", action: "remove", method: "reactions.remove", want: `"removed":true`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := testutil.NewSlackServer(t, nil)
			defer server.Close()

			stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
				"",
				[]string{"reaction", tt.action, "--channel", "C123", "--timestamp", "1746284582.123456", "--emoji", "thumbsup", "--dry-run"},
			)
			if err != nil {
				t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
			}
			if got := len(server.Requests(tt.method)); got != 0 {
				t.Fatalf("%s requests = %d, want 0", tt.method, got)
			}
			if !strings.Contains(stdout, `"dry_run":true`) || !strings.Contains(stdout, tt.want) {
				t.Fatalf("stdout = %s, want dry_run true and %s", stdout, tt.want)
			}
		})
	}
}
