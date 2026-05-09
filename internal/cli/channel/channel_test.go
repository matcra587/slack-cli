package channel_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	clichannel "github.com/matcra587/slack-cli/internal/cli/channel"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
	"github.com/spf13/cobra"
)

func TestLookupChannelListsAndShowsInfo(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"conversations.list": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("types"); got != "public_channel,private_channel" {
				t.Fatalf("types = %q, want public/private channels", got)
			}
			return testutil.JSONResponse(`{"ok":true,"channels":[{"id":"C123","name":"alerts","num_members":12,"topic":{"value":"Ops alerts"}}],"response_metadata":{"next_cursor":"next"}}`)
		},
		"conversations.info": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"channel":{"id":"C123","name":"alerts","num_members":12,"topic":{"value":"Ops alerts"}}}`)
		},
	})
	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"lookup", "channel", "--max-items", "1", "--cursor", "cursor-1"},
	)
	if err != nil {
		t.Fatalf("lookup channel returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "Ops alerts") {
		t.Fatalf("stdout = %s, want topic", stdout)
	}

	stdout, stderr, err = executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"lookup", "channel", "--channel", "C123"},
	)
	if err != nil {
		t.Fatalf("lookup channel info returned error: %v\nstderr=%s", err, stderr)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if envelope["meta"].(map[string]any)["command"] != "lookup.channel" {
		t.Fatalf("meta.command = %q, want lookup.channel", envelope["meta"].(map[string]any)["command"])
	}
}

func TestLookupChannelCanListDMConversationsByType(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"conversations.list": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("types"); got != "im" {
				t.Fatalf("types = %q, want im", got)
			}
			return testutil.JSONResponse(`{"ok":true,"channels":[{"id":"D123","is_im":true,"user":"U123"}]}`)
		},
	})
	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"lookup", "channel", "--types", "im", "--max-items", "1"},
	)
	if err != nil {
		t.Fatalf("lookup channel --types im returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "D123") || !strings.Contains(stdout, `"type":"im"`) {
		t.Fatalf("stdout = %s, want DM conversation", stdout)
	}
}

func TestLookupChannelMapsMissingChannelToNotFound(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"conversations.info": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":false,"error":"channel_not_found"}`)
		},
	})
	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"lookup", "channel", "--channel", "C404"},
	)
	if err == nil {
		t.Fatal("Execute returned nil error, want not-found error")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, `"type":"not_found"`) {
		t.Fatalf("stderr = %s, want not_found", stderr)
	}
}

func buildTestRoot(cfg *config.Config, baseURL string, stdin interface{ Read([]byte) (int, error) }, stdout, stderr *bytes.Buffer) *cobra.Command {
	runtime := &cliruntime.RootRuntime{
		Stdin:     stdin,
		Stdout:    stdout,
		Stderr:    stderr,
		IsTTY:     false,
		Now:       func() time.Time { return time.Date(2026, 5, 3, 13, 8, 0, 0, time.UTC) },
		RequestID: func() string { return "test-request" },
	}
	if cfg != nil {
		runtime.Config = cfg
	}
	if baseURL != "" {
		runtime.SlackBaseURL = baseURL
	}
	runtime.TokenResolver = cliruntime.TokenResolverFunc(func(_ context.Context, _ config.WorkspaceProfile) (string, error) {
		return "xox-test", nil
	})

	root := &cobra.Command{
		Use:           "slick",
		Short:         "Slack command line interface",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)

	flags := root.PersistentFlags()
	flags.StringP("workspace", "w", "", "Workspace profile")
	flags.BoolP("json", "j", false, "Force JSON output")
	flags.BoolP("plain", "P", false, "Force plain text output")
	flags.BoolP("compact", "k", false, "Output command data without envelope")
	flags.BoolP("raw", "X", false, "Output Slack-native data")
	flags.BoolP("agent", "a", false, "Force agent mode")
	flags.BoolP("no-agent-attribution", "z", false, "Disable agent attribution for this command")
	flags.StringP("agent-label", "G", "", "Override agent attribution label")
	flags.StringP("agent-emoji", "Y", "", "Override agent attribution emoji")
	flags.StringP("agent-message", "O", "", "Override agent attribution message")
	flags.BoolP("no-throttle", "Q", false, "Disable proactive Slack API throttling")
	flags.BoolP("debug", "D", false, "Enable debug-level output")
	root.MarkFlagsMutuallyExclusive("json", "plain", "compact", "raw")

	root.AddCommand(clichannel.NewCommand(runtime))
	return root
}

func executeTestRoot(t *testing.T, cfg *config.Config, baseURL, stdin string, args []string) (string, string, error) {
	t.Helper()
	stdoutBuf := &bytes.Buffer{}
	stderrBuf := &bytes.Buffer{}
	cmd := buildTestRoot(cfg, baseURL, strings.NewReader(stdin), stdoutBuf, stderrBuf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdoutBuf.String(), stderrBuf.String(), err
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
