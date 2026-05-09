package search_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	clisearch "github.com/matcra587/slack-cli/internal/cli/search"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
	"github.com/spf13/cobra"
)

func TestSearchMessagesCommandWritesPaginatedEnvelope(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"search.messages": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"messages":{"matches":[{"channel":{"id":"C123","name":"alerts"},"user":"U1","text":"deploy failed in prod","ts":"1746284582.123456","permalink":"https://example.slack.com/archives/C123/p1746284582123456","snippet":"deploy failed"}],"pagination":{"page":1,"page_count":2}}}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(),
		"",
		[]string{"lookup", "messages", "--query", "deploy failed", "--max-items", "1", "--cursor", "1"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if envelope["meta"].(map[string]any)["command"] != "search.messages" {
		t.Fatalf("meta.command = %q, want search.messages", envelope["meta"].(map[string]any)["command"])
	}
	if !strings.Contains(stdout, "deploy failed in prod") {
		t.Fatalf("stdout = %s, want full message text", stdout)
	}
}

func TestSearchMessagesCommandReturnsEmptyMatchesForNoResults(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"search.messages": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"messages":{"matches":[],"pagination":{"page":1,"page_count":1}}}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(),
		"",
		[]string{"lookup", "messages", "--query", "no such message", "--max-items", "10"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	var envelope struct {
		Data struct {
			Matches []clioutput.CliSearchMessage `json:"matches"`
		} `json:"data"`
		Errors []clioutput.CLIError `json:"errors"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if len(envelope.Data.Matches) != 0 {
		t.Fatalf("matches = %#v, want empty", envelope.Data.Matches)
	}
	if len(envelope.Errors) != 0 {
		t.Fatalf("errors = %#v, want empty", envelope.Errors)
	}
}

func TestSearchMessagesCommandRejectsMissingQuery(t *testing.T) {
	server := testutil.NewSlackServer(t, nil)
	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(),
		"",
		[]string{"lookup", "messages"},
	)
	if err == nil {
		t.Fatal("Execute returned nil error, want validation error")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, `"type":"validation_error"`) {
		t.Fatalf("stderr = %s, want validation_error", stderr)
	}
}

func TestSearchMessagesRequiresUserToken(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"search.messages": func(testutil.SlackRequest) testutil.SlackResponse {
			t.Fatal("search.messages should not be called for bot-token profiles")
			return testutil.JSONResponse(`{"ok":true}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"lookup", "messages", "--query", "deploy failed"},
	)
	if err == nil {
		t.Fatal("Execute returned nil error, want auth failure")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, `"type":"auth_failure"`) || !strings.Contains(stderr, "user token") || !strings.Contains(stderr, "search:read") {
		t.Fatalf("stderr = %s, want user-token search scope auth failure", stderr)
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

	lookupCmd := &cobra.Command{Use: "lookup", Short: "Look up Slack channels and users"}
	lookupCmd.AddCommand(clisearch.NewLookupMessagesCommand(runtime))
	root.AddCommand(lookupCmd)
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
