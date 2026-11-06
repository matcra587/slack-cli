//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestBinaryHistoryRequiresAcknowledgementAndRecovers(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, method := range []string{"conversations.history", "conversations.replies"} {
		for _, tc := range []struct {
			name, body string
			exit       int
		}{
			{"missing", `{"messages":[]}`, 5},
			{"false", `{"ok":false,"messages":[]}`, 5},
			{"null ack", `{"ok":null,"messages":[]}`, 5},
			{"blank error", `{"ok":false,"error":" ","messages":[]}`, 5},
			{"contradictory", `{"ok":true,"error":"internal_error","messages":[]}`, 5},
			{"missing messages", `{"ok":true}`, 5},
			{"null messages", `{"ok":true,"messages":null}`, 5},
			{"wrong messages", `{"ok":true,"messages":{}}`, 5},
			{"trailing", `{"ok":true,"messages":[]} {"private_field":"synthetic-history-canary"}`, 5},
			{"malformed", `{"private_field":"synthetic-history-canary`, 5},
			{"permission", `{"ok":false,"error":"missing_scope","needed":"channels:history"}`, 1},
			{"missing target", `{"ok":false,"error":"channel_not_found"}`, 2},
			{"empty", `{"ok":true,"messages":[]}`, 0},
		} {
			for _, mode := range []string{"json", "compact", "human"} {
				t.Run(method+"/"+tc.name+"/"+mode, func(t *testing.T) {
					configPath := writeStatusUserConfig(t)
					before, err := os.ReadFile(configPath)
					if err != nil {
						t.Fatal(err)
					}
					var calls atomic.Int32
					var recovered atomic.Bool
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.URL.Path == "/api/auth.test" || r.URL.Path == "/api/conversations.info" {
							writeSlackHTTPRecovery(t, w, r, strings.TrimPrefix(r.URL.Path, "/api/"))
							return
						}
						calls.Add(1)
						if err := r.ParseForm(); err != nil {
							t.Error(err)
						}
						if r.Method != http.MethodPost || r.URL.Path != "/api/"+method || r.URL.RawQuery != "" || r.PostForm.Get("token") != "xoxb-test" || r.PostForm.Get("channel") != "C123" {
							t.Error("history validation changed request method/path/auth/target")
						}
						if method == "conversations.replies" && r.PostForm.Get("ts") != "1746284582.123456" {
							t.Error("history validation lost thread timestamp precision")
						}
						if recovered.Load() {
							writeJSON(w, `{"ok":true,"messages":[],"response_metadata":{"next_cursor":"opaque-next"}}`)
						} else {
							writeJSON(w, tc.body)
						}
					}))
					defer server.Close()
					args := []string{"history", "list", "--channel=C123", "--output=" + mode, "--color=never", "--no-throttle", "--timeout=2s"}
					if method == "conversations.replies" {
						args = append(args, "--thread=1746284582.123456")
					}
					out, diagnostic, err := runSlackBinary(t, binary, configPath, server.URL, "", args...)
					if exitCode(err) != tc.exit || calls.Load() != 1 {
						t.Fatalf("history acknowledgement accepted invalid result or replayed: exit=%d want=%d calls=%d", exitCode(err), tc.exit, calls.Load())
					}
					if strings.Contains(out+diagnostic, "synthetic-history-canary") || strings.Contains(out+diagnostic, "xoxb-test") {
						t.Fatal("history result exposed credentials or private response data")
					}
					if tc.exit != 0 {
						if mode == "human" {
							if out != "" || diagnostic == "" {
								t.Fatal("history failure used wrong human streams")
							}
						} else {
							var frame map[string]json.RawMessage
							if json.Unmarshal([]byte(out), &frame) != nil || len(frame) != 4 || string(frame["ok"]) != "false" || string(frame["data"]) != "null" || diagnostic != "" {
								t.Fatal("history failure lost common machine envelope")
							}
						}
					}
					// Recovery is a new explicit invocation, never a transport replay.
					recovered.Store(true)
					out, diagnostic, err = runSlackBinary(t, binary, configPath, server.URL, "", args...)
					if err != nil || diagnostic != "" || calls.Load() != 2 || !strings.Contains(out, "opaque-next") {
						t.Fatal("history did not recover with the empty page's continuation")
					}
					if mode != "human" {
						data := json.RawMessage(out)
						if mode == "json" {
							var frame struct {
								OK   bool
								Data json.RawMessage
							}
							if json.Unmarshal(data, &frame) != nil || !frame.OK {
								t.Fatal("history recovery lost success envelope")
							}
							data = frame.Data
						}
						var got struct {
							Messages   []json.RawMessage
							Pagination struct {
								HasMore       bool   `json:"has_more"`
								NextCursor    string `json:"next_cursor"`
								ItemsReturned int    `json:"items_returned"`
							}
						}
						if json.Unmarshal(data, &got) != nil || got.Messages == nil || len(got.Messages) != 0 || !got.Pagination.HasMore || got.Pagination.NextCursor != "opaque-next" || got.Pagination.ItemsReturned != 0 {
							t.Fatal("history recovery lost explicit empty data or pagination")
						}
					}
					after, readErr := os.ReadFile(configPath)
					if readErr != nil || string(before) != string(after) {
						t.Fatal("history read changed prior workspace settings")
					}
				})
			}
		}
	}
}
