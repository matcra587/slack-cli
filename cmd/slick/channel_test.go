package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestLookupChannelListsAndShowsInfo(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"conversations.list": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("types"); got != "public_channel,private_channel" {
				t.Fatalf("types = %q, want public/private channels", got)
			}
			return testutil.JSONResponse(`{"ok":true,"channels":[{"id":"C123","name":"alerts","num_members":12,"topic":{"value":"Ops alerts"}}],"response_metadata":{"next_cursor":"next"}}`)
		},
		"conversations.info": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"channel":{"id":"C123","name":"alerts","num_members":12,"topic":{"value":"Ops alerts"}}}`)
		},
	})
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"lookup", "channel", "--max-items", "1", "--cursor", "cursor-1"},
	)
	if err != nil {
		t.Fatalf("lookup channel returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "Ops alerts") {
		t.Fatalf("stdout = %s, want topic", stdout)
	}

	stdout, stderr, err = executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"lookup", "channel", "--channel", "C123"},
	)
	if err != nil {
		t.Fatalf("lookup channel info returned error: %v\nstderr=%s", err, stderr)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if envelope["meta"].(map[string]any)["command"] != "lookup.channel" {
		t.Fatalf("meta.command = %q, want lookup.channel", envelope["meta"].(map[string]any)["command"])
	}
}

func TestLookupChannelCanListDMConversationsByType(t *testing.T) {
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
		[]string{"lookup", "channel", "--types", "im", "--max-items", "1"},
	)
	if err != nil {
		t.Fatalf("lookup channel --types im returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "D123") || !strings.Contains(stdout, `"type":"im"`) {
		t.Fatalf("stdout = %s, want DM conversation", stdout)
	}
}

func TestLookupChannelMapsMissingChannelToNotFound(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"conversations.info": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":false,"error":"channel_not_found"}`)
		},
	})
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"lookup", "channel", "--channel", "C404"},
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
