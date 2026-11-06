//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestBinaryMessageEditUsesSDKTextIncludingEmpty(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tc := range []struct{ name, body, want string }{
		{"empty", `{"ok":true,"channel":"C123","ts":"1746284582.000001","text":""}`, ""},
		{"normalized", `{"ok":true,"channel":"C123","ts":"1746284582.000001","text":"Server normalized"}`, "Server normalized"},
		{"missing SDK zero", `{"ok":true,"channel":"C123","ts":"1746284582.000001"}`, ""},
		{"whitespace", `{"ok":true,"channel":"C123","ts":"1746284582.000001","text":" "}`, " "},
	} {
		for _, mode := range []string{"json", "compact", "human"} {
			for _, route := range []string{"message", "stdin"} {
				t.Run(tc.name+"/"+mode+"/"+route, func(t *testing.T) {
					path := writeStatusUserConfig(t)
					prior, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					var calls atomic.Int32
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if err := r.ParseForm(); err != nil {
							t.Error(err)
						}
						method := strings.TrimPrefix(r.URL.Path, "/api/")
						if method == "chat.update" {
							calls.Add(1)
							if r.Method != http.MethodPost || r.PostForm.Get("token") != "xoxb-test" || r.PostForm.Get("channel") != "C123" || r.PostForm.Get("ts") != "1746284582.000001" || r.PostForm.Get("text") != "Requested fixture text" {
								t.Error("edit result handling changed request/auth/target/content")
							}
							writeJSON(w, tc.body)
							return
						}
						if method != "auth.test" && method != "conversations.info" {
							t.Error("edit result triggered an unexpected request")
							w.WriteHeader(404)
							return
						}
						writeSlackHTTPRecovery(t, w, r, method)
					}))
					defer server.Close()
					args := []string{"message", "edit", "--channel=C123", "--timestamp=1746284582.000001", "--no-attribution", "--output=" + mode, "--color=never", "--no-throttle", "--timeout=2s"}
					stdin := ""
					if route == "message" {
						args = append(args, "--message=Requested fixture text")
					} else {
						args = append(args, "--file=-")
						stdin = "Requested fixture text"
					}
					out, diagnostic, err := runSlackBinary(t, binary, path, server.URL, stdin, args...)
					if err != nil || diagnostic != "" || calls.Load() != 1 {
						t.Fatalf("edit result failed or replayed: %v", err)
					}
					if strings.Contains(out, "Requested fixture text") {
						t.Fatal("edit result substituted requested text for the SDK result")
					}
					if mode != "human" {
						data := json.RawMessage(out)
						if mode == "json" {
							var frame struct {
								OK   bool
								Data json.RawMessage
								Meta struct{ Command string }
							}
							if json.Unmarshal(data, &frame) != nil || !frame.OK || frame.Meta.Command != "message.edit" {
								t.Fatal("edit text lost success envelope")
							}
							data = frame.Data
						}
						var got struct {
							Message struct {
								Text *string
								TS   string
							}
						}
						if json.Unmarshal(data, &got) != nil || got.Message.Text == nil || *got.Message.Text != tc.want || got.Message.TS != "1746284582.000001" {
							t.Fatal("edit result lost exact SDK text or timestamp")
						}
					}
					args = append(args, "--dry-run")
					out, diagnostic, err = runSlackBinary(t, binary, path, server.URL, stdin, args...)
					if err != nil || diagnostic != "" || calls.Load() != 1 {
						t.Fatal("edit preview failed or accessed Slack")
					}
					if mode == "human" {
						if !strings.Contains(out, "dry_run=true") {
							t.Fatal("human edit preview lost dry-run marker")
						}
					} else {
						data := json.RawMessage(out)
						if mode == "json" {
							var frame struct{ Data json.RawMessage }
							if json.Unmarshal(data, &frame) != nil {
								t.Fatal("edit preview lost envelope")
							}
							data = frame.Data
						}
						var preview struct {
							Message struct{ Text string }
							DryRun  bool `json:"dry_run"`
						}
						if json.Unmarshal(data, &preview) != nil || !preview.DryRun || preview.Message.Text != "Requested fixture text" {
							t.Fatal("edit preview lost requested text")
						}
					}
					after, err := os.ReadFile(path)
					if err != nil || !bytes.Equal(prior, after) {
						t.Fatal("edit result handling changed configuration")
					}
				})
			}
		}
	}
}
