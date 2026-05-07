package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/agent"
)

func TestBinaryAgentAttributionAddsContextBlock(t *testing.T) {
	binary := buildSlackBinary(t)
	configPath := writePipeConfig(t)
	var sawAttribution bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth.test":
			writeJSON(w, `{"ok":true,"user_id":"U123"}`)
		case "/api/chat.postMessage":
			var blocks []map[string]any
			if err := json.Unmarshal([]byte(r.FormValue("blocks")), &blocks); err != nil {
				t.Fatalf("blocks is not JSON: %v", err)
			}
			last := blocks[len(blocks)-1]
			if last["type"] == "context" && strings.Contains(r.FormValue("blocks"), ":robot_face: _Sent via slick (agent mode)_") {
				sawAttribution = true
			}
			writeJSON(w, `{"ok":true,"channel":"C123","ts":"1746284582.123456","message":{"type":"message","text":"hello","ts":"1746284582.123456"}}`)
		case "/api/chat.getPermalink":
			writeJSON(w, `{"ok":true,"permalink":"https://example.slack.com/archives/C123/p1746284582123456"}`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	_, stderr, err := runSlackBinary(t, binary, configPath, server.URL, "hello", "message", "send", "--channel", "C123", "--file", "-")
	if err != nil {
		t.Fatalf("command returned error: %v\nstderr=%s", err, stderr)
	}
	if !sawAttribution {
		t.Fatal("attribution context block was not sent")
	}
}

func TestBinaryProfileAttributionAddsContextBlockWithoutAgentEnv(t *testing.T) {
	binary := buildSlackBinary(t)
	configPath := writeProfileAttributionConfig(t)
	var sawAttribution bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth.test":
			writeJSON(w, `{"ok":true,"user_id":"U123"}`)
		case "/api/chat.postMessage":
			var blocks []map[string]any
			if err := json.Unmarshal([]byte(r.FormValue("blocks")), &blocks); err != nil {
				t.Fatalf("blocks is not JSON: %v", err)
			}
			last := blocks[len(blocks)-1]
			if last["type"] == "context" && strings.Contains(r.FormValue("blocks"), ":rocket: _Sent from profile_") {
				sawAttribution = true
			}
			writeJSON(w, `{"ok":true,"channel":"C123","ts":"1746284582.123456","message":{"type":"message","text":"hello","ts":"1746284582.123456"}}`)
		case "/api/chat.getPermalink":
			writeJSON(w, `{"ok":true,"permalink":"https://example.slack.com/archives/C123/p1746284582123456"}`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	stdout, stderr, err := runSlackBinaryWithoutAgentEnv(t, binary, configPath, server.URL, "message", "send", "--channel", "C123", "--message", "hello")
	if err != nil {
		t.Fatalf("command returned error: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if !sawAttribution {
		t.Fatal("profile attribution context block was not sent")
	}
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
	cmd := exec.Command(binary, args...)
	env := make([]string, 0, len(os.Environ())+3)
	skip := map[string]bool{}
	for _, key := range agent.KnownEnvVars() {
		skip[key] = true
	}
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if !skip[key] {
			env = append(env, item)
		}
	}
	env = append(env,
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
