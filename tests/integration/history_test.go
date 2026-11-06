//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestBinaryHistoryTimePaginationAndInputValidation(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tc := range []struct {
		name, mode, method, body, nextKey string
		args                              []string
		exit                              int
		requests                          int32
	}{
		{name: "channel time continuation", mode: "json", method: "conversations.history", body: `{"ok":true,"has_more":true,"messages":[{"type":"message","user":"UOTHER","ts":"1746284582.123456"}]}`, nextKey: "next_until", requests: 1},
		{name: "thread time continuation", mode: "compact", method: "conversations.replies", body: `{"ok":true,"has_more":true,"messages":[{"type":"message","user":"UOTHER","ts":"1746284582.123456"}]}`, nextKey: "next_since", args: []string{"--thread=1746284500.000001"}, requests: 1},
		{name: "missing page", mode: "json", method: "conversations.history", body: `{"ok":true}`, exit: 5, requests: 1},
		{name: "invalid page size", mode: "compact", args: []string{"--max-items=-1"}, exit: 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const token = "xoxp-synthetic-binary-history"
			var requests, authRequests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.PostForm.Get("token") != token {
					t.Error("wrong method or synthetic authentication")
				}
				switch r.URL.Path {
				case "/api/auth.test":
					authRequests.Add(1)
					w.Header().Set("X-OAuth-Scopes", "channels:history")
					writeJSON(w, `{"ok":true,"user_id":"U1"}`)
				case "/api/conversations.info":
					writeJSON(w, `{"ok":true,"channel":{"id":"C123","name":"fixture"}}`)
				default:
					if r.URL.Path != "/api/"+tc.method {
						t.Errorf("unexpected path: %s", r.URL.Path)
						writeJSON(w, `{"ok":false,"error":"method_not_found"}`)
						return
					}
					requests.Add(1)
					if r.PostForm.Get("channel") != "C123" || (tc.method == "conversations.replies" && r.PostForm.Get("ts") != "1746284500.000001") {
						t.Error("wrong history target")
					}
					writeJSON(w, tc.body)
				}
			}))
			defer server.Close()
			isolated := t.TempDir()
			configPath := filepath.Join(isolated, "config.toml")
			config := "schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"T123\"\ntoken_type=\"user\"\ntoken=\"env:SLICK_HISTORY_FIXTURE_TOKEN\"\n"
			if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			args := append([]string{"history", "list", "--channel=C123", "--user=USELECTED", "--output=" + tc.mode, "--no-throttle", "--timeout=8s"}, tc.args...)
			cmd := exec.CommandContext(ctx, binary, args...)
			cmd.Env = []string{"HOME=" + isolated, "SLICK_CONFIG=" + configPath, "SLICK_HISTORY_FIXTURE_TOKEN=" + token, "SLACK_CLI_BASE_URL=" + server.URL}
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			if ctx.Err() != nil || exitCode(err) != tc.exit || stderr.Len() != 0 || strings.Contains(stdout.String(), token) {
				t.Fatalf("wrong failure or output: err=%v stdout=%s stderr=%s", err, &stdout, &stderr)
			}
			if requests.Load() != tc.requests || (tc.requests == 0 && authRequests.Load() != 0) {
				t.Fatal("invalid input reached Slack or a history request was replayed")
			}
			var frame map[string]json.RawMessage
			if err := json.Unmarshal(stdout.Bytes(), &frame); err != nil {
				t.Fatal(err)
			}
			if tc.exit != 0 {
				if len(frame) != 4 || string(frame["ok"]) != "false" || string(frame["data"]) != "null" {
					t.Fatal("invalid page lost the common failure envelope")
				}
				return
			}
			data := stdout.Bytes()
			if tc.mode == "json" {
				if len(frame) != 4 || string(frame["ok"]) != "true" {
					t.Fatal("success lost the common envelope")
				}
				data = frame["data"]
			}
			var result struct {
				Messages   []json.RawMessage          `json:"messages"`
				Pagination map[string]json.RawMessage `json:"pagination"`
			}
			if err := json.Unmarshal(data, &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Messages) != 0 || string(result.Pagination["has_more"]) != "true" || string(result.Pagination[tc.nextKey]) != `"1746284582.123456"` || string(result.Pagination["items_returned"]) != "0" {
				t.Fatal("filtered binary output lost its time boundary or zero count")
			}
		})
	}
}
