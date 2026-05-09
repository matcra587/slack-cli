package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestStatusSetCommandSetsCustomStatus(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"users.profile.set": func(req testutil.SlackRequest) testutil.SlackResponse {
			var payload struct {
				StatusText       string `json:"status_text"`
				StatusEmoji      string `json:"status_emoji"`
				StatusExpiration int64  `json:"status_expiration"`
			}
			if err := json.Unmarshal([]byte(req.Form.Get("profile")), &payload); err != nil {
				t.Fatalf("profile form value is not JSON: %v", err)
			}
			if payload.StatusText != "Heads down" || payload.StatusEmoji != ":headphones:" {
				t.Fatalf("profile = %#v, want text and normalized emoji", payload)
			}
			if want := time.Date(2026, 5, 3, 15, 8, 0, 0, time.UTC).Unix(); payload.StatusExpiration != want {
				t.Fatalf("status_expiration = %d, want %d", payload.StatusExpiration, want)
			}
			return testutil.JSONResponse(`{"ok":true,"profile":{"status_text":"Heads down","status_emoji":":headphones:","status_expiration":1777820880}}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(), "",
		[]string{"status", "set", "--text", "Heads down", "--emoji", "headphones", "--expires-in", "2h"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"command":"status.set"`) || !strings.Contains(stdout, `"text":"Heads down"`) {
		t.Fatalf("stdout = %s, want status.set result", stdout)
	}
}

func TestStatusClearCommandClearsCustomStatus(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"users.profile.set": func(req testutil.SlackRequest) testutil.SlackResponse {
			var payload struct {
				StatusText       string `json:"status_text"`
				StatusEmoji      string `json:"status_emoji"`
				StatusExpiration int64  `json:"status_expiration"`
			}
			if err := json.Unmarshal([]byte(req.Form.Get("profile")), &payload); err != nil {
				t.Fatalf("profile form value is not JSON: %v", err)
			}
			if payload.StatusText != "" || payload.StatusEmoji != "" || payload.StatusExpiration != 0 {
				t.Fatalf("profile = %#v, want cleared status", payload)
			}
			return testutil.JSONResponse(`{"ok":true,"profile":{"status_text":"","status_emoji":"","status_expiration":0}}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(), "",
		[]string{"status", "clear"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"command":"status.clear"`) || !strings.Contains(stdout, `"cleared":true`) {
		t.Fatalf("stdout = %s, want status.clear result", stdout)
	}
}
