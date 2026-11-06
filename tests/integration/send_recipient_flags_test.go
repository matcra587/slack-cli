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

func TestBinarySendRecipientFlagsResolveExactTargets(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, scheduled := range []bool{false, true} {
		for _, kind := range []string{"bot", "user"} {
			for _, mode := range []string{"json", "compact", "human"} {
				for _, short := range []bool{false, true} {
					t.Run(fmt.Sprintf("scheduled=%t/%s/%s/short=%t", scheduled, kind, mode, short), func(t *testing.T) {
						for _, tc := range []struct {
							name, channel, users, email string
							recipients                  []string
							preview, invalid            bool
						}{
							{name: "padded channel alias", channel: " release "},
							{name: "direct channel ID", channel: " C_TARGET "},
							{name: "single recipient alias", recipients: []string{" first "}, users: "U_ONE"},
							{name: "email alias", recipients: []string{" mail "}, users: "U_MAIL", email: "dev@example.test"},
							{name: "repeated and CSV recipients", recipients: []string{" first , second ", " third "}, users: "U_ONE,U_TWO,U_THREE"},
							{name: "group email and ID", recipients: []string{" mail , second "}, users: "U_MAIL,U_TWO", email: "dev@example.test"},
							{name: "blank channel alias cannot use default", channel: " blank ", invalid: true},
							{name: "blank recipient alias cannot use default", recipients: []string{" blank "}, invalid: true},
							{name: "remove target uses default only for immediate send", invalid: scheduled},
							{name: "preview group keeps aliases local", recipients: []string{" first , second "}, users: "U_ONE,U_TWO", preview: true},
						} {
							t.Run(tc.name, func(t *testing.T) {
								path := filepath.Join(t.TempDir(), "config.toml")
								config := fmt.Sprintf("schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"T123\"\ntoken_type=%q\ntoken_ref=\"env:SLACK_TEST_TOKEN\"\ndefault_channel=\"release\"\n[workspaces.fixture.aliases]\nrelease=\" C_TARGET \"\nfirst=\" U_ONE \"\nsecond=\" U_TWO \"\nthird=\" U_THREE \"\nmail=\" dev@example.test \"\nblank=\"  \"\n", kind)
								if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
									t.Fatal(err)
								}
								var calls, sends, opens, emails atomic.Int32
								target, scopes := "C_TARGET", "chat:write"
								if tc.users != "" {
									target = "D_TARGET"
									if strings.Contains(tc.users, ",") {
										target, scopes = "G_TARGET", scopes+",mpim:write"
									} else {
										scopes += ",im:write"
									}
								}
								if tc.email != "" {
									scopes += ",users:read.email"
								}
								server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
									calls.Add(1)
									if r.URL.Path == "/api/chat.getPermalink" {
										if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer xoxb-test" {
											t.Error("recipient send changed permalink authentication")
										}
										writeJSON(w, `{"ok":false,"error":"missing_scope"}`)
										return
									}
									if err := r.ParseForm(); err != nil {
										t.Error(err)
									}
									if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.PostForm.Get("token") != "xoxb-test" {
										t.Error("recipient send changed POST form authentication")
									}
									switch r.URL.Path {
									case "/api/auth.test":
										w.Header().Set("X-OAuth-Scopes", scopes)
										writeJSON(w, `{"ok":true,"user_id":"U_SELF"}`)
									case "/api/users.lookupByEmail":
										emails.Add(1)
										if tc.email == "" || r.PostForm.Get("email") != tc.email {
											t.Error("recipient email alias was not normalized before lookup")
										}
										writeJSON(w, `{"ok":true,"user":{"id":"U_MAIL"}}`)
									case "/api/conversations.open":
										opens.Add(1)
										if tc.users == "" || r.PostForm.Get("users") != tc.users || r.PostForm.Get("return_im") != "true" || r.PostForm.Has("channel") {
											t.Error("recipient aliases lost exact order or whitespace normalization")
										}
										writeJSON(w, `{"ok":true,"channel":{"id":"`+target+`"}}`)
									case "/api/chat.postMessage", "/api/chat.scheduleMessage":
										sends.Add(1)
										method := "/api/chat.postMessage"
										if scheduled {
											method = "/api/chat.scheduleMessage"
										}
										if r.URL.Path != method || r.PostForm.Get("channel") != target || r.PostForm.Get("text") != "Release review ready" {
											t.Error("send lost its exact resolved conversation or content")
										}
										if scheduled {
											if r.PostForm.Get("post_at") == "" {
												t.Error("recipient send lost scheduled time")
											}
											writeJSON(w, `{"ok":true,"channel":"`+target+`","scheduled_message_id":"Q_TARGET"}`)
										} else {
											writeJSON(w, `{"ok":true,"channel":"`+target+`","ts":"1746284582.000001","message":{"type":"message","text":"Release review ready","ts":"1746284582.000001"}}`)
										}
									case "/api/conversations.info":
										writeJSON(w, `{"ok":false,"error":"missing_scope"}`)
									default:
										t.Errorf("unexpected recipient request %s", r.URL.Path)
										w.WriteHeader(http.StatusInternalServerError)
									}
								}))
								defer server.Close()
								channelFlag, userFlag := "--channel", "--user"
								if short {
									channelFlag, userFlag = "-c", "-u"
								}
								args := []string{"message", "send", "--message", "Release review ready", "--no-attribution", "--no-throttle", "--color=never", "--output=" + mode}
								if tc.channel != "" {
									args = append(args, channelFlag, tc.channel)
								}
								for _, recipient := range tc.recipients {
									args = append(args, userFlag, recipient)
								}
								if scheduled {
									args = append(args, "--schedule", "1h")
								}
								if tc.preview {
									args = append(args, "--dry-run")
								}
								out, diagnostic, err := runSlackBinary(t, binary, path, server.URL, "", args...)
								code := 0
								if tc.invalid {
									code = 4
								}
								if exitCode(err) != code {
									t.Fatalf("recipient exit=%v want=%d stdout=%s stderr=%s", err, code, out, diagnostic)
								}
								if tc.invalid || tc.preview {
									if calls.Load() != 0 {
										t.Error("invalid or preview recipient accessed Slack")
									}
								} else if sends.Load() != 1 || (tc.users != "" && opens.Load() != 1) || (tc.users == "" && opens.Load() != 0) || (tc.email != "" && emails.Load() != 1) || (tc.email == "" && emails.Load() != 0) {
									t.Error("recipient resolution omitted or replayed a lookup/open/send")
								}
								if mode == "human" {
									if (code == 0 && (out == "" || diagnostic != "")) || (code != 0 && (out != "" || diagnostic == "")) {
										t.Error("recipient human streams differ")
									}
								} else {
									if diagnostic != "" {
										t.Error("recipient machine diagnostic escaped stdout")
									}
									var payload map[string]any
									if err := json.Unmarshal([]byte(out), &payload); err != nil {
										t.Fatal(err)
									}
									if mode == "json" || code != 0 {
										if payload["ok"] != (code == 0) || payload["meta"].(map[string]any)["command"] != "message.send" {
											t.Error("recipient envelope lost command or success state")
										}
										if code != 0 && (payload["data"] != nil || payload["errors"].([]any)[0].(map[string]any)["type"] != "validation_error") {
											t.Error("invalid recipient lost structured validation")
										}
										if code == 0 {
											payload = payload["data"].(map[string]any)
										}
									}
									if code == 0 {
										wantTarget := target
										if tc.preview && tc.users != "" {
											wantTarget = tc.users
										}
										gotTarget := payload["channel"]
										if !scheduled {
											gotTarget = payload["message"].(map[string]any)["channel"]
										}
										if gotTarget != wantTarget {
											t.Error("recipient receipt lost normalized target")
										}
									}
								}
								if strings.Contains(out+diagnostic, "xoxb-test") {
									t.Error("recipient output leaked credentials")
								}
								after, err := os.ReadFile(path)
								if err != nil || string(after) != config {
									t.Error("recipient flags changed saved settings")
								}
							})
						}
					})
				}
			}
		}
	}
}
