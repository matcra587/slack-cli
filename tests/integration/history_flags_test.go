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

func TestBinaryHistoryFlagsKeepWindowsFiltersAndNestedPages(t *testing.T) {
	binary := buildSlackBinary(t)
	const parent = "1746284500.000001"
	const since, until = "1746284000.000001", "1746285000.000001"
	const cursor = "opaque+/= with space "
	for _, route := range []string{"channel", "thread", "nested"} {
		for _, tokenType := range []string{"bot", "user"} {
			for _, mode := range []string{"json", "compact", "human"} {
				for _, short := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/%s/short=%t", route, tokenType, mode, short), func(t *testing.T) {
						channel, scope, token := "C_TARGET", "channels:history", "xoxp-synthetic-history-flags"
						if tokenType == "bot" {
							channel, scope, token = "D_TARGET", "im:history", "xoxb-synthetic-history-flags"
						}
						var phase, rootCalls, nestedCalls atomic.Int32
						var activeTest atomic.Pointer[testing.T]
						server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							t := activeTest.Load()
							if r.URL.Path == "/api/chat.getPermalink" {
								if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer "+token || r.URL.Query().Get("channel") != channel || r.URL.Query().Has("token") {
									t.Error("history permalink changed GET/header authentication or target")
								}
								writeJSON(w, `{"ok":false,"error":"missing_scope"}`)
								return
							}
							if err := r.ParseForm(); err != nil {
								t.Error(err)
							}
							if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.PostForm.Get("token") != token {
								t.Error("history changed POST form authentication")
							}
							switch r.URL.Path {
							case "/api/auth.test":
								w.Header().Set("X-OAuth-Scopes", scope)
								writeJSON(w, `{"ok":true,"user_id":"U_SELF"}`)
							case "/api/conversations.info":
								writeJSON(w, `{"ok":false,"error":"missing_scope"}`)
							case "/api/conversations.history", "/api/conversations.replies":
								p := phase.Load()
								thread := route == "thread" && p != 2
								nested := route == "nested" && p == 0 && r.URL.Path == "/api/conversations.replies"
								method := "/api/conversations.history"
								if thread || nested {
									method = "/api/conversations.replies"
								}
								if nested {
									nestedCalls.Add(1)
								} else {
									rootCalls.Add(1)
								}
								if r.URL.Path != method || r.PostForm.Get("channel") != channel || ((thread || nested) && r.PostForm.Get("ts") != parent) {
									t.Error("history selected the wrong endpoint or exact target")
								}
								if r.PostForm.Has("user") || r.PostForm.Get("inclusive") != "0" {
									t.Error("history transmitted a local author filter or changed exclusive bounds")
								}
								if nested {
									if r.PostForm.Get("limit") != "3" || r.PostForm.Has("cursor") || r.PostForm.Has("oldest") || r.PostForm.Has("latest") {
										t.Error("nested replies lost their limit or inherited parent filters")
									}
								} else if p == 0 {
									if r.PostForm.Get("oldest") != since || r.PostForm.Get("latest") != until || r.PostForm.Get("limit") != "3" || r.PostForm.Get("cursor") != cursor {
										t.Error("history lost trimmed time bounds, page size, or opaque cursor bytes")
									}
								} else {
									for _, name := range []string{"oldest", "latest", "cursor", "limit"} {
										if r.PostForm.Has(name) {
											t.Errorf("removed history option remained in request: %s", name)
										}
									}
								}
								if nested {
									writeJSON(w, `{"ok":true,"messages":[{"type":"message","user":"U_PARENT","ts":"`+parent+`"},{"type":"message","user":"U_CHILD_ONE","text":"First reply","ts":"1746284582.000001"},{"type":"message","user":"U_CHILD_TWO","text":"Second reply","ts":"1746284583.000001"}],"response_metadata":{"next_cursor":"next-nested"}}`)
								} else if thread {
									writeJSON(w, `{"ok":true,"messages":[{"type":"message","user":"U_PARENT","text":"Parent review","ts":"`+parent+`"},{"type":"message","user":"U_OTHER","text":"Other reply","ts":"1746284582.000001"},{"type":"message","user":"U_PARENT","text":"Author reply","ts":"1746284583.000001"}],"response_metadata":{"next_cursor":"next-root"}}`)
								} else {
									writeJSON(w, `{"ok":true,"messages":[{"type":"message","user":"U_PARENT","text":"Parent review","ts":"`+parent+`","reply_count":2},{"type":"message","user":"U_OTHER","text":"Other message","ts":"1746284582.000001"}],"response_metadata":{"next_cursor":"next-root"}}`)
								}
							default:
								t.Errorf("unexpected history API request: %s", r.URL.Path)
								w.WriteHeader(http.StatusInternalServerError)
							}
						}))
						defer server.Close()
						root := t.TempDir()
						path := filepath.Join(root, "config.toml")
						prior := []byte(fmt.Sprintf("schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"T1\"\ntoken_type=%q\ntoken_ref=\"env:SLICK_HISTORY_FLAGS_TOKEN\"\n[workspaces.fixture.aliases]\ntarget=%q\nkeep=\"C_UNRELATED\"\n", tokenType, " "+channel+" "))
						if err := os.WriteFile(path, prior, 0o600); err != nil {
							t.Fatal(err)
						}
						flag := func(name, alias, value string) []string {
							if short {
								return []string{alias, value}
							}
							return []string{name + "=" + value}
						}
						for i, name := range []string{"explicit padded options", "remove filters", "remove thread", "empty filtered page"} {
							t.Run(name, func(t *testing.T) {
								activeTest.Store(t)
								phase.Store(int32(i))
								beforeRoot, beforeNested := rootCalls.Load(), nestedCalls.Load()
								args := append([]string{"history", "list", "--output=" + mode, "--color=never", "--no-throttle"}, flag("--channel", "-c", " target ")...)
								if route == "thread" && i != 2 {
									args = append(args, flag("--thread", "-t", " "+parent+" ")...)
								}
								switch i {
								case 0:
									for _, f := range [][]string{flag("--since", "-s", " "+since+" "), flag("--until", "-u", "\t"+until+"\t"), flag("--user", "-U", " U_PARENT "), flag("--max-items", "-M", "3"), flag("--cursor", "-C", cursor)} {
										args = append(args, f...)
									}
									if route == "nested" {
										inclusion := "--include-replies"
										if short {
											inclusion = "-R"
										}
										args = append(args, inclusion)
										args = append(args, flag("--reply-limit", "-L", "2")...)
									}
								case 1:
									for _, f := range [][]string{flag("--since", "-s", " \t "), flag("--until", "-u", ""), flag("--user", "-U", " "), flag("--max-items", "-M", "0")} {
										args = append(args, f...)
									}
									inclusion := "--include-replies=false"
									if short {
										inclusion = "-R=false"
									}
									args = append(args, inclusion)
								case 2:
									args = append(args, flag("--thread", "-t", " \t ")...)
								case 3:
									args = append(args, flag("--user", "-U", "U_MISSING")...)
									args = append(args, "--dry-run") // Reads still run; this flag previews Slack mutations.
								}
								ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
								defer cancel()
								command := exec.CommandContext(ctx, binary, args...)
								command.Dir = root
								command.Env = append(isolatedRuntimeEnv(t), "SLICK_CONFIG="+path, "SLACK_CLI_CONFIG="+path, "SLACK_CLI_BASE_URL="+server.URL, "SLICK_HISTORY_FLAGS_TOKEN="+token)
								var out, diagnostic strings.Builder
								command.Stdout, command.Stderr = &out, &diagnostic
								err := command.Run()
								if ctx.Err() != nil || err != nil || diagnostic.Len() != 0 {
									t.Fatalf("valid history options failed: %v %s %s", err, out.String(), diagnostic.String())
								}
								if strings.Contains(out.String(), token) {
									t.Fatal("history exposed credentials")
								}
								wantCount := 2
								if i == 3 {
									wantCount = 0
								} else if route == "thread" && i != 2 {
									if i == 1 {
										wantCount = 3
									}
								} else if i == 0 {
									wantCount = 1
								}
								wantNested := int32(0)
								if route == "nested" && i == 0 {
									wantNested = 1
								}
								if rootCalls.Load()-beforeRoot != 1 || nestedCalls.Load()-beforeNested != wantNested {
									t.Fatal("history retried, fetched another root page, or ignored reply inclusion/removal")
								}
								assertHistoryFlagResult(t, mode, out.String(), wantCount, wantNested != 0)
								after, readErr := os.ReadFile(path)
								if readErr != nil || string(after) != string(prior) {
									t.Fatal("history changed prior config or unrelated aliases")
								}
							})
						}
					})
				}
			}
		}
	}
}

func assertHistoryFlagResult(t *testing.T, mode, out string, count int, nested bool) {
	t.Helper()
	if mode == "human" {
		if !strings.Contains(out, "next_cursor=next-root") || !strings.Contains(out, fmt.Sprintf("items_returned=%d", count)) {
			t.Fatal("human history lost its filtered count or continuation")
		}
		return
	}
	data := json.RawMessage(out)
	if mode == "json" {
		var fields map[string]json.RawMessage
		var frame struct {
			OK     bool
			Data   json.RawMessage
			Errors []json.RawMessage
			Meta   struct{ Command, Workspace string }
		}
		if json.Unmarshal([]byte(out), &fields) != nil || len(fields) != 4 || json.Unmarshal([]byte(out), &frame) != nil || !frame.OK || len(frame.Errors) != 0 || frame.Meta.Command != "history.list" || frame.Meta.Workspace != "fixture" {
			t.Fatal("history lost common success envelope or metadata")
		}
		data = frame.Data
	}
	var result struct {
		Messages []struct {
			TS      string
			Replies []struct{ TS, User string }
			Paging  struct {
				Next  string `json:"next_cursor"`
				Count int    `json:"items_returned"`
			} `json:"replies_pagination"`
		}
		Paging struct {
			Next  string `json:"next_cursor"`
			More  bool   `json:"has_more"`
			Count int    `json:"items_returned"`
		} `json:"pagination"`
	}
	if json.Unmarshal(data, &result) != nil || result.Messages == nil || len(result.Messages) != count || result.Paging.Count != count || result.Paging.Next != "next-root" || !result.Paging.More {
		t.Fatal("history lost author filtering, empty collection, or root pagination")
	}
	if count > 0 && result.Messages[0].TS != "1746284500.000001" {
		t.Fatal("history changed timestamp precision")
	}
	if nested {
		parent := result.Messages[0]
		if len(parent.Replies) != 2 || parent.Replies[0].TS != "1746284582.000001" || parent.Replies[1].TS != "1746284583.000001" || parent.Replies[0].User != "U_CHILD_ONE" || parent.Replies[1].User != "U_CHILD_TWO" || parent.Paging.Next != "next-nested" || parent.Paging.Count != 2 {
			t.Fatal("nested history lost independent authors, exact replies, or continuation")
		}
	} else {
		for _, message := range result.Messages {
			if len(message.Replies) != 0 {
				t.Fatal("removed reply inclusion still fetched nested data")
			}
		}
	}
}
