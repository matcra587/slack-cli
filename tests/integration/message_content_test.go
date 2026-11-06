//go:build integration

package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestBinaryMessageContentSurvivesSDKProjection(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, route := range []string{"channel", "thread", "nested", "send", "reply"} {
		for _, kind := range []string{"rich", "empty", "missing", "access stub", "malformed files", "permission"} {
			for _, mode := range []string{"json", "compact", "human"} {
				t.Run(route+"/"+kind+"/"+mode, func(t *testing.T) {
					configPath := writeStatusUserConfig(t)
					before, err := os.ReadFile(configPath)
					if err != nil {
						t.Fatal(err)
					}
					var phase, calls atomic.Int32
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						method := strings.TrimPrefix(r.URL.Path, "/api/")
						if method == "chat.getPermalink" {
							if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer xoxb-test" || r.URL.Query().Get("channel") != "C123" || r.URL.Query().Get("message_ts") == "" || r.URL.Query().Has("token") {
								t.Error("content projection changed permalink enrichment authentication/target")
							}
							writeSlackHTTPRecovery(t, w, r, method)
							return
						}
						if err := r.ParseForm(); err != nil {
							t.Error(err)
						}
						if r.Method != http.MethodPost || !strings.HasPrefix(r.URL.Path, "/api/") || r.URL.RawQuery != "" || r.PostForm.Get("token") != "xoxb-test" {
							t.Error("content projection changed SDK authentication or followed a file URL")
						}
						if method == "auth.test" || method == "conversations.info" {
							writeSlackHTTPRecovery(t, w, r, method)
							return
						}
						if method != "conversations.history" && method != "conversations.replies" && method != "chat.postMessage" {
							t.Error("message projection triggered an unexpected API request or file download")
							w.WriteHeader(404)
							return
						}
						calls.Add(1)
						if r.PostForm.Get("channel") != "C123" {
							t.Error("content projection changed the selected channel")
						}
						if method == "conversations.replies" && r.PostForm.Get("ts") != "1746284500.000001" {
							t.Error("content projection changed the thread target")
						}
						if method == "chat.postMessage" && (r.PostForm.Get("text") != "Requested content" || (route == "reply" && r.PostForm.Get("thread_ts") != "1746284500.000001")) {
							t.Error("content projection changed the outgoing message")
						}
						if route == "nested" && method == "conversations.history" {
							writeJSON(w, `{"ok":true,"messages":[{"type":"message","ts":"1746284500.000001","reply_count":1}]}`)
							return
						}
						selected := kind
						if phase.Load() == 1 {
							selected = "rich"
						} else if phase.Load() == 2 {
							selected = "empty"
						}
						if selected == "permission" {
							writeJSON(w, `{"ok":false,"error":"missing_scope","needed":"channels:history"}`)
							return
						}
						message := contentFixtureMessage(selected, "http://"+r.Host+"/private-download")
						if method == "chat.postMessage" {
							writeJSON(w, `{"ok":true,"channel":"C123","ts":"1746284582.123456","message":`+message+`}`)
						} else {
							writeJSON(w, `{"ok":true,"messages":[`+message+`],"response_metadata":{"next_cursor":"opaque-next"}}`)
						}
					}))
					defer server.Close()
					args := []string{"history", "list", "--channel=C123"}
					switch route {
					case "thread":
						args = append(args, "--thread=1746284500.000001")
					case "nested":
						args = append(args, "--include-replies", "--reply-limit=1")
					case "send":
						args = []string{"message", "send", "--channel=C123", "--message=Requested content", "--no-attribution"}
					case "reply":
						args = []string{"reply", "--channel=C123", "--parent=1746284500.000001", "--message=Requested content", "--no-attribution"}
					}
					args = append(args, "--output="+mode, "--color=never", "--no-throttle", "--timeout=2s")
					perCall := int32(1)
					if route == "nested" {
						perCall = 2
					}
					for step, selected := range []string{kind, "rich", "empty"} {
						phase.Store(int32(step))
						out, diagnostic, err := runSlackBinary(t, binary, configPath, server.URL, "", args...)
						wantExit := 0
						switch selected {
						case "malformed files":
							wantExit = 5
						case "permission":
							wantExit = 1
						}
						if exitCode(err) != wantExit || calls.Load() != int32(step+1)*perCall {
							t.Fatalf("message content lost failure/recovery or replay guard: exit=%d want=%d calls=%d", exitCode(err), wantExit, calls.Load())
						}
						if strings.Contains(out+diagnostic, "synthetic-unknown-secret") || strings.Contains(out+diagnostic, "xoxb-test") {
							t.Fatal("message content exposed credentials or unreviewed response fields")
						}
						if wantExit != 0 {
							if mode == "human" {
								if out != "" || diagnostic == "" {
									t.Fatal("message content failure used wrong human streams")
								}
							} else {
								var frame map[string]json.RawMessage
								if json.Unmarshal([]byte(out), &frame) != nil || len(frame) != 4 || string(frame["ok"]) != "false" || string(frame["data"]) != "null" || diagnostic != "" {
									t.Fatal("message content failure lost common envelope")
								}
							}
							continue
						}
						if diagnostic != "" {
							t.Fatal("successful message content wrote diagnostics")
						}
						if mode != "human" {
							assertMessageContent(t, out, mode, route, selected)
						}
					}
					after, readErr := os.ReadFile(configPath)
					if readErr != nil || string(before) != string(after) {
						t.Fatal("message content handling changed prior workspace settings")
					}
				})
			}
		}
	}
}

func contentFixtureMessage(kind, downloadURL string) string {
	base := `"type":"message","user":"U123","text":"Review complete","ts":"1746284582.123456"`
	if kind == "rich" {
		base += `,"client_msg_id":"fixture-client-message-id","edited":{"user":"UEDITOR","ts":"1746284590.000001"}`
	}
	switch kind {
	case "empty":
		return `{` + base + `,"attachments":[],"files":[]}`
	case "missing":
		return `{` + base + `}`
	case "malformed files":
		return `{` + base + `,"files":{}}`
	case "access stub":
		return `{` + base + `,"files":[{"id":"FSTUB","file_access":"check_file_info"}]}`
	default:
		return fmt.Sprintf(`{%s,"attachments":[{"title":"Release review","text":"Ready to ship","fallback":"Release review approved","ts":"1746284582.123456","fields":[{"title":"Status","value":"Approved","short":true}],"blocks":[{"type":"section","text":{"type":"mrkdwn","text":"*Approved*"}}],"is_share":true,"files":[{"id":"FFORWARD","name":"forwarded.txt","url_private":%q}],"access_token":"synthetic-unknown-secret"}],"files":[{"id":"F123","name":"résumé.txt","title":"Release notes","mimetype":"text/plain","size":42,"url_private":%q,"last_read":1746284582123,"show_badge":false,"access_token":"synthetic-unknown-secret"}]}`, base, downloadURL, downloadURL)
	}
}

func assertMessageContent(t *testing.T, out, mode, route, kind string) {
	t.Helper()
	data := json.RawMessage(out)
	if mode == "json" {
		var frame struct {
			OK   bool
			Data json.RawMessage
		}
		if json.Unmarshal(data, &frame) != nil || !frame.OK {
			t.Fatal("message content lost success envelope")
		}
		data = frame.Data
	}
	var payload struct {
		Message  json.RawMessage
		Messages []json.RawMessage
	}
	if json.Unmarshal(data, &payload) != nil {
		t.Fatal("message payload is not JSON")
	}
	message := payload.Message
	if route != "send" && route != "reply" {
		if len(payload.Messages) != 1 {
			t.Fatal("message content lost history page")
		}
		message = payload.Messages[0]
		if route == "nested" {
			var parent struct{ Replies []json.RawMessage }
			if json.Unmarshal(message, &parent) != nil || len(parent.Replies) != 1 {
				t.Fatal("message content lost nested reply")
			}
			message = parent.Replies[0]
		}
	}
	var got struct {
		TS          string
		Text        string
		ClientMsgID string `json:"client_msg_id"`
		Edited      *struct {
			User string
			TS   string `json:"ts"`
		}
		Attachments []struct {
			Title, Text, Fallback string
			TS                    json.Number `json:"ts"`
			Blocks                []json.RawMessage
			Fields                []struct{ Title, Value string }
			Files                 []struct{ ID, Name string }
		}
		Files []struct {
			ID, Name, Title string
			URL             string `json:"url_private"`
			LastRead        int64  `json:"last_read"`
			ShowBadge       *bool  `json:"show_badge"`
			Access          string `json:"file_access"`
		}
	}
	if json.Unmarshal(message, &got) != nil || got.TS != "1746284582.123456" || got.Text != "Review complete" {
		t.Fatal("message content changed exact timestamp or server text")
	}
	if kind == "rich" {
		if got.ClientMsgID != "fixture-client-message-id" || got.Edited == nil || got.Edited.User != "UEDITOR" || got.Edited.TS != "1746284590.000001" {
			t.Fatal("message projection lost client identifier or exact edit metadata")
		}
	} else if got.ClientMsgID != "" || got.Edited != nil {
		t.Fatal("removed client identifier or edit metadata persisted in output")
	}
	switch kind {
	case "rich":
		if len(got.Attachments) != 1 || len(got.Files) != 1 {
			t.Fatal("message projection dropped attachments or files")
		}
		a, f := got.Attachments[0], got.Files[0]
		if a.Title != "Release review" || a.Text != "Ready to ship" || a.Fallback != "Release review approved" || a.TS.String() != "1746284582.123456" || len(a.Blocks) != 1 || len(a.Fields) != 1 || a.Fields[0].Value != "Approved" || len(a.Files) != 1 || a.Files[0].ID != "FFORWARD" || a.Files[0].Name != "forwarded.txt" {
			t.Fatal("message projection lost attachment content, forwarded files or timestamp precision")
		}
		if f.ID != "F123" || f.Name != "résumé.txt" || f.Title != "Release notes" || !strings.HasSuffix(f.URL, "/private-download") || f.LastRead != 1746284582123 || f.ShowBadge == nil || *f.ShowBadge {
			t.Fatal("message projection lost typed file data or explicit false value")
		}
	case "access stub":
		if len(got.Files) != 1 || got.Files[0].ID != "FSTUB" || got.Files[0].Access != "check_file_info" {
			t.Fatal("message projection dropped restricted file metadata")
		}
	default:
		if len(got.Attachments) != 0 || len(got.Files) != 0 {
			t.Fatal("removed message attachments or files persisted in output")
		}
	}
}

func TestBinaryMarkdownLinksUseSlackSyntax(t *testing.T) {
	binary := buildSlackBinary(t)
	const source = "**Ready** ~~old~~ [Slack guide](https://docs.slack.dev/) <!channel>"
	const want = "*Ready* ~old~ <https://docs.slack.dev/|Slack guide> &lt;!channel&gt;"
	for _, kind := range []string{"inline send", "file reply", "stdin edit"} {
		t.Run(kind, func(t *testing.T) {
			configPath := writeStatusUserConfig(t)
			var calls atomic.Int32
			method := "chat.postMessage"
			if kind == "stdin edit" {
				method = "chat.update"
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				current := strings.TrimPrefix(r.URL.Path, "/api/")
				if current == "chat.getPermalink" {
					writeSlackHTTPRecovery(t, w, r, current)
					return
				}
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.PostForm.Get("token") != "xoxb-test" {
					t.Error("wrong method or synthetic authentication")
				}
				if current == "auth.test" || current == "conversations.info" {
					writeSlackHTTPRecovery(t, w, r, current)
					return
				}
				calls.Add(1)
				if current != method || r.PostForm.Get("channel") != "C123" {
					t.Error("wrong mutation or channel")
				}
				if kind == "file reply" && r.PostForm.Get("thread_ts") != "1746284500.000001" {
					t.Error("wrong reply parent")
				}
				if kind == "stdin edit" && r.PostForm.Get("ts") != "1746284500.000001" {
					t.Error("wrong edit timestamp")
				}
				var blocks []struct{ Text struct{ Text string } }
				if json.Unmarshal([]byte(r.PostForm.Get("blocks")), &blocks) != nil || len(blocks) != 1 || blocks[0].Text.Text != want {
					t.Error("Markdown was not translated and escaped in the outgoing request")
				}
				writeJSON(w, `{"ok":true,"channel":"C123","ts":"1746284500.000001","message":{"type":"message","user":"U123","ts":"1746284500.000001","text":"posted"}}`)
			}))
			defer server.Close()
			args := []string{"message", "send", "--channel=C123", "--message", source}
			stdin := ""
			switch kind {
			case "file reply":
				path := t.TempDir() + "/message.md"
				if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
					t.Fatal(err)
				}
				args = []string{"reply", "--channel=C123", "--parent=1746284500.000001", "--file", path}
			case "stdin edit":
				args = []string{"message", "edit", "--channel=C123", "--timestamp=1746284500.000001", "--file=-"}
				stdin = source
			}
			args = append(args, "--no-attribution", "--no-throttle", "--output=json", "--timeout=2s")
			out, diagnostic, err := runSlackBinary(t, binary, configPath, server.URL, stdin, args...)
			if exitCode(err) != 0 || diagnostic != "" || calls.Load() != 1 || strings.Contains(out, "xoxb-test") {
				t.Fatalf("Markdown command failed or replayed: exit=%d calls=%d", exitCode(err), calls.Load())
			}
		})
	}
}
