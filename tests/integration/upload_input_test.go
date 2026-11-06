//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

func TestBinaryUploadInputRecoveryPreservesBytesAndFilenameRemoval(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, mode := range []string{"json", "compact", "human"} {
		for _, stdin := range []bool{false, true} {
			for _, dryRun := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/stdin=%t/dry=%t", mode, stdin, dryRun), func(t *testing.T) {
					const token = "xoxp-synthetic-upload-input"
					const customName = " report 名字.dat "
					data := []byte{0, ' ', '\n', '\t'}
					var mu sync.Mutex
					expectedName := customName
					stats := [3]int{}
					currentID := ""
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.URL.Path == "/upload/private-capability" {
							mu.Lock()
							stats[1]++
							name := expectedName
							mu.Unlock()
							if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer "+token || r.URL.RawQuery != "" {
								t.Error("wrong multipart authentication or method")
							}
							f, header, err := r.FormFile("file")
							if err != nil {
								t.Error(err)
								w.WriteHeader(400)
								return
							}
							content, readErr := io.ReadAll(f)
							closeErr := f.Close()
							if readErr != nil || closeErr != nil || !bytes.Equal(content, data) || header.Filename != name || r.FormValue("token") != "" {
								t.Error("upload bytes/name changed or credential entered multipart fields")
							}
							w.WriteHeader(200)
							return
						}
						if err := r.ParseForm(); err != nil {
							t.Error(err)
						}
						if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.PostForm.Get("token") != token {
							t.Error("wrong SDK API method or synthetic authentication")
						}
						w.Header().Set("Content-Type", "application/json")
						switch r.URL.Path {
						case "/api/auth.test":
							w.Header().Set("X-OAuth-Scopes", "files:write")
							writeJSON(w, `{"ok":true,"user_id":"U1"}`)
						case "/api/files.getUploadURLExternal":
							mu.Lock()
							stats[0]++
							name := expectedName
							currentID = fmt.Sprintf("F%d", stats[0])
							id := currentID
							mu.Unlock()
							if r.PostForm.Get("filename") != name || r.PostForm.Get("length") != "4" {
								t.Error("upload preparation changed name or byte length")
							}
							writeJSON(w, `{"ok":true,"file_id":"`+id+`","upload_url":"http://`+r.Host+`/upload/private-capability"}`)
						case "/api/files.completeUploadExternal":
							mu.Lock()
							stats[2]++
							id := currentID
							mu.Unlock()
							var files []struct {
								ID string `json:"id"`
							}
							if err := json.Unmarshal([]byte(r.PostForm.Get("files")), &files); err != nil || len(files) != 1 || files[0].ID != id || r.PostForm.Get("channel_id") != "C123" {
								t.Error("wrong finalized file or channel")
							}
							writeJSON(w, `{"ok":true,"files":[{"id":"`+id+`"}]}`)
						case "/api/files.info":
							writeJSON(w, `{"ok":false,"error":"missing_scope","needed":"files:read"}`)
						case "/api/conversations.info":
							writeJSON(w, `{"ok":true,"channel":{"id":"C123","name":"fixture"}}`)
						default:
							t.Errorf("unexpected path: %s", r.URL.Path)
							writeJSON(w, `{"ok":false,"error":"method_not_found"}`)
						}
					}))
					defer server.Close()
					isolated := t.TempDir()
					configPath := filepath.Join(isolated, "config.toml")
					path := filepath.Join(isolated, "original.bin")
					if err := os.WriteFile(path, nil, 0o600); err != nil {
						t.Fatal(err)
					}
					cfg := "schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"T123\"\ntoken_type=\"user\"\ntoken=\"env:SLICK_INPUT_FIXTURE_TOKEN\"\n"
					if err := os.WriteFile(configPath, []byte(cfg), 0o600); err != nil {
						t.Fatal(err)
					}
					run := func(content []byte, override bool) (string, error) {
						ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
						defer cancel()
						source := path
						if stdin {
							source = "-"
						}
						args := []string{"file", "upload", "--channel=C123", "--file=" + source, "--no-attribution", "--no-throttle", "--timeout=3s", "--color=never", "--output=" + mode}
						if dryRun {
							args = []string{"file", "upload", "-c", "C123", "-f", source, "--no-attribution", "--no-throttle", "--timeout=3s", "--color=never", "--output=" + mode, "-n"}
						}
						if override {
							if dryRun {
								args = append(args, "-N", customName)
							} else {
								args = append(args, "--filename="+customName)
							}
						}
						cmd := exec.CommandContext(ctx, binary, args...)
						cmd.Env = []string{"HOME=" + isolated, "SLICK_CONFIG=" + configPath, "SLACK_CLI_BASE_URL=" + server.URL, "SLICK_INPUT_FIXTURE_TOKEN=" + token}
						cmd.Stdin = bytes.NewReader(content)
						var out, diagnostic bytes.Buffer
						cmd.Stdout, cmd.Stderr = &out, &diagnostic
						err := cmd.Run()
						if ctx.Err() != nil {
							t.Fatal("upload fixture exceeded outer timeout")
						}
						if strings.Contains(out.String()+diagnostic.String(), token) || strings.Contains(out.String()+diagnostic.String(), "private-capability") {
							t.Fatal("credential or upload capability entered output")
						}
						if mode != "human" && diagnostic.Len() != 0 {
							t.Fatalf("machine upload used stderr: %s", &diagnostic)
						}
						if err == nil && diagnostic.Len() != 0 {
							t.Fatal("successful upload used stderr")
						}
						if mode == "json" || (mode == "compact" && err != nil) {
							var frame map[string]json.RawMessage
							if decodeErr := json.Unmarshal(out.Bytes(), &frame); decodeErr != nil {
								t.Fatal(decodeErr)
							}
							if len(frame) != 4 || (err != nil && string(frame["data"]) != "null") {
								t.Fatal("upload lost common machine frame")
							}
						}
						if err == nil && mode != "human" {
							data := out.Bytes()
							if mode == "json" {
								var frame struct {
									Data json.RawMessage `json:"data"`
								}
								if e := json.Unmarshal(data, &frame); e != nil {
									t.Fatal(e)
								}
								data = frame.Data
							}
							var result struct {
								Channel string `json:"channel"`
								DryRun  bool   `json:"dry_run"`
								File    struct {
									Name string `json:"name"`
									Size int    `json:"size"`
								} `json:"file"`
							}
							if e := json.Unmarshal(data, &result); e != nil {
								t.Fatal(e)
							}
							mu.Lock()
							name := expectedName
							mu.Unlock()
							if result.Channel != "C123" || result.DryRun != dryRun || result.File.Name != name || result.File.Size != 4 {
								t.Fatalf("upload flags lost target, filename, bytes or dry-run state: %s", data)
							}
						}
						return out.String(), err
					}
					_, err := run(nil, true)
					if exitCode(err) != 4 {
						t.Fatalf("empty upload did not fail validation: %v", err)
					}
					mu.Lock()
					got := stats
					mu.Unlock()
					if got != [3]int{} {
						t.Fatal("empty upload reached Slack")
					}
					if err := os.WriteFile(path, data, 0o600); err != nil {
						t.Fatal(err)
					}
					output, err := run(data, true)
					if err != nil || !strings.Contains(output, customName) {
						t.Fatalf("valid binary upload/name recovery failed: err=%v output=%s", err, output)
					}
					mu.Lock()
					expectedName = "original.bin"
					mu.Unlock()
					output, err = run(data, false)
					if stdin {
						if exitCode(err) != 4 {
							t.Fatal("removing required stdin filename did not fail validation")
						}
					} else if err != nil || !strings.Contains(output, "original.bin") {
						t.Fatalf("removing optional filename override failed: err=%v output=%s", err, output)
					}
					want := 1
					if !stdin {
						want = 2
					}
					if dryRun {
						want = 0
					}
					mu.Lock()
					got = stats
					mu.Unlock()
					if got != [3]int{want, want, want} {
						t.Fatalf("upload steps replayed or dry-run mutated Slack: %v", got)
					}
					content, err := os.ReadFile(path)
					if err != nil || !bytes.Equal(content, data) {
						t.Fatal("upload changed local source bytes")
					}
					after, err := os.ReadFile(configPath)
					if err != nil || string(after) != cfg {
						t.Fatal("upload changed workspace configuration")
					}
				})
			}
		}
	}
}
