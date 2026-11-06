//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestBinarySlackHTTPFailuresPropagateWithoutUnsafeReplay(t *testing.T) {
	binary := buildSlackBinary(t)
	type httpFailureCase struct {
		name, mode   string
		status, exit int
		headers      []string
		seconds      string
	}
	// Every route keeps a server failure and an unusable rate limit. These
	// sentinels retain header/status propagation at each distinct transport stage;
	// SDK and output tests own the exhaustive header parser and rendering cases.
	readFailures := []httpFailureCase{
		{name: "untrusted delay", mode: "human", status: 429, exit: 3, headers: []string{"synthetic-http-rate-canary"}},
		{name: "duplicate delay", mode: "compact", status: 429, exit: 3, headers: []string{"7", "9"}},
		{name: "duration overflow", mode: "json", status: 429, exit: 3, headers: []string{"10000000000"}, seconds: "10000000000"},
		{name: "unauthorized HTTP", mode: "compact", status: 401, exit: 5},
		{name: "forbidden HTTP", mode: "human", status: 403, exit: 5},
	}
	mutationFailures := []httpFailureCase{
		{name: "untrusted delay", mode: "compact", status: 429, exit: 3, headers: []string{"synthetic-http-rate-canary"}},
		{name: "duplicate delay", mode: "human", status: 429, exit: 3, headers: []string{"7", "9"}},
		{name: "duration overflow", mode: "json", status: 429, exit: 3, headers: []string{"10000000000"}, seconds: "10000000000"},
		{name: "unauthorized HTTP", mode: "human", status: 401, exit: 5},
		{name: "forbidden HTTP", mode: "compact", status: 403, exit: 5},
	}
	preflightFailures := []httpFailureCase{
		{name: "untrusted delay", mode: "human", status: 429, exit: 3, headers: []string{"synthetic-http-rate-canary"}},
		{name: "duplicate delay", mode: "human", status: 429, exit: 3, headers: []string{"7", "9"}},
		{name: "duration overflow", mode: "compact", status: 429, exit: 3, headers: []string{"10000000000"}, seconds: "10000000000"},
		{name: "unauthorized HTTP", mode: "json", status: 401, exit: 5},
		{name: "forbidden HTTP", mode: "human", status: 403, exit: 5},
	}
	transferFailures := []httpFailureCase{
		{name: "untrusted delay", mode: "compact", status: 429, exit: 3, headers: []string{"synthetic-http-rate-canary"}},
		{name: "duplicate delay", mode: "json", status: 429, exit: 3, headers: []string{"7", "9"}},
		{name: "duration overflow", mode: "human", status: 429, exit: 3, headers: []string{"10000000000"}, seconds: "10000000000"},
		{name: "unauthorized HTTP", mode: "compact", status: 401, exit: 5},
		{name: "forbidden HTTP", mode: "json", status: 403, exit: 5},
	}
	completionFailures := []httpFailureCase{
		{name: "untrusted delay", mode: "json", status: 429, exit: 3, headers: []string{"synthetic-http-rate-canary"}},
		{name: "duplicate delay", mode: "human", status: 429, exit: 3, headers: []string{"7", "9"}},
		{name: "duration overflow", mode: "compact", status: 429, exit: 3, headers: []string{"10000000000"}, seconds: "10000000000"},
		{name: "unauthorized HTTP", mode: "human", status: 401, exit: 5},
		{name: "forbidden HTTP", mode: "json", status: 403, exit: 5},
	}
	operations := []struct {
		name, command, method, mode string
		args                        []string
		additional                  []httpFailureCase
	}{
		{"status set", "status.set", "users.profile.set", "json", []string{"status", "set", "--text=PR review"}, nil},
		{"status clear", "status.clear", "users.profile.set", "human", []string{"status", "clear"}, nil},
		{"status preflight", "status.set", "auth.test", "json", []string{"status", "set", "--text=PR review"}, preflightFailures},
		{"history", "history.list", "conversations.history", "json", []string{"history", "list", "--channel=C123"}, readFailures},
		{"thread history", "history.list", "conversations.replies", "compact", []string{"history", "list", "--channel=C123", "--thread=1746284582.123456"}, nil},
		{"channel list", "lookup.channel", "conversations.list", "human", []string{"lookup", "channel"}, nil},
		{"channel info", "lookup.channel", "conversations.info", "compact", []string{"lookup", "channel", "--channel=C123"}, nil},
		{"user list", "lookup.user", "users.list", "json", []string{"lookup", "user"}, nil},
		{"user info", "lookup.user", "users.info", "human", []string{"lookup", "user", "--user=U123"}, nil},
		{"search", "lookup.messages", "search.messages", "compact", []string{"lookup", "messages", "--query=PR review"}, nil},
		{"message send", "message.send", "chat.postMessage", "json", []string{"message", "send", "--channel=C123", "--message=PR review", "--no-attribution"}, mutationFailures},
		{"message edit", "message.edit", "chat.update", "compact", []string{"message", "edit", "--channel=C123", "--timestamp=1746284582.123456", "--message=PR review", "--no-attribution"}, nil},
		{"message delete", "message.delete", "chat.delete", "human", []string{"message", "delete", "--channel=C123", "--timestamp=1746284582.123456", "--force"}, nil},
		{"message schedule", "message.send", "chat.scheduleMessage", "human", []string{"message", "send", "--channel=C123", "--message=PR review", "--schedule=1h", "--no-attribution"}, nil},
		{"scheduled list", "message.scheduled.list", "chat.scheduledMessages.list", "json", []string{"message", "scheduled", "list"}, nil},
		{"scheduled delete", "message.scheduled.delete", "chat.deleteScheduledMessage", "compact", []string{"message", "scheduled", "delete", "--channel=C123", "--scheduled-id=Q123"}, nil},
		{"reply", "reply", "chat.postMessage", "human", []string{"reply", "--channel=C123", "--parent=1746284582.123456", "--message=PR review", "--no-attribution"}, nil},
		{"react add", "react.add", "reactions.add", "compact", []string{"react", "add", "--channel=C123", "--timestamp=1746284582.123456", "--emoji=eyes"}, nil},
		{"react remove", "react.remove", "reactions.remove", "json", []string{"react", "remove", "--channel=C123", "--timestamp=1746284582.123456", "--emoji=eyes"}, nil},
		{"react list", "react.list", "reactions.get", "human", []string{"react", "list", "--channel=C123", "--timestamp=1746284582.123456"}, nil},
		{"react watch", "react.watch", "reactions.get", "compact", []string{"react", "watch", "--channel=C123", "--timestamp=1746284582.123456", "--emoji=eyes"}, nil},
		{"cache channels", "cache.channels", "conversations.list", "json", []string{"cache", "channels", "--refresh"}, nil},
		{"cache users", "cache.users", "users.list", "human", []string{"cache", "users", "--refresh"}, nil},
		{"file upload", "file.upload", "files.getUploadURLExternal", "json", []string{"file", "upload", "--channel=C123", "--no-attribution"}, nil},
		{"file transfer", "file.upload", "/upload", "human", []string{"file", "upload", "--channel=C123", "--no-attribution"}, transferFailures},
		{"file completion", "file.upload", "files.completeUploadExternal", "compact", []string{"file", "upload", "--channel=C123", "--no-attribution"}, completionFailures},
	}
	for _, op := range operations {
		scenarios := []httpFailureCase{
			{name: "missing delay", mode: op.mode, status: 429, exit: 3},
			{name: "unavailable HTTP", mode: op.mode, status: 503, exit: 5},
		}
		scenarios = append(scenarios, op.additional...)
		for _, tc := range scenarios {
			mode := tc.mode
			t.Run(tc.name+"/"+op.name+"/"+mode, func(t *testing.T) {
				path := writeStatusUserConfig(t)
				before, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				file := filepath.Join(t.TempDir(), "fixture.txt")
				content := []byte("Release notes\n")
				if err := os.WriteFile(file, content, 0o600); err != nil {
					t.Fatal(err)
				}
				var repaired atomic.Bool
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					token := r.Form.Get("token")
					if token == "" {
						token = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
					}
					method := strings.TrimPrefix(r.URL.Path, "/api/")
					if method == "chat.getPermalink" {
						if r.Method != http.MethodGet || r.Form.Get("channel") != "C123" || r.Form.Get("message_ts") != "1746284582.123456" || len(r.URL.Query()) != 2 || r.Header.Get("Authorization") != "Bearer xoxb-test" || token != "xoxb-test" {
							t.Error("wrong permalink GET parameters or authentication")
						}
					} else if r.Method != http.MethodPost || r.URL.RawQuery != "" || token != "xoxb-test" {
						t.Error("HTTP failure changed method/path/authentication")
					}

					if method == op.method {
						calls.Add(1)
						if !repaired.Load() {
							for _, value := range tc.headers {
								w.Header().Add("Retry-After", value)
							}
							w.WriteHeader(tc.status)
							_, _ = io.WriteString(w, "synthetic-http-rate-canary")
							return
						}
					}
					writeSlackHTTPRecovery(t, w, r, method)
				}))
				defer server.Close()
				args := append([]string{}, op.args...)
				if strings.HasPrefix(op.name, "file ") {
					args = append(args, "--file="+file)
				}
				args = append(args, "--output="+mode, "--color=never", "--no-throttle", "--timeout=500ms")
				out, diagnostic, err := runSlackBinary(t, binary, path, server.URL, "", args...)
				if exitCode(err) != tc.exit || calls.Load() != 1 {
					t.Fatalf("HTTP failure lost class or replayed: exit=%d want=%d calls=%d", exitCode(err), tc.exit, calls.Load())
				}
				if strings.Contains(out+diagnostic, "synthetic-http-rate-canary") || strings.Contains(out+diagnostic, "xoxb-test") {
					t.Fatal("HTTP error leaked response/header/credentials")
				}
				delay := tc.seconds
				if strconv.IntSize == 32 {
					delay = ""
				}
				if mode == "human" {
					if out != "" || diagnostic == "" {
						t.Fatal("HTTP human failure used wrong streams")
					}
					if delay != "" && !strings.Contains(diagnostic, "retry_after_seconds="+delay) {
						t.Fatal("HTTP human error lost exact retry seconds")
					}
					if delay == "" && strings.Contains(diagnostic, "retry_after_seconds=") {
						t.Fatal("HTTP human error invented retry timing")
					}
				} else {
					var keys map[string]json.RawMessage
					var frame struct {
						OK   bool
						Data json.RawMessage
						Meta struct {
							Command string
							Exit    int `json:"exit_code"`
						}
						Errors []struct {
							Type    string
							Exit    int             `json:"exit_code"`
							Seconds json.RawMessage `json:"retry_after_seconds"`
						}
					}
					kind := "server_error"
					if tc.status == 429 {
						kind = "rate_limit"
					}
					if json.Unmarshal([]byte(out), &keys) != nil || len(keys) != 4 || json.Unmarshal([]byte(out), &frame) != nil || frame.OK || string(frame.Data) != "null" || frame.Meta.Command != op.command || frame.Meta.Exit != tc.exit || len(frame.Errors) != 1 || frame.Errors[0].Type != kind || frame.Errors[0].Exit != tc.exit || string(frame.Errors[0].Seconds) != delay || diagnostic != "" {
						t.Fatal("HTTP error lost common envelope, class or optional retry timing")
					}
				}
				repaired.Store(true)
				out, diagnostic, err = runSlackBinary(t, binary, path, server.URL, "", args...)
				if err != nil || diagnostic != "" || out == "" || calls.Load() != 2 {
					t.Fatalf("HTTP error did not recover exactly once on explicit invocation: %v calls=%d", err, calls.Load())
				}
				if strings.Contains(out, "synthetic-http-rate-canary") || strings.Contains(out, "xoxb-test") {
					t.Fatal("HTTP recovery leaked private fields")
				}
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatal("HTTP failure/recovery changed configuration")
				}
				after, err = os.ReadFile(file)
				if err != nil || !bytes.Equal(content, after) {
					t.Fatal("HTTP failure/recovery changed upload input")
				}
			})
		}
	}
}

func writeSlackHTTPRecovery(t *testing.T, w http.ResponseWriter, r *http.Request, method string) {
	t.Helper()
	switch method {
	case "auth.test":
		w.Header().Set("X-Oauth-Scopes", "users.profile:write,users:read,users:read.email,channels:history,groups:history,im:history,mpim:history,channels:read,groups:read,im:read,mpim:read,reactions:read,reactions:write,chat:write,search:read,files:write,files:read,im:write,mpim:write")
		writeJSON(w, `{"ok":true,"user_id":"U123"}`)
	case "users.profile.set", "reactions.add", "reactions.remove", "chat.deleteScheduledMessage":
		writeJSON(w, `{"ok":true}`)
	case "conversations.history", "conversations.replies":
		writeJSON(w, `{"ok":true,"messages":[]}`)
	case "conversations.list":
		writeJSON(w, `{"ok":true,"channels":[]}`)
	case "conversations.info":
		writeJSON(w, `{"ok":true,"channel":{"id":"C123","name":"fixture","is_channel":true}}`)
	case "users.list":
		writeJSON(w, `{"ok":true,"members":[]}`)
	case "users.info":
		writeJSON(w, `{"ok":true,"user":{"id":"U123","name":"fixture"}}`)
	case "search.messages":
		writeJSON(w, `{"ok":true,"messages":{"matches":[],"paging":{"page":1,"pages":1,"count":20,"total":0},"pagination":{"page":1,"page_count":1,"per_page":20,"total_count":0}}}`)
	case "chat.postMessage", "chat.update", "chat.delete":
		writeJSON(w, `{"ok":true,"channel":"C123","ts":"1746284582.123456","message":{"type":"message","text":"PR review","user":"U123","ts":"1746284582.123456"}}`)
	case "chat.getPermalink":
		writeJSON(w, `{"ok":true,"permalink":"https://fixture.invalid/message"}`)
	case "chat.scheduleMessage":
		writeJSON(w, `{"ok":true,"channel":"C123","scheduled_message_id":"Q123"}`)
	case "chat.scheduledMessages.list":
		writeJSON(w, `{"ok":true,"scheduled_messages":[]}`)
	case "reactions.get":
		writeJSON(w, `{"ok":true,"type":"message","message":{"ts":"1746284582.123456","text":"PR review","reactions":[{"name":"eyes","count":1,"users":["U123"]}]}}`)
	case "files.getUploadURLExternal":
		writeJSON(w, `{"ok":true,"file_id":"F1","upload_url":"http://`+r.Host+`/upload"}`)
	case "files.completeUploadExternal":
		writeJSON(w, `{"ok":true,"files":[{"id":"F1","title":"Release notes"}]}`)
	case "files.info":
		writeJSON(w, `{"ok":true,"file":{"id":"F1","name":"fixture.txt","shares":{"public":{"C123":[{"ts":"1746284582.123456"}]}}}}`)
	case "/upload":
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		body, err := io.ReadAll(file)
		_ = file.Close()
		if err != nil || string(body) != "Release notes\n" {
			t.Error("HTTP recovery upload lost its input")
		}
		_, _ = io.WriteString(w, "OK")
	default:
		t.Errorf("unexpected HTTP recovery request: %s", method)
		w.WriteHeader(404)
	}
}
