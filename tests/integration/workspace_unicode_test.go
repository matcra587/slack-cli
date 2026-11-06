//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func unicodeWorkspaceFixture(first, second, refFirst, refSecond string) []byte {
	return fmt.Appendf(nil, "schema_version=\"1\"\ndefault_workspace=%q\n[workspaces.%q]\nname=%q\nteam_id=\"T_FIRST\"\ntoken_type=\"bot\"\ntoken_ref=%q\n[workspaces.%q]\nname=%q\nteam_id=\"T_SECOND\"\ntoken_type=\"bot\"\ntoken_ref=%q\n", first, first, first, refFirst, second, second, refSecond)
}

func TestBinaryAmbiguousUnicodeProfilesFailBeforeCredentialsOrHTTP(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, pair := range []struct{ name, first, second, selector string }{
		{"ascii", "default", "Default", "DEFAULT"},
		{"long s", "s", "ſ", "S"},
		{"sigma", "σ", "ς", "Σ"},
		{"kelvin", "K", "K", "k"},
	} {
		for _, command := range []struct {
			name string
			args []string
			exit int
		}{
			{"workspace.list", []string{"workspace", "list"}, 4},
			{"lookup.user", []string{"lookup", "user", "--user=U1"}, 4},
			{"cache.users", []string{"cache", "users"}, 4},
			{"config.get", []string{"config", "get", "default_workspace"}, 5},
			{"config.set", []string{"config", "set", "default_workspace", pair.first}, 5},
		} {
			for _, mode := range []string{"json", "compact", "human"} {
				t.Run(pair.name+"/"+command.name+"/"+mode, func(t *testing.T) {
					root := t.TempDir()
					path := filepath.Join(root, "config.toml")
					prior := unicodeWorkspaceFixture(pair.first, pair.second, "op://synthetic/first/token", "op://synthetic/second/token")
					if err := os.WriteFile(path, prior, 0o600); err != nil {
						t.Fatal(err)
					}
					bin := filepath.Join(root, "bin")
					if err := os.Mkdir(bin, 0o700); err != nil {
						t.Fatal(err)
					}
					trace := filepath.Join(root, "provider-called")
					if err := os.WriteFile(filepath.Join(bin, "op"), []byte("#!/bin/sh\nprintf called >> \"$SLICK_PROVIDER_TRACE\"\nprintf xoxb-synthetic-forbidden\n"), 0o700); err != nil {
						t.Fatal(err)
					}
					var requests atomic.Int32
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						requests.Add(1)
						writeJSON(w, `{"ok":false,"error":"no_permission"}`)
					}))
					defer server.Close()
					ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
					defer cancel()
					args := append([]string{"--output=" + mode, "--color=never", "--workspace=" + pair.selector}, command.args...)
					cmd := exec.CommandContext(ctx, binary, args...)
					cmd.Env = append(isolatedRuntimeEnv(t), "SLICK_CONFIG="+path, "PATH="+bin, "SLICK_PROVIDER_TRACE="+trace, "SLACK_CLI_BASE_URL="+server.URL)
					var out, diagnostic bytes.Buffer
					cmd.Stdout, cmd.Stderr = &out, &diagnostic
					err := cmd.Run()
					if ctx.Err() != nil || exitCode(err) != command.exit || !strings.Contains(out.String()+diagnostic.String(), "duplicate workspace profile") {
						t.Fatal("ambiguous config did not fail for its duplicate names")
					}
					if mode == "human" {
						if out.Len() != 0 || diagnostic.Len() == 0 {
							t.Fatal("ambiguous config used wrong human stream")
						}
					} else {
						var frame struct {
							OK   bool            `json:"ok"`
							Data json.RawMessage `json:"data"`
							Meta struct {
								Command string `json:"command"`
								Exit    int    `json:"exit_code"`
							} `json:"meta"`
						}
						if json.Unmarshal(out.Bytes(), &frame) != nil || frame.OK || string(frame.Data) != "null" || frame.Meta.Command != command.name || frame.Meta.Exit != command.exit || diagnostic.Len() != 0 {
							t.Fatal("ambiguous config lost common machine failure envelope")
						}
					}
					if requests.Load() != 0 {
						t.Fatal("ambiguous config called Slack")
					}
					if _, err := os.Stat(trace); !os.IsNotExist(err) {
						t.Fatal("ambiguous config read credentials")
					}
					after, err := os.ReadFile(path)
					if err != nil || !bytes.Equal(prior, after) {
						t.Fatal("failed command modified ambiguous config")
					}
				})
			}
		}
	}
}

func TestBinaryUnicodeSelectorsKeepDistinctProfilesAndTokenBinding(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, pair := range []struct{ name, first, second, selectFirst, selectSecond string }{
		{"dotless i", "I", "ı", "i", "ı"},
		{"dotted i", "İ", "i", "İ", "I"},
		{"sharp s", "ß", "ss", "ß", "SS"},
		{"normalization", "é", "e\u0301", "É", "E\u0301"},
		{"confusable", "a", "а", "A", "А"},
		{"long s alias", "s", "other", "ſ", "OTHER"},
		{"sigma alias", "σ", "other", "ς", "OTHER"},
	} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(pair.name+"/"+mode, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "config.toml")
				prior := unicodeWorkspaceFixture(pair.first, pair.second, "env:SLICK_FIRST_TOKEN", "env:SLICK_SECOND_TOKEN")
				if err := os.WriteFile(path, prior, 0o600); err != nil {
					t.Fatal(err)
				}
				var mu sync.Mutex
				var tokens []string
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "" {
						t.Errorf("selected profile changed lookup method, target or encoding: method=%s path=%s user=%q", r.Method, r.URL.Path, r.PostForm.Get("user"))
					}
					mu.Lock()
					tokens = append(tokens, r.PostForm.Get("token"))
					mu.Unlock()
					w.Header().Set("X-OAuth-Scopes", "users:read")
					switch r.URL.Path {
					case "/api/auth.test":
						writeJSON(w, `{"ok":true,"user_id":"U1"}`)
					case "/api/users.info":
						if r.PostForm.Get("user") != "U1" {
							t.Error("Unicode lookup changed user target")
						}
						writeJSON(w, `{"ok":true,"user":{"id":"U1","name":"fixture"}}`)
					default:
						t.Error("Unicode lookup called an unexpected method")
						writeJSON(w, `{"ok":false,"error":"no_permission"}`)
					}
				}))
				defer server.Close()
				for i, selector := range []string{pair.selectFirst, pair.selectSecond} {
					ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
					cmd := exec.CommandContext(ctx, binary, "--output="+mode, "--color=never", "--workspace="+selector, "lookup", "user", "--user=U1")
					cmd.Env = append(isolatedRuntimeEnv(t), "SLICK_CONFIG="+path, "SLACK_CLI_BASE_URL="+server.URL, "SLICK_FIRST_TOKEN=xoxb-first-synthetic", "SLICK_SECOND_TOKEN=xoxb-second-synthetic")
					var out, diagnostic bytes.Buffer
					cmd.Stdout, cmd.Stderr = &out, &diagnostic
					err := cmd.Run()
					cancel()
					if err != nil || diagnostic.Len() != 0 {
						t.Fatalf("distinct Unicode profile lookup failed: %v", err)
					}
					if strings.Contains(out.String(), "xoxb-") || strings.Contains(out.String(), "SLICK_FIRST_TOKEN") || strings.Contains(out.String(), "SLICK_SECOND_TOKEN") {
						t.Fatal("lookup output disclosed credentials")
					}
					if mode == "json" {
						want := []string{pair.first, pair.second}[i]
						var frame struct{ Meta struct{ Workspace string } }
						if json.Unmarshal(out.Bytes(), &frame) != nil || frame.Meta.Workspace != want {
							t.Fatal("lookup metadata selected another Unicode profile")
						}
					}
				}
				mu.Lock()
				if len(tokens) != 4 || tokens[0] != "xoxb-first-synthetic" || tokens[1] != "xoxb-first-synthetic" || tokens[2] != "xoxb-second-synthetic" || tokens[3] != "xoxb-second-synthetic" {
					t.Error("Unicode selector chose the wrong token or retried the lookup")
				}
				mu.Unlock()
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(after, prior) {
					t.Fatal("Unicode lookup modified configuration")
				}
			})
		}
	}
}
