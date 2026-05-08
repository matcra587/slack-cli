package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func TestFileUploadCommandUploadsPathAndWritesJSON(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(filePath, []byte("hello world"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	server := fileUploadServer(t)
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.URL,
		"",
		[]string{"file", "upload", "--channel", "C123", "--file", filePath},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"id":"F123"`) {
		t.Fatalf("stdout = %s, want file metadata", stdout)
	}
	if !strings.Contains(stdout, `"permalink":"https://example.slack.com/files/F123"`) {
		t.Fatalf("stdout = %s, want file permalink metadata", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty (upload progress is debug-level only)", stderr)
	}
}

func TestFileUploadCommandExpandsFilePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	filePath := filepath.Join(home, "report.txt")
	if err := os.WriteFile(filePath, []byte("hello world"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	server := fileUploadServer(t)
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.URL,
		"",
		[]string{"file", "upload", "--channel", "C123", "--file", "~/report.txt"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"id":"F123"`) {
		t.Fatalf("stdout = %s, want file metadata", stdout)
	}
}

func TestFileUploadCommandReadsStdinWithFilename(t *testing.T) {
	server := fileUploadServer(t)
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.URL,
		"hello world",
		[]string{"file", "upload", "--channel", "C123", "--file", "-", "--filename", "stdin.txt"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"name":"stdin.txt"`) {
		t.Fatalf("stdout = %s, want stdin filename", stdout)
	}
	if !strings.Contains(stdout, `"permalink":"https://example.slack.com/files/F123"`) {
		t.Fatalf("stdout = %s, want file permalink metadata", stdout)
	}
}

func TestFileUploadCommandDryRunSkipsSlackUpload(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(filePath, []byte("hello world"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	server := fileUploadServer(t)
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.URL,
		"",
		[]string{"file", "upload", "--channel", "C123", "--file", filePath, "--dry-run"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	if strings.Contains(stderr, "unexpected path") {
		t.Fatalf("stderr = %s, want no Slack upload request", stderr)
	}
	if !strings.Contains(stdout, `"dry_run":true`) || !strings.Contains(stdout, `"id":"dry-run"`) {
		t.Fatalf("stdout = %s, want dry-run upload preview", stdout)
	}
}

func TestFileUploadCommandAppliesAgentAttributionBlocksToUploadMessage(t *testing.T) {
	t.Setenv("CLAUDE_CODE", "1")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth.test":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"user_id":"U123"}`))
		case "/api/files.getUploadURLExternal":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"upload_url":"` + "http://" + r.Host + `/upload","file_id":"F123"}`))
		case "/upload":
			_, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusOK)
		case "/api/files.completeUploadExternal":
			blocksValue := r.FormValue("blocks")
			if blocksValue == "" {
				t.Fatalf("blocks form value is empty, want Block Kit upload comment with attribution")
			}
			var blocks []map[string]any
			if err := json.Unmarshal([]byte(blocksValue), &blocks); err != nil {
				t.Fatalf("blocks form value is not JSON: %v", err)
			}
			if len(blocks) != 2 || blocks[1]["type"] != "context" {
				t.Fatalf("blocks = %#v, want message block plus attribution context", blocks)
			}
			if got := r.FormValue("initial_comment"); got != "" {
				t.Fatalf("initial_comment = %q, want empty because Slack ignores blocks when it is set", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"files":[{"id":"F123","name":"stdin.txt","size":11,"permalink":"https://example.slack.com/files/F123"}]}`))
		case "/api/files.info":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"file":{"id":"F123","name":"stdin.txt","title":"stdin.txt","size":11,"permalink":"https://example.slack.com/files/F123"}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	_, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.URL,
		"hello world",
		[]string{"file", "upload", "--channel", "C123", "--file", "-", "--filename", "stdin.txt", "--message", "Build artifact"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
}

func TestFileUploadCommandSupportsBlockInputForUploadMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth.test":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"user_id":"U123"}`))
		case "/api/files.getUploadURLExternal":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"upload_url":"` + "http://" + r.Host + `/upload","file_id":"F123"}`))
		case "/upload":
			_, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusOK)
		case "/api/files.completeUploadExternal":
			var blocks []map[string]any
			if err := json.Unmarshal([]byte(r.FormValue("blocks")), &blocks); err != nil {
				t.Fatalf("blocks form value is not JSON: %v", err)
			}
			text := blocks[0]["text"].(map[string]any)
			if text["text"] != "upload block" {
				t.Fatalf("block text = %q, want upload block", text["text"])
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"files":[{"id":"F123","name":"stdin.txt","size":11,"permalink":"https://example.slack.com/files/F123"}]}`))
		case "/api/files.info":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"file":{"id":"F123","name":"stdin.txt","title":"stdin.txt","size":11,"permalink":"https://example.slack.com/files/F123"}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	_, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.URL,
		"hello world",
		[]string{"file", "upload", "--channel", "C123", "--file", "-", "--filename", "stdin.txt", "--blocks", "--message", `[{"type":"section","text":{"type":"mrkdwn","text":"upload block"}}]`},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
}

func TestFileUploadCommandRejectsMalformedBlockComment(t *testing.T) {
	server := fileUploadServer(t)
	defer server.Close()

	_, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.URL,
		"hello world",
		[]string{"file", "upload", "--channel", "C123", "--file", "-", "--filename", "stdin.txt", "--blocks", "--message", `not-json`},
	)
	if err == nil {
		t.Fatal("Execute returned nil error, want malformed block validation error")
	}
	if !strings.Contains(stderr, `"type":"validation_error"`) {
		t.Fatalf("stderr = %s, want validation_error", stderr)
	}
}

func TestFileUploadCommandPreservesUnsupportedMarkdownSourceFallbackInComment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth.test":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"user_id":"U123"}`))
		case "/api/files.getUploadURLExternal":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"upload_url":"` + "http://" + r.Host + `/upload","file_id":"F123"}`))
		case "/upload":
			_, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusOK)
		case "/api/files.completeUploadExternal":
			var blocks []map[string]any
			if err := json.Unmarshal([]byte(r.FormValue("blocks")), &blocks); err != nil {
				t.Fatalf("blocks form value is not JSON: %v", err)
			}
			if got := rawSectionText(t, blocks[0]); got != "- artifact\n- report" {
				t.Fatalf("block text = %q, want source-preserving list", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"files":[{"id":"F123","name":"stdin.txt","size":11,"permalink":"https://example.slack.com/files/F123"}]}`))
		case "/api/files.info":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"file":{"id":"F123","name":"stdin.txt","title":"stdin.txt","size":11,"permalink":"https://example.slack.com/files/F123"}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	_, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.URL,
		"hello world",
		[]string{"file", "upload", "--channel", "C123", "--file", "-", "--filename", "stdin.txt", "--message", "- artifact\n- report\n"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
}

func TestFileUploadCommandRejectsInvalidRawBlockCommentBeforeSlackRequest(t *testing.T) {
	server := fileUploadServer(t)
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.URL,
		"hello world",
		[]string{"file", "upload", "--channel", "C123", "--file", "-", "--filename", "stdin.txt", "--blocks", "--message", `[{"type":"file","external_id":"F123"}]`},
	)
	if err == nil {
		t.Fatal("Execute returned nil error, want raw block validation error")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	for _, path := range []string{"auth.test", "files.getUploadURLExternal", "files.completeUploadExternal"} {
		if got := strings.Count(stderr, path); got > 0 {
			t.Fatalf("stderr = %s, want no Slack upload call mentioning %s", stderr, path)
		}
	}
	if !strings.Contains(stderr, `"type":"validation_error"`) || !strings.Contains(stderr, "file external_id and source are required") {
		t.Fatalf("stderr = %s, want file validation error", stderr)
	}
}

func fileUploadServer(t *testing.T) *httptest.Server {
	t.Helper()
	uploadedName := "report.txt"
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth.test":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"user_id":"U123"}`))
		case "/api/files.getUploadURLExternal":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"upload_url":"` + "http://" + r.Host + `/upload","file_id":"F123"}`))
		case "/upload":
			_, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusOK)
		case "/api/files.completeUploadExternal":
			name := "report.txt"
			if strings.Contains(r.FormValue("files"), "stdin.txt") {
				name = "stdin.txt"
			}
			uploadedName = name
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"files":[{"id":"F123","name":"` + name + `","size":11,"permalink":"https://example.slack.com/files/F123"}]}`))
		case "/api/files.info":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"file":{"id":"F123","name":"` + uploadedName + `","title":"` + uploadedName + `","size":11,"permalink":"https://example.slack.com/files/F123"}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
}
