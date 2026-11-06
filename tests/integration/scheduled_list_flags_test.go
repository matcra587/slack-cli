//go:build integration

package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestBinaryScheduledListRequestContract(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, kind := range []string{"bot", "user"} {
		for _, mode := range []string{"json", "compact", "human"} {
			for _, tc := range []struct {
				name                   string
				args                   []string
				channel, cursor, limit string
				code                   int
			}{
				{name: "removed optional arguments"},
				{name: "long aliases", args: []string{"--channel= release ", "--cursor=NEXT +/=", "--limit=1"}, channel: "C_RELEASE", cursor: "NEXT +/=", limit: "1"},
				{name: "short flags", args: []string{"-c", "release", "-C", "NEXT +/=", "-L", "2"}, channel: "C_RELEASE", cursor: "NEXT +/=", limit: "2"},
				{name: "zero omits limit", args: []string{"--limit=0"}},
				{name: "positive limit forwarded", args: []string{"--limit=1000000"}, limit: "1000000"},
				{name: "negative limit", args: []string{"--limit=-1"}, code: 4},
			} {
				t.Run(kind+"/"+mode+"/"+tc.name, func(t *testing.T) {
					path := filepath.Join(t.TempDir(), "config.toml")
					config := fmt.Sprintf("schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"T123\"\ntoken_type=%q\ntoken_ref=\"env:SLACK_TEST_TOKEN\"\ndefault_channel=\"C_OTHER\"\n[workspaces.fixture.aliases]\nrelease=\"C_RELEASE\"\n", kind)
					if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
						t.Fatal(err)
					}
					var lists, preflights atomic.Int32
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if err := r.ParseForm(); err != nil {
							t.Error(err)
						}
						if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.PostForm.Get("token") != "xoxb-test" || r.Header.Get("Authorization") != "" {
							t.Error("scheduled list request method/authentication/encoding changed")
						}
						switch r.URL.Path {
						case "/api/auth.test":
							preflights.Add(1)
							w.Header().Set("X-OAuth-Scopes", "reactions:read")
							writeJSON(w, `{"ok":true,"user_id":"U1"}`)
						case "/api/chat.scheduledMessages.list":
							lists.Add(1)
							if r.PostForm.Get("channel") != tc.channel || r.PostForm.Get("cursor") != tc.cursor || r.PostForm.Get("limit") != tc.limit {
								t.Error("scheduled list parameters differ")
							}
							if (tc.channel == "" && r.PostForm.Has("channel")) || (tc.cursor == "" && r.PostForm.Has("cursor")) || (tc.limit == "" && r.PostForm.Has("limit")) {
								t.Error("removed optional argument still sent")
							}
							if r.PostForm.Has("oldest") || r.PostForm.Has("latest") || r.PostForm.Has("team_id") {
								t.Error("unrequested filter added")
							}
							writeJSON(w, `{"ok":true,"scheduled_messages":[],"response_metadata":{"next_cursor":"NEXT2"}}`)
						default:
							t.Errorf("unexpected method: %s", r.URL.Path)
							w.WriteHeader(500)
						}
					}))
					defer server.Close()
					args := append([]string{"message", "scheduled", "list", "--output=" + mode, "--color=never", "--no-throttle"}, tc.args...)
					out, diagnostic, err := runSlackBinary(t, binary, path, server.URL, "", args...)
					if exitCode(err) != tc.code {
						t.Fatalf("scheduled list wrong exit: %v %s %s", err, out, diagnostic)
					}
					want := int32(1)
					if tc.code != 0 {
						want = 0
					}
					if lists.Load() != want || preflights.Load() != 0 {
						t.Fatalf("listing required an undocumented scope preflight or reached Slack for invalid input: lists=%d preflights=%d", lists.Load(), preflights.Load())
					}
					if mode == "human" {
						if tc.code != 0 && (out != "" || diagnostic == "") {
							t.Fatal("wrong human error streams")
						}
					} else {
						if diagnostic != "" {
							t.Fatal("machine diagnostics escaped stdout")
						}
						raw := []byte(out)
						if mode == "json" || tc.code != 0 {
							var frame struct {
								OK   bool
								Data json.RawMessage
								Meta struct {
									Command    string
									Code       int `json:"exit_code"`
									Pagination struct {
										Next string `json:"next_cursor"`
									}
								}
							}
							if json.Unmarshal(raw, &frame) != nil || frame.OK != (tc.code == 0) || frame.Meta.Command != "message.scheduled.list" || frame.Meta.Code != tc.code {
								t.Fatal("invalid scheduled-list envelope")
							}
							if tc.code == 0 && frame.Meta.Pagination.Next != "NEXT2" {
								t.Fatal("compatibility pagination missing")
							}
							raw = frame.Data
						}
						if tc.code == 0 {
							var data struct {
								Messages   []json.RawMessage `json:"scheduled_messages"`
								Pagination struct {
									Next    string `json:"next_cursor"`
									HasMore bool   `json:"has_more"`
								}
							}
							if json.Unmarshal(raw, &data) != nil || data.Messages == nil || len(data.Messages) != 0 || data.Pagination.Next != "NEXT2" || !data.Pagination.HasMore {
								t.Fatalf("empty intermediate page lost continuation: %s", raw)
							}
						}
					}
					if strings.Contains(out+diagnostic, "xoxb-test") {
						t.Fatal("token leaked")
					}
					after, err := os.ReadFile(path)
					if err != nil || string(after) != config {
						t.Fatal("listing changed prior configuration")
					}
				})
			}
		}
	}
}
