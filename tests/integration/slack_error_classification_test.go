//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBinarySlackErrorClassesPreserveNumericExitAndStreams(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tc := range []struct {
		code, kind string
		exit       int
	}{
		{"token_expired", "auth_failure", 1},
		{"accesslimited", "auth_failure", 1},
		{"message_not_found", "not_found", 2},
		{"bad_timestamp", "validation_error", 4},
		{"too_many_emoji", "validation_error", 4},
		{"internal_error", "server_error", 5},
		{"future_slack_error", "server_error", 5},
	} {
		t.Run(tc.code, func(t *testing.T) {
			const token = "xoxp-synthetic-error-classification"
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/auth.test":
					w.Header().Set("X-OAuth-Scopes", "reactions:write")
					writeJSON(w, `{"ok":true,"user_id":"U1"}`)
				case "/api/conversations.info":
					writeJSON(w, `{"ok":true,"channel":{"id":"C123","name":"fixture"}}`)
				case "/api/reactions.add":
					calls.Add(1)
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.PostForm.Get("token") != token || r.PostForm.Get("timestamp") != "1746284582.123456" || r.PostForm.Get("channel") != "C123" {
						t.Error("wrong method, target or synthetic authentication")
					}
					writeJSON(w, `{"ok":false,"error":"`+tc.code+`"}`)
				default:
					t.Errorf("unexpected path: %s", r.URL.Path)
					writeJSON(w, `{"ok":false,"error":"method_not_found"}`)
				}
			}))
			defer server.Close()
			isolated := t.TempDir()
			configPath := filepath.Join(isolated, "config.toml")
			cfg := "schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"T123\"\ntoken_type=\"user\"\ntoken=\"env:SLICK_ERROR_FIXTURE_TOKEN\"\n"
			if err := os.WriteFile(configPath, []byte(cfg), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binary, "react", "add", "--channel=C123", "--timestamp=1746284582.123456", "--emoji=rocket", "--output=compact", "--no-throttle", "--timeout=8s")
			cmd.Env = []string{"HOME=" + isolated, "SLICK_CONFIG=" + configPath, "SLICK_ERROR_FIXTURE_TOKEN=" + token, "SLACK_CLI_BASE_URL=" + server.URL}
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			if ctx.Err() != nil || exitCode(err) != tc.exit || stderr.Len() != 0 || strings.Contains(stdout.String(), token) || calls.Load() != 1 {
				t.Fatalf("wrong exit, stream, redaction or request count: err=%v stdout=%s stderr=%s", err, &stdout, &stderr)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(stdout.Bytes(), &fields); err != nil {
				t.Fatal(err)
			}
			if len(fields) != 4 || string(fields["ok"]) != "false" || string(fields["data"]) != "null" {
				t.Fatal("failure changed the four-key machine envelope")
			}
			var frame struct {
				OK   bool `json:"ok"`
				Meta struct {
					Exit int `json:"exit_code"`
				} `json:"meta"`
				Data   any `json:"data"`
				Errors []struct {
					Type string `json:"type"`
					Exit int    `json:"exit_code"`
				} `json:"errors"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &frame); err != nil {
				t.Fatal(err)
			}
			if frame.OK || frame.Meta.Exit != tc.exit || frame.Data != nil || len(frame.Errors) != 1 || frame.Errors[0].Type != tc.kind || frame.Errors[0].Exit != tc.exit {
				t.Fatal("machine failure lost its numeric class or common envelope")
			}
		})
	}
}
