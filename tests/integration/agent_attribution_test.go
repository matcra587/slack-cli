//go:build integration

package integration_test

import (
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

func TestBinaryAgentAttributionAddsContextBlock(t *testing.T) {
	binary := buildSlackBinary(t)
	configPath := writePipeConfig(t)
	server, sawAttribution := attributionServer(t, ":robot_face: _Sent via slick (agent mode)_")
	defer server.Close()

	_, stderr, err := runSlackBinary(t, binary, configPath, server.URL, "hello", "message", "send", "--channel", "C123", "--file", "-")
	if err != nil {
		t.Fatalf("command returned error: %v\nstderr=%s", err, stderr)
	}
	if !sawAttribution.Load() {
		t.Fatal("attribution context block was not sent")
	}
}

func TestBinaryProfileAttributionAddsContextBlockWithoutAgentEnv(t *testing.T) {
	binary := buildSlackBinary(t)
	configPath := writeProfileAttributionConfig(t)
	server, sawAttribution := attributionServer(t, ":rocket: _Sent from profile_")
	defer server.Close()

	stdout, stderr, err := runSlackBinaryWithoutAgentEnv(t, binary, configPath, server.URL, "message", "send", "--channel", "C123", "--message", "hello")
	if err != nil {
		t.Fatalf("command returned error: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !sawAttribution.Load() {
		t.Fatal("profile attribution context block was not sent")
	}
}

// Both startup sources share the same recorded HTTP contract. Handler failures
// return a response so the child can finish and the test goroutine can report.
func attributionServer(t *testing.T, marker string) (*httptest.Server, *atomic.Bool) {
	t.Helper()
	var sawAttribution atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/conversations.info":
			writeJSON(w, `{"ok":true,"channel":{"id":"C123","name":"general"}}`)
		case "/api/auth.test":
			writeJSON(w, `{"ok":true,"user_id":"U123"}`)
		case "/api/chat.postMessage":
			var blocks []map[string]any
			raw := r.FormValue("blocks")
			if err := json.Unmarshal([]byte(raw), &blocks); err != nil {
				t.Errorf("blocks is not JSON: %v", err)
				http.Error(w, "invalid fixture blocks", http.StatusBadRequest)
				return
			}
			if len(blocks) == 0 {
				t.Error("message did not contain any blocks")
				http.Error(w, "empty fixture blocks", http.StatusBadRequest)
				return
			}
			last := blocks[len(blocks)-1]
			if last["type"] == "context" && strings.Contains(raw, marker) {
				sawAttribution.Store(true)
			}
			writeJSON(w, `{"ok":true,"channel":"C123","ts":"1746284582.123456","message":{"type":"message","text":"hello","ts":"1746284582.123456"}}`)
		case "/api/chat.getPermalink":
			writeJSON(w, `{"ok":true,"permalink":"https://example.slack.com/archives/C123/p1746284582123456"}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.Error(w, "unexpected fixture request", http.StatusInternalServerError)
		}
	}))
	return server, &sawAttribution
}

func writeProfileAttributionConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `schema_version = "1"
default_workspace = "default"

[workspaces.default]
name = "default"
team_id = "T123"
token_type = "bot"
token = "env:SLACK_TEST_TOKEN"

[workspaces.default.attribution]
enabled = true
emoji = ":rocket:"
message = "Sent from profile"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func runSlackBinaryWithoutAgentEnv(t *testing.T, binary, configPath, baseURL string, args ...string) (stdoutText, stderrText string, runErr error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.WaitDelay = time.Second
	env := append(isolatedRuntimeEnv(t),
		"SLACK_CLI_CONFIG="+configPath,
		"SLACK_CLI_BASE_URL="+baseURL,
		"SLACK_TEST_TOKEN=xoxb-test",
	)
	cmd.Env = env
	var stdout strings.Builder
	var stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}
