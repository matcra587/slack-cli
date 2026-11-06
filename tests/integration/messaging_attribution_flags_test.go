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

func TestBinaryMessagingAttributionOverridesAndRemoval(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, operation := range []string{"send", "edit", "reply", "schedule"} {
		for _, kind := range []string{"bot", "user"} {
			for _, mode := range []string{"json", "compact", "human"} {
				// Both profile kinds retain every attribution precedence/removal row.
				// Presentation sentinels use the bot profile for each operation.
				if mode != "json" && kind != "bot" {
					continue
				}
				t.Run(operation+"/"+kind+"/"+mode, func(t *testing.T) {
					path := filepath.Join(t.TempDir(), "config.toml")
					var calls, mutations atomic.Int32
					var activeTest atomic.Pointer[testing.T]
					var wantContext atomic.Pointer[string]
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						t := activeTest.Load()
						calls.Add(1)
						if r.URL.Path == "/api/chat.getPermalink" {
							if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer xoxb-test" {
								t.Error("attribution changed permalink authentication")
							}
							writeJSON(w, `{"ok":false,"error":"missing_scope"}`)
							return
						}
						if err := r.ParseForm(); err != nil {
							t.Error(err)
						}
						if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.PostForm.Get("token") != "xoxb-test" {
							t.Error("attribution changed POST form authentication")
						}
						switch r.URL.Path {
						case "/api/auth.test":
							w.Header().Set("X-OAuth-Scopes", "chat:write")
							writeJSON(w, `{"ok":true,"user_id":"U_SELF"}`)
						case "/api/conversations.info":
							writeJSON(w, `{"ok":false,"error":"missing_scope"}`)
						case "/api/chat.postMessage", "/api/chat.update", "/api/chat.scheduleMessage":
							mutations.Add(1)
							method := "/api/chat.postMessage"
							switch operation {
							case "edit":
								method = "/api/chat.update"
							case "schedule":
								method = "/api/chat.scheduleMessage"
							}
							if r.URL.Path != method || r.PostForm.Get("channel") != "C_TARGET" || r.PostForm.Get("text") != "Review ready" || r.PostForm.Get("attachments") != "" {
								t.Error("attribution changed message method, target or primary content")
							}
							var blocks []map[string]any
							if err := json.Unmarshal([]byte(r.PostForm.Get("blocks")), &blocks); err != nil {
								t.Error("attribution request lost its Block Kit body")
							}
							contexts := []string{}
							for _, b := range blocks {
								if b["type"] != "context" {
									continue
								}
								for _, e := range b["elements"].([]any) {
									contexts = append(contexts, e.(map[string]any)["text"].(string))
								}
							}
							want := *wantContext.Load()
							if (want == "" && len(contexts) != 0) || (want != "" && (len(contexts) != 1 || contexts[0] != want)) {
								t.Errorf("attribution context = %q, want %q", contexts, want)
							}
							switch operation {
							case "schedule":
								if r.PostForm.Get("post_at") == "" {
									t.Error("attribution lost scheduled time")
								}
								writeJSON(w, `{"ok":true,"channel":"C_TARGET","scheduled_message_id":"Q_FIXTURE"}`)
							case "edit":
								writeJSON(w, `{"ok":true,"channel":"C_TARGET","ts":"1746284582.000001","text":"Review ready"}`)
							default:
								writeJSON(w, `{"ok":true,"channel":"C_TARGET","ts":"1746284582.000002","message":{"type":"message","text":"Review ready","ts":"1746284582.000002"}}`)
							}
						default:
							t.Errorf("unexpected attribution request %s", r.URL.Path)
							w.WriteHeader(http.StatusInternalServerError)
						}
					}))
					defer server.Close()
					for _, tc := range []struct {
						name, context string
						flags         []string
						disabled      bool
						code          int
					}{
						{name: "profile defaults", context: ":satellite: _Sent via slick (Profile helper)_"},
						{name: "label and emoji", flags: []string{"--attribution", "--attribution-label", "Release helper", "--attribution-emoji", ":rocket:"}, context: ":rocket: _Sent via slick (Release helper)_"},
						{name: "message overrides label", flags: []string{"--attribution-label", "Release helper", "--attribution-message", "Ready for review", "--attribution-emoji", ":pencil:"}, context: ":pencil: _Ready for review_"},
						{name: "force off", flags: []string{"--no-attribution"}},
						{name: "remove overrides", context: ":satellite: _Sent via slick (Profile helper)_"},
						{name: "presentation does not enable", disabled: true, flags: []string{"--attribution-label", "Release helper", "--attribution-message", "Ready for review", "--attribution-emoji", ":rocket:"}},
						{name: "force on overrides profile opt out", disabled: true, flags: []string{"--attribution"}, context: ":satellite: _Sent via slick (Profile helper)_"},
						{name: "contradictory switches", flags: []string{"--attribution", "--no-attribution"}, code: 4},
						{name: "value form rejected", flags: []string{"--attribution=false"}, code: 4},
						{name: "recovery after invalid switches", context: ":satellite: _Sent via slick (Profile helper)_"},
					} {
						if mode != "json" {
							// Cover enabled/disabled receipts, presentation-only overrides and
							// validation streams without repeating every context string.
							switch tc.name {
							case "profile defaults", "force off", "presentation does not enable", "contradictory switches":
							default:
								continue
							}
						}
						t.Run(tc.name, func(t *testing.T) {
							activeTest.Store(t)
							wantContext.Store(&tc.context)
							config := fmt.Sprintf("schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"T123\"\ntoken_type=%q\ntoken_ref=\"env:SLACK_TEST_TOKEN\"\ndefault_channel=\"C_UNRELATED\"\n[workspaces.fixture.attribution]\nenabled=%t\nlabel=\"Profile helper\"\nemoji=\":satellite:\"\n", kind, !tc.disabled)
							if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
								t.Fatal(err)
							}
							calls.Store(0)
							mutations.Store(0)
							args := []string{"message", "send"}
							command := "message.send"
							switch operation {
							case "edit":
								args = []string{"message", "edit", "--timestamp", "1746284582.000001"}
								command = "message.edit"
							case "reply":
								args = []string{"reply", "--parent", "1746284582.000001"}
								command = "reply"
							case "schedule":
								args = append(args, "--schedule", "1h")
							}
							args = append(args, "--channel", "C_TARGET", "--message", "Review ready", "--output="+mode, "--no-throttle", "--color=never")
							args = append(args, tc.flags...)
							out, diagnostic, err := runSlackBinaryWithoutAgentEnv(t, binary, path, server.URL, args...)
							if exitCode(err) != tc.code {
								t.Fatalf("attribution exit=%v stdout=%s stderr=%s", err, out, diagnostic)
							}
							wantMutations := int32(1)
							if tc.code != 0 {
								wantMutations = 0
								if calls.Load() != 0 {
									t.Error("invalid attribution switches accessed Slack")
								}
							}
							if mutations.Load() != wantMutations {
								t.Error("attribution omitted or replayed a mutation")
							}
							if mode == "human" {
								if (tc.code == 0 && (out == "" || diagnostic != "")) || (tc.code != 0 && (out != "" || diagnostic == "")) {
									t.Error("attribution human streams differ")
								}
							} else {
								if diagnostic != "" {
									t.Error("attribution machine diagnostics escaped stdout")
								}
								var payload map[string]any
								if err := json.Unmarshal([]byte(out), &payload); err != nil {
									t.Fatal(err)
								}
								if mode == "json" || tc.code != 0 {
									if payload["ok"] != (tc.code == 0) || payload["meta"].(map[string]any)["command"] != command {
										t.Error("attribution envelope lost canonical command or success state")
									}
									if tc.code != 0 {
										if payload["data"] != nil || payload["errors"].([]any)[0].(map[string]any)["type"] != "validation_error" {
											t.Error("invalid attribution switches lost structured failure")
										}
									} else {
										payload = payload["data"].(map[string]any)
									}
								}
								if tc.code == 0 {
									enabled := payload["attribution"]
									switch operation {
									case "schedule":
										enabled = enabled.(map[string]any)["enabled"]
									}
									if enabled != (tc.context != "") {
										t.Error("receipt does not report actual attribution")
									}
								}
							}
							if strings.Contains(out+diagnostic, "xoxb-test") {
								t.Error("attribution output leaked credentials")
							}
							after, err := os.ReadFile(path)
							if err != nil || string(after) != config {
								t.Error("attribution flags changed saved settings")
							}
						})
					}
				})
			}
		}
	}
}
