//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBinaryLookupFailuresCannotBecomeEmptySuccessOrTriggerLaterSteps(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, route := range []struct {
		name, method, field, value, scope string
		args                              []string
		list                              bool
	}{
		{"channel", "conversations.info", "channel", `{"id":"C1","name":"fixture"}`, "channels:read", []string{"lookup", "channel", "--channel=C1"}, false},
		{"channels", "conversations.list", "channels", `[{"id":"C1","name":"fixture"}]`, "channels:read", []string{"lookup", "channel", "--max-items=2", "--cursor=page-in"}, true},
		{"user", "users.info", "user", `{"id":"U1","name":"fixture"}`, "users:read", []string{"lookup", "user", "--user=U1"}, false},
		{"users", "users.list", "members", `[{"id":"U1","name":"fixture"},{"id":"W1","name":"retired","deleted":true}]`, "users:read", []string{"lookup", "user", "--include-deleted", "--max-items=2", "--cursor=page-in"}, true},
		{"presence", "users.getPresence", "presence", `"away"`, "users:read", []string{"lookup", "user", "--user=U1", "--presence"}, false},
		{"email", "users.lookupByEmail", "user", `{"id":"U1","name":"fixture"}`, "users:read.email", []string{"message", "send", "--user=person@example.test", "--message=Review complete", "--no-attribution"}, false},
	} {
		valid := `{"ok":true,"` + route.field + `":` + route.value + `,"response_metadata":{"next_cursor":"page-next"}}`
		empty := `{}`
		if route.list {
			empty = `[{}]`
		}
		if route.field == "presence" {
			empty = `""`
		}
		for _, tc := range []struct {
			name, body string
			exit       int
		}{
			{"missing acknowledgement", `{"` + route.field + `":` + route.value + `}`, 5},
			{"missing data", `{"ok":true}`, 5},
			{"null data", `{"ok":true,"` + route.field + `":null}`, 5},
			{"empty identity", `{"ok":true,"` + route.field + `":` + empty + `}`, 5},
			{"conflicting acknowledgement", `{"ok":true,"error":"no_permission","` + route.field + `":` + route.value + `}`, 5},
			{"permission", `{"ok":false,"error":"missing_scope","needed":"` + route.scope + `","provided":"synthetic-private-data"}`, 1},
			{"malformed", `{"synthetic-private-data":`, 5},
		} {
			for _, mode := range []string{"json", "compact", "human"} {
				t.Run(route.name+"/"+tc.name+"/"+mode, func(t *testing.T) {
					const token = "xoxp-synthetic-lookup-binary"
					var mu sync.Mutex
					recoverNow := false
					targets, later := 0, 0
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if err := r.ParseForm(); err != nil {
							t.Error(err)
						}
						if r.URL.Path == "/api/chat.getPermalink" {
							if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer "+token {
								t.Error("wrong permalink authentication")
							}
						} else if r.Method != http.MethodPost || r.PostForm.Get("token") != token || r.URL.RawQuery != "" {
							t.Error("wrong lookup/RPC authentication or method")
						}
						if r.URL.Path == "/api/"+route.method {
							mu.Lock()
							targets++
							good := recoverNow
							mu.Unlock()
							switch route.method {
							case "conversations.info":
								if r.PostForm.Get("channel") != "C1" || r.PostForm.Get("include_num_members") != "true" {
									t.Error("channel lookup dropped request options")
								}
							case "conversations.list":
								if r.PostForm.Get("limit") != "2" || r.PostForm.Get("cursor") != "page-in" || r.PostForm.Get("types") != "public_channel,private_channel" {
									t.Error("channel list lost pagination/types")
								}
							case "users.info", "users.getPresence":
								if r.PostForm.Get("user") != "U1" {
									t.Error("wrong user target")
								}
							case "users.list":
								if r.PostForm.Get("limit") != "2" || r.PostForm.Get("cursor") != "page-in" || r.PostForm.Has("presence") {
									t.Error("user list lost pagination or sent retired presence option")
								}
							case "users.lookupByEmail":
								if r.PostForm.Get("email") != "person@example.test" {
									t.Error("wrong lookup email")
								}
							}
							if good {
								writeJSON(w, valid)
							} else {
								writeJSON(w, tc.body)
							}
							return
						}
						switch r.URL.Path {
						case "/api/auth.test":
							w.Header().Set("X-OAuth-Scopes", "users:read,users:read.email,channels:read,groups:read,chat:write,im:write")
							writeJSON(w, `{"ok":true,"user_id":"U1"}`)
						case "/api/users.info":
							writeJSON(w, `{"ok":true,"user":{"id":"U1","name":"fixture"}}`)
						case "/api/conversations.open":
							mu.Lock()
							later++
							mu.Unlock()
							writeJSON(w, `{"ok":true,"channel":{"id":"D1","is_im":true,"user":"U1"}}`)
						case "/api/conversations.info":
							writeJSON(w, `{"ok":true,"channel":{"id":"D1","is_im":true,"user":"U1"}}`)
						case "/api/chat.postMessage":
							mu.Lock()
							later++
							mu.Unlock()
							writeJSON(w, `{"ok":true,"channel":"D1","ts":"1746284582.001000","message":{"text":"Review complete","ts":"1746284582.001000"}}`)
						case "/api/chat.getPermalink":
							writeJSON(w, `{"ok":true,"permalink":"https://example.slack.com/archives/D1/p1746284582001000"}`)
						default:
							t.Errorf("unexpected API step: %s", r.URL.Path)
							w.WriteHeader(500)
						}
					}))
					defer server.Close()
					home := t.TempDir()
					cfgPath := filepath.Join(home, "config.toml")
					cfg := "schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"T123\"\ntoken_type=\"user\"\ntoken=\"env:SLICK_LOOKUP_FIXTURE_TOKEN\"\n[workspaces.fixture.aliases]\nkeep=\"CKEEP\"\n"
					if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
						t.Fatal(err)
					}
					run := func() (string, string, error) {
						ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
						defer cancel()
						args := append(append([]string{}, route.args...), "--output="+mode, "--color=never", "--no-throttle", "--timeout=3s")
						cmd := exec.CommandContext(ctx, binary, args...)
						cmd.Env = []string{"HOME=" + home, "XDG_CACHE_HOME=" + filepath.Join(home, "cache"), "SLICK_CONFIG=" + cfgPath, "SLACK_CLI_BASE_URL=" + server.URL, "SLICK_LOOKUP_FIXTURE_TOKEN=" + token}
						var out, diagnostic bytes.Buffer
						cmd.Stdout, cmd.Stderr = &out, &diagnostic
						err := cmd.Run()
						if ctx.Err() != nil {
							t.Fatal("lookup exceeded fixture timeout")
						}
						if strings.Contains(out.String()+diagnostic.String(), token) || strings.Contains(out.String()+diagnostic.String(), "synthetic-private-data") {
							t.Fatal("lookup diagnostics exposed credentials or opaque response data")
						}
						return out.String(), diagnostic.String(), err
					}
					out, diagnostic, err := run()
					if exitCode(err) != tc.exit {
						t.Fatalf("lookup failure exited incorrectly: %v stdout=%s stderr=%s", err, out, diagnostic)
					}
					kind := "server_error"
					if tc.exit == 1 {
						kind = "auth_failure"
					}
					if mode == "human" {
						if out != "" || !strings.Contains(diagnostic, "type="+kind) {
							t.Fatal("human lookup failure lost class/stream")
						}
					} else {
						var frame map[string]json.RawMessage
						if err := json.Unmarshal([]byte(out), &frame); err != nil || len(frame) != 4 || string(frame["ok"]) != "false" || string(frame["data"]) != "null" || diagnostic != "" {
							t.Fatal("lookup failure lost machine contract")
						}
						var failures []struct {
							Type    string
							Exit    int `json:"exit_code"`
							Details struct {
								Needed []string `json:"needed"`
							} `json:"details"`
						}
						if err := json.Unmarshal(frame["errors"], &failures); err != nil || len(failures) != 1 || failures[0].Type != kind || failures[0].Exit != tc.exit {
							t.Fatal("lookup failure lost error class")
						}
						if tc.exit == 1 && (len(failures[0].Details.Needed) != 1 || failures[0].Details.Needed[0] != route.scope) {
							t.Fatal("lookup lost structured missing scopes")
						}
					}
					mu.Lock()
					gotTargets, gotLater := targets, later
					recoverNow = true
					mu.Unlock()
					if gotTargets != 1 || gotLater != 0 {
						t.Fatal("lookup replayed or proceeded to a dependent mutation")
					}
					out, diagnostic, err = run()
					if err != nil || out == "" || diagnostic != "" {
						t.Fatalf("lookup recovery failed: %v stdout=%s stderr=%s", err, out, diagnostic)
					}
					mu.Lock()
					gotTargets, gotLater = targets, later
					mu.Unlock()
					wantLater := 0
					if route.name == "email" {
						wantLater = 2
					}
					if gotTargets != 2 || gotLater != wantLater {
						t.Fatal("explicit recovery replayed lookup or dependent steps")
					}
					after, err := os.ReadFile(cfgPath)
					if err != nil || string(after) != cfg {
						t.Fatal("lookup failure/recovery changed saved configuration")
					}
				})
			}
		}
	}
}
