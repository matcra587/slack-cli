package user_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	cliuser "github.com/matcra587/slack-cli/internal/cli/user"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
	"github.com/spf13/cobra"
)

func TestLookupUserListsAndShowsInfo(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"users.list": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"members":[{"id":"U123","name":"matt","tz":"America/Toronto","profile":{"status_text":"Deploying"}}]}`)
		},
		"users.info": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"user":{"id":"U123","name":"matt","tz":"America/Toronto","profile":{"status_text":"Deploying"}}}`)
		},
		"users.getPresence": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"presence":"active"}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"lookup", "user", "--max-items", "1", "--presence"},
	)
	if err != nil {
		t.Fatalf("lookup user returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "Deploying") {
		t.Fatalf("stdout = %s, want status text", stdout)
	}

	stdout, stderr, err = executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"lookup", "user", "--user", "U123", "--presence"},
	)
	if err != nil {
		t.Fatalf("lookup user info returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"presence":"active"`) {
		t.Fatalf("stdout = %s, want presence", stdout)
	}
}

func TestLookupUserHidesEmptyPresence(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"users.list": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"members":[{"id":"U123","name":"matt","tz":"America/Toronto"}]}`)
		},
		"users.info": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"user":{"id":"U123","name":"matt","tz":"America/Toronto"}}`)
		},
		"users.getPresence": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"presence":""}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"--plain", "lookup", "user", "--max-items", "1", "--presence"},
	)
	if err != nil {
		t.Fatalf("lookup user returned error: %v\nstderr=%s", err, stderr)
	}
	if strings.Contains(stdout, "PRESENCE") {
		t.Fatalf("stdout = %s, should hide all-empty presence column", stdout)
	}

	stdout, stderr, err = executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"lookup", "user", "--user", "U123", "--presence"},
	)
	if err != nil {
		t.Fatalf("lookup user info returned error: %v\nstderr=%s", err, stderr)
	}
	if strings.Contains(stdout, `"presence"`) {
		t.Fatalf("stdout = %s, should omit empty presence field", stdout)
	}
}

func TestLookupUserShowsPresenceColumnWhenAnyUserHasPresence(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"users.list": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"members":[{"id":"U123","name":"matt","presence":"active"},{"id":"U456","name":"deploy"}]}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"--plain", "lookup", "user", "--max-items", "2", "--presence"},
	)
	if err != nil {
		t.Fatalf("lookup user returned error: %v\nstderr=%s", err, stderr)
	}
	for _, want := range []string{"PRESENCE", "active"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout = %s, want %q", stdout, want)
		}
	}
}

func TestLookupUserListExcludesDeletedUsersByDefault(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"users.list": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"members":[{"id":"UACTIVE","name":"active","deleted":false},{"id":"UDELETED","name":"deleted","deleted":true}]}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"lookup", "user", "--max-items", "2"},
	)
	if err != nil {
		t.Fatalf("lookup user returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "UACTIVE") {
		t.Fatalf("stdout = %s, want active user", stdout)
	}
	if strings.Contains(stdout, "UDELETED") {
		t.Fatalf("stdout = %s, did not want deleted user by default", stdout)
	}
}

func TestLookupUserListCanIncludeDeletedUsers(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"users.list": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"members":[{"id":"UACTIVE","name":"active","deleted":false},{"id":"UDELETED","name":"deleted","deleted":true}]}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"lookup", "user", "--max-items", "2", "--include-deleted"},
	)
	if err != nil {
		t.Fatalf("lookup user --include-deleted returned error: %v\nstderr=%s", err, stderr)
	}
	for _, want := range []string{"UACTIVE", "UDELETED"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout = %s, want %s", stdout, want)
		}
	}
}

func TestLookupUserMapsMissingUserToNotFound(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"users.info": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":false,"error":"user_not_found"}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"lookup", "user", "--user", "U404"},
	)
	if err == nil {
		t.Fatal("Execute returned nil error, want not-found")
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

	lookupCmd := &cobra.Command{Use: "lookup", Short: "Look up Slack channels and users"}
	lookupCmd.AddCommand(cliuser.NewLookupUserCommand(runtime))
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
