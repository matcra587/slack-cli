//go:build integration

package integration_test

import (
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

func TestBinarySearchFlagsPreserveQueryPagingAndFullText(t *testing.T) {
	binary := buildSlackBinary(t)
	const token = "xoxp-synthetic-search-flags"
	const tail = "END-OF-RELEASE-REVIEW"
	text := strings.Repeat("Release review complete. ", 20) + tail
	const ts = "1746284582.123456"
	for _, short := range []bool{false, true} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(fmt.Sprintf("short=%t/%s", short, mode), func(t *testing.T) {
				flag := func(name, alias, value string) []string {
					if short {
						return []string{alias, value}
					}
					return []string{name + "=" + value}
				}
				query := func(value string) []string { return flag("--query", "-q", value) }
				count := func(value string) []string { return flag("--max-items", "-M", value) }
				page := func(value string) []string { return flag("--cursor", "-C", value) }
				full := "--full"
				if short {
					full = "-F"
				}
				type scenario struct {
					name, query, count, page, failure string
					args                              []string
					code                              int
					full, empty, remote               bool
				}
				const specialQuery = `in:release "ship & ready" +owner@example.test 🔧`
				scenarios := []scenario{
					{name: "explicit full", query: specialQuery, count: "5", page: "2", args: append(append(append(query(specialQuery), count("5")...), page("2")...), full), full: true, remote: true},
					{name: "remove paging and full", query: "release-v2", args: query("release-v2"), remote: true},
					{name: "explicit empty defaults", query: "release-v3", args: append(append(append(query("release-v3"), count("0")...), page("")...), full+"=false"), remote: true},
					{name: "maximum page and count", query: "release-v4", count: "100", page: "100", args: append(append(append(query("release-v4"), count("100")...), page("100")...), full), full: true, remote: true},
					{name: "remote permission", query: "release-v5", args: query("release-v5"), failure: "missing_scope", code: 1, remote: true},
					{name: "recovery empty result", query: "release-v6", args: query("release-v6"), remote: true, empty: true},
					{name: "missing query", code: 4},
					{name: "blank query", args: query(" \t"), code: 4},
					{name: "negative count", args: append(query("release"), count("-1")...), code: 4},
					{name: "too many matches", args: append(query("release"), count("101")...), code: 4},
					{name: "zero page", args: append(query("release"), page("0")...), code: 4},
					{name: "too many pages", args: append(query("release"), page("101")...), code: 4},
					{name: "malformed page", args: append(query("release"), page("opaque+/=")...), code: 4},
					{name: "malformed full", args: append(query("release"), full+"=maybe"), code: 4},
					{name: "bot profile", args: append(query("release"), "--workspace=bot"), code: 1},
				}
				var current, calls, searches atomic.Int32
				var activeTest atomic.Pointer[testing.T]
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					t := activeTest.Load()
					calls.Add(1)
					sc := scenarios[current.Load()]
					if !sc.remote {
						t.Error("invalid search or bot profile reached Slack")
					}
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.PostForm.Get("token") != token || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
						t.Error("search changed POST form authentication")
					}
					switch r.URL.Path {
					case "/api/auth.test":
						w.Header().Set("X-OAuth-Scopes", "search:read")
						writeJSON(w, `{"ok":true,"user_id":"U1"}`)
					case "/api/search.messages":
						searches.Add(1)
						if r.PostForm.Get("query") != sc.query || r.PostForm.Get("count") != sc.count || r.PostForm.Get("page") != sc.page {
							t.Error("search changed query encoding or paging values")
						}
						for _, key := range []string{"highlight", "full", "sort", "sort_dir", "cursor", "team_id"} {
							if r.PostForm.Has(key) {
								t.Errorf("search invented optional parameter %s", key)
							}
						}
						if sc.failure != "" {
							writeJSON(w, `{"ok":false,"error":"missing_scope","needed":"search:read","provided":"users:read"}`)
							return
						}
						resultPage := 1
						if sc.page == "2" {
							resultPage = 2
						}
						if sc.page == "100" {
							resultPage = 100
						}
						matches := []map[string]any{}
						if !sc.empty {
							matches = append(matches, map[string]any{"channel": map[string]string{"id": "C1", "name": "release"}, "user": "U1", "text": text, "ts": ts, "permalink": "https://example.test/message", "unreviewed_secret": "synthetic-unreviewed-search-field"})
						}
						body, err := json.Marshal(map[string]any{"ok": true, "messages": map[string]any{"matches": matches, "pagination": map[string]int{"page": resultPage, "page_count": max(resultPage, 2)}}})
						if err != nil {
							t.Error(err)
						}
						writeJSON(w, string(body))
					default:
						t.Errorf("search called an unexpected method: %s", r.URL.Path)
						w.WriteHeader(http.StatusInternalServerError)
					}
				}))
				defer server.Close()
				root := t.TempDir()
				path := filepath.Join(root, "config.toml")
				prior := []byte("schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"T1\"\ntoken_type=\"user\"\ntoken_ref=\"env:SLICK_SEARCH_FLAGS_TOKEN\"\n[workspaces.fixture.aliases]\nkeep=\"C_KEEP\"\n[workspaces.bot]\nname=\"bot\"\nteam_id=\"T1\"\ntoken_type=\"bot\"\ntoken_ref=\"env:SLICK_BOT_FLAGS_TOKEN\"\n")
				if err := os.WriteFile(path, prior, 0o600); err != nil {
					t.Fatal(err)
				}
				for i, sc := range scenarios {
					t.Run(sc.name, func(t *testing.T) {
						activeTest.Store(t)
						current.Store(int32(i))
						beforeCalls, beforeSearches := calls.Load(), searches.Load()
						ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
						defer cancel()
						args := append([]string{"lookup", "messages", "--output=" + mode, "--color=never", "--no-throttle"}, sc.args...)
						command := exec.CommandContext(ctx, binary, args...)
						command.Dir = root
						command.Env = append(isolatedRuntimeEnv(t), "SLICK_CONFIG="+path, "SLACK_CLI_CONFIG="+path, "SLACK_CLI_BASE_URL="+server.URL, "SLICK_SEARCH_FLAGS_TOKEN="+token, "SLICK_BOT_FLAGS_TOKEN=xoxb-synthetic-forbidden-bot")
						var out, diagnostic strings.Builder
						command.Stdout, command.Stderr = &out, &diagnostic
						err := command.Run()
						if ctx.Err() != nil || exitCode(err) != sc.code {
							t.Fatalf("search exit=%d want=%d: %s %s", exitCode(err), sc.code, out.String(), diagnostic.String())
						}
						for _, secret := range []string{token, "xoxb-synthetic-forbidden-bot", "synthetic-unreviewed-search-field"} {
							if strings.Contains(out.String()+diagnostic.String(), secret) {
								t.Fatal("search disclosed credentials or an unreviewed response field")
							}
						}
						wantSearches := int32(0)
						if sc.remote {
							wantSearches = 1
						}
						if searches.Load()-beforeSearches != wantSearches || (!sc.remote && calls.Load() != beforeCalls) {
							t.Fatal("search retried, fetched another page, or reached Slack before validation")
						}
						if mode == "human" {
							if sc.code != 0 {
								if out.Len() != 0 || diagnostic.Len() == 0 {
									t.Fatal("search failure used wrong human streams")
								}
							} else if diagnostic.Len() != 0 || (sc.empty && !strings.Contains(out.String(), "items_returned=0")) || (!sc.empty && strings.Contains(out.String(), tail) != sc.full) {
								t.Fatal("human search ignored full-text selection or empty-page pagination")
							}
						} else {
							assertSearchFlagResult(t, mode, sc.code, sc.query, sc.empty, text, out.String(), diagnostic.String())
						}
						after, readErr := os.ReadFile(path)
						if readErr != nil || string(after) != string(prior) {
							t.Fatal("search changed prior configuration or unrelated aliases")
						}
					})
				}
			})
		}
	}
}

func assertSearchFlagResult(t *testing.T, mode string, code int, query string, empty bool, text, out, diagnostic string) {
	t.Helper()
	if diagnostic != "" {
		t.Fatal("machine search wrote diagnostics outside stdout")
	}
	data := json.RawMessage(out)
	if mode == "json" || code != 0 {
		var keys map[string]json.RawMessage
		var frame struct {
			OK     bool
			Data   json.RawMessage
			Errors []struct {
				Type    string
				Code    int `json:"exit_code"`
				Details struct {
					Needed []string `json:"needed"`
					Match  string   `json:"needed_match"`
				} `json:"details"`
			}
			Meta struct {
				Command, Workspace string
				Code               int `json:"exit_code"`
			}
		}
		if json.Unmarshal([]byte(out), &keys) != nil || len(keys) != 4 || json.Unmarshal([]byte(out), &frame) != nil || frame.OK != (code == 0) || frame.Meta.Command != "lookup.messages" || frame.Meta.Code != code {
			t.Fatalf("search lost common envelope or command metadata: command=%q code=%d ok=%t", frame.Meta.Command, frame.Meta.Code, frame.OK)
		}
		if code != 0 {
			kind := "validation_error"
			if code == 1 {
				kind = "auth_failure"
			}
			if string(frame.Data) != "null" || len(frame.Errors) != 1 || frame.Errors[0].Code != code || frame.Errors[0].Type != kind {
				t.Fatal("search lost failure classification")
			}
			if query == "release-v5" && (len(frame.Errors[0].Details.Needed) != 1 || frame.Errors[0].Details.Needed[0] != "search:read" || frame.Errors[0].Details.Match != "reported") {
				t.Fatal("search lost structured missing-scope diagnostics")
			}
			return
		}
		if frame.Meta.Workspace != "fixture" || len(frame.Errors) != 0 {
			t.Fatal("search changed workspace metadata or retained errors")
		}
		data = frame.Data
	}
	var result struct {
		Query   string
		Matches []struct {
			Text, TS, Permalink string
		}
	}
	if json.Unmarshal(data, &result) != nil || result.Query != query || result.Matches == nil {
		t.Fatal("search lost query or empty-list identity")
	}
	if empty {
		if len(result.Matches) != 0 {
			t.Fatal("empty search invented matches")
		}
		return
	}
	if len(result.Matches) != 1 || result.Matches[0].Text != text || result.Matches[0].TS != "1746284582.123456" || result.Matches[0].Permalink != "https://example.test/message" {
		t.Fatal("search full flag changed machine text, timestamp precision, or permalink")
	}
}
