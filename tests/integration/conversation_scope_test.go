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
	"sync/atomic"
	"testing"
	"time"
)

func TestBinaryConversationListsAcceptAlternativeReadScopes(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, scope := range []string{"channels:read", "groups:read", "im:read", "mpim:read"} {
		for _, tokenType := range []string{"bot", "user"} {
			for _, route := range []string{"lookup", "cache"} {
				for _, mode := range []string{"json", "compact", "human"} {
					t.Run(scope+"/"+tokenType+"/"+route+"/"+mode, func(t *testing.T) {
						token := "xoxb-synthetic-conversation-scopes"
						if tokenType == "user" {
							token = "xoxp-synthetic-conversation-scopes"
						}
						dir := t.TempDir()
						configPath := filepath.Join(dir, "config.toml")
						cache := t.TempDir()
						config := fmt.Sprintf("schema_version='1'\ndefault_workspace='fixture'\n[workspaces.fixture]\nname='fixture'\nteam_id='TFIXTURE'\ntoken_type='%s'\ntoken_ref='env:SLICK_CONVERSATION_FIXTURE_TOKEN'\n", tokenType)
						if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
							t.Fatal(err)
						}
						var authCalls, listCalls atomic.Int32
						visible := route == "cache" || scope == "channels:read" || scope == "groups:read"
						server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							if err := r.ParseForm(); err != nil {
								t.Error(err)
							}
							if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.PostForm.Get("token") != token {
								t.Error("wrong conversation method or synthetic authentication")
							}
							w.Header().Set("Content-Type", "application/json")
							switch r.URL.Path {
							case "/api/auth.test":
								authCalls.Add(1)
								w.Header().Set("X-OAuth-Scopes", scope)
								writeJSON(w, `{"ok":true,"user_id":"UFIXTURE"}`)
							case "/api/conversations.list":
								listCalls.Add(1)
								want := "public_channel,private_channel"
								next := "NEXT"
								if route == "cache" {
									want = "public_channel,private_channel,im,mpim"
									next = ""
								}
								if r.PostForm.Get("types") != want {
									t.Error("scope handling narrowed or changed requested conversation types")
								}
								channels := []map[string]any{}
								if visible {
									channels = append(channels, map[string]any{"id": "CVISIBLE", "name": "visible", "is_im": scope == "im:read", "is_mpim": scope == "mpim:read", "is_private": scope == "groups:read", "unreviewed_secret": "synthetic-private-conversation-field"})
								}
								if err := json.NewEncoder(w).Encode(map[string]any{"ok": true, "channels": channels, "response_metadata": map[string]string{"next_cursor": next}}); err != nil {
									t.Error(err)
								}
							default:
								t.Error("unexpected conversation endpoint")
								w.WriteHeader(500)
							}
						}))
						defer server.Close()
						args := []string{"lookup", "channel"}
						if route == "cache" {
							args = []string{"cache", "channels", "--refresh"}
						}
						args = append(args, "--output="+mode, "--color=never", "--no-throttle")
						ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
						defer cancel()
						cmd := exec.CommandContext(ctx, binary, args...)
						cmd.Env = append(isolatedRuntimeEnv(t), "SLICK_CONFIG="+configPath, "XDG_CACHE_HOME="+cache, "SLACK_CLI_BASE_URL="+server.URL, "SLICK_CONVERSATION_FIXTURE_TOKEN="+token)
						var out, diagnostic bytes.Buffer
						cmd.Stdout = &out
						cmd.Stderr = &diagnostic
						err := cmd.Run()
						if err != nil || ctx.Err() != nil || diagnostic.Len() != 0 || authCalls.Load() != 1 || listCalls.Load() != 1 {
							t.Fatalf("native single-scope list did not succeed once: %v", err)
						}
						if strings.Contains(out.String(), token) || strings.Contains(out.String(), "unreviewed_secret") {
							t.Fatal("native conversation output exposed unreviewed values")
						}
						if mode != "human" {
							raw := json.RawMessage(out.Bytes())
							if mode == "json" {
								var frame struct {
									OK   bool
									Data json.RawMessage
								}
								if json.Unmarshal(raw, &frame) != nil || !frame.OK {
									t.Fatal("success envelope lost")
								}
								raw = frame.Data
							}
							var data struct {
								Channels   []struct{ ID string }
								Pagination struct {
									NextCursor string `json:"next_cursor"`
									HasMore    bool   `json:"has_more"`
								}
							}
							want := 0
							if visible {
								want = 1
							}
							if json.Unmarshal(raw, &data) != nil || data.Channels == nil || len(data.Channels) != want {
								t.Fatal("native list lost visible or empty results")
							}
							if mode == "compact" && route == "lookup" && (!data.Pagination.HasMore || data.Pagination.NextCursor != "NEXT") {
								t.Fatal("native scoped page lost continuation")
							}
						} else if out.Len() == 0 || strings.HasPrefix(out.String(), "{") {
							t.Fatal("native human list lost text protocol")
						}
						after, err := os.ReadFile(configPath)
						if err != nil || !bytes.Equal([]byte(config), after) {
							t.Fatal("native scope handling changed configuration")
						}
						if route == "cache" {
							saved, err := os.ReadFile(filepath.Join(cache, "slick", "fixture", "channels.json"))
							if err != nil || !bytes.Contains(saved, []byte("CVISIBLE")) || bytes.Contains(saved, []byte("unreviewed_secret")) || bytes.Contains(saved, []byte(token)) {
								t.Fatal("native cache did not preserve reviewed visible data only")
							}
						}
					})
				}
			}
		}
	}
}
