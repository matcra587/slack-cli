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

func TestBinaryEditReplyTargetFlags(t *testing.T) {
	binary := buildSlackBinary(t)
	const parent = "1746284582.000001"
	for _, operation := range []string{"edit", "reply"} {
		for _, kind := range []string{"bot", "user"} {
			for _, mode := range []string{"json", "compact", "human"} {
				for _, short := range []bool{false, true} {
					for _, dry := range []bool{false, true} {
						for _, target := range []struct {
							name, channel, timestamp string
							code                     int
						}{
							{"id", "C_TARGET", parent, 0},
							{"alias", "release", parent, 0},
							{"padded alias and timestamp", " release ", " " + parent + " ", 0},
							{"empty channel ignores default", "", parent, 4},
							{"empty timestamp", "release", " ", 4},
						} {
							t.Run(fmt.Sprintf("%s/%s/%s/short=%t/dry=%t/%s", operation, kind, mode, short, dry, target.name), func(t *testing.T) {
								path := filepath.Join(t.TempDir(), "config.toml")
								config := fmt.Sprintf("schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"T123\"\ntoken_type=%q\ntoken_ref=\"env:SLACK_TEST_TOKEN\"\ndefault_channel=\"C_OTHER\"\n[workspaces.fixture.aliases]\nrelease=\"C_TARGET\"\n", kind)
								if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
									t.Fatal(err)
								}
								var requests, mutations atomic.Int32
								server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
									requests.Add(1)
									if r.URL.Path == "/api/chat.getPermalink" {
										if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer xoxb-test" || r.URL.Query().Get("channel") != "C_TARGET" || r.URL.Query().Get("message_ts") != "1746284582.000002" {
											t.Error("wrong reply permalink request")
										}
										writeJSON(w, `{"ok":false,"error":"missing_scope"}`)
										return
									}
									if err := r.ParseForm(); err != nil {
										t.Error(err)
									}
									if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.PostForm.Get("token") != "xoxb-test" {
										t.Error("wrong request method/authentication/encoding")
									}
									switch r.URL.Path {
									case "/api/auth.test":
										w.Header().Set("X-OAuth-Scopes", "chat:write")
										writeJSON(w, `{"ok":true,"user_id":"U1"}`)
									case "/api/chat.update", "/api/chat.postMessage":
										mutations.Add(1)
										method := map[string]string{"edit": "/api/chat.update", "reply": "/api/chat.postMessage"}[operation]
										field := map[string]string{"edit": "ts", "reply": "thread_ts"}[operation]
										if r.URL.Path != method || r.PostForm.Get("channel") != "C_TARGET" || r.PostForm.Get(field) != parent {
											t.Errorf("wrong exact message target: method=%s channel=%q timestamp=%q", r.URL.Path, r.PostForm.Get("channel"), r.PostForm.Get(field))
										}
										if r.PostForm.Get("text") != "PR review complete" || r.PostForm.Get("blocks") == "" {
											t.Error("target normalization changed message content")
										}
										if operation == "edit" {
											writeJSON(w, `{"ok":true,"channel":"C_TARGET","ts":"`+parent+`","text":"Slack stored text"}`)
										} else {
											writeJSON(w, `{"ok":true,"channel":"C_TARGET","ts":"1746284582.000002","message":{"type":"message","ts":"1746284582.000002","thread_ts":"`+parent+`","text":"Slack stored text"}}`)
										}
									case "/api/conversations.info":
										writeJSON(w, `{"ok":false,"error":"missing_scope"}`)
									default:
										t.Errorf("unexpected request: %s", r.URL.Path)
										w.WriteHeader(500)
									}
								}))
								defer server.Close()
								channelFlag, timeFlag, messageFlag := "--channel", "--timestamp", "--message"
								args := []string{"message", "edit"}
								command := "message.edit"
								if operation == "reply" {
									args = []string{"reply"}
									timeFlag = "--parent"
									command = "reply"
								}
								if short {
									channelFlag = "-c"
									messageFlag = "-m"
									timeFlag = "-t"
									if operation == "reply" {
										timeFlag = "-p"
									}
								}
								args = append(args, channelFlag, target.channel, timeFlag, target.timestamp, messageFlag, "PR review complete", "--no-attribution", "--output="+mode, "--color=never", "--no-throttle")
								if dry {
									args = append(args, "--dry-run")
								}
								out, diagnostic, err := runSlackBinary(t, binary, path, server.URL, "", args...)
								if exitCode(err) != target.code {
									t.Fatalf("wrong command exit: %v %s %s", err, out, diagnostic)
								}
								want := int32(1)
								if dry || target.code != 0 {
									want = 0
								}
								if mutations.Load() != want {
									t.Fatal("message mutation omitted or replayed")
								}
								if want == 0 && requests.Load() != 0 {
									t.Fatal("preview or invalid target accessed Slack")
								}
								if mode == "human" {
									if target.code != 0 && (out != "" || diagnostic == "") {
										t.Fatal("wrong human failure streams")
									}
								} else {
									if diagnostic != "" {
										t.Fatal("machine diagnostics escaped stdout")
									}
									raw := []byte(out)
									if mode == "json" || target.code != 0 {
										var frame struct {
											OK   bool
											Data json.RawMessage
											Meta struct {
												Command string
												Code    int `json:"exit_code"`
											}
										}
										if json.Unmarshal(raw, &frame) != nil || frame.OK != (target.code == 0) || frame.Meta.Command != command || frame.Meta.Code != target.code {
											t.Fatal("invalid message envelope")
										}
										raw = frame.Data
									}
									if target.code == 0 {
										var data struct {
											Message struct {
												Channel, TS, Text string
												Thread            string `json:"thread_ts"`
											}
											Dry bool `json:"dry_run"`
										}
										if json.Unmarshal(raw, &data) != nil || data.Message.Channel != "C_TARGET" || data.Dry != dry {
											t.Fatalf("normalized result target lost: %s", raw)
										}
										wantTS := parent
										if operation == "reply" {
											wantTS = "1746284582.000002"
											if dry {
												wantTS = "dry-run"
											}
											if data.Message.Thread != parent {
												t.Fatalf("parent precision or whitespace lost: %s", raw)
											}
										}
										if data.Message.TS != wantTS {
											t.Fatalf("message timestamp changed: %s", raw)
										}
										wantText := "Slack stored text"
										if dry {
											wantText = "PR review complete"
										}
										if data.Message.Text != wantText {
											t.Fatal("normalization changed returned message content")
										}
									}
								}
								if strings.Contains(out+diagnostic, "xoxb-test") {
									t.Fatal("credential leaked")
								}
								after, err := os.ReadFile(path)
								if err != nil || string(after) != config {
									t.Fatal("message command changed prior configuration")
								}
							})
						}
					}
				}
			}
		}
	}
}
