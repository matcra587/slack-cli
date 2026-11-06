package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

// Reaction methods decode SlackResponse directly. Some SDK history/user response
// types shadow response_metadata; fallback diagnostics are available only when
// the SDK preserves that metadata. Top-level Needed is checked on all paths.
func TestSlackErrorDiagnosticsDoNotEchoResponseCredentials(t *testing.T) {
	t.Setenv("SLICK_CONFIG", filepath.Join(t.TempDir(), "unused.toml"))
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, tc := range []struct {
		name, body, needed string
		exit               int
	}{
		{name: "opaque error field", body: `{"ok":false,"error":"abcdef0123456789abcdef0123456789"}`, exit: 5},
		{name: "unknown scope field", body: `{"ok":false,"error":"missing_scope","needed":"credential:abcdef0123456789abcdef0123456789"}`, exit: 1},
		{name: "error field", body: `{"ok":false,"error":"xox-test"}`, exit: 5},
		{name: "structured needed", body: `{"ok":false,"error":"missing_scope","needed":"xox-test"}`, exit: 1},
		{name: "legacy metadata", body: `{"ok":false,"error":"missing_scope","response_metadata":{"messages":["missing required scope: xox-test"]}}`, exit: 1},
		{name: "safe legacy fallback", body: `{"ok":false,"error":"missing_scope","needed":"xox-test","response_metadata":{"messages":["missing required scope: channels:history"]}}`, exit: 1, needed: "channels:history"},
		{name: "valid scope list", body: `{"ok":false,"error":"missing_scope","needed":"channels:history, groups:history"}`, exit: 1, needed: "channels:history,groups:history"},
	} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
					"auth.test": func(testutil.SlackRequest) testutil.SlackResponse {
						return testutil.JSONResponse(`{"ok":true,"user_id":"U1"}`)
					},
					"reactions.add": func(req testutil.SlackRequest) testutil.SlackResponse {
						if req.Form.Get("token") != "xox-test" {
							t.Error("fixture did not receive its explicit synthetic token")
						}
						return testutil.JSONResponse(tc.body)
					},
				})
				stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(), "", []string{"react", "add", "--channel", "C123", "--timestamp", "1746284582.123456", "--emoji", "rocket", "--output=" + mode, "--no-throttle"})
				if err == nil {
					t.Fatal("Slack error reported success")
				}
				if strings.Contains(stdout+stderr+err.Error(), "xox-test") || strings.Contains(stdout+stderr+err.Error(), "abcdef0123456789abcdef0123456789") {
					t.Fatal("upstream error fields disclosed the synthetic credential")
				}
				if len(server.Requests("reactions.add")) != 1 {
					t.Fatal("failed request was replayed")
				}
				if tc.needed != "" && !strings.Contains(stdout+stderr, tc.needed) {
					t.Fatal("validated scope diagnostics were lost")
				}
				if mode == "human" {
					if stdout != "" || stderr == "" {
						t.Fatal("human error used the wrong stream")
					}
					return
				}
				var frame struct {
					OK   bool `json:"ok"`
					Meta struct {
						ExitCode int `json:"exit_code"`
					} `json:"meta"`
					Data any `json:"data"`
				}
				if decodeErr := json.Unmarshal([]byte(stdout), &frame); decodeErr != nil || frame.OK || frame.Data != nil || frame.Meta.ExitCode != tc.exit || stderr != "" {
					t.Fatal("safe error lost its class or common failure envelope")
				}
			})
		}
	}
}

func TestSlackHTTPStatusDiagnosticsDoNotEchoReasonPhrase(t *testing.T) {
	t.Setenv("SLICK_CONFIG", filepath.Join(t.TempDir(), "unused.toml"))
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth.test" || r.URL.Path == "/api/conversations.info" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, `{"ok":true,"user_id":"U1","channel":{"id":"C123","name":"fixture"}}`)
			return
		}
		if r.URL.Path != "/api/reactions.add" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		requests.Add(1)
		conn, buffer, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = fmt.Fprint(buffer, "HTTP/1.1 500 xox-test\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
		_ = buffer.Flush()
	}))
	defer server.Close()
	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.URL, "", []string{"react", "add", "--channel", "C123", "--timestamp", "1746284582.123456", "--emoji", "rocket", "--output=json", "--no-throttle"})
	if err == nil || stderr != "" || strings.Contains(stdout+err.Error(), "xox-test") || !strings.Contains(stdout, "500") || !strings.Contains(stdout, `"exit_code":5`) {
		t.Fatal("HTTP error exposed unreviewed reason text or lost the numeric status")
	}
	if requests.Load() != 1 {
		t.Fatal("failed mutation was replayed")
	}
}
