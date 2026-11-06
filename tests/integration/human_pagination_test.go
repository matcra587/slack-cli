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

func TestBinaryListsPreserveContinuationWithAndWithoutRowsInEveryMode(t *testing.T) {
	binary := buildSlackBinary(t)
	const ts = "1746284582.123456"
	for _, route := range []struct {
		name, method, key, row, nextField, nextValue string
		args                                         []string
	}{
		{"history cursor", "conversations.history", "messages", `{"type":"message","ts":"` + ts + `","user":"U1","text":"Review complete"}`, "next_cursor", "page-next", []string{"history", "list", "--channel=C123"}},
		{"channel time", "conversations.history", "messages", `{"type":"message","ts":"` + ts + `","user":"U1","text":"Review complete"}`, "next_until", ts, []string{"history", "list", "--channel=C123"}},
		{"thread time", "conversations.replies", "messages", `{"type":"message","ts":"` + ts + `","user":"U1","text":"Review complete"}`, "next_since", ts, []string{"history", "list", "--channel=C123", "--thread=1746284500.000001"}},
		{"search", "search.messages", "matches", `{"ts":"` + ts + `","user":"U1","text":"Review complete","channel":{"id":"C123","name":"fixture"}}`, "next_cursor", "2", []string{"lookup", "messages", "--query=release"}},
		{"full search", "search.messages", "matches", `{"ts":"` + ts + `","user":"U1","text":"Review complete","channel":{"id":"C123","name":"fixture"}}`, "next_cursor", "2", []string{"lookup", "messages", "--query=release", "--full"}},
		{"channels", "conversations.list", "channels", `{"id":"C123","name":"fixture"}`, "next_cursor", "page-next", []string{"lookup", "channel"}},
		{"users", "users.list", "members", `{"id":"U1","name":"fixture"}`, "next_cursor", "page-next", []string{"lookup", "user"}},
		{"scheduled", "chat.scheduledMessages.list", "scheduled_messages", `{"id":"Q1","channel_id":"C123","post_at":1800000000,"text":"Review complete"}`, "next_cursor", "page-next", []string{"message", "scheduled", "list"}},
	} {
		for _, empty := range []bool{false, true} {
			for _, more := range []bool{false, true} {
				// Slack cannot supply a time boundary for an empty raw history page.
				if empty && more && route.nextField != "next_cursor" {
					continue
				}
				for _, mode := range []string{"human", "json", "compact"} {
					t.Run(fmt.Sprintf("%s/empty=%t/more=%t/%s", route.name, empty, more, mode), func(t *testing.T) {
						const token = "xoxp-synthetic-human-pagination"
						var calls atomic.Int32
						server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							if err := r.ParseForm(); err != nil {
								t.Error(err)
							}
							if r.URL.Path == "/api/chat.getPermalink" {
								if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer "+token || r.URL.Query().Get("channel") != "C123" || r.URL.Query().Get("message_ts") != ts {
									t.Error("wrong permalink lookup contract")
								}
								writeJSON(w, `{"ok":true,"permalink":"https://fixture.invalid/message"}`)
								return
							}
							if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.PostForm.Get("token") != token {
								t.Error("wrong method or synthetic authentication")
							}
							w.Header().Set("Content-Type", "application/json")
							switch r.URL.Path {
							case "/api/auth.test":
								w.Header().Set("X-OAuth-Scopes", "channels:history,channels:read,groups:read,users:read,search:read,chat:write")
								writeJSON(w, `{"ok":true,"user_id":"U1"}`)
								return
							case "/api/conversations.info":
								writeJSON(w, `{"ok":true,"channel":{"id":"C123","name":"fixture"}}`)
								return
							}
							if r.URL.Path != "/api/"+route.method {
								t.Errorf("unexpected path: %s", r.URL.Path)
								writeJSON(w, `{"ok":false,"error":"method_not_found"}`)
								return
							}
							calls.Add(1)
							rows := []json.RawMessage{}
							if !empty {
								rows = append(rows, json.RawMessage(route.row))
							}
							next := ""
							if more && route.nextField == "next_cursor" {
								next = route.nextValue
							}
							body := map[string]any{"ok": true, route.key: rows, "has_more": more, "response_metadata": map[string]string{"next_cursor": next}}
							if route.method == "search.messages" {
								pages := 1
								if more {
									pages = 2
								}
								body = map[string]any{"ok": true, "messages": map[string]any{"matches": rows, "pagination": map[string]int{"page": 1, "page_count": pages}}}
							}
							if err := json.NewEncoder(w).Encode(body); err != nil {
								t.Error(err)
							}
						}))
						defer server.Close()
						isolated := t.TempDir()
						configPath := filepath.Join(isolated, "config.toml")
						cfg := "schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"T123\"\ntoken_type=\"user\"\ntoken=\"env:SLICK_PAGE_FIXTURE_TOKEN\"\n"
						if err := os.WriteFile(configPath, []byte(cfg), 0o600); err != nil {
							t.Fatal(err)
						}
						ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
						defer cancel()
						args := append(append([]string{}, route.args...), "--output="+mode, "--color=never", "--no-throttle", "--timeout=3s")
						cmd := exec.CommandContext(ctx, binary, args...)
						cmd.Env = []string{"HOME=" + isolated, "SLICK_CONFIG=" + configPath, "SLACK_CLI_BASE_URL=" + server.URL, "SLICK_PAGE_FIXTURE_TOKEN=" + token}
						var out, diagnostic bytes.Buffer
						cmd.Stdout, cmd.Stderr = &out, &diagnostic
						if err := cmd.Run(); err != nil || ctx.Err() != nil {
							t.Fatalf("human list failed: err=%v stderr=%s", err, &diagnostic)
						}
						output := out.String()
						if diagnostic.Len() != 0 || calls.Load() != 1 || strings.Contains(output, token) {
							t.Fatal("list changed streams, exposed a credential or replayed a request")
						}
						if mode == "human" {
							if !strings.Contains(output, fmt.Sprintf("has_more=%t", more)) {
								t.Fatalf("human output hid continuation state: %s", output)
							}
							count := 1
							if empty {
								count = 0
							}
							if !strings.Contains(output, fmt.Sprintf("items_returned=%d", count)) {
								t.Fatalf("human output hid result count: %s", output)
							}
							if more && !strings.Contains(output, route.nextField+"="+route.nextValue) {
								t.Fatalf("human output hid continuation value: %s", output)
							}
							if !more && (strings.Contains(output, "next_cursor=") || strings.Contains(output, "next_since=") || strings.Contains(output, "next_until=")) {
								t.Fatal("final human page invented continuation")
							}
							if !empty {
								marker := "Review complete"
								if route.name == "channels" || route.name == "users" {
									marker = "fixture"
								}
								if !strings.Contains(output, marker) {
									t.Fatal("pagination footer lost the table rows")
								}
							}
						} else {
							var frame map[string]json.RawMessage
							if err := json.Unmarshal(out.Bytes(), &frame); err != nil {
								t.Fatal(err)
							}
							data := out.Bytes()
							if mode == "json" {
								if len(frame) != 4 || string(frame["ok"]) != "true" {
									t.Fatal("list lost common success envelope")
								}
								data = frame["data"]
							}
							var result struct {
								Pagination map[string]json.RawMessage `json:"pagination"`
							}
							if err := json.Unmarshal(data, &result); err != nil {
								t.Fatal(err)
							}
							count := 1
							if empty {
								count = 0
							}
							if string(result.Pagination["has_more"]) != fmt.Sprint(more) || string(result.Pagination["items_returned"]) != fmt.Sprint(count) {
								t.Fatalf("machine page lost explicit state/count: %s", data)
							}
							if more && string(result.Pagination[route.nextField]) != fmt.Sprintf("%q", route.nextValue) {
								t.Fatal("machine page lost continuation")
							}
							if !more {
								for _, key := range []string{"next_cursor", "next_since", "next_until"} {
									if _, exists := result.Pagination[key]; exists {
										t.Fatal("final machine page invented continuation")
									}
								}
							}
						}
						after, err := os.ReadFile(configPath)
						if err != nil || string(after) != cfg {
							t.Fatal("read-only list changed workspace settings")
						}
					})
				}
			}
		}
	}
}
