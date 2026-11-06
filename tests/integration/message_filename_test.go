//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestBinaryDeprecatedMessageFilenameOnlyWarns(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, mode := range []string{"json", "compact", "human"} {
		for _, flag := range []string{"--filename", "-N", "removed"} {
			t.Run(mode+"/"+flag, func(t *testing.T) {
				const private = "synthetic-unreviewed-compatibility-value"
				path := writePipeConfig(t)
				before, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var sends atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/api/chat.getPermalink" {
						writeJSON(w, `{"ok":false,"error":"missing_scope"}`)
						return
					}
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if r.Method != http.MethodPost || r.PostForm.Get("token") != "xoxb-test" {
						t.Error("compatibility flag changed authentication or method")
					}
					switch r.URL.Path {
					case "/api/auth.test":
						w.Header().Set("X-OAuth-Scopes", "chat:write")
						writeJSON(w, `{"ok":true,"user_id":"U_SELF"}`)
					case "/api/conversations.info":
						writeJSON(w, `{"ok":false,"error":"missing_scope"}`)
					case "/api/chat.postMessage":
						sends.Add(1)
						if r.PostForm.Has("filename") || r.PostForm.Get("channel") != "C_TARGET" || r.PostForm.Get("text") != "Release review ready" {
							t.Error("compatibility flag changed body, target or request fields")
						}
						writeJSON(w, `{"ok":true,"channel":"C_TARGET","ts":"1746284582.000001","message":{"text":"Release review ready","ts":"1746284582.000001"}}`)
					default:
						t.Errorf("unexpected compatibility request %s", r.URL.Path)
					}
				}))
				defer server.Close()
				args := []string{"message", "send", "--channel", "C_TARGET", "--message", "Release review ready", "--no-attribution", "--no-throttle", "--color=never", "--output=" + mode}
				if flag != "removed" {
					args = append(args, flag, private)
				}
				out, diagnostic, err := runSlackBinary(t, binary, path, server.URL, "", args...)
				if err != nil || sends.Load() != 1 {
					t.Fatalf("compatibility invocation failed or replayed: err=%v", err)
				}
				wantWarning := "Flag --filename has been deprecated, message bodies have no filename; use file upload --filename for attachments\n"
				if flag == "removed" {
					wantWarning = ""
				}
				if diagnostic != wantWarning {
					t.Error("compatibility warning escaped stderr or survived flag removal")
				}
				if mode != "human" {
					var data map[string]any
					if err := json.Unmarshal([]byte(out), &data); err != nil {
						t.Fatal(err)
					}
					if mode == "json" && (data["ok"] != true || data["meta"].(map[string]any)["command"] != "message.send") {
						t.Error("compatibility warning corrupted success envelope")
					}
				}
				if strings.Contains(out+diagnostic, private) || strings.Contains(out+diagnostic, "xoxb-test") {
					t.Error("compatibility option exposed its unused value or credential")
				}
				after, err := os.ReadFile(path)
				if err != nil || string(after) != string(before) {
					t.Error("compatibility option changed saved config")
				}
			})
		}
	}
}
