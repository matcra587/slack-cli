package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/matcra587/slack-cli/internal/config"
)

func TestDynamicCompletionHonorsThrottleFlag(t *testing.T) {
	for _, tc := range []struct {
		name    string
		flags   []string
		unpaced bool
	}{
		{name: "default"},
		{name: "explicit false", flags: []string{"--no-throttle=false"}},
		{name: "long", flags: []string{"--no-throttle"}, unpaced: true},
		{name: "short", flags: []string{"-Q"}, unpaced: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := t.TempDir()
			path := filepath.Join(t.TempDir(), "config.toml")
			t.Setenv("XDG_CACHE_HOME", cache)
			t.Setenv("SLICK_CONFIG", path)
			t.Setenv("SLACK_CLI_DEFAULT_WORKSPACE", "")
			t.Setenv("SLACK_CLI_TOKEN", "")
			t.Setenv("SLACK_CLI_TOKEN_DEFAULT", "")
			cfg := &config.Config{SchemaVersion: config.SchemaVersion, DefaultWorkspace: "default", Workspaces: map[string]config.WorkspaceProfile{"default": {Name: "default"}}}
			if err := config.SaveFile(path, cfg); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			synctest.Test(t, func(t *testing.T) {
				const token = "xoxb-synthetic-completion-pacing"
				var out, diagnostic bytes.Buffer
				requests, resolutions := 0, 0
				root, runtime := NewRootCommandWithRuntime(WithConfig(cfg), WithConfigPath(path), WithSlackBaseURL("https://fixture.invalid"), WithTTY(false), WithIO(strings.NewReader(""), &out, &diagnostic), WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
					resolutions++
					return token, nil
				})), func(runtime *RootRuntime) {
					runtime.HTTPClient = &http.Client{Transport: completionThrottleTransport(func(r *http.Request) (*http.Response, error) {
						requests++
						if r.Method != http.MethodPost || r.URL.Path != "/api/users.list" {
							t.Fatal("completion called an unexpected method")
						}
						if err := r.ParseForm(); err != nil {
							t.Fatal(err)
						}
						cursor := ""
						body := `{"ok":true,"members":[{"id":"U_FIRST","name":"first"}],"response_metadata":{"next_cursor":"page-2"}}`
						if requests == 2 {
							cursor = "page-2"
							body = `{"ok":true,"members":[{"id":"U_SECOND","name":"second"}],"response_metadata":{"next_cursor":""}}`
						}
						if requests > 2 || r.PostForm.Get("cursor") != cursor || r.PostForm.Get("token") != token || r.PostForm.Get("limit") != "100" {
							t.Fatal("completion changed auth, pagination, or retried")
						}
						return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
					})}
				})
				defer func() {
					if runtime.CancelTimeout != nil {
						runtime.CancelTimeout()
					}
				}()
				args := []string{"--@complete=user", "--@shell=fish", "--timeout=150ms", "--color=never"}
				root.SetArgs(append(args, tc.flags...))
				start := time.Now()
				if err := root.ExecuteContext(t.Context()); err != nil {
					t.Fatalf("completion failed: %v", err)
				}
				if tc.unpaced {
					if requests != 2 || !strings.Contains(out.String(), "U_FIRST") || !strings.Contains(out.String(), "U_SECOND") || time.Since(start) != 0 {
						t.Fatalf("no-throttle did not complete both pages: requests=%d elapsed=%s output=%q", requests, time.Since(start), out.String())
					}
				} else if requests != 1 || out.Len() != 0 || time.Since(start) != 150*time.Millisecond {
					t.Fatalf("default pacing did not respect timeout: requests=%d elapsed=%s output=%q", requests, time.Since(start), out.String())
				}
				if resolutions != 1 || strings.Contains(out.String()+diagnostic.String(), token) {
					t.Fatal("completion repeatedly resolved or disclosed its credential")
				}
			})
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("completion changed saved config")
			}
			entries, err := os.ReadDir(cache)
			if err != nil || len(entries) != 0 {
				t.Fatal("completion wrote metadata during a read")
			}
		})
	}
}

type completionThrottleTransport func(*http.Request) (*http.Response, error)

func (f completionThrottleTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
