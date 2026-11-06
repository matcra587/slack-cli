//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBinaryReactionWatchApprovalAndFailures(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, scenario := range []struct {
		name, response string
		exit           int
		polls          int32
	}{
		{name: "wait for selected user", response: `{"ok":true,"type":"message","message":{"reactions":[{"name":"+1","count":2,"users":["UOTHER","UAPPROVER"]}]}}`, polls: 2},
		{name: "timeout without match", response: `{"ok":true,"type":"message","message":{"reactions":[]}}`, exit: 7, polls: 1},
		{name: "permission denied", response: `{"ok":false,"error":"missing_scope","needed":"reactions:read","provided":"chat:write"}`, exit: 1, polls: 1},
		{name: "message disappeared", response: `{"ok":false,"error":"message_not_found"}`, exit: 2, polls: 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var polls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/auth.test":
					writeJSON(w, `{"ok":true,"user_id":"USELF"}`)
				case "/api/reactions.get":
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if r.Method != http.MethodPost || r.PostForm.Get("channel") != "C123" || r.PostForm.Get("timestamp") != "1746284582.123456" || r.PostForm.Get("token") != "xoxb-synthetic-watch" || r.PostForm.Get("full") != "true" {
						t.Error("reaction request did not match the SDK request contract")
					}
					if polls.Add(1) == 1 && scenario.polls == 2 {
						writeJSON(w, `{"ok":true,"type":"message","message":{"reactions":[{"name":"+1","count":1,"users":["UOTHER"]}]}}`)
						return
					}
					writeJSON(w, scenario.response)
				case "/api/conversations.info":
					writeJSON(w, `{"ok":true,"channel":{"id":"C123","name":"approval"}}`)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					writeJSON(w, `{"ok":false,"error":"method_not_found"}`)
				}
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
			defer cancel()
			deadline := "150ms"
			if scenario.polls == 2 {
				deadline = "4s"
			}
			cmd := exec.CommandContext(ctx, binary, "react", "watch", "--channel", "C123", "--timestamp", "1746284582.123456", "--emoji", ":thumbsup:", "--user", "UAPPROVER", "--interval", "1s", "--timeout", deadline, "--output=json", "--no-throttle")
			isolated := t.TempDir()
			cmd.Env = []string{"HOME=" + isolated, "XDG_CACHE_HOME=" + filepath.Join(isolated, "cache"), "SLICK_CONFIG=" + writePipeConfig(t), "SLACK_TEST_TOKEN=xoxb-synthetic-watch", "SLACK_CLI_BASE_URL=" + server.URL}
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			if ctx.Err() != nil {
				t.Fatalf("binary exceeded outer test deadline: %v", ctx.Err())
			}
			if exitCode(err) != scenario.exit {
				t.Fatalf("exit=%d, want %d; stdout=%s stderr=%s", exitCode(err), scenario.exit, &stdout, &stderr)
			}
			if stderr.Len() != 0 || strings.Contains(stdout.String(), "xoxb-synthetic-watch") || strings.Count(strings.TrimSpace(stdout.String()), "\n") != 0 {
				t.Fatal("watch must write one credential-free JSON frame to stdout")
			}
			var result struct {
				OK   bool `json:"ok"`
				Meta struct {
					Command  string `json:"command"`
					ExitCode int    `json:"exit_code"`
				} `json:"meta"`
				Data struct {
					Target struct {
						TS string `json:"ts"`
					} `json:"target"`
				} `json:"data"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.OK != (scenario.exit == 0) || result.Meta.ExitCode != scenario.exit || result.Meta.Command != "react.watch" {
				t.Fatalf("unexpected envelope: %s", &stdout)
			}
			if result.OK && result.Data.Target.TS != "1746284582.123456" {
				t.Fatal("timestamp changed in watch result")
			}
			if polls.Load() != scenario.polls {
				t.Fatalf("requests=%d, want %d", polls.Load(), scenario.polls)
			}
		})
	}
}
