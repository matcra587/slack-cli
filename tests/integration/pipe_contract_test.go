package integration_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPipeContractForMessageEditFileUploadAndHistory(t *testing.T) {
	binary := buildSlackBinary(t)
	server := pipeMockSlackServer(t)
	defer server.Close()
	configPath := writePipeConfig(t)

	tests := []struct {
		name   string
		args   []string
		stdin  string
		stderr bool
	}{
		{name: "send stdin", args: []string{"message", "send", "--channel", "C123", "--file", "-"}, stdin: "hello"},
		{name: "edit stdin", args: []string{"message", "edit", "--channel", "C123", "--timestamp", "1746284582.123456", "--file", "-"}, stdin: "updated"},
		{name: "file upload stdin", args: []string{"file", "upload", "--channel", "C123", "--file", "-", "--filename", "stdin.txt"}, stdin: "artifact", stderr: true},
		{name: "history json", args: []string{"history", "list", "--channel", "C123", "--max-items", "1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, err := runSlackBinary(t, binary, configPath, server.URL, tt.stdin, tt.args...)
			if err != nil {
				t.Fatalf("command returned error: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
			}
			var payload map[string]any
			if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
				t.Fatalf("stdout is not JSON data: %v\n%s", err, stdout)
			}
			if tt.stderr && stderr == "" {
				t.Fatalf("stderr = empty, want diagnostics/progress")
			}
		})
	}
}

func buildSlackBinary(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	binary := filepath.Join(t.TempDir(), "slack")
	cmd := exec.Command("go", "build", "-o", binary, "./cmd/slack")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build failed: %v\n%s", err, string(out))
	}
	return binary
}

func runSlackBinary(t *testing.T, binary, configPath, baseURL, stdin string, args ...string) (string, string, error) {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Env = append(os.Environ(),
		"SLACK_CLI_CONFIG="+configPath,
		"SLACK_CLI_BASE_URL="+baseURL,
		"SLACK_TEST_TOKEN=xoxb-test",
		"CLAUDE_CODE=1",
	)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func writePipeConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `schema_version = "1"
default_workspace = "default"

[workspaces.default]
name = "default"
team_id = "T123"
token_type = "bot"
token = "env:SLACK_TEST_TOKEN"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func pipeMockSlackServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/chat.postMessage":
			writeJSON(w, `{"ok":true,"channel":"C123","ts":"1746284582.123456","message":{"type":"message","user":"U123","text":"hello","ts":"1746284582.123456"}}`)
		case "/api/chat.getPermalink":
			writeJSON(w, `{"ok":true,"permalink":"https://example.slack.com/archives/C123/p1746284582123456"}`)
		case "/api/auth.test":
			writeJSON(w, `{"ok":true,"user_id":"U123"}`)
		case "/api/conversations.replies":
			writeJSON(w, `{"ok":true,"messages":[{"type":"message","user":"U123","text":"old","ts":"1746284582.123456"}]}`)
		case "/api/chat.update":
			writeJSON(w, `{"ok":true,"channel":"C123","ts":"1746284582.123456","message":{"type":"message","user":"U123","text":"updated","ts":"1746284582.123456"}}`)
		case "/api/files.getUploadURLExternal":
			writeJSON(w, `{"ok":true,"upload_url":"`+"http://"+r.Host+`/upload","file_id":"F123"}`)
		case "/upload":
			_, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusOK)
		case "/api/files.completeUploadExternal":
			writeJSON(w, `{"ok":true,"files":[{"id":"F123","name":"stdin.txt","size":8,"permalink":"https://example.slack.com/files/F123"}]}`)
		case "/api/conversations.history":
			writeJSON(w, `{"ok":true,"messages":[{"type":"message","user":"U123","text":"hello","ts":"1746284582.123456"}]}`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
}

func writeJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return -1
}
