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

func TestBinaryUploadFlagsPreserveValuesAndRemoveOptionalArguments(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, mode := range []string{"json", "compact", "human"} {
		for _, tokenType := range []string{"user", "bot"} {
			for _, short := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/short=%t", mode, tokenType, short), func(t *testing.T) {
					token := "xoxp-synthetic-upload-flags"
					if tokenType == "bot" {
						token = "xoxb-synthetic-upload-flags"
					}
					const thread = "1746284582.001000"
					type expectation struct {
						title, thread, context, comment string
						blocks, attribution, raw        bool
					}
					var mu sync.Mutex
					expected := expectation{}
					counts := [3]int{}
					requests := 0
					currentID := ""
					completed := map[string]bool{}
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						mu.Lock()
						requests++
						want := expected
						id := currentID
						mu.Unlock()
						if r.URL.Path == "/upload" {
							if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer "+token {
								t.Error("wrong multipart authentication or method")
							}
							file, header, err := r.FormFile("file")
							if err != nil {
								t.Error(err)
								w.WriteHeader(400)
								return
							}
							data, readErr := io.ReadAll(file)
							closeErr := file.Close()
							if readErr != nil || closeErr != nil || string(data) != " \t\r\n" || header.Filename != "artifact.txt" {
								t.Error("upload changed whitespace-only bytes or filename")
							}
							mu.Lock()
							counts[1]++
							mu.Unlock()
							w.WriteHeader(200)
							return
						}
						if err := r.ParseForm(); err != nil {
							t.Error(err)
						}
						if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.PostForm.Get("token") != token {
							t.Error("wrong RPC method or authentication")
						}
						switch r.URL.Path {
						case "/api/auth.test":
							w.Header().Set("X-OAuth-Scopes", "files:write")
							writeJSON(w, `{"ok":true,"user_id":"U1"}`)
						case "/api/conversations.info":
							writeJSON(w, `{"ok":true,"channel":{"id":"C123","name":"fixture"}}`)
						case "/api/files.getUploadURLExternal":
							if r.PostForm.Get("filename") != "artifact.txt" || r.PostForm.Get("length") != "4" {
								t.Error("preparation changed filename or byte length")
							}
							mu.Lock()
							counts[0]++
							id = fmt.Sprintf("F%d", counts[0])
							currentID = id
							mu.Unlock()
							writeJSON(w, `{"ok":true,"file_id":"`+id+`","upload_url":"http://`+r.Host+`/upload"}`)
						case "/api/files.completeUploadExternal":
							var files []struct {
								ID    string `json:"id"`
								Title string `json:"title"`
							}
							if err := json.Unmarshal([]byte(r.PostForm.Get("files")), &files); err != nil || len(files) != 1 || files[0].ID != id || files[0].Title != want.title || r.PostForm.Get("channel_id") != "C123" || r.PostForm.Has("channels") {
								t.Error("completion changed file title or channel")
							}
							if r.PostForm.Get("thread_ts") != want.thread || r.PostForm.Has("thread_ts") != (want.thread != "") {
								t.Error("thread flag lost precision or survived removal")
							}
							if r.PostForm.Has("initial_comment") {
								t.Error("initial_comment would suppress Block Kit")
							}
							if r.PostForm.Has("blocks") != want.blocks {
								t.Error("comment blocks survived removal or were lost")
							}
							if want.blocks {
								var blocks []map[string]json.RawMessage
								if err := json.Unmarshal([]byte(r.PostForm.Get("blocks")), &blocks); err != nil {
									t.Error(err)
								}
								if want.raw {
									var text struct{ Type, Text string }
									if len(blocks) != 1 || string(blocks[0]["type"]) != `"section"` {
										t.Error("raw Block Kit input was reinterpreted as Markdown")
									} else if err := json.Unmarshal(blocks[0]["text"], &text); err != nil || text.Type != "plain_text" || text.Text != "Release ready" {
										t.Error("raw Block Kit text object was changed")
									}
								}
								contexts := 0
								for _, block := range blocks {
									if string(block["type"]) == `"context"` {
										contexts++
										if !strings.Contains(string(block["elements"]), want.context) {
											t.Error("attribution flags did not reach the upload comment")
										}
									}
								}
								wantContexts := 0
								if want.attribution {
									wantContexts = 1
								}
								if contexts != wantContexts {
									t.Error("attribution toggle was ignored")
								}
								if want.comment != "" && !strings.Contains(r.PostForm.Get("blocks"), want.comment) {
									t.Error("message flag did not reach upload comment")
								}
							}
							mu.Lock()
							if completed[id] {
								t.Error("file completion was replayed")
							}
							completed[id] = true
							counts[2]++
							mu.Unlock()
							writeJSON(w, `{"ok":true,"files":[{"id":"`+id+`"}]}`)
						case "/api/files.info":
							if r.PostForm.Get("file") != id || r.PostForm.Get("count") != "1" || r.PostForm.Get("page") != "1" {
								t.Error("metadata lookup targeted another file or unbounded page")
							}
							writeJSON(w, `{"ok":false,"error":"missing_scope","needed":"files:read"}`)
						default:
							t.Errorf("unexpected request: %s", r.URL.Path)
							w.WriteHeader(500)
						}
					}))
					defer server.Close()
					home := t.TempDir()
					cfgPath := filepath.Join(home, "config.toml")
					cfg := "schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"T123\"\ntoken_type=\"" + tokenType + "\"\ntoken=\"env:SLICK_FLAGS_FIXTURE_TOKEN\"\n"
					if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
						t.Fatal(err)
					}
					flag := func(long, alias, value string) []string {
						if short {
							return []string{alias, value}
						}
						return []string{long + "=" + value}
					}
					scenarios := []struct {
						name string
						args []string
						want expectation
					}{
						{"custom label", append(append(append(flag("--title", "-T", "Release evidence"), flag("--thread", "-t", thread)...), flag("--message", "-m", "Review complete")...), "--attribution", "--attribution-label=Release helper", "--attribution-emoji=:rocket:"), expectation{title: "Release evidence", thread: thread, context: ":rocket: _Sent via slick (Release helper)_", comment: "Review complete", blocks: true, attribution: true}},
						{"remove title thread and comment", []string{"--no-attribution"}, expectation{}},
						{"message overrides label", []string{"--attribution", "--attribution-label=Release helper", "--attribution-message=Prepared by release automation"}, expectation{context: ":robot_face: _Prepared by release automation_", blocks: true, attribution: true}},
						{"raw comment", append(flag("--message", "-m", `[{"type":"section","text":{"type":"plain_text","text":"Release ready"}}]`), "--no-attribution"), expectation{blocks: true, comment: "Release ready", raw: true}},
						{"remove raw comment and attribution", nil, expectation{}},
						{"disable in CI", []string{"--no-attribution"}, expectation{}},
						{"presentation does not enable attribution", []string{"--attribution-label=Release helper", "--attribution-emoji=:rocket:", "--attribution-message=Prepared by release automation"}, expectation{}},
					}
					for index, scenario := range scenarios {
						mu.Lock()
						expected = scenario.want
						mu.Unlock()
						args := []string{"file", "upload", "--no-throttle", "--output=" + mode, "--color=never", "--timeout=3s"}
						args = append(args, flag("--channel", "-c", "C123")...)
						args = append(args, flag("--file", "-f", "-")...)
						args = append(args, flag("--filename", "-N", "artifact.txt")...)
						args = append(args, scenario.args...)
						if scenario.name == "raw comment" {
							if short {
								args = append(args, "-b")
							} else {
								args = append(args, "--blocks")
							}
						}
						ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
						cmd := exec.CommandContext(ctx, binary, args...)
						cmd.Env = []string{"HOME=" + home, "XDG_CACHE_HOME=" + filepath.Join(home, "cache"), "SLICK_CONFIG=" + cfgPath, "SLACK_CLI_BASE_URL=" + server.URL, "SLICK_FLAGS_FIXTURE_TOKEN=" + token}
						if scenario.name == "disable in CI" {
							cmd.Env = append(cmd.Env, "CI=true")
						}
						cmd.Stdin = strings.NewReader(" \t\r\n")
						var out, diagnostic bytes.Buffer
						cmd.Stdout, cmd.Stderr = &out, &diagnostic
						err := cmd.Run()
						expired := ctx.Err() != nil
						cancel()
						if err != nil || expired || diagnostic.Len() != 0 || strings.Contains(out.String(), token) {
							t.Fatalf("valid upload flags or whitespace-only bytes were rejected in %s: %v", scenario.name, err)
						}
						if mode == "human" && (!strings.Contains(out.String(), "File uploaded") || !strings.Contains(out.String(), "artifact.txt")) {
							t.Fatal("human upload result was lost")
						}
						if mode != "human" {
							data := out.Bytes()
							if mode == "json" {
								var frame map[string]json.RawMessage
								if err := json.Unmarshal(data, &frame); err != nil || len(frame) != 4 || string(frame["ok"]) != "true" || string(frame["errors"]) != "[]" {
									t.Fatal("upload success changed JSON envelope")
								}
								var meta struct {
									Command, Workspace, Timestamp string
									Exit                          int `json:"exit_code"`
								}
								if err := json.Unmarshal(frame["meta"], &meta); err != nil || meta.Command != "file.upload" || meta.Workspace != "fixture" || meta.Exit != 0 {
									t.Fatal("upload success lost common metadata")
								}
								if _, err := time.Parse(time.RFC3339, meta.Timestamp); err != nil {
									t.Fatal("upload metadata timestamp is malformed")
								}
								data = frame["data"]
							}
							var result struct {
								Attribution bool `json:"attribution"`
								File        struct {
									Title, Name, ID string
									Size            int
								} `json:"file"`
							}
							if err := json.Unmarshal(data, &result); err != nil || result.Attribution != scenario.want.attribution || result.File.Title != scenario.want.title || result.File.Name != "artifact.txt" || result.File.ID != fmt.Sprintf("F%d", index+1) || result.File.Size != 4 {
								t.Fatal("upload result lost flag values or byte count")
							}
						}
						mu.Lock()
						got := counts
						mu.Unlock()
						want := [3]int{index + 1, index + 1, index + 1}
						if got != want {
							t.Fatal("upload replayed or skipped a stage")
						}
					}
					for _, invalid := range [][]string{{"--attribution", "--no-attribution"}, {"--attribution=false"}, {"--no-attribution=true"}, {"--attribution-message"}} {
						mu.Lock()
						before := requests
						mu.Unlock()
						args := []string{"file", "upload", "--channel=C123", "--file=-", "--filename=artifact.txt", "--output=" + mode, "--color=never", "--no-throttle"}
						args = append(args, invalid...)
						ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
						cmd := exec.CommandContext(ctx, binary, args...)
						cmd.Env = []string{"HOME=" + home, "XDG_CACHE_HOME=" + filepath.Join(home, "cache"), "SLICK_CONFIG=" + cfgPath, "SLACK_CLI_BASE_URL=" + server.URL, "SLICK_FLAGS_FIXTURE_TOKEN=" + token}
						cmd.Stdin = strings.NewReader(" \t\r\n")
						var out, diagnostic bytes.Buffer
						cmd.Stdout, cmd.Stderr = &out, &diagnostic
						err := cmd.Run()
						expired := ctx.Err() != nil
						cancel()
						if expired || exitCode(err) != 4 {
							t.Fatal("invalid attribution flags were accepted or exceeded the fixture deadline")
						}
						mu.Lock()
						after := requests
						mu.Unlock()
						if after != before {
							t.Fatal("invalid flags reached Slack")
						}
						if strings.Contains(out.String()+diagnostic.String(), token) {
							t.Fatal("invalid flags exposed authentication")
						}
						if mode == "human" {
							if out.Len() != 0 || !strings.Contains(diagnostic.String(), "validation_error") {
								t.Fatal("human flag failure lost stream or class")
							}
						} else {
							var frame map[string]json.RawMessage
							if err := json.Unmarshal(out.Bytes(), &frame); err != nil || len(frame) != 4 || string(frame["ok"]) != "false" || string(frame["data"]) != "null" || diagnostic.Len() != 0 {
								t.Fatal("flag failure lost common machine envelope")
							}
							var failures []struct {
								Type string
								Exit int `json:"exit_code"`
							}
							if err := json.Unmarshal(frame["errors"], &failures); err != nil || len(failures) != 1 || failures[0].Type != "validation_error" || failures[0].Exit != 4 {
								t.Fatal("flag failure lost error classification")
							}
						}
					}
					after, err := os.ReadFile(cfgPath)
					if err != nil || string(after) != cfg {
						t.Fatal("flags changed saved configuration")
					}
				})
			}
		}
	}
}
