package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	slackcache "github.com/matcra587/slack-cli/internal/cache"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestConversationReadScopesFilterResultsRatherThanBlockTypes(t *testing.T) {
	grants := []struct{ name, scope, kind string }{
		{"public", "channels:read", "public_channel"}, {"private", "groups:read", "private_channel"}, {"im", "im:read", "im"}, {"mpim", "mpim:read", "mpim"},
	}
	operations := []struct {
		name, types string
		cache       bool
	}{
		{"default", "public_channel,private_channel", false}, {"public_channel", "public_channel", false}, {"private_channel", "private_channel", false}, {"im", "im", false}, {"dm", "im", false}, {"mpim", "mpim", false}, {"all", "public_channel,private_channel,im,mpim", false}, {"cache", "public_channel,private_channel,im,mpim", true},
	}
	for _, grant := range grants {
		for _, tokenType := range []config.TokenType{config.TokenTypeBot, config.TokenTypeUser} {
			for _, op := range operations {
				for _, mode := range []string{"json", "compact", "human"} {
					// Keep the full grant/token/type grid in JSON. The public grant keeps
					// every token and operation in each other presentation mode.
					if mode != "json" && grant.name != "public" {
						continue
					}
					t.Run(grant.name+"/"+string(tokenType)+"/"+op.name+"/"+mode, func(t *testing.T) {
						t.Setenv("XDG_CACHE_HOME", t.TempDir())
						cfg := workspaceConfig(tokenType)
						before, err := json.Marshal(cfg)
						if err != nil {
							t.Fatal(err)
						}
						if _, err := slackcache.Write("default", "users", json.RawMessage(`{"users":[{"id":"UUNRELATED","name":"untouched"}]}`)); err != nil {
							t.Fatal(err)
						}
						unrelated, err := slackcache.Path("default", "users")
						if err != nil {
							t.Fatal(err)
						}
						prior, err := os.ReadFile(unrelated)
						if err != nil {
							t.Fatal(err)
						}
						visible := slices.Contains(strings.Split(op.types, ","), grant.kind)
						channels := []map[string]any{}
						if visible {
							channels = append(channels, map[string]any{"id": "CVISIBLE", "name": "visible", "is_channel": grant.kind == "public_channel", "is_private": grant.kind == "private_channel", "is_im": grant.kind == "im", "is_mpim": grant.kind == "mpim", "unreviewed_secret": "synthetic-private-channel-field"})
						}
						next := "NEXT"
						if op.cache {
							next = ""
						}
						body, err := json.Marshal(map[string]any{"ok": true, "channels": channels, "response_metadata": map[string]string{"next_cursor": next}})
						if err != nil {
							t.Fatal(err)
						}
						server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
							"auth.test": func(req testutil.SlackRequest) testutil.SlackResponse {
								if req.Method != http.MethodPost || req.Form.Get("token") != "xox-test" {
									t.Error("wrong preflight method/auth")
								}
								return testutil.SlackResponse{Status: 200, Header: http.Header{"X-Oauth-Scopes": []string{grant.scope}}, Body: `{"ok":true,"user_id":"UFIXTURE"}`}
							},
							"conversations.list": func(req testutil.SlackRequest) testutil.SlackResponse {
								if req.Method != http.MethodPost || req.Form.Get("token") != "xox-test" || req.Form.Get("types") != op.types {
									t.Error("wrong list method, auth or requested type filter")
								}
								if op.cache && req.Form.Get("exclude_archived") != "true" {
									t.Error("cache lost archived filter")
								}
								if !op.cache && (req.Form.Has("limit") || req.Form.Has("cursor")) {
									t.Error("default list arguments were not omitted")
								}
								return testutil.JSONResponse(string(body))
							},
						})
						args := []string{"lookup", "channel"}
						command := "lookup.channel"
						if op.cache {
							args = []string{"cache", "channels", "--refresh"}
							command = "cache.channels"
						} else if op.name != "default" {
							args = append(args, "--types="+op.name)
						}
						args = append(args, "--output="+mode, "--color=never", "--no-throttle")
						out, diagnostic, err := executeTestRoot(t, cfg, server.BaseURL(), "", args)
						if err != nil {
							t.Fatalf("one accepted read scope blocked requested types: %v", err)
						}
						if diagnostic != "" || strings.Contains(out, "xox-test") || strings.Contains(out, "synthetic-private-channel-field") || len(server.Requests("auth.test")) != 1 || len(server.Requests("conversations.list")) != 1 {
							t.Fatal("scope handling leaked data, retried, or used wrong streams")
						}
						if mode != "human" {
							raw := json.RawMessage(out)
							if mode == "json" {
								var envelope struct {
									OK   bool
									Data json.RawMessage
									Meta struct{ Command string }
								}
								if json.Unmarshal(raw, &envelope) != nil || !envelope.OK || envelope.Meta.Command != command {
									t.Fatal("scope success lost envelope metadata")
								}
								raw = envelope.Data
							}
							var data struct {
								Channels   []struct{ ID string }
								Pagination struct {
									NextCursor string `json:"next_cursor"`
									HasMore    bool   `json:"has_more"`
								}
							}
							if json.Unmarshal(raw, &data) != nil || data.Channels == nil || len(data.Channels) != len(channels) {
								t.Fatal("scope-filtered rows or explicit empty list lost")
							}
							if mode == "compact" && !op.cache && (data.Pagination.NextCursor != "NEXT" || !data.Pagination.HasMore) {
								t.Fatal("empty or populated scope-filtered page lost continuation")
							}
						} else if out == "" || strings.HasPrefix(out, "{") {
							t.Fatal("human scope-filtered results lost their text protocol")
						}
						after, err := json.Marshal(cfg)
						if err != nil || !bytes.Equal(before, after) {
							t.Fatal("scope handling changed profile state")
						}
						afterFile, err := os.ReadFile(unrelated)
						if err != nil || !bytes.Equal(prior, afterFile) {
							t.Fatal("channel fetch changed unrelated users cache")
						}
						if op.cache {
							path, err := slackcache.Path("default", "channels")
							if err != nil {
								t.Fatal(err)
							}
							saved, err := os.ReadFile(path)
							if err != nil || !bytes.Contains(saved, []byte("CVISIBLE")) || bytes.Contains(saved, []byte("unreviewed_secret")) || bytes.Contains(saved, []byte("xox-test")) {
								t.Fatal("cache failed to retain reviewed visible data only")
							}
							if info, err := os.Stat(filepath.Clean(path)); err != nil || runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
								t.Fatal("cache output permissions widened")
							}
						}
					})
				}
			}
		}
	}
}

func TestConversationScopeFailuresPreserveCacheAndRecover(t *testing.T) {
	for _, tc := range []struct {
		name, header, match string
		needed              []string
		requests            int
	}{
		{"irrelevant", "chat:write", "any", []string{"channels:read", "groups:read", "im:read", "mpim:read"}, 0},
		{"remote", "channels:read", "reported", []string{"groups:read"}, 1},
		{"unknown header", "", "reported", []string{"groups:read"}, 1},
	} {
		for _, route := range []string{"lookup", "cache"} {
			for _, mode := range []string{"json", "compact", "human"} {
				t.Run(tc.name+"/"+route+"/"+mode, func(t *testing.T) {
					t.Setenv("XDG_CACHE_HOME", t.TempDir())
					cfg := workspaceConfig(config.TokenTypeBot)
					before, err := json.Marshal(cfg)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := slackcache.Write("default", "channels", json.RawMessage(`{"channels":[{"id":"CPRIOR","name":"prior"}]}`)); err != nil {
						t.Fatal(err)
					}
					path, err := slackcache.Path("default", "channels")
					if err != nil {
						t.Fatal(err)
					}
					prior, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					var phase atomic.Int32
					server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
						"auth.test": func(req testutil.SlackRequest) testutil.SlackResponse {
							if req.Method != http.MethodPost || req.Form.Get("token") != "xox-test" {
								t.Error("wrong preflight auth/method")
							}
							header := tc.header
							if phase.Load() == 1 {
								header = "channels:read"
							}
							return testutil.SlackResponse{Status: 200, Header: http.Header{"X-Oauth-Scopes": []string{header}}, Body: `{"ok":true,"user_id":"UFIXTURE"}`}
						},
						"conversations.list": func(req testutil.SlackRequest) testutil.SlackResponse {
							if req.Method != http.MethodPost || req.Form.Get("token") != "xox-test" {
								t.Error("wrong list auth/method")
							}
							if phase.Load() == 0 {
								return testutil.JSONResponse(`{"ok":false,"error":"missing_scope","needed":"groups:read"}`)
							}
							return testutil.JSONResponse(`{"ok":true,"channels":[{"id":"CRECOVERED","name":"recovered"}]}`)
						},
					})
					args := []string{"lookup", "channel"}
					if route == "cache" {
						args = []string{"cache", "channels", "--refresh"}
					}
					args = append(args, "--output="+mode, "--color=never", "--no-throttle")
					out, diagnostic, err := executeTestRoot(t, cfg, server.BaseURL(), "", args)
					typed, ok := errors.AsType[CommandError](err)
					if !ok || typed.CLIError.ExitCode != 1 || len(server.Requests("auth.test")) != 1 || len(server.Requests("conversations.list")) != tc.requests {
						t.Fatal("scope failure lost class or sent unexpected requests")
					}
					if strings.Contains(out+diagnostic, "xox-test") {
						t.Fatal("scope failure exposed credential")
					}
					if mode == "human" {
						if out != "" || diagnostic == "" {
							t.Fatal("human scope failure used wrong stream")
						}
					} else {
						var frame struct {
							OK     bool
							Data   json.RawMessage
							Errors []struct {
								Details struct {
									Needed []string `json:"needed"`
									Match  string   `json:"needed_match"`
								}
							}
						}
						if json.Unmarshal([]byte(out), &frame) != nil || frame.OK || string(frame.Data) != "null" || len(frame.Errors) != 1 || !reflect.DeepEqual(frame.Errors[0].Details.Needed, tc.needed) || frame.Errors[0].Details.Match != tc.match || diagnostic != "" {
							t.Fatal("scope failure lost common envelope or alternative-scope meaning")
						}
					}
					after, err := os.ReadFile(path)
					if err != nil || !bytes.Equal(prior, after) {
						t.Fatal("failed fetch replaced prior channels cache")
					}
					phase.Store(1)
					out, diagnostic, err = executeTestRoot(t, cfg, server.BaseURL(), "", args)
					if err != nil || diagnostic != "" || len(server.Requests("auth.test")) != 2 || len(server.Requests("conversations.list")) != tc.requests+1 {
						t.Fatalf("explicit scope recovery failed or replayed API: %v", err)
					}
					if (mode != "human" || route != "cache") && !strings.Contains(out, "CRECOVERED") {
						t.Fatal("recovery lost returned rows")
					}
					cached, cacheErr := os.ReadFile(path)
					if cacheErr != nil || (route == "cache" && !bytes.Contains(cached, []byte("CRECOVERED"))) || (route == "lookup" && !bytes.Equal(prior, cached)) {
						t.Fatal("recovery lost cache state or changed unrelated cached data")
					}
					saved, err := json.Marshal(cfg)
					if err != nil || !bytes.Equal(before, saved) {
						t.Fatal("scope failure/recovery changed stored settings")
					}
				})
			}
		}
	}
}
