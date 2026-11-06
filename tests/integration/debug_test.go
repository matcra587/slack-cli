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

func TestBinaryDebugOmitsOpaqueSecretsAndPreservesMutationRecovery(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, mode := range []string{"json", "compact", "human"} {
		for _, debug := range []string{"", "--debug", "-D"} {
			t.Run(mode+"/"+debug, func(t *testing.T) {
				const token = "xoxp-synthetic-debug-fixture"
				var calls atomic.Int32
				var recovered atomic.Bool
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.PostForm.Get("token") != token {
						t.Error("wrong method or synthetic authentication")
					}
					w.Header().Set("Content-Type", "application/json")
					switch r.URL.Path {
					case "/api/auth.test":
						w.Header().Set("X-OAuth-Scopes", "reactions:write")
						writeJSON(w, `{"ok":true,"user_id":"U1"}`)
					case "/api/conversations.info":
						writeJSON(w, `{"ok":true,"channel":{"id":"C123","name":"fixture"}}`)
					case "/api/reactions.add":
						calls.Add(1)
						if r.PostForm.Get("channel") != "C123" || r.PostForm.Get("timestamp") != "1746284582.123456" || r.PostForm.Get("name") != "rocket" {
							t.Error("wrong reaction target or emoji")
						}
						if recovered.Load() {
							writeJSON(w, `{"ok":true}`)
							return
						}
						w.Header().Set("Set-Cookie", "session=synthetic-private-cookie")
						w.Header().Set("X-Unreviewed", "synthetic-private-header")
						w.WriteHeader(http.StatusBadGateway)
						writeJSON(w, `{"upload_url":"https://fixture.invalid/synthetic-upload-capability","unreviewed_secret":"synthetic-unreviewed-secret"}`)
					default:
						t.Errorf("unexpected path: %s", r.URL.Path)
						writeJSON(w, `{"ok":false,"error":"method_not_found"}`)
					}
				}))
				defer server.Close()
				isolated := t.TempDir()
				configPath := filepath.Join(isolated, "config.toml")
				configText := "schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"T123\"\ntoken_type=\"user\"\ntoken=\"env:SLICK_DEBUG_FIXTURE_TOKEN\"\n"
				if err := os.WriteFile(configPath, []byte(configText), 0o600); err != nil {
					t.Fatal(err)
				}
				run := func() (string, string, error) {
					ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
					defer cancel()
					args := []string{"react", "add", "--channel=C123", "--timestamp=1746284582.123456", "--emoji=rocket", "--output=" + mode, "--no-throttle", "--timeout=3s", "--color=never"}
					if debug != "" {
						args = append(args, debug)
					}
					cmd := exec.CommandContext(ctx, binary, args...)
					cmd.Env = []string{"HOME=" + isolated, "SLICK_CONFIG=" + configPath, "SLACK_CLI_BASE_URL=" + server.URL, "SLICK_DEBUG_FIXTURE_TOKEN=" + token}
					var out, diagnostic bytes.Buffer
					cmd.Stdout, cmd.Stderr = &out, &diagnostic
					err := cmd.Run()
					if ctx.Err() != nil {
						t.Fatal("debug fixture exceeded outer timeout")
					}
					combined := out.String() + diagnostic.String()
					for _, secret := range []string{token, "synthetic-private-cookie", "synthetic-private-header", "synthetic-upload-capability", "synthetic-unreviewed-secret"} {
						if strings.Contains(combined, secret) {
							t.Fatal("credential or unreviewed response secret entered command output")
						}
					}
					return out.String(), diagnostic.String(), err
				}
				stdout, stderr, err := run()
				if exitCode(err) != 5 || calls.Load() != 1 {
					t.Fatalf("failed mutation changed exit or replayed: err=%v requests=%d", err, calls.Load())
				}
				if debug != "" && !strings.Contains(stderr, "502 Bad Gateway") {
					t.Fatal("debug mode lost useful HTTP status")
				}
				if debug == "" && strings.Contains(stderr, "Slack HTTP response:") {
					t.Fatal("SDK diagnostics ignored debug flag")
				}
				if mode == "human" {
					if stdout != "" || !strings.Contains(stderr, "slack server error: 502") {
						t.Fatal("human failure changed output streams")
					}
				} else {
					var frame map[string]json.RawMessage
					if err := json.Unmarshal([]byte(stdout), &frame); err != nil {
						t.Fatal(err)
					}
					if len(frame) != 4 || string(frame["ok"]) != "false" || string(frame["data"]) != "null" || strings.Contains(stderr, "slack server error:") {
						t.Fatal("debug mode changed machine envelope or duplicated human error")
					}
				}
				recovered.Store(true)
				_, _, err = run()
				if err != nil || calls.Load() != 2 {
					t.Fatalf("explicit recovery failed or replayed: err=%v requests=%d", err, calls.Load())
				}
				after, err := os.ReadFile(configPath)
				if err != nil || string(after) != configText {
					t.Fatal("debug failure/recovery changed workspace settings")
				}
			})
		}
	}
}
