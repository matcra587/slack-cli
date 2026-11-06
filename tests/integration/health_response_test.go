//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBinaryHealthProbeRequiresAcknowledgementAndRecovers(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tc := range []struct {
		name, body   string
		status, exit int
	}{
		{"missing", `{}`, 200, 5},
		{"null", `null`, 200, 5},
		{"false", `{"ok":false}`, 200, 5},
		{"blank error", `{"ok":false,"error":" "}`, 200, 5},
		{"contradictory", `{"ok":true,"error":"internal_error"}`, 200, 5},
		{"wrong ack", `{"ok":"true"}`, 200, 5},
		{"trailing document", `{"ok":true} {"secret":"synthetic-health-canary"}`, 200, 5},
		{"trailing garbage", `{"ok":true} synthetic-health-canary`, 200, 5},
		{"truncated", `{"secret":"synthetic-health-canary`, 200, 5},
		{"empty", ``, 200, 5},
		{"oversized", `{"ok":true}` + strings.Repeat(" ", 5<<20), 200, 5},
		{"permission", `{"ok":false,"error":"access_denied"}`, 200, 1},
		{"untrusted error", `{"ok":false,"error":"synthetic-health-canary"}`, 200, 5},
		{"http failure", `synthetic-health-canary`, 503, 5},
	} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "config.toml")
				prior := []byte("synthetic malformed config; health must not load credentials\n")
				if err := os.WriteFile(path, prior, 0o600); err != nil {
					t.Fatal(err)
				}
				var recovery atomic.Bool
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					body, err := io.ReadAll(r.Body)
					if err != nil || r.Method != http.MethodPost || r.URL.Path != "/api/api.test" || r.URL.RawQuery != "" || len(body) != 0 || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
						t.Error("unauthenticated probe sent wrong request or credentials")
					}
					if recovery.Load() {
						writeJSON(w, `{"ok":true,"args":{}}`)
					} else {
						w.WriteHeader(tc.status)
						_, _ = io.WriteString(w, tc.body)
					}
				}))
				defer server.Close()
				args := []string{"health", "api-test", "--output=" + mode, "--color=never", "--timeout=2s"}
				out, diagnostic, err := runSlackBinary(t, binary, path, server.URL, "", args...)
				if exitCode(err) != tc.exit {
					t.Fatalf("probe exit=%d want=%d", exitCode(err), tc.exit)
				}
				if strings.Contains(out+diagnostic, "synthetic-health-canary") {
					t.Fatal("response canary leaked")
				}
				if mode == "human" {
					if out != "" || diagnostic == "" {
						t.Fatal("wrong human failure streams")
					}
				} else {
					var frame struct {
						OK   bool
						Data json.RawMessage
						Meta struct {
							Command string
							Exit    int `json:"exit_code"`
						}
					}
					if json.Unmarshal([]byte(out), &frame) != nil || frame.OK || string(frame.Data) != "null" || frame.Meta.Command != "health.api-test" || frame.Meta.Exit != tc.exit || diagnostic != "" {
						t.Fatal("wrong failure envelope")
					}
				}
				if calls.Load() != 1 {
					t.Fatal("failed probe was retried")
				}
				recovery.Store(true)
				out, diagnostic, err = runSlackBinary(t, binary, path, server.URL, "", args...)
				if err != nil || diagnostic != "" || out == "" || calls.Load() != 2 {
					t.Fatalf("probe did not recover on next invocation: %v", err)
				}
				if mode == "json" {
					var frame struct {
						OK   bool
						Data struct{ OK bool }
						Meta struct{ Command string }
					}
					if json.Unmarshal([]byte(out), &frame) != nil || !frame.OK || !frame.Data.OK || frame.Meta.Command != "health.api-test" {
						t.Fatal("probe recovery lost result acknowledgement")
					}
				}
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(prior, after) {
					t.Fatal("health probe changed unrelated local state")
				}
			})
		}
	}
}

func TestBinaryHealthProbeTimeoutDuringHeadersAndBodyRecovers(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, phase := range []string{"headers", "body"} {
		for _, flag := range []string{"--timeout", "-I"} {
			for _, mode := range []string{"json", "compact", "human"} {
				t.Run(phase+"/"+flag+"/"+mode, func(t *testing.T) {
					path := writePipeConfig(t)
					before, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					var recovered atomic.Bool
					var calls atomic.Int32
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						calls.Add(1)
						if r.Method != http.MethodPost || r.URL.Path != "/api/api.test" || r.Header.Get("Authorization") != "" {
							t.Error("timeout probe changed method/path/auth")
						}
						if recovered.Load() {
							writeJSON(w, `{"ok":true}`)
							return
						}
						if phase == "body" {
							_, _ = io.WriteString(w, `{"secret":"synthetic-timeout-canary`)
							w.(http.Flusher).Flush()
						}
						select {
						case <-r.Context().Done():
						case <-time.After(3 * time.Second):
							t.Error("probe did not cancel the HTTP request")
						}
					}))
					defer server.Close()
					out, diagnostic, err := runSlackBinary(t, binary, path, server.URL, "", "health", "api-test", "--output="+mode, "--color=never", flag, "100ms")
					if exitCode(err) != 7 || calls.Load() != 1 {
						t.Fatalf("probe timeout changed exit or retried: exit=%d calls=%d", exitCode(err), calls.Load())
					}
					if strings.Contains(out+diagnostic, "synthetic-timeout-canary") {
						t.Fatal("partial response leaked on timeout")
					}
					if mode == "human" {
						if out != "" || diagnostic == "" {
							t.Fatal("wrong timeout streams")
						}
					} else {
						var frame struct {
							OK   bool
							Data json.RawMessage
							Meta struct {
								Command string
								Exit    int `json:"exit_code"`
							}
						}
						if json.Unmarshal([]byte(out), &frame) != nil || frame.OK || string(frame.Data) != "null" || frame.Meta.Command != "health.api-test" || frame.Meta.Exit != 7 || diagnostic != "" {
							t.Fatal("wrong timeout envelope")
						}
					}
					recovered.Store(true)
					out, diagnostic, err = runSlackBinary(t, binary, path, server.URL, "", "health", "api-test", "--output="+mode, "--color=never", flag, "2s")
					if err != nil || diagnostic != "" || out == "" || calls.Load() != 2 {
						t.Fatalf("timeout recovery failed: %v", err)
					}
					after, err := os.ReadFile(path)
					if err != nil || !bytes.Equal(before, after) {
						t.Fatal("timeout probe changed config")
					}
				})
			}
		}
	}
}
