//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
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

func TestBinaryResultPipeFailuresPreserveAcknowledgedMutationsWithoutReplay(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tc := range []struct {
		name string
		args []string
		want map[string]int
	}{
		{"message", []string{"message", "send", "--channel=C123", "--file=-", "--no-attribution"}, map[string]int{"/api/chat.postMessage": 1}},
		{"reaction", []string{"react", "add", "--channel=C123", "--timestamp=1746284582.123456", "--emoji=eyes"}, map[string]int{"/api/reactions.add": 1}},
		{"upload", []string{"file", "upload", "--channel=C123", "--file=-", "--filename=artifact.txt", "--no-attribution"}, map[string]int{"/api/files.getUploadURLExternal": 1, "/upload": 1, "/api/files.completeUploadExternal": 1}},
		{"status", []string{"status", "clear"}, map[string]int{"/api/users.profile.set": 1}},
		{"empty history", []string{"history", "list", "--channel=C123"}, map[string]int{}},
		{"empty scheduled", []string{"message", "scheduled", "list", "--channel=C123"}, map[string]int{}},
		{"local path", []string{"config", "path"}, map[string]int{}},
		{"manifest JSON", []string{"manifest", "template", "--format=json"}, map[string]int{}},
		{"manifest YAML", []string{"manifest", "template", "--format=yaml"}, map[string]int{}},
		{"agent guide", []string{"agent", "guide", "send-msg"}, map[string]int{}},
		{"agent schema", []string{"agent", "schema"}, map[string]int{}},
		{"scheduled cancellation", []string{"message", "scheduled", "delete", "--channel=C123", "--scheduled-id=Q123"}, map[string]int{"/api/chat.deleteScheduledMessage": 1}},
	} {
		for _, mode := range []string{"human", "json", "compact"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				const token = "xoxp-synthetic-result-pipe"
				var mu sync.Mutex
				calls := map[string]int{}
				var accepted bool
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/api/chat.getPermalink" {
						if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer "+token || r.URL.Query().Get("channel") != "C123" || r.URL.Query().Get("message_ts") != "1746284582.123456" || r.URL.Query().Has("token") {
							t.Error("wrong permalink method, authentication or parameters")
						}
					} else if r.Method != http.MethodPost {
						t.Error("wrong request method")
					}
					if r.URL.Path != "/upload" && r.URL.Path != "/api/chat.getPermalink" {
						if err := r.ParseForm(); err != nil {
							t.Error(err)
						}
						if r.PostForm.Get("token") != token {
							t.Error("missing synthetic authentication")
						}
					} else if r.URL.Path == "/upload" {
						if r.Header.Get("Authorization") != "Bearer "+token {
							t.Error("missing upload authentication")
						}
						if _, err := io.Copy(io.Discard, r.Body); err != nil {
							t.Error(err)
						}
					}
					switch r.URL.Path {
					case "/api/auth.test":
						w.Header().Set("X-OAuth-Scopes", "chat:write,reactions:write,files:write,users.profile:write,channels:history")
						writeJSON(w, `{"ok":true,"user_id":"U1"}`)
					case "/api/conversations.info":
						writeJSON(w, `{"ok":true,"channel":{"id":"C123","name":"fixture"}}`)
					case "/api/chat.getPermalink":
						writeJSON(w, `{"ok":true,"permalink":"https://example.slack.com/archives/C123/p1746284582123456"}`)
					case "/api/files.info":
						writeJSON(w, `{"ok":false,"error":"missing_scope","needed":"files:read"}`)
					case "/api/conversations.history":
						writeJSON(w, `{"ok":true,"messages":[],"has_more":false}`)
					case "/api/chat.scheduledMessages.list":
						writeJSON(w, `{"ok":true,"scheduled_messages":[],"response_metadata":{"next_cursor":""}}`)
					case "/api/chat.postMessage", "/api/reactions.add", "/api/users.profile.set", "/api/files.getUploadURLExternal", "/upload", "/api/files.completeUploadExternal", "/api/chat.deleteScheduledMessage":
						mu.Lock()
						calls[r.URL.Path]++
						if r.URL.Path != "/api/files.getUploadURLExternal" && r.URL.Path != "/upload" {
							accepted = true
						}
						mu.Unlock()
						switch r.URL.Path {
						case "/api/chat.postMessage":
							writeJSON(w, `{"ok":true,"channel":"C123","ts":"1746284582.123456","message":{"text":"Review complete","ts":"1746284582.123456"}}`)
						case "/api/files.getUploadURLExternal":
							writeJSON(w, `{"ok":true,"file_id":"F1","upload_url":"http://`+r.Host+`/upload"}`)
						case "/api/files.completeUploadExternal":
							writeJSON(w, `{"ok":true,"files":[{"id":"F1"}]}`)
						default:
							writeJSON(w, `{"ok":true}`)
						}
					default:
						t.Errorf("unexpected request or cleanup: %s", r.URL.Path)
						w.WriteHeader(http.StatusInternalServerError)
					}
				}))
				defer server.Close()
				home := t.TempDir()
				cfgPath := filepath.Join(home, "config.toml")
				cfg := "schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"T123\"\ntoken_type=\"user\"\ntoken=\"env:SLICK_RESULT_FIXTURE_TOKEN\"\n"
				if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
					t.Fatal(err)
				}
				reader, writer, err := os.Pipe()
				if err != nil {
					t.Fatal(err)
				}
				if err := reader.Close(); err != nil {
					t.Fatal(err)
				}
				defer func() { _ = writer.Close() }()
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				args := append(append([]string{}, tc.args...), "--output="+mode, "--no-throttle", "--color=never", "--timeout=3s")
				cmd := exec.CommandContext(ctx, binary, args...)
				env := []string{"HOME=" + home, "XDG_CACHE_HOME=" + filepath.Join(home, "cache"), "SLICK_CONFIG=" + cfgPath, "SLACK_CLI_BASE_URL=" + server.URL, "SLICK_RESULT_FIXTURE_TOKEN=" + token}
				cmd.Env = env
				cmd.Stdin = strings.NewReader("Review complete")
				cmd.Stdout = writer
				var diagnostic bytes.Buffer
				cmd.Stderr = &diagnostic
				err = cmd.Run()
				if ctx.Err() != nil {
					t.Fatal("pipe failure exceeded fixture timeout")
				}
				if exitCode(err) != 8 || strings.Count(diagnostic.String(), "output_write_failed") != 1 {
					t.Fatalf("pipe failure did not report exit 8 once: %v %s", err, &diagnostic)
				}
				if strings.Contains(diagnostic.String(), token) {
					t.Fatal("diagnostic leaked authentication")
				}
				mu.Lock()
				if len(calls) != len(tc.want) || (len(tc.want) > 0 && !accepted) {
					t.Error("mutation was not acknowledged before result failed")
				}
				for path, want := range tc.want {
					if calls[path] != want {
						t.Error("result failure replayed or undid mutation")
					}
				}
				mu.Unlock()
				recovered := exec.CommandContext(ctx, binary, "config", "path", "--output="+mode, "--color=never")
				recovered.Env = env
				var out, errout bytes.Buffer
				recovered.Stdout = &out
				recovered.Stderr = &errout
				if err := recovered.Run(); err != nil || out.Len() == 0 || errout.Len() != 0 {
					t.Fatal("fresh output destination did not recover")
				}
				after, err := os.ReadFile(cfgPath)
				if err != nil || string(after) != cfg {
					t.Fatal("result failure changed config")
				}
			})
		}
	}
}

func TestBinaryClosedErrorPipeUsesExit8OnlyWhenItIsTheResultDestination(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, mode := range []string{"human", "json", "compact"} {
		t.Run(mode, func(t *testing.T) {
			home := t.TempDir()
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := reader.Close(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = writer.Close() }()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binary, "react", "add", "--output="+mode, "--color=never")
			cmd.Env = []string{"HOME=" + home, "SLICK_CONFIG=" + filepath.Join(home, "missing.toml")}
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = writer
			err = cmd.Run()
			if ctx.Err() != nil {
				t.Fatal("closed error pipe exceeded fixture timeout")
			}
			want := 4
			if mode == "human" {
				want = 8
			}
			if exitCode(err) != want {
				t.Fatalf("wrong failed error-output exit: %v", err)
			}
			if mode == "human" {
				if out.Len() != 0 {
					t.Fatal("human error spilled onto stdout")
				}
			} else {
				var frame struct {
					OK     bool `json:"ok"`
					Errors []struct {
						Exit int `json:"exit_code"`
					} `json:"errors"`
				}
				if err := json.Unmarshal(out.Bytes(), &frame); err != nil || frame.OK || len(frame.Errors) != 1 || frame.Errors[0].Exit != 4 {
					t.Fatal("closed diagnostic pipe changed machine failure result")
				}
			}
		})
	}
}
