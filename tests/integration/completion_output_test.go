//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The root matrix retains all nine candidate paths, modes and writer failures.
// Keep real OS failures for local, cached and both remote request families here.
func TestBinaryDynamicCompletionWriteFailuresAndRecovery(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("/dev/full fixture requires Linux")
	}
	binary := buildSlackBinary(t)
	for _, tc := range []struct {
		name, mode, kind, contains, resource, payload string
		live                                          bool
	}{
		{"local workspace json", "json", "workspace", "default", "", "", false},
		{"cached channels compact", "compact", "channel", "CCACHED", "channels", `{"channels":[{"id":"CCACHED","name":"cached"}]}`, false},
		{"remote channels human", "human", "channel", "CREMOTE", "", "", true},
		{"remote users json", "json", "user", "UREMOTE", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writePipeConfig(t)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			cache := t.TempDir()
			var cachePath string
			var cached []byte
			if tc.resource != "" {
				cachePath = filepath.Join(cache, "slick", "default", tc.resource+".json")
				if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
					t.Fatal(err)
				}
				cached, err = json.Marshal(map[string]any{"profile": "default", "resource": tc.resource, "fetched_at": time.Now().UTC(), "data": json.RawMessage(tc.payload)})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(cachePath, cached, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != http.MethodPost || r.FormValue("token") != "xoxb-test" {
					t.Error("completion failure changed method/auth")
				}
				switch r.URL.Path {
				case "/api/conversations.list":
					writeJSON(w, `{"ok":true,"channels":[{"id":"CREMOTE","name":"remote"}]}`)
				case "/api/users.list":
					writeJSON(w, `{"ok":true,"members":[{"id":"UREMOTE","name":"remote"}]}`)
				default:
					t.Error("unexpected completion request")
					w.WriteHeader(500)
				}
			}))
			defer server.Close()
			args := []string{"--output=" + tc.mode, "--color=never", "--@shell=fish", "--@complete=" + tc.kind}
			run := func(dst io.Writer) (string, error) {
				ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, binary, args...)
				cmd.Env = append(isolatedRuntimeEnv(t), "SLICK_CONFIG="+path, "SLACK_TEST_TOKEN=xoxb-test", "XDG_CACHE_HOME="+cache, "SLACK_CLI_BASE_URL="+server.URL)
				var diagnostic bytes.Buffer
				cmd.Stdout = dst
				cmd.Stderr = &diagnostic
				err := cmd.Run()
				if ctx.Err() != nil {
					t.Fatal("completion fixture timed out")
				}
				return diagnostic.String(), err
			}
			full, err := os.OpenFile("/dev/full", os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			diagnostic, runErr := run(full)
			if err := full.Close(); err != nil {
				t.Fatal(err)
			}
			want := int32(0)
			if tc.live {
				want = 1
			}
			if exitCode(runErr) != 8 || strings.Count(diagnostic, "output_write_failed") != 1 || !strings.Contains(diagnostic, "exit_code=8") || requests.Load() != want {
				t.Fatal("native completion lost IO8/diagnostic or replayed request")
			}
			var healthy bytes.Buffer
			diagnostic, runErr = run(&healthy)
			if runErr != nil || diagnostic != "" || !strings.Contains(healthy.String(), tc.contains) || strings.HasPrefix(healthy.String(), "{") || requests.Load() != want*2 {
				t.Fatalf("actual completion invocation did not recover with shell protocol: %v", runErr)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("completion changed config state")
			}
			if cachePath != "" {
				after, err := os.ReadFile(cachePath)
				if err != nil || !bytes.Equal(cached, after) {
					t.Fatal("completion changed cache state")
				}
			}
		})
	}
}
