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

func TestBinaryRecipientErrorsStopBeforeSendingAndRecover(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tc := range []struct {
		code, kind string
		exit       int
	}{
		{"too_many_users", "validation_error", 4},
		{"not_enough_users", "validation_error", 4},
		{"users_list_not_supplied", "validation_error", 4},
		{"internal_error", "server_error", 5},
		{"future_recipient_error", "server_error", 5},
	} {
		for _, scheduled := range []bool{false, true} {
			for _, kind := range []string{"bot", "user"} {
				for _, mode := range []string{"json", "compact", "human"} {
					t.Run(fmt.Sprintf("%s/scheduled=%t/%s/%s", tc.code, scheduled, kind, mode), func(t *testing.T) {
						path := filepath.Join(t.TempDir(), "config.toml")
						config := fmt.Sprintf("schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"T123\"\ntoken_type=%q\ntoken_ref=\"env:SLACK_TEST_TOKEN\"\ndefault_channel=\"C_UNRELATED\"\n", kind)
						if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
							t.Fatal(err)
						}
						var recovered atomic.Bool
						var opens, sends atomic.Int32
						var activeTest atomic.Pointer[testing.T]
						const private = "synthetic-private-recipient-response"
						const eight = "U_1,U_2,U_3,U_4,U_5,U_6,U_7,U_8"
						server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							t := activeTest.Load()
							if r.URL.Path == "/api/chat.getPermalink" {
								writeJSON(w, `{"ok":false,"error":"missing_scope"}`)
								return
							}
							if err := r.ParseForm(); err != nil {
								t.Error(err)
							}
							if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.PostForm.Get("token") != "xoxb-test" {
								t.Error("recipient error path changed POST form authentication")
							}
							switch r.URL.Path {
							case "/api/auth.test":
								w.Header().Set("X-OAuth-Scopes", "chat:write,mpim:write")
								writeJSON(w, `{"ok":true,"user_id":"U_SELF"}`)
							case "/api/conversations.open":
								opens.Add(1)
								wantUsers := "U_1,U_2"
								if tc.code == "too_many_users" {
									wantUsers = eight + ",U_9"
								}
								if recovered.Load() {
									wantUsers = eight
								}
								if r.PostForm.Get("users") != wantUsers || r.PostForm.Get("return_im") != "true" || r.PostForm.Has("channel") {
									t.Error("recipient boundary changed exact IDs or order")
								}
								if !recovered.Load() {
									writeJSON(w, `{"ok":false,"error":"`+tc.code+`","unreviewed_secret":"`+private+`"}`)
								} else {
									writeJSON(w, `{"ok":true,"channel":{"id":"G_RECOVERED"}}`)
								}
							case "/api/chat.postMessage", "/api/chat.scheduleMessage":
								sends.Add(1)
								method := "/api/chat.postMessage"
								if scheduled {
									method = "/api/chat.scheduleMessage"
								}
								if !recovered.Load() || r.URL.Path != method || r.PostForm.Get("channel") != "G_RECOVERED" || r.PostForm.Get("text") != "Release review ready" {
									t.Error("failed recipient open reached send or recovery changed target")
								}
								if scheduled {
									writeJSON(w, `{"ok":true,"channel":"G_RECOVERED","scheduled_message_id":"Q_RECOVERED"}`)
								} else {
									writeJSON(w, `{"ok":true,"channel":"G_RECOVERED","ts":"1746284582.000001","message":{"text":"Release review ready","ts":"1746284582.000001"}}`)
								}
							case "/api/conversations.info":
								writeJSON(w, `{"ok":false,"error":"missing_scope"}`)
							default:
								t.Errorf("unexpected recipient recovery path %s", r.URL.Path)
								w.WriteHeader(http.StatusInternalServerError)
							}
						}))
						defer server.Close()
						for _, recovery := range []bool{false, true} {
							t.Run(fmt.Sprintf("recovery=%t", recovery), func(t *testing.T) {
								activeTest.Store(t)
								users := "U_1,U_2"
								if tc.code == "too_many_users" {
									users = eight + ",U_9"
								}
								wantExit := tc.exit
								if recovery {
									users, wantExit = eight, 0
								}
								recovered.Store(recovery)
								opens.Store(0)
								sends.Store(0)
								args := []string{"message", "send", "--user", users, "--message", "Release review ready", "--no-attribution", "--no-throttle", "--color=never", "--output=" + mode}
								if scheduled {
									args = append(args, "--schedule", "1h")
								}
								out, diagnostic, err := runSlackBinary(t, binary, path, server.URL, "", args...)
								if exitCode(err) != wantExit {
									t.Fatalf("recipient error class exit=%d, want=%d stdout=%s stderr=%s", exitCode(err), wantExit, out, diagnostic)
								}
								wantSends := int32(0)
								if recovery {
									wantSends = 1
								}
								if opens.Load() != 1 || sends.Load() != wantSends {
									t.Error("recipient failure/recovery replayed open or sent after failure")
								}
								if mode == "human" {
									if (recovery && (out == "" || diagnostic != "")) || (!recovery && (out != "" || !strings.Contains(diagnostic, tc.code))) {
										t.Error("recipient failure changed human streams")
									}
								} else {
									if diagnostic != "" {
										t.Error("recipient failure escaped machine stdout")
									}
									var frame map[string]any
									if err := json.Unmarshal([]byte(out), &frame); err != nil {
										t.Fatal(err)
									}
									if !recovery {
										meta := frame["meta"].(map[string]any)
										errors := frame["errors"].([]any)
										if len(frame) != 4 || frame["ok"] != false || frame["data"] != nil || meta["command"] != "message.send" || meta["exit_code"] != float64(tc.exit) || len(errors) != 1 {
											t.Error("recipient failure lost common four-field envelope")
										} else if e := errors[0].(map[string]any); e["type"] != tc.kind || e["message"] != tc.code || e["exit_code"] != float64(tc.exit) {
											t.Error("recipient failure lost structured error class")
										}
									} else if mode == "json" && frame["ok"] != true {
										t.Error("recipient recovery lost success envelope")
									}
								}
								if strings.Contains(out+diagnostic, private) || strings.Contains(out+diagnostic, "xoxb-test") {
									t.Error("recipient failure exposed unreviewed secret fields")
								}
								after, err := os.ReadFile(path)
								if err != nil || string(after) != config {
									t.Error("recipient failure/recovery changed saved settings")
								}
							})
						}
					})
				}
			}
		}
	}
}
