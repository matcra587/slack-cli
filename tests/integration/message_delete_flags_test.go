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

func TestBinaryMessageDeleteTargetFlags(t *testing.T) {
	binary := buildSlackBinary(t)
	const timestamp = "1746284582.000001"
	for _, tokenType := range []string{"bot", "user"} {
		for _, mode := range []string{"json", "compact", "human"} {
			for _, short := range []bool{false, true} {
				for _, tc := range []struct {
					name      string
					flags     []string
					code      int
					mutations int
					dry       bool
				}{
					{"preview", []string{"--dry-run"}, 0, 0, true},
					{"confirmed", []string{"--force"}, 0, 1, false},
					{"short confirmation", []string{"-F"}, 0, 1, false},
					{"removed confirmation", nil, 4, 0, false},
					{"false confirmation", []string{"--force=false"}, 4, 0, false},
					{"preview overrides confirmation", []string{"--force", "-n"}, 0, 0, true},
					{"removed preview", []string{"--dry-run=false", "--force"}, 0, 1, false},
					{"empty channel ignores default", []string{"--channel=", "--force"}, 4, 0, false},
					{"blank timestamp", []string{"--timestamp= ", "--force"}, 4, 0, false},
				} {
					t.Run(fmt.Sprintf("%s/%s/short=%t/%s", tokenType, mode, short, tc.name), func(t *testing.T) {
						path := filepath.Join(t.TempDir(), "config.toml")
						config := fmt.Sprintf("schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"T123\"\ntoken_type=%q\ntoken_ref=\"env:SLACK_TEST_TOKEN\"\ndefault_channel=\"C_OTHER\"\n[workspaces.fixture.aliases]\nrelease=\"C_DELETE\"\n", tokenType)
						if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
							t.Fatal(err)
						}
						var calls, mutations atomic.Int32
						server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							calls.Add(1)
							if err := r.ParseForm(); err != nil {
								t.Error(err)
							}
							if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.PostForm.Get("token") != "xoxb-test" {
								t.Error("wrong method, encoding, or synthetic authentication")
							}
							switch r.URL.Path {
							case "/api/auth.test":
								w.Header().Set("X-OAuth-Scopes", "chat:write")
								writeJSON(w, `{"ok":true,"user_id":"U1"}`)
							case "/api/chat.delete":
								mutations.Add(1)
								if r.PostForm.Get("channel") != "C_DELETE" || r.PostForm.Get("ts") != timestamp {
									t.Errorf("wrong exact deletion target: channel=%q ts=%q", r.PostForm.Get("channel"), r.PostForm.Get("ts"))
								}
								writeJSON(w, `{"ok":true,"channel":"C_DELETE","ts":"`+timestamp+`"}`)
							case "/api/conversations.info":
								writeJSON(w, `{"ok":false,"error":"missing_scope"}`)
							default:
								t.Errorf("unexpected request: %s", r.URL.Path)
								w.WriteHeader(500)
							}
						}))
						defer server.Close()
						args := []string{"message", "delete", "--channel", " release ", "--timestamp", " " + timestamp + " "}
						if short {
							args = []string{"message", "delete", "-c", " release ", "-t", " " + timestamp + " "}
						}
						args = append(args, tc.flags...)
						args = append(args, "--output="+mode, "--color=never", "--no-throttle")
						out, diagnostic, err := runSlackBinary(t, binary, path, server.URL, "", args...)
						if exitCode(err) != tc.code {
							t.Fatalf("wrong exit: %v %s %s", err, out, diagnostic)
						}
						if int(mutations.Load()) != tc.mutations {
							t.Fatal("deletion omitted or retried")
						}
						if tc.mutations == 0 && calls.Load() != 0 {
							t.Fatal("preview or validation reached credentials/Slack")
						}
						if mode == "human" {
							if tc.code != 0 && (out != "" || diagnostic == "") {
								t.Fatal("wrong human failure streams")
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
										Command string
										Code    int `json:"exit_code"`
									}
								}
								if json.Unmarshal(raw, &frame) != nil || frame.OK != (tc.code == 0) || frame.Meta.Command != "message.delete" || frame.Meta.Code != tc.code {
									t.Fatal("invalid delete envelope")
								}
								raw = frame.Data
							}
							if tc.code == 0 {
								var data struct {
									Channel, TS string
									Dry         bool `json:"dry_run"`
								}
								if json.Unmarshal(raw, &data) != nil || data.Channel != "C_DELETE" || data.TS != timestamp || data.Dry != tc.dry {
									t.Fatalf("deletion target/result lost: %s", raw)
								}
							}
						}
						if strings.Contains(out+diagnostic, "xoxb-test") {
							t.Fatal("credential leaked")
						}
						after, err := os.ReadFile(path)
						if err != nil || string(after) != config {
							t.Fatal("delete changed configuration")
						}
					})
				}
			}
		}
	}
}
