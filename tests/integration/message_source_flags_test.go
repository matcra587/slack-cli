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
	"sync"
	"sync/atomic"
	"testing"
)

func TestBinaryMessageSourcesKeepRawBlocksOutOfNotificationText(t *testing.T) {
	binary := buildSlackBinary(t)
	const private = "synthetic-unreviewed-block-field"
	const rawBody = `[{"type":"section","text":{"type":"mrkdwn","text":"Review ready <@U_REVIEWER>"},"unreviewed_secret":"` + private + `"}]`
	const plainBody = "Review ready\nline two"
	for _, operation := range []string{"send", "edit", "reply", "schedule"} {
		for _, mode := range []string{"json", "compact", "human"} {
			for _, short := range []bool{false, true} {
				// Both spellings retain the entire source lifecycle in JSON. Presentation
				// sentinels below use long flags; source parsing does not depend on mode.
				if mode != "json" && short {
					continue
				}
				t.Run(fmt.Sprintf("%s/%s/short=%t", operation, mode, short), func(t *testing.T) {
					type inputCase struct {
						name, body, source                   string
						raw, rawFalse, preview, denied, both bool
						code                                 int
					}
					cases := []inputCase{
						{name: "raw argument", body: rawBody, raw: true},
						{name: "raw stdin", body: rawBody, raw: true, source: "stdin"},
						{name: "raw file with spaces", body: rawBody, raw: true, source: "file"},
						{name: "remove raw mode", body: plainBody},
						{name: "explicit false raw mode", body: plainBody, rawFalse: true, source: "file"},
						{name: "raw preview", body: rawBody, raw: true, preview: true},
						{name: "conflicting sources", body: plainBody, source: "file", both: true, code: 4},
						{name: "missing file", source: "missing", code: 4},
						{name: "malformed raw source", body: "{", raw: true, code: 4},
						{name: "empty raw array", body: "[]", raw: true},
						{name: "permission failure preserves prior state", body: rawBody, raw: true, denied: true, code: 1},
						{name: "explicit recovery", body: rawBody, raw: true, source: "stdin"},
						{name: "remove file source", body: plainBody},
						{name: "missing source", source: "absent", code: 4},
						{name: "whitespace argument", body: " \t\n ", code: 4},
						{name: "empty file", source: "file", code: 4},
						{name: "whitespace stdin", body: " \n\t", source: "stdin", code: 4},
					}
					if operation != "edit" {
						cases[9].code = 4
					}
					path := filepath.Join(t.TempDir(), "config.toml")
					config := "schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"T123\"\ntoken_type=\"user\"\ntoken_ref=\"env:SLACK_TEST_TOKEN\"\n"
					if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
						t.Fatal(err)
					}
					var mu sync.Mutex
					storedText, storedBlocks := "Prior fallback", "[]"
					var requests, mutations atomic.Int32
					var activeTest atomic.Pointer[testing.T]
					var activeInput atomic.Pointer[inputCase]
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						t, tc := activeTest.Load(), activeInput.Load()
						requests.Add(1)
						if r.URL.Path == "/api/chat.getPermalink" {
							if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer xoxb-test" {
								t.Error("message source changed permalink authentication")
							}
							writeJSON(w, `{"ok":false,"error":"missing_scope"}`)
							return
						}
						if err := r.ParseForm(); err != nil {
							t.Error(err)
						}
						if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.PostForm.Get("token") != "xoxb-test" {
							t.Error("message source changed POST form authentication")
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
							if r.URL.Path != method || r.PostForm.Get("channel") != "C_TARGET" || (operation == "edit" && r.PostForm.Get("ts") != "1746284582.000001") || (operation == "reply" && r.PostForm.Get("thread_ts") != "1746284582.000001") {
								t.Error("message source changed exact method or target")
							}
							for _, field := range []string{"metadata", "attachments", "filename"} {
								if r.PostForm.Has(field) {
									t.Error("message source overwrote unrelated message fields")
								}
							}
							if tc.raw {
								if r.PostForm.Has("text") {
									t.Error("raw Block Kit source was copied into notification text")
								}
								var decoded []map[string]any
								if err := json.Unmarshal([]byte(r.PostForm.Get("blocks")), &decoded); err != nil {
									t.Error("raw Block Kit request is not a JSON array")
								} else if tc.body == "[]" {
									if len(decoded) != 0 {
										t.Error("empty raw edit lost clear intent")
									}
								} else if len(decoded) != 1 || decoded[0]["text"].(map[string]any)["text"] != "Review ready <@U_REVIEWER>" {
									t.Error("raw Block Kit lost reviewed text or deliberate mention")
								}
								if strings.Contains(r.PostForm.Get("blocks"), private) {
									t.Error("raw Block Kit retained unreviewed fields")
								}
							} else if r.PostForm.Get("text") != plainBody || !strings.Contains(r.PostForm.Get("blocks"), "Review ready") {
								t.Error("Markdown source lost its readable fallback after raw/file removal")
							}
							if tc.denied {
								writeJSON(w, `{"ok":false,"error":"no_permission"}`)
								return
							}
							mu.Lock()
							defer mu.Unlock()
							if r.PostForm.Has("text") {
								storedText = r.PostForm.Get("text")
							}
							storedBlocks = r.PostForm.Get("blocks")
							switch operation {
							case "schedule":
								if r.PostForm.Get("post_at") == "" {
									t.Error("message source lost scheduled time")
								}
								writeJSON(w, `{"ok":true,"channel":"C_TARGET","scheduled_message_id":"Q_TARGET"}`)
							case "edit":
								_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "channel": "C_TARGET", "ts": "1746284582.000001", "text": storedText})
							default:
								_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "channel": "C_TARGET", "ts": "1746284582.000002", "message": map[string]any{"type": "message", "text": storedText, "ts": "1746284582.000002", "blocks": json.RawMessage(storedBlocks)}})
							}
						default:
							t.Errorf("unexpected message source path %s", r.URL.Path)
							w.WriteHeader(http.StatusInternalServerError)
						}
					}))
					defer server.Close()
					for _, tc := range cases {
						if mode != "json" {
							// Keep raw/Markdown receipts, preview plans, clear/reject behavior,
							// validation and permission failures in each presentation mode.
							switch tc.name {
							case "raw argument", "remove raw mode", "raw preview", "empty raw array",
								"malformed raw source", "permission failure preserves prior state":
							default:
								continue
							}
						}
						t.Run(tc.name, func(t *testing.T) {
							activeTest.Store(t)
							activeInput.Store(&tc)
							requests.Store(0)
							mutations.Store(0)
							mu.Lock()
							priorText, priorBlocks := storedText, storedBlocks
							mu.Unlock()
							messageFlag, fileFlag, rawFlag := "--message", "--file", "--blocks"
							if short {
								messageFlag, fileFlag, rawFlag = "-m", "-f", "-b"
							}
							args, command := []string{"message", "send"}, "message.send"
							switch operation {
							case "edit":
								args, command = []string{"message", "edit", "--timestamp", "1746284582.000001"}, "message.edit"
							case "reply":
								args, command = []string{"reply", "--parent", "1746284582.000001"}, "reply"
							case "schedule":
								args = append(args, "--schedule", "1h")
							}
							args = append(args, "--channel", "C_TARGET", "--no-attribution", "--no-throttle", "--color=never", "--output="+mode)
							inputPath := filepath.Join(t.TempDir(), "message body.txt")
							if err := os.WriteFile(inputPath, []byte(tc.body), 0o600); err != nil {
								t.Fatal(err)
							}
							stdin := ""
							switch tc.source {
							case "absent":
								// Leave both source flags unset.
							case "stdin":
								args = append(args, fileFlag, "-")
								stdin = tc.body
							case "file":
								args = append(args, fileFlag, inputPath)
							case "missing":
								args = append(args, fileFlag, filepath.Join(t.TempDir(), "missing.txt"))
							default:
								args = append(args, messageFlag, tc.body)
							}
							if tc.both {
								args = append(args, messageFlag, tc.body)
							}
							if tc.raw {
								args = append(args, rawFlag)
							} else if tc.rawFalse {
								args = append(args, rawFlag+"=false")
							}
							if tc.preview {
								args = append(args, "--dry-run")
							}
							out, diagnostic, err := runSlackBinary(t, binary, path, server.URL, stdin, args...)
							if exitCode(err) != tc.code {
								t.Fatalf("message source exit=%v want=%d stdout=%s stderr=%s", err, tc.code, out, diagnostic)
							}
							wantMutations := int32(1)
							if tc.code == 4 || tc.preview {
								wantMutations = 0
								if requests.Load() != 0 {
									t.Error("invalid source or preview accessed Slack")
								}
							}
							if mutations.Load() != wantMutations {
								t.Error("message source omitted or replayed mutation")
							}
							if mode == "human" {
								if (tc.code == 0 && (out == "" || diagnostic != "")) || (tc.code != 0 && (out != "" || diagnostic == "")) {
									t.Error("message source changed human streams")
								}
							} else {
								if diagnostic != "" {
									t.Error("message source machine diagnostic escaped stdout")
								}
								var data map[string]any
								if err := json.Unmarshal([]byte(out), &data); err != nil {
									t.Fatal(err)
								}
								if mode == "json" || tc.code != 0 {
									if data["ok"] != (tc.code == 0) || data["meta"].(map[string]any)["command"] != command {
										t.Error("message source lost canonical envelope")
									}
									if tc.code == 0 {
										data = data["data"].(map[string]any)
									}
								}
								if tc.raw && tc.code == 0 && (tc.preview || operation == "schedule") {
									message := data
									if operation != "schedule" {
										message = data["message"].(map[string]any)
									}
									if text, exists := message["text"]; exists && text != "" {
										t.Error("raw receipt invented notification text from JSON source")
									}
									if _, exists := message["blocks"]; !exists {
										t.Error("raw receipt lost its reviewed Block Kit plan")
									}
									if operation == "schedule" && tc.preview && data["scheduled_message_id"] != "Q-dry-run" {
										t.Error("scheduled preview changed its established identifier")
									}
								}
							}
							if strings.Contains(out+diagnostic, private) || strings.Contains(out+diagnostic, "xoxb-test") {
								t.Error("message source exposed unreviewed fields or credentials")
							}
							mu.Lock()
							if (tc.code != 0 || tc.preview || tc.body == "[]") && storedText != priorText {
								t.Error("failed/preview/clear operation changed prior fallback text")
							}
							if (tc.code != 0 || tc.preview) && storedBlocks != priorBlocks {
								t.Error("failed operation changed prior blocks")
							}
							mu.Unlock()
							for f, want := range map[string]string{path: config, inputPath: tc.body} {
								after, err := os.ReadFile(f)
								if err != nil || string(after) != want {
									t.Error("message source changed config or input file")
								}
							}
						})
					}
				})
			}
		}
	}
}
