package message_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/agent"
	climessage "github.com/matcra587/slack-cli/internal/cli/message"
	"github.com/matcra587/slack-cli/internal/cli/runtime/runtimetest"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
	"github.com/spf13/cobra"
)

func TestMain(m *testing.M) {
	for _, key := range agent.KnownEnvVars() {
		_ = os.Unsetenv(key)
	}
	os.Exit(m.Run())
}

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
			text := blocks[0]["text"].(map[string]any)
			if text["text"] != "Deploy *complete*\nsecond line" {
				t.Fatalf("message block text = %#v, want newline-preserving markdown text", text["text"])
			}
			if got := blocks[1]["type"]; got != "context" {
				t.Fatalf("attribution block type = %q, want context", got)
			}
			return testutil.JSONResponse(`{"ok":true,"channel":"C123","ts":"1746284582.123456","message":{"type":"message","text":"Deploy complete\nsecond line","ts":"1746284582.123456"}}`)
		},
		"chat.getPermalink": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"permalink":"https://example.slack.com/archives/C123/p1746284582123456"}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"Deploy *complete*\nsecond line\n",
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
			if text != ":rocket: _Sent from deploy job (agent mode)_" {
				t.Fatalf("attribution text = %#v, want custom emoji, message, and agent mode suffix", text)
			}
			return testutil.JSONResponse(`{"ok":true,"channel":"C123","ts":"1746284582.123456","message":{"type":"message","text":"Deploy complete","ts":"1746284582.123456"}}`)
		},
		"chat.getPermalink": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"permalink":"https://example.slack.com/archives/C123/p1746284582123456"}`)
		},
	})

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

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "",
		[]string{"--output=human", "message", "send", "--channel", "C123", "--message", "Preview", "--dry-run"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	for _, fragment := range []string{"Message sent", "channel=C123", "ts=dry-run", "dry_run=true"} {
		if !strings.Contains(stdout, fragment) {
			t.Fatalf("stdout = %q, want fragment %q", stdout, fragment)
		}
	}
	for _, fragment := range []string{"INF", "command=message.send", "attribution=", "permalink=", "thread_ts=", "age=", "time="} {
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

func TestMessageSendCommandSupportsRepeatedAndCommaUserTargets(t *testing.T) {
	cfg := workspaceConfig(config.TokenTypeUser)
	cfg.Workspaces["default"] = withAliases(cfg.Workspaces["default"], map[string]string{
		"oncall":  "U123",
		"release": "U456",
	})

	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"conversations.open": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("users"); got != "U123,U456,U789" {
				t.Fatalf("users = %q, want comma-joined resolved recipients", got)
			}
			return testutil.JSONResponse(`{"ok":true,"channel":{"id":"G123"}}`)
		},
		"chat.postMessage": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("channel"); got != "G123" {
				t.Fatalf("channel = %q, want opened MPIM G123", got)
			}
			return testutil.JSONResponse(`{"ok":true,"channel":"G123","ts":"1746284582.123456","message":{"type":"message","text":"Heads up","ts":"1746284582.123456"}}`)
		},
		"chat.getPermalink": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"permalink":"https://example.slack.com/archives/G123/p1746284582123456"}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, cfg, server.BaseURL(), "",
		[]string{"message", "send", "--user", "oncall,release", "--user", "U789", "--message", "Heads up"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"channel":"G123"`) {
		t.Fatalf("stdout = %s, want MPIM channel", stdout)
	}
}

func TestMessageSendCommandResolvesUserTargetsByEmail(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"users.lookupByEmail": func(req testutil.SlackRequest) testutil.SlackResponse {
			switch req.Form.Get("email") {
			case "dev@example.com":
				return testutil.JSONResponse(`{"ok":true,"user":{"id":"UDEV","name":"dev"}}`)
			case "ops@example.com":
				return testutil.JSONResponse(`{"ok":true,"user":{"id":"UOPS","name":"ops"}}`)
			default:
				t.Fatalf("unexpected email lookup %q", req.Form.Get("email"))
				return testutil.JSONResponse(`{"ok":false,"error":"users_not_found"}`)
			}
		},
		"conversations.open": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("users"); got != "UDEV,UOPS" {
				t.Fatalf("users = %q, want looked-up user IDs", got)
			}
			return testutil.JSONResponse(`{"ok":true,"channel":{"id":"G123"}}`)
		},
		"chat.postMessage": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("channel"); got != "G123" {
				t.Fatalf("channel = %q, want opened MPIM G123", got)
			}
			return testutil.JSONResponse(`{"ok":true,"channel":"G123","ts":"1746284582.123456","message":{"type":"message","text":"Heads up","ts":"1746284582.123456"}}`)
		},
		"chat.getPermalink": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"permalink":"https://example.slack.com/archives/G123/p1746284582123456"}`)
		},
	})

	_, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(), "",
		[]string{"message", "send", "--user", "dev@example.com,ops@example.com", "--message", "Heads up"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
}

func TestMessageSendCommandUsesDefaultChannelWhenNoTargetFlag(t *testing.T) {
	cfg := workspaceConfig(config.TokenTypeBot)
	profile := cfg.Workspaces["default"]
	profile.DefaultChannel = "alerts"
	cfg.Workspaces["default"] = withAliases(profile, map[string]string{"alerts": "C999"})

	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"chat.postMessage": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("channel"); got != "C999" {
				t.Fatalf("channel = %q, want resolved default channel C999", got)
			}
			return testutil.JSONResponse(`{"ok":true,"channel":"C999","ts":"1746284582.123456","message":{"type":"message","text":"Deploy complete","ts":"1746284582.123456"}}`)
		},
		"chat.getPermalink": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"permalink":"https://example.slack.com/archives/C999/p1746284582123456"}`)
		},
	})

	_, stderr, err := executeTestRoot(t, cfg, server.BaseURL(), "",
		[]string{"message", "send", "--message", "Deploy complete"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
}

func TestMessageSendCommandBotUserTargetLetsSlackDecide(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"conversations.open": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("users"); got != "U123" {
				t.Fatalf("users = %q, want U123", got)
			}
			return testutil.JSONResponse(`{"ok":false,"error":"not_allowed_token_type"}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "",
		[]string{"message", "send", "--user", "U123", "--message", "Heads up"},
	)
	if err == nil {
		t.Fatal("Execute returned nil error, want Slack rejection mapped structurally")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, `"type":"validation_error"`) || !strings.Contains(stderr, "not_allowed_token_type") {
		t.Fatalf("stderr = %s, want structured not_allowed_token_type validation error", stderr)
	}
	if got := len(server.Requests("conversations.open")); got != 1 {
		t.Fatalf("conversations.open requests = %d, want Slack-decided attempt", got)
	}
}

func TestMessageSendCommandDeclaresCobraTargetFlagGroup(t *testing.T) {
	root := newTestRootCommand()
	sendCmd, _, err := root.Find([]string{"message", "send"})
	if err != nil {
		t.Fatalf("find message send: %v", err)
	}
	if sendCmd.Name() != "send" || sendCmd.Parent().Name() != "message" {
		t.Fatalf("found command = %s, want slick message send", sendCmd.CommandPath())
	}

	for _, flagName := range []string{"channel", "user"} {
		flag := sendCmd.Flags().Lookup(flagName)
		if flag == nil {
			t.Fatalf("message send flag %q is missing", flagName)
		}
		if got := flag.Annotations["cobra_annotation_mutually_exclusive"]; !containsString(got, "channel user") {
			t.Fatalf("%s mutually-exclusive annotations = %#v, want channel/user group", flagName, got)
		}
	}
}

func TestMessageSendCommandRejectsBothChannelAndUserBeforeSlackRequest(t *testing.T) {
	server := testutil.NewSlackServer(t, nil)

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "",
		[]string{"message", "send", "--channel", "C123", "--user", "U123", "--message", "Heads up"},
	)
	if err == nil {
		t.Fatal("Execute returned nil error, want target validation error")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if got := len(server.Requests("chat.postMessage")); got != 0 {
		t.Fatalf("chat.postMessage requests = %d, want 0", got)
	}
	if got := len(server.Requests("conversations.open")); got != 0 {
		t.Fatalf("conversations.open requests = %d, want 0", got)
	}
	if !strings.Contains(stderr, `"type":"validation_error"`) {
		t.Fatalf("stderr = %s, want structured validation_error", stderr)
	}
}

func TestMessageSendCommandRejectsBlockKitOverLimitBeforeSlackRequest(t *testing.T) {
	server := testutil.NewSlackServer(t, nil)

	rawBlocks := `[` + strings.TrimRight(strings.Repeat(`{"type":"divider"},`, 51), ",") + `]`
	_, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "",
		[]string{"message", "send", "--channel", "C123", "--blocks", "--message", rawBlocks},
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

func TestMessageSendCommandRejectsMalformedBlockKitBeforeSlackRequest(t *testing.T) {
	server := testutil.NewSlackServer(t, nil)

	_, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "",
		[]string{"message", "send", "--channel", "C123", "--blocks", "--message", `{"type":"section"`},
	)
	if err == nil {
		t.Fatal("Execute returned nil error, want malformed block validation failure")
	}
	if got := len(server.Requests("chat.postMessage")); got != 0 {
		t.Fatalf("chat.postMessage requests = %d, want 0", got)
	}
	if !strings.Contains(stderr, `"type":"validation_error"`) {
		t.Fatalf("stderr = %s, want validation_error", stderr)
	}
}

func TestMessageSendCommandAcceptsHeaderBlockInput(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"chat.postMessage": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"channel":"C123","ts":"1746284582.123456","message":{"type":"message","text":"header test","ts":"1746284582.123456"}}`)
		},
	})

	_, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "",
		[]string{"message", "send", "--channel", "C123", "--blocks", "--message", `[{"type":"header","text":{"type":"plain_text","text":"Release Notes"}}]`},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
}

func TestMessageSendCommandPreservesUnsupportedMarkdownSourceFallback(t *testing.T) {
	markdown := strings.Join([]string{
		"- alpha",
		"- beta",
		"",
		"> quoted",
		"",
		"```sh",
		"echo hello",
		"```",
		"",
		"<details>",
		"<summary>Deploy</summary>",
		"</details>",
	}, "\n")

	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"chat.postMessage": func(req testutil.SlackRequest) testutil.SlackResponse {
			var blocks []map[string]any
			if err := json.Unmarshal([]byte(req.Form.Get("blocks")), &blocks); err != nil {
				t.Fatalf("blocks form value is not JSON: %v", err)
			}
			got := make([]string, 0, len(blocks))
			for _, block := range blocks {
				if block["type"] == "section" {
					got = append(got, rawSectionText(t, block))
				}
			}
			for _, want := range []string{
				"- alpha\n- beta",
				"> quoted",
				"```sh\necho hello\n```",
				"<details>\n<summary>Deploy</summary>\n</details>",
			} {
				if !containsString(got, want) {
					t.Fatalf("section texts = %#v, want source-preserving fallback %q", got, want)
				}
			}
			return testutil.JSONResponse(`{"ok":true,"channel":"C123","ts":"1746284582.123456","message":{"type":"message","text":"fallback","ts":"1746284582.123456"}}`)
		},
		"chat.getPermalink": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"permalink":"https://example.slack.com/archives/C123/p1746284582123456"}`)
		},
	})

	_, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "",
		[]string{"message", "send", "--channel", "C123", "--message", markdown},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
}

func TestMessageSendCommandRejectsInvalidRawBlockRequiredFieldsBeforeSlackRequest(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "section missing text", raw: `[{"type":"section"}]`, want: "section text or fields are required"},
		{name: "context empty elements", raw: `[{"type":"context","elements":[]}]`, want: "context elements are required"},
		{name: "image missing url", raw: `[{"type":"image","alt_text":"diagram"}]`, want: "image_url or slack_file is required"},
		{name: "image missing alt", raw: `[{"type":"image","image_url":"https://example.com/image.png"}]`, want: "alt_text is required"},
		{name: "file missing source", raw: `[{"type":"file","external_id":"F123"}]`, want: "file external_id and source are required"},
		{name: "table too many columns", raw: `[` + rawTableWithColumns(21) + `]`, want: "20 columns"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := testutil.NewSlackServer(t, nil)

			stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "",
				[]string{"message", "send", "--channel", "C123", "--blocks", "--message", tt.raw},
			)
			if err == nil {
				t.Fatal("Execute returned nil error, want raw block validation failure")
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if got := len(server.Requests("chat.postMessage")); got != 0 {
				t.Fatalf("chat.postMessage requests = %d, want 0", got)
			}
			if !strings.Contains(stderr, `"type":"validation_error"`) || !strings.Contains(stderr, tt.want) {
				t.Fatalf("stderr = %s, want validation_error containing %q", stderr, tt.want)
			}
		})
	}
}

func TestMessageSendCommandMapsSlackPermissionFailuresToFixedContract(t *testing.T) {
	tests := []struct {
		name     string
		slackErr string
		args     []string
		wantType string
	}{
		{name: "missing scope", slackErr: "missing_scope", args: []string{"message", "send", "--channel", "C123", "--message", "hello"}, wantType: "auth_failure"},
		{name: "not in channel", slackErr: "not_in_channel", args: []string{"message", "send", "--channel", "C123", "--message", "hello"}, wantType: "not_found"},
		{name: "channel not found", slackErr: "channel_not_found", args: []string{"message", "send", "--channel", "C123", "--message", "hello"}, wantType: "not_found"},
		{name: "no permission", slackErr: "no_permission", args: []string{"message", "send", "--channel", "C123", "--message", "hello"}, wantType: "auth_failure"},
		{name: "user not found", slackErr: "user_not_found", args: []string{"message", "send", "--user", "U404", "--message", "hello"}, wantType: "not_found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
				"auth.test": func(testutil.SlackRequest) testutil.SlackResponse {
					return testutil.JSONResponse(`{"ok":true,"user_id":"U123"}`)
				},
				"conversations.open": func(testutil.SlackRequest) testutil.SlackResponse {
					return testutil.JSONResponse(`{"ok":false,"error":"` + tt.slackErr + `"}`)
				},
				"chat.postMessage": func(testutil.SlackRequest) testutil.SlackResponse {
					return testutil.JSONResponse(`{"ok":false,"error":"` + tt.slackErr + `"}`)
				},
			})

			stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(), "", tt.args)
			if err == nil {
				t.Fatal("Execute returned nil error, want Slack permission failure")
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, `"type":"`+tt.wantType+`"`) || !strings.Contains(stderr, tt.slackErr) {
				t.Fatalf("stderr = %s, want %s containing %s", stderr, tt.wantType, tt.slackErr)
			}
		})
	}
}

func TestMessageSendCommandRejectsMissingWorkspaceBeforeSlackMutation(t *testing.T) {
	server := testutil.NewSlackServer(t, nil)

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

func TestMessageSendCommandValidatesRequiredScopeBeforeSlackMutation(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"auth.test": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.SlackResponse{
				Status: http.StatusOK,
				Body:   `{"ok":true,"user_id":"U123"}`,
				Header: http.Header{
					"Content-Type":   []string{"application/json"},
					"X-OAuth-Scopes": []string{"channels:read"},
				},
			}
		},
		"chat.postMessage": func(testutil.SlackRequest) testutil.SlackResponse {
			t.Fatal("chat.postMessage was called before scope validation failed")
			return testutil.JSONResponse(`{"ok":false}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(), "",
		[]string{"message", "send", "--channel", "C123", "--message", "Needs chat scope"},
	)
	if err == nil {
		t.Fatal("Execute returned nil error, want missing-scope auth failure")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if got := len(server.Requests("chat.postMessage")); got != 0 {
		t.Fatalf("chat.postMessage requests = %d, want 0", got)
	}
	if !strings.Contains(stderr, `"type":"auth_failure"`) || !strings.Contains(stderr, "chat:write") {
		t.Fatalf("stderr = %s, want auth failure naming missing chat:write scope", stderr)
	}
}

func TestMessageSendCommandDryRunSkipsSlackMutation(t *testing.T) {
	server := testutil.NewSlackServer(t, nil)

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

	_, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"message", "send", "--channel", "C123", "--file", messageFile, "--filename", "ignored.md"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
}

func TestMessageSendCommandExpandsMessageFilePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	messageFile := filepath.Join(home, "message.md")
	if err := os.WriteFile(messageFile, []byte("From expanded file"), 0o600); err != nil {
		t.Fatalf("write message file: %v", err)
	}

	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"chat.postMessage": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("text"); got != "From expanded file" {
				t.Fatalf("text = %q, want expanded file content", got)
			}
			return testutil.JSONResponse(`{"ok":true,"channel":"C123","ts":"1746284582.123456","message":{"type":"message","text":"From expanded file","ts":"1746284582.123456"}}`)
		},
		"chat.getPermalink": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"permalink":"https://example.slack.com/archives/C123/p1746284582123456"}`)
		},
	})

	_, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"message", "send", "--channel", "C123", "--file", "~/message.md"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
}

func TestMessageSendCommandBlockInputIsPreserved(t *testing.T) {
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

	_, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"message", "send", "--channel", "C123", "--blocks", "--message", `[{"type":"section","text":{"type":"mrkdwn","text":"raw block"}}]`},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
}

func TestMessageSendCommandBlockInputCanComeFromFile(t *testing.T) {
	messageFile := filepath.Join(t.TempDir(), "blocks.json")
	if err := os.WriteFile(messageFile, []byte(`[{"type":"section","text":{"type":"mrkdwn","text":"file block"}}]`), 0o600); err != nil {
		t.Fatalf("write block file: %v", err)
	}
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"chat.postMessage": func(req testutil.SlackRequest) testutil.SlackResponse {
			var blocks []map[string]any
			if err := json.Unmarshal([]byte(req.Form.Get("blocks")), &blocks); err != nil {
				t.Fatalf("blocks form value is not JSON: %v", err)
			}
			text := blocks[0]["text"].(map[string]any)
			if text["text"] != "file block" {
				t.Fatalf("raw block text = %q, want file block", text["text"])
			}
			return testutil.JSONResponse(`{"ok":true,"channel":"C123","ts":"1746284582.123456","message":{"type":"message","text":"file block","ts":"1746284582.123456"}}`)
		},
		"chat.getPermalink": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"permalink":"https://example.slack.com/archives/C123/p1746284582123456"}`)
		},
	})

	_, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"message", "send", "--channel", "C123", "--blocks", "--file", messageFile},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
}

// --- helpers ---

func newTestRootCommand() *cobra.Command {
	runtime, stdout, stderr := runtimetest.NewRuntime(nil, runtimetest.Options{})
	root := runtimetest.NewRoot(runtime, stdout, stderr)
	root.AddCommand(climessage.NewCommand(runtime))
	return root
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

func rawTableWithColumns(columns int) string {
	cells := make([]string, 0, columns)
	for range columns {
		cells = append(cells, `{"type":"rich_text","elements":[]}`)
	}
	return `{"type":"table","rows":[[` + strings.Join(cells, ",") + `]]}`
}

func withAliases(profile config.WorkspaceProfile, aliases map[string]string) config.WorkspaceProfile {
	profile.Aliases = aliases
	return profile
}

func containsString(values []string, want string) bool {
	return slices.Contains(values, want)
}

func executeTestRoot(t *testing.T, cfg *config.Config, baseURL, stdin string, args []string) (string, string, error) {
	t.Helper()
	runtime, stdout, stderr := runtimetest.NewRuntime(t, runtimetest.Options{
		Config:       cfg,
		SlackBaseURL: baseURL,
		Stdin:        strings.NewReader(stdin),
	})
	root := runtimetest.NewRoot(runtime, stdout, stderr)
	root.AddCommand(climessage.NewCommand(runtime))
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
