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

func TestBinaryReactionListAndPartialFailureContracts(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tc := range []struct {
		name, mode, body string
		args             []string
		exit             int
		calls            int32
	}{
		{name: "unexpected argument", mode: "json", args: []string{"list", "ignored"}, exit: 4},
		{name: "empty complete list", mode: "compact", args: []string{"list"}, calls: 1},
		{name: "partial add", mode: "json", args: []string{"add", "--emoji=rocket,heart,white_check_mark"}, exit: 1, calls: 2},
		{name: "missing mutation acknowledgement", mode: "json", args: []string{"add", "--emoji=rocket"}, body: `{}`, exit: 5, calls: 1},
		{name: "false removal without error", mode: "compact", args: []string{"remove", "--emoji=rocket"}, body: `{"ok":false}`, exit: 5, calls: 1},
		{name: "missing list message", mode: "json", args: []string{"list"}, body: `{"ok":true,"type":"message"}`, exit: 5, calls: 1},
		{name: "missing watch message", mode: "compact", args: []string{"watch", "--emoji=rocket"}, body: `{"ok":true,"type":"message"}`, exit: 5, calls: 1},
		{name: "add conflicting acknowledgement json", mode: "json", args: []string{"add", "--emoji=rocket"}, body: `{"ok":true,"error":"invalid_auth"}`, exit: 5, calls: 1},
		{name: "add conflicting acknowledgement compact", mode: "compact", args: []string{"add", "--emoji=rocket"}, body: `{"ok":true,"error":"invalid_auth"}`, exit: 5, calls: 1},
		{name: "remove conflicting acknowledgement json", mode: "json", args: []string{"remove", "--emoji=rocket"}, body: `{"ok":true,"error":"invalid_auth"}`, exit: 5, calls: 1},
		{name: "remove conflicting acknowledgement compact", mode: "compact", args: []string{"remove", "--emoji=rocket"}, body: `{"ok":true,"error":"invalid_auth"}`, exit: 5, calls: 1},
		{name: "list conflicting acknowledgement json", mode: "json", args: []string{"list"}, body: `{"ok":true,"error":"invalid_auth","type":"message","message":{"reactions":[{"name":"rocket","count":1}]}}`, exit: 5, calls: 1},
		{name: "list conflicting acknowledgement compact", mode: "compact", args: []string{"list"}, body: `{"ok":true,"error":"invalid_auth","type":"message","message":{"reactions":[{"name":"rocket","count":1}]}}`, exit: 5, calls: 1},
		{name: "watch conflicting acknowledgement json", mode: "json", args: []string{"watch", "--emoji=rocket"}, body: `{"ok":true,"error":"invalid_auth","type":"message","message":{"reactions":[{"name":"rocket","count":1}]}}`, exit: 5, calls: 1},
		{name: "watch conflicting acknowledgement compact", mode: "compact", args: []string{"watch", "--emoji=rocket"}, body: `{"ok":true,"error":"invalid_auth","type":"message","message":{"reactions":[{"name":"rocket","count":1}]}}`, exit: 5, calls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const token = "xoxp-synthetic-binary-reaction"
			var calls, authCalls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Method != http.MethodPost || r.PostForm.Get("token") != token || r.URL.RawQuery != "" {
					t.Error("wrong request method or synthetic authentication")
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/auth.test":
					authCalls.Add(1)
					w.Header().Set("X-OAuth-Scopes", "reactions:read,reactions:write")
					writeJSON(w, `{"ok":true,"user_id":"U1"}`)
				case "/api/conversations.info":
					writeJSON(w, `{"ok":true,"channel":{"id":"C123","name":"fixture"}}`)
				case "/api/reactions.get":
					calls.Add(1)
					if r.PostForm.Get("full") != "true" {
						t.Error("reaction list did not request full results")
					}
					if tc.body != "" {
						writeJSON(w, tc.body)
					} else {
						writeJSON(w, `{"ok":true,"type":"message","message":{"ts":"1746284582.123456"}}`)
					}
				case "/api/reactions.remove":
					calls.Add(1)
					writeJSON(w, tc.body)
				case "/api/reactions.add":
					calls.Add(1)
					if r.PostForm.Get("channel") != "C123" || r.PostForm.Get("timestamp") != "1746284582.123456" {
						t.Error("reaction mutation lost its exact target")
					}
					if tc.body != "" {
						writeJSON(w, tc.body)
						return
					}
					switch r.PostForm.Get("name") {
					case "rocket":
						writeJSON(w, `{"ok":true}`)
					case "heart":
						writeJSON(w, `{"ok":false,"error":"missing_scope","needed":"reactions:write"}`)
					default:
						t.Error("unattempted mutation was sent")
						writeJSON(w, `{"ok":true}`)
					}
				default:
					t.Errorf("unexpected path: %s", r.URL.Path)
					writeJSON(w, `{"ok":false,"error":"method_not_found"}`)
				}
			}))
			defer server.Close()
			isolated := t.TempDir()
			configPath := filepath.Join(isolated, "config.toml")
			config := "schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"T123\"\ntoken_type=\"user\"\ntoken=\"env:SLICK_REACTION_FIXTURE_TOKEN\"\n"
			if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			args := append(append([]string{"react"}, tc.args...), "--channel=C123", "--timestamp=1746284582.123456", "--output="+tc.mode, "--no-throttle", "--timeout=8s")
			cmd := exec.CommandContext(ctx, binary, args...)
			cmd.Env = []string{"HOME=" + isolated, "SLICK_CONFIG=" + configPath, "SLICK_REACTION_FIXTURE_TOKEN=" + token, "SLACK_CLI_BASE_URL=" + server.URL}
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			if ctx.Err() != nil || exitCode(err) != tc.exit || stderr.Len() != 0 || strings.Contains(stdout.String(), token) {
				t.Fatalf("wrong error or output: err=%v stdout=%s stderr=%s", err, &stdout, &stderr)
			}
			if calls.Load() != tc.calls || (tc.calls == 0 && authCalls.Load() != 0) {
				t.Fatal("invalid arguments reached Slack or mutation was repeated")
			}
			var frame map[string]json.RawMessage
			if err := json.Unmarshal(stdout.Bytes(), &frame); err != nil {
				t.Fatal(err)
			}
			if tc.exit == 0 {
				if string(frame["reactions"]) != "[]" {
					t.Fatal("empty compact result lost its reactions array")
				}
				return
			}
			if len(frame) != 4 || string(frame["ok"]) != "false" || string(frame["data"]) != "null" {
				t.Fatal("failure did not use the common envelope")
			}
			if strings.Contains(tc.name, "conflicting acknowledgement") {
				var failure struct {
					Meta struct {
						Code int `json:"exit_code"`
					}
					Errors []struct{ Type string }
				}
				if json.Unmarshal(stdout.Bytes(), &failure) != nil || failure.Meta.Code != 5 || len(failure.Errors) != 1 || failure.Errors[0].Type != "server_error" {
					t.Fatal("conflicting acknowledgement lost server-error classification")
				}
				if tc.args[0] == "add" || tc.args[0] == "remove" {
					var details struct {
						Completed   []string `json:"completed_emojis"`
						Attempted   string   `json:"attempted_emoji"`
						Unattempted []string `json:"unattempted_emojis"`
					}
					var errors []struct{ Details json.RawMessage }
					if json.Unmarshal(frame["errors"], &errors) != nil || json.Unmarshal(errors[0].Details, &details) != nil || details.Completed == nil || len(details.Completed) != 0 || details.Unattempted == nil || len(details.Unattempted) != 0 || details.Attempted != "rocket" {
						t.Fatal("conflicting acknowledgement claimed unacknowledged work or lost attempted target")
					}
				}
			}
			if tc.name == "partial add" {
				var errors []struct {
					Details struct {
						Completed   []string `json:"completed_emojis"`
						Attempted   string   `json:"attempted_emoji"`
						Unattempted []string `json:"unattempted_emojis"`
					} `json:"details"`
				}
				if err := json.Unmarshal(frame["errors"], &errors); err != nil {
					t.Fatal(err)
				}
				if len(errors) != 1 || len(errors[0].Details.Completed) != 1 || errors[0].Details.Completed[0] != "rocket" || errors[0].Details.Attempted != "heart" || len(errors[0].Details.Unattempted) != 1 || errors[0].Details.Unattempted[0] != "white_check_mark" {
					t.Fatal("partial mutation lost acknowledged/attempted/unattempted work")
				}
			}
		})
	}
}
