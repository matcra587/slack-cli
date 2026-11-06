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
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func completionProviderFixture(t *testing.T) (string, string, string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("synthetic Unix credential-provider fixture")
	}
	home := t.TempDir()
	path := filepath.Join(home, "config.toml")
	trace := filepath.Join(home, "provider-trace")
	body := []byte("schema_version='1'\ndefault_workspace='default'\n[workspaces.default]\nname='default'\nteam_id='T_SYNTHETIC'\ntoken_type='bot'\ntoken_ref='op://synthetic/completion/token'\n")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	op := filepath.Join(home, "op")
	script := []byte(`#!/bin/sh
printf 'read\n' >> "$SLICK_PROVIDER_TRACE"
case "$SLICK_PROVIDER_MODE" in
wait) exec /bin/sleep 2 ;;
error) printf 'synthetic-provider-private' >&2; exit 1 ;;
esac
printf '{"access_token":"xoxb-synthetic-completion-fixture","unreviewed_secret":"synthetic-provider-private"}\n'
`)
	if err := os.WriteFile(op, script, 0o700); err != nil {
		t.Fatal(err)
	}
	return path, home, trace, filepath.Join(home, "cache")
}

func runProviderCompletion(t *testing.T, binary, path, providerDir, trace, cache, baseURL, mode string, args ...string) (string, string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, append([]string{"--color=never"}, args...)...)
	cmd.Env = append(isolatedRuntimeEnv(t), "SLICK_CONFIG="+path, "PATH="+providerDir, "SLICK_PROVIDER_TRACE="+trace, "SLICK_PROVIDER_MODE="+mode, "XDG_CACHE_HOME="+cache, "SLACK_CLI_BASE_URL="+baseURL)
	var out, diagnostic bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &diagnostic
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatal("completion fixture exceeded its outer timeout")
	}
	if strings.Contains(out.String()+diagnostic.String(), "xoxb-synthetic-") || strings.Contains(out.String()+diagnostic.String(), "synthetic-provider-private") {
		t.Fatal("completion exposed provider credentials/diagnostics")
	}
	return out.String(), diagnostic.String(), err
}

func TestBinaryLocalAndCachedCompletionsSkipCredentialProvider(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tc := range []struct {
		name                        string
		args                        []string
		contains, resource, payload string
		empty                       bool
	}{
		{name: "discovery", contains: `"commands"`},
		{name: "public script", args: []string{"completion", "fish"}, contains: "complete -c slick"},
		{name: "hidden script", args: []string{"--print-completion", "--@shell=fish"}, contains: "complete -c slick"},
		{name: "workspace", args: []string{"--@complete=workspace"}, contains: "default"},
		{name: "config keys", args: []string{"--@complete=config_key"}, contains: "workspaces.default.default_channel"},
		{name: "config bool", args: []string{"--@complete=config_value", "--", "workspaces.default.attribution.enabled"}, contains: "true"},
		{name: "guides", args: []string{"--@complete=guide_workflow"}, contains: "core-contract"},
		{name: "cache names", args: []string{"--@complete=cache_resource"}, contains: "channels"},
		{name: "unknown kind", args: []string{"--@complete=unknown"}, empty: true},
		{name: "unknown profile key", args: []string{"--@complete=config_value", "--", "workspaces.Unknown.default_channel"}, empty: true},
		{name: "cached channel", args: []string{"--@complete=channel"}, contains: "CCACHED", resource: "channels", payload: `{"channels":[{"id":"CCACHED","name":"cached"}]}`},
		{name: "empty channels", args: []string{"--@complete=channel"}, resource: "channels", payload: `{"channels":[]}`, empty: true},
		{name: "empty users", args: []string{"--@complete=user"}, resource: "users", payload: `{"users":[]}`, empty: true},
		{name: "deleted users", args: []string{"--@complete=user"}, resource: "users", payload: `{"users":[{"id":"UGONE","deleted":true}]}`, empty: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, provider, trace, cache := completionProviderFixture(t)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
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
			out, diagnostic, err := runProviderCompletion(t, binary, path, provider, trace, cache, "http://127.0.0.1:1", "normal", tc.args...)
			if err != nil || diagnostic != "" {
				t.Fatalf("local completion failed: %v", err)
			}
			if _, err := os.Stat(trace); !os.IsNotExist(err) {
				t.Fatal("local/cache completion read credential provider")
			}
			if tc.empty {
				if out != "" {
					t.Fatal("empty completion unexpectedly emitted data")
				}
			} else if !strings.Contains(out, tc.contains) {
				t.Fatal("completion did not return actual expected content")
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

func TestBinaryCompletionProviderFallbackDeadlineAndRecovery(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, mode := range []string{"normal", "error", "wait"} {
		t.Run(mode, func(t *testing.T) {
			path, provider, trace, cache := completionProviderFixture(t)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Method != http.MethodPost || r.URL.Path != "/api/conversations.list" || r.URL.RawQuery != "" || r.Form.Get("token") != "xoxb-synthetic-completion-fixture" || r.Form.Get("limit") != "100" {
					t.Error("completion sent wrong method/path/limit/authentication")
				}
				writeJSON(w, `{"ok":true,"channels":[{"id":"CREMOTE","name":"remote"}]}`)
			}))
			defer server.Close()
			args := []string{"--@complete=channel", "--timeout=2s"}
			if mode == "wait" {
				args[1] = "--timeout=200ms"
			}
			started := time.Now()
			out, diagnostic, err := runProviderCompletion(t, binary, path, provider, trace, cache, server.URL, mode, args...)
			if err != nil || diagnostic != "" {
				t.Fatalf("completion provider failure was not quiet: %v", err)
			}
			if mode == "wait" && time.Since(started) > time.Second {
				t.Fatal("credential provider exceeded the completion command deadline")
			}
			data, err := os.ReadFile(trace)
			if err != nil || strings.Count(string(data), "read\n") != 1 {
				t.Fatal("completion provider missing or retried")
			}
			wantCalls := int32(0)
			if mode == "normal" {
				wantCalls = 1
				if !strings.Contains(out, "CREMOTE") {
					t.Fatal("successful fallback returned no candidate")
				}
			} else if out != "" {
				t.Fatal("failed provider emitted candidates")
			}
			if calls.Load() != wantCalls {
				t.Fatal("completion reached Slack after provider failure or retried")
			}
			out, diagnostic, err = runProviderCompletion(t, binary, path, provider, trace, cache, server.URL, "normal", "--@complete=channel", "--timeout=2s")
			if err != nil || diagnostic != "" || !strings.Contains(out, "CREMOTE") || calls.Load() != wantCalls+1 {
				t.Fatal("explicit provider recovery failed")
			}
			data, err = os.ReadFile(trace)
			if err != nil || strings.Count(string(data), "read\n") != 2 {
				t.Fatal("recovery did not read provider exactly once")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("provider completion changed config")
			}
			if entries, err := os.ReadDir(cache); err == nil && len(entries) != 0 {
				t.Fatal("completion unexpectedly wrote a cache")
			} else if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
		})
	}
}
