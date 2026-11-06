package thread_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/agent"
	climessage "github.com/matcra587/slack-cli/internal/cli/message"
	"github.com/matcra587/slack-cli/internal/cli/runtime/runtimetest"
	clithread "github.com/matcra587/slack-cli/internal/cli/thread"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestMain(m *testing.M) {
	for _, key := range agent.KnownEnvVars() {
		_ = os.Unsetenv(key)
	}
	os.Exit(m.Run())
}

func TestThreadReplyCommandPostsNestedReply(t *testing.T) {
	t.Setenv("CLAUDE_CODE", "1")

	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"chat.postMessage": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("thread_ts"); got != "1746284582.123456" {
				t.Fatalf("thread_ts = %q, want parent timestamp", got)
			}
			return testutil.JSONResponse(`{"ok":true,"channel":"C123","ts":"1746284599.123456","message":{"type":"message","text":"reply","ts":"1746284599.123456","thread_ts":"1746284582.123456"}}`)
		},
		"chat.getPermalink": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"permalink":"https://example.slack.com/archives/C123/p1746284599123456"}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"reply", "--channel", "C123", "--parent", "1746284582.123456", "--message", "reply"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}

	var envelope map[string]any
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if envelope["meta"].(map[string]any)["command"] != "reply" {
		t.Fatalf("meta.command = %q, want reply", envelope["meta"].(map[string]any)["command"])
	}
	data := envelope["data"].(map[string]any)
	if data["attribution"] != true {
		t.Fatalf("attribution = %#v, want true", data["attribution"])
	}
}

func TestThreadReplyCommandMapsInvalidParentToNotFound(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"chat.postMessage": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":false,"error":"message_not_found"}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"reply", "--channel", "C123", "--parent", "1746284582.123456", "--message", "reply"},
	)
	if err == nil {
		t.Fatal("Execute returned nil error, want not-found error")
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, `"type":"not_found"`) {
		t.Fatalf("stdout = %s, want not_found", stdout)
	}
}

func TestThreadReplyCommandSupportsBlockInput(t *testing.T) {
	t.Setenv("CLAUDE_CODE", "1")
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"chat.postMessage": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("thread_ts"); got != "1746284582.123456" {
				t.Fatalf("thread_ts = %q, want parent timestamp", got)
			}
			var blocks []map[string]any
			if err := json.Unmarshal([]byte(req.Form.Get("blocks")), &blocks); err != nil {
				t.Fatalf("blocks form value is not JSON: %v", err)
			}
			if len(blocks) != 2 || blocks[1]["type"] != "context" {
				t.Fatalf("blocks = %#v, want raw block plus attribution context", blocks)
			}
			text := blocks[0]["text"].(map[string]any)
			if text["text"] != "thread block" {
				t.Fatalf("block text = %q, want thread block", text["text"])
			}
			return testutil.JSONResponse(`{"ok":true,"channel":"C123","ts":"1746284599.123456","message":{"type":"message","text":"thread block","ts":"1746284599.123456","thread_ts":"1746284582.123456"}}`)
		},
		"chat.getPermalink": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"permalink":"https://example.slack.com/archives/C123/p1746284599123456"}`)
		},
	})

	_, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"reply", "--channel", "C123", "--parent", "1746284582.123456", "--blocks", "--message", `[{"type":"section","text":{"type":"mrkdwn","text":"thread block"}}]`},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
}

func TestThreadReplyCommandRejectsMalformedBlockInput(t *testing.T) {
	server := testutil.NewSlackServer(t, nil)

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"reply", "--channel", "C123", "--parent", "1746284582.123456", "--blocks", "--message", `not-json`},
	)
	if stderr != "" {
		t.Fatalf("unexpected machine diagnostics: %s", stderr)
	}
	if err == nil {
		t.Fatal("Execute returned nil error, want malformed block validation error")
	}
	if got := len(server.Requests("chat.postMessage")); got != 0 {
		t.Fatalf("chat.postMessage requests = %d, want 0", got)
	}
	if !strings.Contains(stdout, `"type":"validation_error"`) {
		t.Fatalf("stdout = %s, want validation_error", stdout)
	}
}

func TestThreadReplyCommandPreservesUnsupportedMarkdownSourceFallback(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"chat.postMessage": func(req testutil.SlackRequest) testutil.SlackResponse {
			var blocks []map[string]any
			if err := json.Unmarshal([]byte(req.Form.Get("blocks")), &blocks); err != nil {
				t.Fatalf("blocks form value is not JSON: %v", err)
			}
			// `>` is escaped to `&gt;` on the wire by slackutilsx.EscapeMessage;
			// Slack decodes it before mrkdwn parsing so blockquote rendering
			// is preserved. See internal/blockkit/markdown.go::FromMarkdown.
			if got := rawSectionText(t, blocks[0]); got != "&gt; threaded context" {
				t.Fatalf("block text = %q, want escaped blockquote", got)
			}
			return testutil.JSONResponse(`{"ok":true,"channel":"C123","ts":"1746284599.123456","message":{"type":"message","text":"reply","ts":"1746284599.123456","thread_ts":"1746284582.123456"}}`)
		},
		"chat.getPermalink": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"permalink":"https://example.slack.com/archives/C123/p1746284599123456"}`)
		},
	})

	_, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"reply", "--channel", "C123", "--parent", "1746284582.123456", "--message", "> threaded context\n"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
}

func TestThreadReplyCommandRejectsInvalidRawBlockRequiredFieldsBeforeSlackRequest(t *testing.T) {
	server := testutil.NewSlackServer(t, nil)

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"reply", "--channel", "C123", "--parent", "1746284582.123456", "--blocks", "--message", `[{"type":"context","elements":[]}]`},
	)
	if err == nil {
		t.Fatal("Execute returned nil error, want raw block validation error")
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	if got := len(server.Requests("chat.postMessage")); got != 0 {
		t.Fatalf("chat.postMessage requests = %d, want 0", got)
	}
	if !strings.Contains(stdout, `"type":"validation_error"`) || !strings.Contains(stdout, "context elements are required") {
		t.Fatalf("stdout = %s, want context validation error", stdout)
	}
}

func TestThreadReplyCommandDryRunSkipsSlackMutation(t *testing.T) {
	server := testutil.NewSlackServer(t, nil)

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"reply", "--channel", "C123", "--parent", "1746284582.123456", "--message", "preview", "--dry-run"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	if got := len(server.Requests("chat.postMessage")); got != 0 {
		t.Fatalf("chat.postMessage requests = %d, want 0", got)
	}
	if !strings.Contains(stdout, `"dry_run":true`) || !strings.Contains(stdout, `"thread_ts":"1746284582.123456"`) {
		t.Fatalf("stdout = %s, want dry-run threaded reply preview", stdout)
	}
}

func TestThreadReplyCommandSupportsAttributionOverride(t *testing.T) {
	t.Setenv("CLAUDE_CODE", "1")

	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"chat.postMessage": func(req testutil.SlackRequest) testutil.SlackResponse {
			var blocks []map[string]any
			if err := json.Unmarshal([]byte(req.Form.Get("blocks")), &blocks); err != nil {
				t.Fatalf("blocks form value is not JSON: %v", err)
			}
			contextBlock := blocks[len(blocks)-1]
			elements := contextBlock["elements"].([]any)
			text := elements[0].(map[string]any)["text"]
			if text != ":speech_balloon: _Replied from script (agent mode)_" {
				t.Fatalf("attribution text = %#v, want overridden values", text)
			}
			return testutil.JSONResponse(`{"ok":true,"channel":"C123","ts":"1746284599.123456","message":{"type":"message","text":"reply","ts":"1746284599.123456","thread_ts":"1746284582.123456"}}`)
		},
		"chat.getPermalink": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"permalink":"https://example.slack.com/archives/C123/p1746284599123456"}`)
		},
	})

	_, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "",
		[]string{
			"reply", "--channel", "C123", "--parent", "1746284582.123456",
			"--message", "reply",
			"--attribution-emoji", ":speech_balloon:",
			"--attribution-message", "Replied from script",
		},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
}

func TestThreadReplyCommandHonorsNoAttribution(t *testing.T) {
	t.Setenv("CLAUDE_CODE", "1")

	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"chat.postMessage": func(req testutil.SlackRequest) testutil.SlackResponse {
			var blocks []map[string]any
			if err := json.Unmarshal([]byte(req.Form.Get("blocks")), &blocks); err != nil {
				t.Fatalf("blocks form value is not JSON: %v", err)
			}
			for _, block := range blocks {
				if block["type"] == "context" {
					t.Fatalf("--no-attribution did not suppress context block: %#v", blocks)
				}
			}
			return testutil.JSONResponse(`{"ok":true,"channel":"C123","ts":"1746284599.123456","message":{"type":"message","text":"reply","ts":"1746284599.123456","thread_ts":"1746284582.123456"}}`)
		},
		"chat.getPermalink": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"permalink":"https://example.slack.com/archives/C123/p1746284599123456"}`)
		},
	})

	_, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "",
		[]string{"reply", "--channel", "C123", "--parent", "1746284582.123456", "--message", "reply", "--no-attribution"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
}

func TestThreadCommandIsNotRegistered(t *testing.T) {
	stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", []string{"thread", "reply", "--help"})
	if err == nil {
		t.Fatal("Execute returned nil error, want unknown legacy command")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	if !strings.Contains(err.Error(), `unknown command "thread"`) {
		t.Fatalf("err = %v, want unknown legacy command", err)
	}
}

// --- helpers ---

func executeTestRoot(t *testing.T, cfg *config.Config, baseURL, stdin string, args []string) (string, string, error) {
	t.Helper()
	runtime, stdout, stderr := runtimetest.NewRuntime(t, runtimetest.Options{
		Config:       cfg,
		SlackBaseURL: baseURL,
		Stdin:        strings.NewReader(stdin),
	})
	root := runtimetest.NewRoot(runtime, stdout, stderr)
	root.AddCommand(climessage.NewCommand(runtime))
	root.AddCommand(clithread.NewCommand(runtime))
	// These fixtures assert requests/results; dedicated SDK and binary tests own pacing.
	args = append([]string{"--no-throttle"}, args...)
	return runtimetest.Run(t, root, args, stdout, stderr)
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

func TestReplyReceiptUsesActualThreadPlacement(t *testing.T) {
	for _, tc := range []struct{ name, thread string }{
		{"posted in thread", `,"thread_ts":"1746284582.123456"`},
		{"parent deleted and reply posted outside thread", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
				"chat.postMessage": func(testutil.SlackRequest) testutil.SlackResponse {
					return testutil.JSONResponse(`{"ok":true,"channel":"C123","ts":"1746284599.123456","message":{"type":"message","text":"Stored reply","user":"U456","ts":"1746284599.123456"` + tc.thread + `}}`)
				},
				"chat.getPermalink": func(testutil.SlackRequest) testutil.SlackResponse {
					return testutil.JSONResponse(`{"ok":true,"permalink":"https://example.com/reply"}`)
				},
			})
			stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "", []string{"reply", "--channel", "C123", "--parent", "1746284582.123456", "--message", "Submitted reply"})
			if err != nil {
				t.Fatalf("reply failed: %v; stderr=%s", err, stderr)
			}
			var result map[string]any
			if err := json.Unmarshal([]byte(stdout), &result); err != nil {
				t.Fatal(err)
			}
			message := result["data"].(map[string]any)["message"].(map[string]any)
			if message["text"] != "Stored reply" || message["user"] != "U456" {
				t.Fatalf("receipt does not reflect Slack message: %#v", message)
			}
			thread, exists := message["thread_ts"]
			if tc.thread == "" && exists {
				t.Fatalf("invented thread placement: %#v", message)
			}
			if tc.thread != "" && thread != "1746284582.123456" {
				t.Fatalf("thread_ts = %#v", thread)
			}
			if n := len(server.Requests("chat.postMessage")); n != 1 {
				t.Fatalf("post requests = %d, want 1", n)
			}
		})
	}
}
