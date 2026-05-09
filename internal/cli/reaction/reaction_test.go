package reaction_test

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/matcra587/slack-cli/internal/agent"
	clireaction "github.com/matcra587/slack-cli/internal/cli/reaction"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
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

func TestReactionCommandAddRemoveAndList(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"reactions.add": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("name"); got != "thumbsup" {
				t.Fatalf("add name = %q, want thumbsup", got)
			}
			return testutil.JSONResponse(`{"ok":true}`)
		},
		"reactions.remove": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("name"); got != "thumbsup" {
				t.Fatalf("remove name = %q, want thumbsup", got)
			}
			return testutil.JSONResponse(`{"ok":true}`)
		},
		"reactions.get": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"type":"message","channel":"C123","message":{"reactions":[{"name":"thumbsup","count":1,"users":["U1"]}]}}`)
		},
	})

	for _, args := range [][]string{
		{"react", "add", "--channel", "C123", "--timestamp", "1746284582.123456", "--emoji", ":thumbsup:"},
		{"react", "remove", "--channel", "C123", "--timestamp", "1746284582.123456", "--emoji", "thumbsup"},
		{"react", "list", "--channel", "C123", "--timestamp", "1746284582.123456"},
	} {
		stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "", args)
		if err != nil {
			t.Fatalf("Execute %v returned error: %v\nstderr=%s", args, err, stderr)
		}
		if !strings.Contains(stdout, `"reaction`) {
			t.Fatalf("stdout for %v = %s, want reaction data", args, stdout)
		}
	}
}

func TestReactionCommandDryRunSkipsMutation(t *testing.T) {
	for _, tt := range []struct {
		name   string
		action string
		method string
		want   string
	}{
		{name: "add", action: "add", method: "reactions.add", want: `"dry_run":true`},
		{name: "remove", action: "remove", method: "reactions.remove", want: `"removed":true`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := testutil.NewSlackServer(t, nil)

			stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
				"",
				[]string{"react", tt.action, "--channel", "C123", "--timestamp", "1746284582.123456", "--emoji", "thumbsup", "--dry-run"},
			)
			if err != nil {
				t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
			}
			if got := len(server.Requests(tt.method)); got != 0 {
				t.Fatalf("%s requests = %d, want 0", tt.method, got)
			}
			if !strings.Contains(stdout, `"dry_run":true`) || !strings.Contains(stdout, tt.want) {
				t.Fatalf("stdout = %s, want dry_run true and %s", stdout, tt.want)
			}
		})
	}
}

func TestReactionCommandIsNotRegistered(t *testing.T) {
	stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", []string{"reaction", "add", "--help"})
	if err == nil {
		t.Fatal("Execute returned nil error, want unknown legacy command")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	if !strings.Contains(err.Error(), `unknown command "reaction"`) {
		t.Fatalf("err = %v, want unknown legacy command", err)
	}
}

// --- helpers ---

func executeTestRoot(t *testing.T, cfg *config.Config, baseURL, stdin string, args []string) (string, string, error) {
	t.Helper()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	root := buildTestRoot(cfg, baseURL, strings.NewReader(stdin), stdout, stderr)
	root.SetArgs(args)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
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

	root.AddCommand(clireaction.NewCommand(runtime))
	return root
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
