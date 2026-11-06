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

func TestBinaryRawEmptyBlocksClearOnlyBlocksAndRecoverFromPermissionFailure(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, mode := range []string{"json", "compact", "human"} {
		t.Run(mode, func(t *testing.T) {
			const token = "xoxp-synthetic-raw-blocks"
			var mu sync.Mutex
			text, blocks := "Original fallback", `[{"type":"divider"}]`
			fail := true
			updates := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.PostForm.Get("token") != token {
					t.Error("wrong method or synthetic authentication")
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/auth.test":
					w.Header().Set("X-OAuth-Scopes", "chat:write")
					writeJSON(w, `{"ok":true,"user_id":"U1"}`)
				case "/api/conversations.info":
					writeJSON(w, `{"ok":true,"channel":{"id":"C123","name":"fixture"}}`)
				case "/api/chat.update":
					mu.Lock()
					defer mu.Unlock()
					updates++
					if r.PostForm.Get("channel") != "C123" || r.PostForm.Get("ts") != "1746284582.123456" {
						t.Error("wrong edit target")
					}
					for _, key := range []string{"metadata", "attachments", "thread_ts"} {
						if r.PostForm.Has(key) {
							t.Error("edit overwrote an unrelated setting")
						}
					}
					if fail {
						writeJSON(w, `{"ok":false,"error":"missing_scope","needed":"chat:write"}`)
						return
					}
					value, exists := r.PostForm["blocks"]
					if !exists || len(value) != 1 {
						t.Error("explicit Block Kit request lost blocks parameter")
					} else {
						blocks = value[0]
					}
					if blocks == "[]" {
						if r.PostForm.Has("text") {
							t.Error("clearing blocks replaced prior fallback text")
						}
					} else {
						if r.PostForm.Get("text") != "Restored content" {
							t.Error("removing raw flag did not restore Markdown input")
						}
					}
					if r.PostForm.Has("text") {
						text = r.PostForm.Get("text")
					}
					if err := json.NewEncoder(w).Encode(map[string]any{"ok": true, "channel": "C123", "ts": "1746284582.123456", "text": text}); err != nil {
						t.Error(err)
					}
				default:
					t.Errorf("unexpected path: %s", r.URL.Path)
					writeJSON(w, `{"ok":false,"error":"method_not_found"}`)
				}
			}))
			defer server.Close()
			isolated := t.TempDir()
			configPath := filepath.Join(isolated, "config.toml")
			cfg := "schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"T123\"\ntoken_type=\"user\"\ntoken=\"env:SLICK_RAW_FIXTURE_TOKEN\"\n"
			if err := os.WriteFile(configPath, []byte(cfg), 0o600); err != nil {
				t.Fatal(err)
			}
			for step := range 4 {
				mu.Lock()
				fail = step == 0
				mu.Unlock()
				args := []string{"message", "edit", "--channel=C123", "--timestamp=1746284582.123456", "--no-attribution", "--no-throttle", "--output=" + mode, "--color=never", "--timeout=3s"}
				if step == 2 {
					args = append(args, "--message=Restored content")
				} else {
					args = append(args, "--blocks", "--message=[]")
				}
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				cmd := exec.CommandContext(ctx, binary, args...)
				cmd.Env = []string{"HOME=" + isolated, "SLICK_CONFIG=" + configPath, "SLACK_CLI_BASE_URL=" + server.URL, "SLICK_RAW_FIXTURE_TOKEN=" + token}
				var out, diagnostic bytes.Buffer
				cmd.Stdout, cmd.Stderr = &out, &diagnostic
				err := cmd.Run()
				cancel()
				expectedExit := 0
				if step == 0 {
					expectedExit = 1
				}
				if exitCode(err) != expectedExit || strings.Contains(out.String()+diagnostic.String(), token) {
					t.Fatalf("wrong edit result: step=%d err=%v stdout=%s stderr=%s", step, err, &out, &diagnostic)
				}
				if mode != "human" && diagnostic.Len() != 0 {
					t.Fatal("machine edit duplicated human diagnostic")
				}
				if mode == "json" || (mode == "compact" && step == 0) {
					var frame map[string]json.RawMessage
					if err := json.Unmarshal(out.Bytes(), &frame); err != nil {
						t.Fatal(err)
					}
					if len(frame) != 4 || (step == 0 && string(frame["data"]) != "null") {
						t.Fatal("edit lost common failure/success frame")
					}
				}
				mu.Lock()
				gotText, gotBlocks, gotUpdates := text, blocks, updates
				mu.Unlock()
				wantText := "Original fallback"
				if step >= 2 {
					wantText = "Restored content"
				}
				if gotText != wantText || gotUpdates != step+1 {
					t.Fatal("edit replayed or changed preserved fallback text")
				}
				if step == 0 && gotBlocks != `[{"type":"divider"}]` {
					t.Fatal("permission failure changed prior blocks")
				}
				if (step == 1 || step == 3) && gotBlocks != "[]" {
					t.Fatal("explicit empty array did not clear blocks")
				}
				if step == 2 && !strings.Contains(gotBlocks, "Restored content") {
					t.Fatal("removing raw flag failed to restore Markdown blocks")
				}
				after, err := os.ReadFile(configPath)
				if err != nil || string(after) != cfg {
					t.Fatal("edit changed workspace configuration")
				}
			}
		})
	}
}

func TestBinaryFileUploadPreservesEmptyRawCommentAndOptionalArgumentRemoval(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, mode := range []string{"json", "compact", "human"} {
		for _, kind := range []string{"raw", "empty raw", "no comment", "markdown"} {
			t.Run(mode+"/"+kind, func(t *testing.T) {
				const token = "xoxp-synthetic-upload-comment"
				prepares, uploads, completes := 0, 0, 0
				var mu sync.Mutex
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/upload" {
						mu.Lock()
						uploads++
						mu.Unlock()
						if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer "+token || r.URL.RawQuery != "" {
							t.Error("wrong upload authentication or method")
						}
						file, header, err := r.FormFile("file")
						if err != nil {
							t.Error(err)
							w.WriteHeader(400)
							return
						}
						defer func() {
							if err := file.Close(); err != nil {
								t.Error(err)
							}
						}()
						content, err := io.ReadAll(file)
						if err != nil || string(content) != "artifact" || header.Filename != "artifact.dat" {
							t.Error("uploaded file content/name changed")
						}
						if r.FormValue("token") != "" {
							t.Error("credential entered multipart form fields")
						}
						w.WriteHeader(http.StatusOK)
						return
					}
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if r.Method != http.MethodPost || r.PostForm.Get("token") != token || r.URL.RawQuery != "" {
						t.Error("wrong API method or authentication")
					}
					w.Header().Set("Content-Type", "application/json")
					switch r.URL.Path {
					case "/api/auth.test":
						w.Header().Set("X-OAuth-Scopes", "files:write")
						writeJSON(w, `{"ok":true,"user_id":"U1"}`)
					case "/api/files.getUploadURLExternal":
						mu.Lock()
						prepares++
						mu.Unlock()
						if r.PostForm.Get("filename") != "artifact.dat" || r.PostForm.Get("length") != "8" {
							t.Error("wrong upload filename or byte length")
						}
						writeJSON(w, `{"ok":true,"file_id":"F1","upload_url":"http://`+r.Host+`/upload"}`)
					case "/api/files.completeUploadExternal":
						mu.Lock()
						completes++
						mu.Unlock()
						var files []struct{ ID, Title string }
						if err := json.Unmarshal([]byte(r.PostForm.Get("files")), &files); err != nil || len(files) != 1 || files[0].ID != "F1" {
							t.Error("wrong finalized file")
						}
						if r.PostForm.Get("channel_id") != "C123" {
							t.Error("wrong upload channel")
						}
						if kind == "raw" {
							if r.PostForm.Get("thread_ts") != "1746284582.123456" || files[0].Title != "Release artifact" {
								t.Error("title/thread not passed")
							}
						} else if r.PostForm.Has("thread_ts") || files[0].Title != "" {
							t.Error("removed optional arguments persisted")
						}
						if r.PostForm.Has("initial_comment") {
							t.Error("raw or absent comment became literal JSON text")
						}
						switch kind {
						case "empty raw":
							if r.PostForm.Get("blocks") != "[]" {
								t.Error("explicit empty raw comment was dropped")
							}
						case "no comment":
							if r.PostForm.Has("blocks") {
								t.Error("removed comment retained blocks")
							}
						default:
							if !strings.Contains(r.PostForm.Get("blocks"), "Release artifact") {
								t.Error("comment content lost")
							}
						}
						writeJSON(w, `{"ok":true,"files":[{"id":"F1"}]}`)
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
				cfg := "schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"T123\"\ntoken_type=\"user\"\ntoken=\"env:SLICK_RAW_FIXTURE_TOKEN\"\n"
				if err := os.WriteFile(configPath, []byte(cfg), 0o600); err != nil {
					t.Fatal(err)
				}
				args := []string{"file", "upload", "--channel=C123", "--file=-", "--filename=artifact.dat", "--no-attribution", "--no-throttle", "--output=" + mode, "--timeout=3s", "--color=never"}
				switch kind {
				case "raw":
					args = append(args, "--blocks", "--message=[{\"type\":\"section\",\"text\":{\"type\":\"plain_text\",\"text\":\"Release artifact\"}}]", "--title=Release artifact", "--thread=1746284582.123456")
				case "empty raw":
					args = append(args, "--blocks", "--message=[]")
				case "markdown":
					args = append(args, "--message=Release artifact")
				}
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, binary, args...)
				cmd.Env = []string{"HOME=" + isolated, "SLICK_CONFIG=" + configPath, "SLACK_CLI_BASE_URL=" + server.URL, "SLICK_RAW_FIXTURE_TOKEN=" + token}
				cmd.Stdin = strings.NewReader("artifact")
				var out, diagnostic bytes.Buffer
				cmd.Stdout, cmd.Stderr = &out, &diagnostic
				if err := cmd.Run(); err != nil || ctx.Err() != nil || diagnostic.Len() != 0 || strings.Contains(out.String(), token) {
					t.Fatalf("upload failed: err=%v stdout=%s stderr=%s", err, &out, &diagnostic)
				}
				mu.Lock()
				got := [3]int{prepares, uploads, completes}
				mu.Unlock()
				if got != [3]int{1, 1, 1} {
					t.Fatal("upload step was repeated or skipped")
				}
				if !strings.Contains(out.String(), "F1") || !strings.Contains(out.String(), "artifact.dat") {
					t.Fatal("optional files.info failure lost successful upload metadata")
				}
				after, err := os.ReadFile(configPath)
				if err != nil || string(after) != cfg {
					t.Fatal("upload changed configuration")
				}
			})
		}
	}
}
