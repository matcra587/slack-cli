package file_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/matcra587/slack-cli/internal/agent"
	clifile "github.com/matcra587/slack-cli/internal/cli/file"
	"github.com/matcra587/slack-cli/internal/cli/runtime/runtimetest"
	"github.com/matcra587/slack-cli/internal/config"
)

func TestMain(m *testing.M) {
	for _, key := range agent.KnownEnvVars() {
		_ = os.Unsetenv(key)
	}
	os.Exit(m.Run())
}

func TestFileUploadCommandUploadsPathAndWritesJSON(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(filePath, []byte("hello world"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	server := fileUploadServer(t)

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
		case "/api/conversations.info":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"channel":{"id":"C123","name":"alerts","is_channel":true}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	_, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.URL,
		"hello world",
		[]string{"file", "upload", "--channel", "C123", "--file", "-", "--filename", "stdin.txt", "--message", "Build artifact"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
}

func TestFileUploadCommandHonorsNoAttribution(t *testing.T) {
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
			if blocksValue := r.FormValue("blocks"); blocksValue != "" {
				var blocks []map[string]any
				if err := json.Unmarshal([]byte(blocksValue), &blocks); err != nil {
					t.Fatalf("blocks form value is not JSON: %v", err)
				}
				for _, block := range blocks {
					if block["type"] == "context" {
						t.Fatalf("--no-attribution did not suppress context block: %#v", blocks)
					}
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"files":[{"id":"F123","name":"stdin.txt","size":11,"permalink":"https://example.slack.com/files/F123"}]}`))
		case "/api/files.info":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"file":{"id":"F123","name":"stdin.txt","title":"stdin.txt","size":11,"permalink":"https://example.slack.com/files/F123"}}`))
		case "/api/conversations.info":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"channel":{"id":"C123","name":"alerts","is_channel":true}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	_, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.URL,
		"hello world",
		[]string{"file", "upload", "--channel", "C123", "--file", "-", "--filename", "stdin.txt", "--message", "Build artifact", "--no-attribution"},
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
		case "/api/conversations.info":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"channel":{"id":"C123","name":"alerts","is_channel":true}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

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

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.URL,
		"hello world",
		[]string{"file", "upload", "--channel", "C123", "--file", "-", "--filename", "stdin.txt", "--blocks", "--message", `not-json`},
	)
	if stderr != "" {
		t.Fatalf("unexpected machine diagnostics: %s", stderr)
	}
	if err == nil {
		t.Fatal("Execute returned nil error, want malformed block validation error")
	}
	if !strings.Contains(stdout, `"type":"validation_error"`) {
		t.Fatalf("stdout = %s, want validation_error", stdout)
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
		case "/api/conversations.info":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"channel":{"id":"C123","name":"alerts","is_channel":true}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	_, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.URL,
		"hello world",
		[]string{"file", "upload", "--channel", "C123", "--file", "-", "--filename", "stdin.txt", "--message", "- artifact\n- report\n"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
}

func TestFileUploadCommandRejectsInvalidRawBlockCommentBeforeSlackRequest(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.URL,
		"hello world",
		[]string{"file", "upload", "--channel", "C123", "--file", "-", "--filename", "stdin.txt", "--blocks", "--message", `[{"type":"file","external_id":"F123"}]`},
	)
	if err == nil {
		t.Fatal("Execute returned nil error, want raw block validation error")
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("Slack requests = %d, want zero before validation", got)
	}
	if !strings.Contains(stdout, `"type":"validation_error"`) || !strings.Contains(stdout, "file external_id and source are required") {
		t.Fatalf("stdout = %s, want file validation error", stdout)
	}
}

func fileUploadServer(t *testing.T, titles ...string) *httptest.Server {
	t.Helper()
	uploadedName := "report.txt"
	uploadedTitle := ""
	if len(titles) > 0 {
		uploadedTitle = titles[0]
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth.test":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"user_id":"U123"}`))
		case "/api/files.getUploadURLExternal":
			uploadedName = r.FormValue("filename")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"upload_url":"` + "http://" + r.Host + `/upload","file_id":"F123"}`))
		case "/upload":
			_, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusOK)
		case "/api/files.completeUploadExternal":
			name := uploadedName
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"files":[{"id":"F123","name":"` + name + `","title":"` + uploadedTitle + `","size":11,"permalink":"https://example.slack.com/files/F123"}]}`))
		case "/api/files.info":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"file":{"id":"F123","name":"` + uploadedName + `","title":"` + uploadedTitle + `","size":11,"permalink":"https://example.slack.com/files/F123"}}`))
		case "/api/conversations.info":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"channel":{"id":"C123","name":"alerts","is_channel":true}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func rawSectionText(t *testing.T, block map[string]any) string {
	t.Helper()
	text, ok := block["text"].(map[string]any)
	if !ok {
		t.Fatalf("section block text = %#v, want object", block["text"])
	}
	value, ok := text["text"].(string)
	if !ok {
		t.Fatalf("section text value = %#v, want string", text["text"])
	}
	return value
}

func executeTestRoot(t *testing.T, cfg *config.Config, baseURL, stdin string, args []string) (string, string, error) {
	t.Helper()
	runtime, stdout, stderr := runtimetest.NewRuntime(t, runtimetest.Options{
		Config:       cfg,
		SlackBaseURL: baseURL,
		Stdin:        strings.NewReader(stdin),
	})
	root := runtimetest.NewRoot(runtime, stdout, stderr)
	root.AddCommand(clifile.NewCommand(runtime))
	// These fixtures assert requests/results; dedicated SDK and binary tests own pacing.
	args = append([]string{"--no-throttle"}, args...)
	return runtimetest.Run(t, root, args, stdout, stderr)
}

func workspaceConfig(tokenType config.TokenType) *config.Config {
	return &config.Config{
		SchemaVersion:    config.SchemaVersion,
		DefaultWorkspace: "default",
		Workspaces: map[string]config.WorkspaceProfile{
			"default": {
				Name:      "default",
				TeamID:    "T123",
				TokenType: tokenType,
				TokenRef:  "env:SLACK_TEST_TOKEN",
			},
		},
	}
}

func TestFileUploadKeepsFilenameSeparateFromTitle(t *testing.T) {
	server := fileUploadServer(t, "Release evidence")
	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.URL, "artifact", []string{"file", "upload", "--channel", "C123", "--file", "-", "--filename", "report.txt", "--title", "Release evidence"})
	if err != nil {
		t.Fatalf("error=%v stderr=%s", err, stderr)
	}
	var frame struct {
		Data struct {
			File map[string]any `json:"file"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &frame); err != nil {
		t.Fatal(err)
	}
	if frame.Data.File["name"] != "report.txt" || frame.Data.File["title"] != "Release evidence" {
		t.Fatalf("file metadata=%v; filename and display title must remain distinct", frame.Data.File)
	}
}
