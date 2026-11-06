//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestPipeContractForMessageEditFileUploadAndHistory(t *testing.T) {
	binary := buildSlackBinary(t)
	server := pipeMockSlackServer(t)
	configPath := writePipeConfig(t)

	tests := []struct {
		name   string
		args   []string
		stdin  string
		stderr bool
	}{
		{name: "send stdin", args: []string{"message", "send", "--channel", "C123", "--file", "-"}, stdin: "hello"},
		{name: "send blocks stdin", args: []string{"message", "send", "--channel", "C123", "--file", "-", "--blocks"}, stdin: `[{"type":"section","text":{"type":"mrkdwn","text":"hello"}}]`},
		{name: "edit stdin", args: []string{"message", "edit", "--channel", "C123", "--timestamp", "1746284582.123456", "--file", "-"}, stdin: "updated"},
		{name: "edit blocks stdin", args: []string{"message", "edit", "--channel", "C123", "--timestamp", "1746284582.123456", "--file", "-", "--blocks"}, stdin: `[{"type":"section","text":{"type":"mrkdwn","text":"updated"}}]`},
		{name: "file upload stdin", args: []string{"file", "upload", "--channel", "C123", "--file", "-", "--filename", "stdin.txt"}, stdin: "artifact"},
		{name: "file upload stdin with block comment", args: []string{"file", "upload", "--channel", "C123", "--file", "-", "--filename", "stdin.txt", "--message", `[{"type":"section","text":{"type":"mrkdwn","text":"artifact"}}]`, "--blocks"}, stdin: "artifact"},
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

func TestPipeContractUnknownCommandUsesStructuredValidationError(t *testing.T) {
	binary := buildSlackBinary(t)
	server := pipeMockSlackServer(t)
	configPath := writePipeConfig(t)

	stdout, stderr, err := runSlackBinary(t, binary, configPath, server.URL, "", "dm")
	if err == nil {
		t.Fatal("command returned nil error, want unknown command failure")
	}
	if got := exitCode(err); got != 4 {
		t.Fatalf("exit code = %d, want 4\nstdout=%s", got, stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, `"type":"validation_error"`) || !strings.Contains(stdout, `unknown command \"dm\"`) {
		t.Fatalf("stdout = %q, want structured unknown-command validation error", stdout)
	}
}

func TestPipeContractRejectsInvalidRawBlocksFromStdinBeforeSlackCall(t *testing.T) {
	binary := buildSlackBinary(t)
	server := pipeMockSlackServer(t)
	configPath := writePipeConfig(t)

	stdout, stderr, err := runSlackBinary(t, binary, configPath, server.URL, `[{"type":"section"}]`,
		"message", "send", "--channel", "C123", "--file", "-", "--blocks")
	if err == nil {
		t.Fatal("command returned nil error, want raw Block Kit validation failure")
	}
	if got := exitCode(err); got != 4 {
		t.Fatalf("exit code = %d, want 4\nstdout=%s", got, stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, `"type":"validation_error"`) || !strings.Contains(stdout, "section text or fields are required") {
		t.Fatalf("stdout = %q, want raw Block Kit validation error", stdout)
	}
}

func TestPipeContractPermissionFailuresUseStdoutEnvelope(t *testing.T) {
	binary := buildSlackBinary(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth.test":
			writeJSON(w, `{"ok":true,"user_id":"U123"}`)
		case "/api/chat.postMessage":
			writeJSON(w, `{"ok":false,"error":"not_in_channel"}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.Error(w, "unexpected fixture request", http.StatusInternalServerError)
			return
		}
	}))
	defer server.Close()
	configPath := writePipeConfig(t)

	stdout, stderr, err := runSlackBinary(t, binary, configPath, server.URL, "hello",
		"message", "send", "--channel", "C123", "--file", "-")
	if err == nil {
		t.Fatal("command returned nil error, want Slack permission failure")
	}
	if got := exitCode(err); got != 2 {
		t.Fatalf("exit code = %d, want 2\nstdout=%s", got, stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, `"type":"not_found"`) || !strings.Contains(stdout, "not_in_channel") {
		t.Fatalf("stdout = %q, want fixed not_found error", stdout)
	}
}

func TestBinaryBrokenOutputPipeUsesExitCode8(t *testing.T) {
	binary := buildSlackBinary(t)
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "version", "--output=json")
	cmd.WaitDelay = time.Second
	cmd.Env = isolatedRuntimeEnv(t)
	cmd.Stdout = writer
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("broken-pipe child exceeded its outer deadline: %v", ctx.Err())
	}
	if err := runErr; exitCode(err) != 8 {
		t.Fatalf("error=%v exit=%d stderr=%q; want output failure exit 8", err, exitCode(err), stderr.String())
	}
	if !strings.Contains(stderr.String(), "output_write_failed") {
		t.Fatalf("stderr=%q, want output failure diagnostic", stderr.String())
	}
}

func buildSlackBinary(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	sharedBinary.once.Do(func() {
		sharedBinary.directory, sharedBinary.err = os.MkdirTemp("", "slick-integration-")
		if sharedBinary.err != nil {
			return
		}
		sharedBinary.path = filepath.Join(sharedBinary.directory, "slick")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		args := []string{"build", "-o", sharedBinary.path, "./cmd/slick"}
		if os.Getenv("SLICK_TEST_RACE_BINARY") == "1" {
			args = append([]string{"build", "-race"}, args[1:]...)
		}
		cmd := exec.CommandContext(ctx, "go", args...)
		cmd.Dir = root
		cmd.WaitDelay = 2 * time.Second
		out, err := cmd.CombinedOutput()
		if err != nil {
			sharedBinary.err = fmt.Errorf("build integration binary: %w\n%s", err, out)
		}
	})
	if sharedBinary.err != nil {
		t.Fatal(sharedBinary.err)
	}
	return sharedBinary.path
}

func runSlackBinary(t *testing.T, binary, configPath, baseURL, stdin string, args ...string) (stdoutText, stderrText string, runErr error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.WaitDelay = time.Second
	cmd.Env = append(isolatedRuntimeEnv(t),
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
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/chat.postMessage":
			writeJSON(w, `{"ok":true,"channel":"C123","ts":"1746284582.123456","message":{"type":"message","user":"U123","text":"hello","ts":"1746284582.123456"}}`)
		case "/api/chat.getPermalink":
			writeJSON(w, `{"ok":true,"permalink":"https://example.slack.com/archives/C123/p1746284582123456"}`)
		case "/api/conversations.info":
			writeJSON(w, `{"ok":true,"channel":{"id":"C123","name":"general"}}`)
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
		case "/api/files.info":
			writeJSON(w, `{"ok":true,"file":{"id":"F123","name":"stdin.txt","title":"stdin.txt","size":8,"permalink":"https://example.slack.com/files/F123"}}`)
		case "/api/conversations.history":
			writeJSON(w, `{"ok":true,"messages":[{"type":"message","user":"U123","text":"hello","ts":"1746284582.123456"}]}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.Error(w, "unexpected fixture request", http.StatusInternalServerError)
			return
		}
	}))
	t.Cleanup(s.Close)
	return s
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
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return exitErr.ExitCode()
	}
	return -1
}
