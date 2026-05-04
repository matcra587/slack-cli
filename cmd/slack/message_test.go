package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestMessageSendCommandReadsStdinAppliesAttributionAndWritesEnvelope(t *testing.T) {
	t.Setenv("CLAUDE_CODE", "1")

	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"chat.postMessage": func(req testutil.SlackRequest) testutil.SlackResponse {
			var blocks []map[string]any
			if err := json.Unmarshal([]byte(req.Form.Get("blocks")), &blocks); err != nil {
				t.Fatalf("blocks form value is not JSON: %v", err)
			}
			if len(blocks) != 2 {
				t.Fatalf("blocks length = %d, want markdown block plus attribution", len(blocks))
			}
			if got := blocks[1]["type"]; got != "context" {
				t.Fatalf("attribution block type = %q, want context", got)
			}
			return testutil.JSONResponse(`{"ok":true,"channel":"C123","ts":"1746284582.123456","message":{"type":"message","text":"Deploy complete","ts":"1746284582.123456"}}`)
		},
		"chat.getPermalink": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"permalink":"https://example.slack.com/archives/C123/p1746284582123456"}`)
		},
	})
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"Deploy *complete*\n",
		[]string{"message", "send", "--channel", "C123", "--file", "-"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}

	var envelope map[string]any
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	meta := envelope["meta"].(map[string]any)
	if meta["command"] != "message.send" {
		t.Fatalf("meta.command = %q, want message.send", meta["command"])
	}
	data := envelope["data"].(map[string]any)
	message := data["message"].(map[string]any)
	if message["ts"] != "1746284582.123456" {
		t.Fatalf("message.ts = %q, want 1746284582.123456", message["ts"])
	}
	if data["attribution"] != true {
		t.Fatalf("attribution = %#v, want true", data["attribution"])
	}
}

func TestMessageSendCommandSupportsCustomAttributionPresentation(t *testing.T) {
	t.Setenv("CLAUDE_CODE", "1")

	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"chat.postMessage": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("attachments"); got != "" {
				t.Fatalf("attachments = %s, want no attribution color attachment", got)
			}
			var blocks []map[string]any
			if err := json.Unmarshal([]byte(req.Form.Get("blocks")), &blocks); err != nil {
				t.Fatalf("blocks form value is not JSON: %v", err)
			}
			contextBlock := blocks[len(blocks)-1]
			elements := contextBlock["elements"].([]any)
			text := elements[0].(map[string]any)["text"]
			if text != ":rocket: _Sent from deploy job_" {
				t.Fatalf("attribution text = %#v, want custom emoji and message", text)
			}
			return testutil.JSONResponse(`{"ok":true,"channel":"C123","ts":"1746284582.123456","message":{"type":"message","text":"Deploy complete","ts":"1746284582.123456"}}`)
		},
		"chat.getPermalink": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"permalink":"https://example.slack.com/archives/C123/p1746284582123456"}`)
		},
	})
	defer server.Close()

	_, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "",
		[]string{
			"--agent-emoji", ":rocket:",
			"--agent-message", "Sent from deploy job",
			"message", "send", "--channel", "C123", "--message", "Deploy complete",
		},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
}

func TestMessageSendCommandOmitsAttributionForHumanRun(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"chat.postMessage": func(req testutil.SlackRequest) testutil.SlackResponse {
			var blocks []map[string]any
			if err := json.Unmarshal([]byte(req.Form.Get("blocks")), &blocks); err != nil {
				t.Fatalf("blocks form value is not JSON: %v", err)
			}
			if len(blocks) != 1 {
				t.Fatalf("blocks length = %d, want markdown block without attribution", len(blocks))
			}
			return testutil.JSONResponse(`{"ok":true,"channel":"C123","ts":"1746284582.123456","message":{"type":"message","text":"Hello","ts":"1746284582.123456"}}`)
		},
		"chat.getPermalink": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"permalink":"https://example.slack.com/archives/C123/p1746284582123456"}`)
		},
	})
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "",
		[]string{"message", "send", "--channel", "C123", "--message", "Hello"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"attribution":false`) {
		t.Fatalf("stdout = %s, want attribution false", stdout)
	}
}

func TestMessageSendCommandPlainDryRunUsesClogFields(t *testing.T) {
	server := testutil.NewSlackServer(t, nil)
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "",
		[]string{"--plain", "message", "send", "--channel", "C123", "--message", "Preview", "--dry-run"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	for _, fragment := range []string{"INF", "message send", "command=message.send", "channel=C123", "ts=dry-run", "dry_run=true"} {
		if !strings.Contains(stdout, fragment) {
			t.Fatalf("stdout = %q, want fragment %q", stdout, fragment)
		}
	}
	for _, fragment := range []string{"attribution=", "permalink=", "thread_ts=", "age=", "time="} {
		if strings.Contains(stdout, fragment) {
			t.Fatalf("stdout = %q, did not want debug field %q", stdout, fragment)
		}
	}
}

func TestMessageSendCommandResolvesChannelAlias(t *testing.T) {
	cfg := workspaceConfig(config.TokenTypeBot)
	cfg.Workspaces["default"] = withAliases(cfg.Workspaces["default"], map[string]string{"alerts": "C999"})

	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"chat.postMessage": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("channel"); got != "C999" {
				t.Fatalf("channel = %q, want resolved alias C999", got)
			}
			return testutil.JSONResponse(`{"ok":true,"channel":"C999","ts":"1746284582.123456","message":{"type":"message","text":"Deploy complete","ts":"1746284582.123456"}}`)
		},
		"chat.getPermalink": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"permalink":"https://example.slack.com/archives/C999/p1746284582123456"}`)
		},
	})
	defer server.Close()

	_, stderr, err := executeTestRoot(t, cfg, server.BaseURL(), "",
		[]string{"message", "send", "--channel", "alerts", "--message", "Deploy complete"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
}

func TestMessageSendCommandSupportsUnifiedUserTargetWithAlias(t *testing.T) {
	cfg := workspaceConfig(config.TokenTypeUser)
	cfg.Workspaces["default"] = withAliases(cfg.Workspaces["default"], map[string]string{"oncall": "U123"})

	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"conversations.open": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("users"); got != "U123" {
				t.Fatalf("users = %q, want resolved user alias U123", got)
			}
			return testutil.JSONResponse(`{"ok":true,"channel":{"id":"D123"}}`)
		},
		"chat.postMessage": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("channel"); got != "D123" {
				t.Fatalf("channel = %q, want opened DM D123", got)
			}
			return testutil.JSONResponse(`{"ok":true,"channel":"D123","ts":"1746284582.123456","message":{"type":"message","text":"Heads up","ts":"1746284582.123456"}}`)
		},
		"chat.getPermalink": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"permalink":"https://example.slack.com/archives/D123/p1746284582123456"}`)
		},
	})
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, cfg, server.BaseURL(), "",
		[]string{"message", "send", "--user", "oncall", "--message", "Heads up"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"channel":"D123"`) {
		t.Fatalf("stdout = %s, want DM channel", stdout)
	}
}

func TestMessageSendCommandRejectsRawBlockKitOverLimitBeforeSlackRequest(t *testing.T) {
	server := testutil.NewSlackServer(t, nil)
	defer server.Close()

	rawBlocks := `[` + strings.TrimRight(strings.Repeat(`{"type":"divider"},`, 51), ",") + `]`
	_, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "",
		[]string{"--raw", "message", "send", "--channel", "C123", "--message", rawBlocks},
	)
	if err == nil {
		t.Fatal("Execute returned nil error, want validation failure")
	}
	if got := len(server.Requests("chat.postMessage")); got != 0 {
		t.Fatalf("chat.postMessage requests = %d, want 0", got)
	}
	if !strings.Contains(stderr, `"type":"validation_error"`) {
		t.Fatalf("stderr = %s, want validation_error", stderr)
	}
}

func TestMessageSendCommandRejectsMissingWorkspaceBeforeSlackMutation(t *testing.T) {
	server := testutil.NewSlackServer(t, nil)
	defer server.Close()

	_, stderr, err := executeTestRoot(t, nil, server.BaseURL(), "",
		[]string{"message", "send", "--channel", "C123", "--message", "No workspace"},
	)
	if err == nil {
		t.Fatal("Execute returned nil error, want missing workspace validation")
	}
	if got := len(server.Requests("chat.postMessage")); got != 0 {
		t.Fatalf("chat.postMessage requests = %d, want 0", got)
	}
	if !strings.Contains(stderr, "config is required") {
		t.Fatalf("stderr = %s, want config is required", stderr)
	}
}

func TestMessageSendCommandDryRunSkipsSlackMutation(t *testing.T) {
	server := testutil.NewSlackServer(t, nil)
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"message", "send", "--channel", "C123", "--message", "Preview", "--dry-run"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	if got := len(server.Requests("chat.postMessage")); got != 0 {
		t.Fatalf("chat.postMessage requests = %d, want 0", got)
	}
	if !strings.Contains(stdout, `"dry_run":true`) {
		t.Fatalf("stdout = %s, want dry_run true", stdout)
	}
}

func TestMessageSendCommandReadsMessageFromFilePath(t *testing.T) {
	messageFile := filepath.Join(t.TempDir(), "message.md")
	if err := os.WriteFile(messageFile, []byte("From file"), 0o600); err != nil {
		t.Fatalf("write message file: %v", err)
	}

	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"chat.postMessage": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("text"); got != "From file" {
				t.Fatalf("text = %q, want From file", got)
			}
			return testutil.JSONResponse(`{"ok":true,"channel":"C123","ts":"1746284582.123456","message":{"type":"message","text":"From file","ts":"1746284582.123456"}}`)
		},
		"chat.getPermalink": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"permalink":"https://example.slack.com/archives/C123/p1746284582123456"}`)
		},
	})
	defer server.Close()

	_, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"message", "send", "--channel", "C123", "--file", messageFile, "--filename", "ignored.md"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
}

func TestMessageSendCommandRawBlockInputIsPreserved(t *testing.T) {
	t.Setenv("CLAUDE_CODE", "1")

	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"chat.postMessage": func(req testutil.SlackRequest) testutil.SlackResponse {
			var blocks []map[string]any
			if err := json.Unmarshal([]byte(req.Form.Get("blocks")), &blocks); err != nil {
				t.Fatalf("blocks form value is not JSON: %v", err)
			}
			text := blocks[0]["text"].(map[string]any)
			if text["text"] != "raw block" {
				t.Fatalf("raw block text = %q, want raw block", text["text"])
			}
			return testutil.JSONResponse(`{"ok":true,"channel":"C123","ts":"1746284582.123456","message":{"type":"message","text":"raw block","ts":"1746284582.123456"}}`)
		},
		"chat.getPermalink": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"permalink":"https://example.slack.com/archives/C123/p1746284582123456"}`)
		},
	})
	defer server.Close()

	_, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"--raw", "message", "send", "--channel", "C123", "--message", `[{"type":"section","text":{"type":"mrkdwn","text":"raw block"}}]`},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
}

func withAliases(profile config.WorkspaceProfile, aliases map[string]string) config.WorkspaceProfile {
	profile.Aliases = aliases
	return profile
}

func executeTestRoot(t *testing.T, cfg *config.Config, baseURL string, stdin string, args []string) (string, string, error) {
	t.Helper()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := NewRootCommand(
		WithConfig(cfg),
		WithSlackBaseURL(baseURL),
		WithTokenResolver(TokenResolverFunc(func(config.WorkspaceProfile) (string, error) {
			return "xox-test", nil
		})),
		WithIO(strings.NewReader(stdin), stdout, stderr),
		WithTTY(false),
		WithNow(func() time.Time {
			return time.Date(2026, 5, 3, 13, 8, 0, 0, time.UTC)
		}),
		WithRequestID(func() string {
			return "test-request"
		}),
	)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
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
