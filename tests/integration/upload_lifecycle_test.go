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

func TestBinaryUploadFailuresRetainProgressWithoutReplayOrCleanup(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tc := range []struct {
		name, stage, body, state string
		status, exit             int
		counts                   [3]int
	}{
		{"prepare file_type_not_allowed", "prepare", `{"ok":false,"error":"file_type_not_allowed","provided":"synthetic-private-policy","response_metadata":{"messages":["synthetic-private-policy"]}}`, "prepared", 200, 4, [3]int{1, 0, 0}},
		{"prepare file_upload_size_restricted", "prepare", `{"ok":false,"error":"file_upload_size_restricted","provided":"synthetic-private-policy","response_metadata":{"messages":["synthetic-private-policy"]}}`, "prepared", 200, 4, [3]int{1, 0, 0}},
		{"prepare file_uploads_disabled", "prepare", `{"ok":false,"error":"file_uploads_disabled","provided":"synthetic-private-policy","response_metadata":{"messages":["synthetic-private-policy"]}}`, "prepared", 200, 1, [3]int{1, 0, 0}},
		{"prepare file_uploads_except_images_disabled", "prepare", `{"ok":false,"error":"file_uploads_except_images_disabled","provided":"synthetic-private-policy","response_metadata":{"messages":["synthetic-private-policy"]}}`, "prepared", 200, 4, [3]int{1, 0, 0}},
		{"prepare enterprise_is_restricted", "prepare", `{"ok":false,"error":"enterprise_is_restricted","provided":"synthetic-private-policy","response_metadata":{"messages":["synthetic-private-policy"]}}`, "prepared", 200, 1, [3]int{1, 0, 0}},
		{"prepare user_is_external_guest", "prepare", `{"ok":false,"error":"user_is_external_guest","provided":"synthetic-private-policy","response_metadata":{"messages":["synthetic-private-policy"]}}`, "prepared", 200, 1, [3]int{1, 0, 0}},
		{"prepare snippet_too_large", "prepare", `{"ok":false,"error":"snippet_too_large","provided":"synthetic-private-policy","response_metadata":{"messages":["synthetic-private-policy"]}}`, "prepared", 200, 4, [3]int{1, 0, 0}},
		{"prepare storage_limit_reached", "prepare", `{"ok":false,"error":"storage_limit_reached","provided":"synthetic-private-policy","response_metadata":{"messages":["synthetic-private-policy"]}}`, "prepared", 200, 4, [3]int{1, 0, 0}},
		{"prepare request_timeout", "prepare", `{"ok":false,"error":"request_timeout","provided":"synthetic-private-policy","response_metadata":{"messages":["synthetic-private-policy"]}}`, "prepared", 200, 4, [3]int{1, 0, 0}},
		{"prepare unknown_snippet_type", "prepare", `{"ok":false,"error":"unknown_snippet_type","provided":"synthetic-private-policy","response_metadata":{"messages":["synthetic-private-policy"]}}`, "prepared", 200, 4, [3]int{1, 0, 0}},
		{"prepare unknown_subtype", "prepare", `{"ok":false,"error":"unknown_subtype","provided":"synthetic-private-policy","response_metadata":{"messages":["synthetic-private-policy"]}}`, "prepared", 200, 4, [3]int{1, 0, 0}},
		{"prepare alt_txt_too_large", "prepare", `{"ok":false,"error":"alt_txt_too_large","provided":"synthetic-private-policy","response_metadata":{"messages":["synthetic-private-policy"]}}`, "prepared", 200, 4, [3]int{1, 0, 0}},
		{"complete channels_limit_exceeded", "complete", `{"ok":false,"error":"channels_limit_exceeded","provided":"synthetic-private-policy","response_metadata":{"messages":["synthetic-private-policy"]}}`, "transferred", 200, 4, [3]int{1, 1, 1}},
		{"complete invalid_channel", "complete", `{"ok":false,"error":"invalid_channel","provided":"synthetic-private-policy","response_metadata":{"messages":["synthetic-private-policy"]}}`, "transferred", 200, 2, [3]int{1, 1, 1}},
		{"complete posting_to_channel_denied", "complete", `{"ok":false,"error":"posting_to_channel_denied","provided":"synthetic-private-policy","response_metadata":{"messages":["synthetic-private-policy"]}}`, "transferred", 200, 1, [3]int{1, 1, 1}},
		{"complete file_type_not_allowed", "complete", `{"ok":false,"error":"file_type_not_allowed","provided":"synthetic-private-policy","response_metadata":{"messages":["synthetic-private-policy"]}}`, "transferred", 200, 4, [3]int{1, 1, 1}},
		{"complete file_uploads_except_images_disabled", "complete", `{"ok":false,"error":"file_uploads_except_images_disabled","provided":"synthetic-private-policy","response_metadata":{"messages":["synthetic-private-policy"]}}`, "transferred", 200, 4, [3]int{1, 1, 1}},
		{"complete enterprise_is_restricted", "complete", `{"ok":false,"error":"enterprise_is_restricted","provided":"synthetic-private-policy","response_metadata":{"messages":["synthetic-private-policy"]}}`, "transferred", 200, 1, [3]int{1, 1, 1}},
		{"complete user_is_external_guest", "complete", `{"ok":false,"error":"user_is_external_guest","provided":"synthetic-private-policy","response_metadata":{"messages":["synthetic-private-policy"]}}`, "transferred", 200, 1, [3]int{1, 1, 1}},
		{"complete request_timeout", "complete", `{"ok":false,"error":"request_timeout","provided":"synthetic-private-policy","response_metadata":{"messages":["synthetic-private-policy"]}}`, "transferred", 200, 4, [3]int{1, 1, 1}},
		{"complete file_update_failed", "complete", `{"ok":false,"error":"file_update_failed","provided":"synthetic-private-policy","response_metadata":{"messages":["synthetic-private-policy"]}}`, "transferred", 200, 5, [3]int{1, 1, 1}},
		{"complete fatal_error", "complete", `{"ok":false,"error":"fatal_error","provided":"synthetic-private-policy","response_metadata":{"messages":["synthetic-private-policy"]}}`, "complete", 200, 5, [3]int{1, 1, 1}},
		{"complete internal_error", "complete", `{"ok":false,"error":"internal_error","provided":"synthetic-private-policy","response_metadata":{"messages":["synthetic-private-policy"]}}`, "complete", 200, 5, [3]int{1, 1, 1}},
		{"complete service_unavailable", "complete", `{"ok":false,"error":"service_unavailable","provided":"synthetic-private-policy","response_metadata":{"messages":["synthetic-private-policy"]}}`, "transferred", 200, 5, [3]int{1, 1, 1}},
		{"complete future_file_error", "complete", `{"ok":false,"error":"future_file_error","provided":"synthetic-private-policy","response_metadata":{"messages":["synthetic-private-policy"]}}`, "transferred", 200, 5, [3]int{1, 1, 1}},
		{"complete org_login_required", "complete", `{"ok":false,"error":"org_login_required","provided":"synthetic-private-policy","response_metadata":{"messages":["synthetic-private-policy"]}}`, "transferred", 200, 5, [3]int{1, 1, 1}},
		{"complete team_added_to_org", "complete", `{"ok":false,"error":"team_added_to_org","provided":"synthetic-private-policy","response_metadata":{"messages":["synthetic-private-policy"]}}`, "transferred", 200, 5, [3]int{1, 1, 1}},
		{"prepare missing ack", "prepare", `{"file_id":"F1","upload_url":"URL"}`, "prepared", 200, 5, [3]int{1, 0, 0}},
		{"prepare missing ID", "prepare", `{"ok":true,"upload_url":"URL"}`, "prepared", 200, 5, [3]int{1, 0, 0}},
		{"prepare private ID", "prepare", `{"ok":true,"file_id":"synthetic-private-id","upload_url":"URL"}`, "prepared", 200, 5, [3]int{1, 0, 0}},
		{"prepare missing URL", "prepare", `{"ok":true,"file_id":"F1"}`, "prepared", 200, 5, [3]int{1, 0, 0}},
		{"prepare permission", "prepare", `{"ok":false,"error":"missing_scope","needed":"files:write"}`, "prepared", 200, 1, [3]int{1, 0, 0}},
		{"prepare 503", "prepare", `synthetic-private-body`, "prepared", 503, 5, [3]int{1, 0, 0}},
		{"transfer 503", "transfer", `synthetic-private-body`, "transferred", 503, 5, [3]int{1, 1, 0}},
		{"completion permission", "complete", `{"ok":false,"error":"missing_scope","needed":"files:write"}`, "transferred", 200, 1, [3]int{1, 1, 1}},
		{"completion 503 after acceptance", "complete", `synthetic-private-body`, "complete", 503, 5, [3]int{1, 1, 1}},
		{"completion malformed after acceptance", "complete", `{`, "complete", 200, 5, [3]int{1, 1, 1}},
		{"completion missing ack", "complete", `{"files":[{"id":"F1"}]}`, "complete", 200, 5, [3]int{1, 1, 1}},
		{"completion no files", "complete", `{"ok":true}`, "complete", 200, 5, [3]int{1, 1, 1}},
		{"completion null files", "complete", `{"ok":true,"files":null}`, "complete", 200, 5, [3]int{1, 1, 1}},
		{"completion extra files", "complete", `{"ok":true,"files":[{"id":"F1"},{"id":"F2"}]}`, "complete", 200, 5, [3]int{1, 1, 1}},
		{"completion missing ID", "complete", `{"ok":true,"files":[{}]}`, "complete", 200, 5, [3]int{1, 1, 1}},
		{"completion wrong ID", "complete", `{"ok":true,"files":[{"id":"FOTHER"}]}`, "complete", 200, 5, [3]int{1, 1, 1}},
		{"completion timeout", "complete", `{`, "complete", 200, 7, [3]int{1, 1, 1}},
	} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				const token = "xoxp-synthetic-upload-lifecycle"
				var mu sync.Mutex
				counts := [3]int{}
				resources := map[string]string{"FKEEP": "unrelated resource"}
				recovery := false
				currentID := ""
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					if strings.HasPrefix(r.URL.Path, "/upload/private-capability/") {
						if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer "+token || r.URL.RawQuery != "" {
							t.Error("wrong transfer authentication or method")
						}
						f, h, err := r.FormFile("file")
						if err != nil {
							t.Error(err)
							w.WriteHeader(400)
							return
						}
						body, readErr := io.ReadAll(f)
						closeErr := f.Close()
						if readErr != nil || closeErr != nil || string(body) != "artifact" || h.Filename != "artifact.bin" {
							t.Error("transfer changed artifact bytes/name")
						}
						mu.Lock()
						counts[1]++
						resources[currentID] = "transferred"
						good := recovery
						mu.Unlock()
						if !good && tc.stage == "transfer" {
							w.WriteHeader(tc.status)
							writeJSON(w, tc.body)
						} else {
							w.WriteHeader(200)
						}
						return
					}
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.PostForm.Get("token") != token {
						t.Error("wrong API method or synthetic authentication")
					}
					switch r.URL.Path {
					case "/api/auth.test":
						w.Header().Set("X-OAuth-Scopes", "files:write")
						writeJSON(w, `{"ok":true,"user_id":"U1"}`)
					case "/api/files.getUploadURLExternal":
						mu.Lock()
						counts[0]++
						currentID = fmt.Sprintf("F%d", counts[0])
						id := currentID
						resources[id] = "prepared"
						good := recovery
						mu.Unlock()
						if r.PostForm.Get("filename") != "artifact.bin" || r.PostForm.Get("length") != "8" {
							t.Error("wrong prepare name/length")
						}
						url := "http://" + r.Host + "/upload/private-capability/" + id
						if !good && tc.stage == "prepare" {
							w.WriteHeader(tc.status)
							writeJSON(w, strings.ReplaceAll(tc.body, "URL", url))
						} else {
							writeJSON(w, `{"ok":true,"file_id":"`+id+`","upload_url":"`+url+`"}`)
						}
					case "/api/files.completeUploadExternal":
						mu.Lock()
						counts[2]++
						id := currentID
						good := recovery
						if good || tc.state == "complete" {
							resources[id] = "complete"
						}
						mu.Unlock()
						var files []struct {
							ID string `json:"id"`
						}
						if err := json.Unmarshal([]byte(r.PostForm.Get("files")), &files); err != nil || len(files) != 1 || files[0].ID != id || r.PostForm.Get("channel_id") != "C123" {
							t.Error("wrong completion target")
						}
						if !good && tc.stage == "complete" {
							w.WriteHeader(tc.status)
							writeJSON(w, tc.body)
							if tc.name == "completion timeout" {
								if flush, ok := w.(http.Flusher); ok {
									flush.Flush()
								}
								<-r.Context().Done()
							}
						} else {
							writeJSON(w, `{"ok":true,"files":[{"id":"`+id+`"}]}`)
						}
					case "/api/files.info":
						writeJSON(w, `{"ok":false,"error":"missing_scope","needed":"files:read"}`)
					case "/api/conversations.info":
						writeJSON(w, `{"ok":true,"channel":{"id":"C123","name":"fixture"}}`)
					default:
						t.Errorf("unexpected request or cleanup: %s", r.URL.Path)
						writeJSON(w, `{"ok":false,"error":"method_not_found"}`)
					}
				}))
				defer server.Close()
				isolated := t.TempDir()
				configPath := filepath.Join(isolated, "config.toml")
				filePath := filepath.Join(isolated, "artifact.bin")
				if err := os.WriteFile(filePath, []byte("artifact"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfg := "schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"T123\"\ntoken_type=\"user\"\ntoken=\"env:SLICK_UPLOAD_FIXTURE_TOKEN\"\n"
				if err := os.WriteFile(configPath, []byte(cfg), 0o600); err != nil {
					t.Fatal(err)
				}
				run := func(first bool) (string, string, error) {
					ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
					defer cancel()
					timeout := "3s"
					if first && tc.name == "completion timeout" {
						timeout = "150ms"
					}
					cmd := exec.CommandContext(ctx, binary, "file", "upload", "--channel=C123", "--file="+filePath, "--no-attribution", "--no-throttle", "--output="+mode, "--color=never", "--timeout="+timeout, "--debug")
					cmd.Env = []string{"HOME=" + isolated, "SLICK_CONFIG=" + configPath, "SLACK_CLI_BASE_URL=" + server.URL, "SLICK_UPLOAD_FIXTURE_TOKEN=" + token}
					var out, diagnostic bytes.Buffer
					cmd.Stdout, cmd.Stderr = &out, &diagnostic
					err := cmd.Run()
					if ctx.Err() != nil {
						t.Fatal("upload exceeded outer fixture timeout")
					}
					for _, secret := range []string{token, "private-capability", "synthetic-private"} {
						if strings.Contains(out.String()+diagnostic.String(), secret) {
							t.Fatal("upload diagnostics exposed credentials, capability URL or opaque body")
						}
					}
					return out.String(), diagnostic.String(), err
				}
				out, diagnostic, err := run(true)
				if exitCode(err) != tc.exit {
					t.Fatalf("wrong failed-upload exit: err=%v stdout=%s stderr=%s", err, out, diagnostic)
				}
				wantKind := map[int]string{1: "auth_failure", 2: "not_found", 5: "server_error", 4: "validation_error", 7: "timeout"}[tc.exit]
				if mode == "human" {
					if !strings.Contains(diagnostic, "type="+wantKind) {
						t.Fatal("human upload failure lost its error class")
					}
					if out != "" || !strings.Contains(diagnostic, "stage="+tc.stage) {
						t.Fatal("human failure lost stream or stage")
					}
					if tc.stage != "prepare" && !strings.Contains(diagnostic, "file_id=F1") {
						t.Fatal("human failure lost known file ID")
					}
				} else {
					var frame map[string]json.RawMessage
					if err := json.Unmarshal([]byte(out), &frame); err != nil {
						t.Fatal(err)
					}
					if len(frame) != 4 || string(frame["ok"]) != "false" || string(frame["data"]) != "null" || strings.Contains(diagnostic, "type=") {
						t.Fatal("failure changed common frame or duplicated human error")
					}
					var failures []struct {
						Type    string `json:"type"`
						Exit    int    `json:"exit_code"`
						Details struct {
							Stage  string   `json:"stage"`
							FileID string   `json:"file_id"`
							Needed []string `json:"needed"`
						} `json:"details"`
					}
					if err := json.Unmarshal(frame["errors"], &failures); err != nil {
						t.Fatal(err)
					}
					wantID := "F1"
					if tc.stage == "prepare" {
						wantID = ""
					}
					var meta struct {
						Exit int `json:"exit_code"`
					}
					if err := json.Unmarshal(frame["meta"], &meta); err != nil {
						t.Fatal(err)
					}
					if meta.Exit != tc.exit {
						t.Fatal("machine envelope exit does not match the command exit")
					}
					if len(failures) != 1 || failures[0].Type != wantKind || failures[0].Exit != tc.exit || failures[0].Details.Stage != tc.stage || failures[0].Details.FileID != wantID {
						t.Fatal("failure lost trustworthy upload progress")
					}
					if (tc.name == "prepare permission" || tc.name == "completion permission") && (len(failures[0].Details.Needed) != 1 || failures[0].Details.Needed[0] != "files:write") {
						t.Fatal("progress overwrote normalized scope diagnostics")
					}
					if tc.exit == 1 && tc.name != "prepare permission" && tc.name != "completion permission" && len(failures[0].Details.Needed) != 0 {
						t.Fatal("workspace policy failure invented missing scopes")
					}
				}
				mu.Lock()
				got := counts
				state, keep := resources["F1"], resources["FKEEP"]
				recovery = true
				mu.Unlock()
				if got != tc.counts || state != tc.state || keep != "unrelated resource" {
					t.Fatalf("failure replayed, cleaned up or changed unrelated state: calls=%v state=%s", got, state)
				}
				out, diagnostic, err = run(false)
				if err != nil || !strings.Contains(out, "F2") {
					t.Fatalf("explicit recovery failed: err=%v stdout=%s stderr=%s", err, out, diagnostic)
				}
				mu.Lock()
				got = counts
				oldState, keep, newState := resources["F1"], resources["FKEEP"], resources["F2"]
				mu.Unlock()
				want := [3]int{tc.counts[0] + 1, tc.counts[1] + 1, tc.counts[2] + 1}
				if got != want || oldState != tc.state || keep != "unrelated resource" || newState != "complete" {
					t.Fatal("recovery replayed steps or modified prior/unrelated resources")
				}
				after, err := os.ReadFile(configPath)
				if err != nil || string(after) != cfg {
					t.Fatal("upload failure/recovery changed config")
				}
				body, err := os.ReadFile(filePath)
				if err != nil || string(body) != "artifact" {
					t.Fatal("upload failure/recovery changed source")
				}
			})
		}
	}
}

func TestBinaryUploadMetadataIsOptionalAndMustMatchAcknowledgedFile(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tc := range []struct{ name, body, wantName string }{
		{"valid", `{"ok":true,"file":{"id":"F1","name":"normalized.bin","title":"Slack title","permalink":"https://example.slack.com/files/F1"}}`, "normalized.bin"},
		{"wrong file", `{"ok":true,"file":{"id":"FOTHER","name":"synthetic-private-metadata","permalink":"https://example.slack.com/synthetic-private-link"}}`, "artifact.bin"},
		{"missing ack", `{"file":{"id":"F1","name":"synthetic-private-metadata"}}`, "artifact.bin"},
		{"empty file", `{"ok":true,"file":{}}`, "artifact.bin"},
		{"malformed", `{"file":`, "artifact.bin"},
		{"permission", `{"ok":false,"error":"missing_scope","needed":"files:read"}`, "artifact.bin"},
		{"metadata timeout", `{`, "artifact.bin"},
	} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				const token = "xoxp-synthetic-upload-metadata"
				var mu sync.Mutex
				counts := [4]int{}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/upload" {
						mu.Lock()
						counts[1]++
						mu.Unlock()
						if _, err := io.Copy(io.Discard, r.Body); err != nil {
							t.Error(err)
						}
						w.WriteHeader(200)
						return
					}
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if r.Method != http.MethodPost || r.PostForm.Get("token") != token || r.URL.RawQuery != "" {
						t.Error("wrong API authentication or method")
					}
					w.Header().Set("Content-Type", "application/json")
					switch r.URL.Path {
					case "/api/auth.test":
						w.Header().Set("X-OAuth-Scopes", "files:write")
						writeJSON(w, `{"ok":true,"user_id":"U1"}`)
					case "/api/files.getUploadURLExternal":
						mu.Lock()
						counts[0]++
						mu.Unlock()
						writeJSON(w, `{"ok":true,"file_id":"F1","upload_url":"http://`+r.Host+`/upload"}`)
					case "/api/files.completeUploadExternal":
						mu.Lock()
						counts[2]++
						mu.Unlock()
						writeJSON(w, `{"ok":true,"files":[{"id":"F1","title":"Requested title"}]}`)
					case "/api/files.info":
						mu.Lock()
						counts[3]++
						mu.Unlock()
						if r.PostForm.Get("file") != "F1" {
							t.Error("wrong metadata target")
						}
						if r.PostForm.Get("count") != "1" || r.PostForm.Get("page") != "1" {
							t.Error("metadata lookup did not request first bounded page")
						}

						writeJSON(w, tc.body)
						if tc.name == "metadata timeout" {
							if flush, ok := w.(http.Flusher); ok {
								flush.Flush()
							}
							<-r.Context().Done()
						}
					case "/api/conversations.info":
						writeJSON(w, `{"ok":true,"channel":{"id":"C123","name":"fixture"}}`)
					default:
						t.Errorf("metadata caused unexpected mutation/cleanup: %s", r.URL.Path)
					}
				}))
				defer server.Close()
				isolated := t.TempDir()
				configPath := filepath.Join(isolated, "config.toml")
				filePath := filepath.Join(isolated, "artifact.bin")
				if err := os.WriteFile(filePath, []byte("artifact"), 0o600); err != nil {
					t.Fatal(err)
				}
				cfg := "schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"T123\"\ntoken_type=\"user\"\ntoken=\"env:SLICK_UPLOAD_FIXTURE_TOKEN\"\n"
				if err := os.WriteFile(configPath, []byte(cfg), 0o600); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				timeout := "3s"
				if tc.name == "metadata timeout" {
					timeout = "150ms"
				}
				cmd := exec.CommandContext(ctx, binary, "file", "upload", "--channel=C123", "--file="+filePath, "--title=Requested title", "--no-attribution", "--no-throttle", "--output="+mode, "--timeout="+timeout, "--color=never", "--debug")
				cmd.Env = []string{"HOME=" + isolated, "SLICK_CONFIG=" + configPath, "SLACK_CLI_BASE_URL=" + server.URL, "SLICK_UPLOAD_FIXTURE_TOKEN=" + token}
				var out, diagnostic bytes.Buffer
				cmd.Stdout, cmd.Stderr = &out, &diagnostic
				err := cmd.Run()
				if err != nil || ctx.Err() != nil || strings.Contains(out.String()+diagnostic.String(), "synthetic-private") || strings.Contains(out.String()+diagnostic.String(), token) {
					t.Fatalf("optional metadata changed successful upload or disclosed data: err=%v stdout=%s stderr=%s", err, &out, &diagnostic)
				}
				if !strings.Contains(out.String(), tc.wantName) {
					t.Fatal("optional metadata from another file replaced acknowledged upload data")
				}
				mu.Lock()
				got := counts
				mu.Unlock()
				if got != [4]int{1, 1, 1, 1} {
					t.Fatal("optional metadata failure replayed upload or metadata request")
				}
				if mode != "human" {
					data := out.Bytes()
					if mode == "json" {
						var frame struct {
							Data json.RawMessage `json:"data"`
						}
						if err := json.Unmarshal(data, &frame); err != nil {
							t.Fatal(err)
						}
						data = frame.Data
					}
					var result struct {
						File struct {
							ID, Name, Title string
							Permalink       *string
						} `json:"file"`
					}
					if err := json.Unmarshal(data, &result); err != nil {
						t.Fatal(err)
					}
					wantTitle := "Requested title"
					if tc.name == "valid" {
						wantTitle = "Slack title"
					}
					if result.File.ID != "F1" || result.File.Name != tc.wantName || result.File.Title != wantTitle || (tc.name != "valid" && result.File.Permalink != nil) {
						t.Fatal("optional metadata corrupted completed upload result")
					}
					if tc.name == "valid" && (result.File.Permalink == nil || *result.File.Permalink != "https://example.slack.com/files/F1") {
						t.Fatal("valid upload metadata was discarded")
					}
				}
			})
		}
	}
}
