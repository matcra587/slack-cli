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
	if stderr == "" {
		t.Fatalf("stderr = empty, want clog progress")
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
}

func TestFileUploadCommandAppliesAgentAttributionBlocksToUploadMessage(t *testing.T) {
	t.Setenv("CLAUDE_CODE", "1")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
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

func fileUploadServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
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
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"files":[{"id":"F123","name":"` + name + `","size":11,"permalink":"https://example.slack.com/files/F123"}]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
}
