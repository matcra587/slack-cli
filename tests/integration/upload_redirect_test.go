//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBinaryUploadRedirectsStopWithoutReplayAndPermitExplicitRecovery(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, stage := range []string{"prepare", "transfer", "complete"} {
		for _, status := range []int{301, 302, 303, 307, 308} {
			for _, mode := range []string{"json", "compact", "human"} {
				t.Run(fmt.Sprintf("%s/%d/%s", stage, status, mode), func(t *testing.T) {
					const token = "xoxp-synthetic-upload-redirect"
					var targetCalls, prepares, transfers, completes atomic.Int32
					var recovery atomic.Bool
					target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						targetCalls.Add(1)
						writeJSON(w, `{"ok":true,"files":[{"id":"F1"}]}`)
					}))
					defer target.Close()
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						currentStage := ""
						switch r.URL.Path {
						case "/api/files.getUploadURLExternal":
							currentStage = "prepare"
							prepares.Add(1)
						case "/upload":
							currentStage = "transfer"
							transfers.Add(1)
						case "/api/files.completeUploadExternal":
							currentStage = "complete"
							completes.Add(1)
						}
						if r.Method != http.MethodPost {
							t.Error("upload changed request method")
						}
						if currentStage == "transfer" {
							if r.Header.Get("Authorization") != "Bearer "+token {
								t.Error("transfer authentication missing")
							}
						} else {
							if err := r.ParseForm(); err != nil {
								t.Error(err)
							}
							if r.PostForm.Get("token") != token {
								t.Error("RPC authentication missing")
							}
						}
						if currentStage == stage && !recovery.Load() {
							http.Redirect(w, r, target.URL+"/private-capability", status)
							return
						}
						switch r.URL.Path {
						case "/api/auth.test":
							w.Header().Set("X-OAuth-Scopes", "files:write")
							writeJSON(w, `{"ok":true,"user_id":"U1"}`)
						case "/api/conversations.info":
							writeJSON(w, `{"ok":true,"channel":{"id":"C123","name":"fixture"}}`)
						case "/api/files.getUploadURLExternal":
							writeJSON(w, `{"ok":true,"file_id":"F1","upload_url":"http://`+r.Host+`/upload"}`)
						case "/upload":
							w.WriteHeader(200)
						case "/api/files.completeUploadExternal":
							writeJSON(w, `{"ok":true,"files":[{"id":"F1"}]}`)
						case "/api/files.info":
							writeJSON(w, `{"ok":false,"error":"missing_scope","needed":"files:read"}`)
						default:
							t.Errorf("unexpected request or cleanup: %s", r.URL.Path)
							w.WriteHeader(500)
						}
					}))
					defer server.Close()
					home := t.TempDir()
					configPath, sourcePath := filepath.Join(home, "config.toml"), filepath.Join(home, "artifact.bin")
					cfg := "schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"T123\"\ntoken_type=\"user\"\ntoken=\"env:SLICK_REDIRECT_FIXTURE_TOKEN\"\n"
					for path, data := range map[string]string{configPath: cfg, sourcePath: "artifact"} {
						if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
							t.Fatal(err)
						}
					}
					run := func() (string, string, error) {
						ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
						defer cancel()
						cmd := exec.CommandContext(ctx, binary, "file", "upload", "--channel=C123", "--file="+sourcePath, "--no-attribution", "--no-throttle", "--output="+mode, "--color=never", "--timeout=3s", "--debug")
						cmd.Env = []string{"HOME=" + home, "SLICK_CONFIG=" + configPath, "SLACK_CLI_BASE_URL=" + server.URL, "SLICK_REDIRECT_FIXTURE_TOKEN=" + token}
						var out, diagnostic bytes.Buffer
						cmd.Stdout, cmd.Stderr = &out, &diagnostic
						err := cmd.Run()
						if ctx.Err() != nil {
							t.Fatal("redirect command exceeded fixture timeout")
						}
						for _, secret := range []string{token, "private-capability", target.URL} {
							if strings.Contains(out.String()+diagnostic.String(), secret) {
								t.Fatal("redirect exposed credentials or target URL")
							}
						}
						return out.String(), diagnostic.String(), err
					}
					out, diagnostic, err := run()
					if exitCode(err) != 5 {
						t.Fatalf("redirect should fail with exit 5: %v", err)
					}
					if mode == "human" {
						if out != "" || !strings.Contains(diagnostic, "stage="+stage) {
							t.Fatal("human redirect failure lost stream or progress")
						}
					} else {
						var frame map[string]json.RawMessage
						if err := json.Unmarshal([]byte(out), &frame); err != nil {
							t.Fatal(err)
						}
						var failures []struct {
							Exit    int `json:"exit_code"`
							Details struct {
								Stage  string `json:"stage"`
								FileID string `json:"file_id"`
							} `json:"details"`
						}
						if err := json.Unmarshal(frame["errors"], &failures); err != nil {
							t.Fatal(err)
						}
						wantID := "F1"
						if stage == "prepare" {
							wantID = ""
						}
						if len(frame) != 4 || string(frame["ok"]) != "false" || string(frame["data"]) != "null" || len(failures) != 1 || failures[0].Exit != 5 || failures[0].Details.Stage != stage || failures[0].Details.FileID != wantID {
							t.Fatal("redirect failure changed common envelope or trustworthy progress")
						}
					}
					want := [3]int32{1, 0, 0}
					if stage != "prepare" {
						want[1] = 1
					}
					if stage == "complete" {
						want[2] = 1
					}
					if targetCalls.Load() != 0 || [3]int32{prepares.Load(), transfers.Load(), completes.Load()} != want {
						t.Fatal("redirect followed target, replayed upload, or continued after failure")
					}
					recovery.Store(true)
					out, _, err = run()
					if err != nil || !strings.Contains(out, "F1") {
						t.Fatalf("explicit recovery failed: %v", err)
					}
					want[0]++
					want[1]++
					want[2]++
					if targetCalls.Load() != 0 || [3]int32{prepares.Load(), transfers.Load(), completes.Load()} != want {
						t.Fatal("recovery replayed upload steps")
					}
					for path, data := range map[string]string{configPath: cfg, sourcePath: "artifact"} {
						got, err := os.ReadFile(path)
						if err != nil || string(got) != data {
							t.Fatal("redirect failure/recovery changed config or source")
						}
					}
				})
			}
		}
	}
}
