package cache_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/matcra587/slack-cli/internal/agent"
	slackcache "github.com/matcra587/slack-cli/internal/cache"
	clicache "github.com/matcra587/slack-cli/internal/cli/cache"
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

func TestCacheUsersPrimesActiveUsersAndReusesFreshCache(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"users.list": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"members":[{"id":"UACTIVE","name":"active","deleted":false},{"id":"UDELETED","name":"deleted","deleted":true}]}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "", []string{"cache", "users"})
	if err != nil {
		t.Fatalf("cache users returned error: %v\nstderr=%s", err, stderr)
	}
	data := envelopeData(t, stdout)
	if data["from_cache"] != false {
		t.Fatalf("from_cache = %v, want false", data["from_cache"])
	}
	users := data["users"].([]any)
	if len(users) != 1 || users[0].(map[string]any)["id"] != "UACTIVE" {
		t.Fatalf("users = %#v, want active user only", users)
	}

	stdout, stderr, err = executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "", []string{"cache", "users"})
	if err != nil {
		t.Fatalf("cached cache users returned error: %v\nstderr=%s", err, stderr)
	}
	data = envelopeData(t, stdout)
	if data["from_cache"] != true {
		t.Fatalf("from_cache = %v, want true", data["from_cache"])
	}
	if got := len(server.Requests("users.list")); got != 1 {
		t.Fatalf("users.list requests = %d, want 1", got)
	}
}

func TestCacheChannelsPrimesAllConversationTypesAndReusesFreshCache(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"conversations.list": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("types"); got != "public_channel,private_channel,im,mpim" {
				t.Fatalf("types = %q, want all conversation types", got)
			}
			return testutil.JSONResponse(`{"ok":true,"channels":[{"id":"C123","name":"alerts"},{"id":"D123","is_im":true,"user":"U123"}]}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "", []string{"cache", "channels"})
	if err != nil {
		t.Fatalf("cache channels returned error: %v\nstderr=%s", err, stderr)
	}
	data := envelopeData(t, stdout)
	if data["from_cache"] != false {
		t.Fatalf("from_cache = %v, want false", data["from_cache"])
	}
	channels := data["channels"].([]any)
	if len(channels) != 2 {
		t.Fatalf("channels = %#v, want two cached conversations", channels)
	}

	stdout, stderr, err = executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "", []string{"cache", "channels"})
	if err != nil {
		t.Fatalf("cached cache channels returned error: %v\nstderr=%s", err, stderr)
	}
	data = envelopeData(t, stdout)
	if data["from_cache"] != true {
		t.Fatalf("from_cache = %v, want true", data["from_cache"])
	}
	if got := len(server.Requests("conversations.list")); got != 1 {
		t.Fatalf("conversations.list requests = %d, want 1", got)
	}
}

func TestCacheClearRemovesOneResource(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if _, err := slackcache.Write("default", "users", json.RawMessage(`{"users":[{"id":"U123"}]}`)); err != nil {
		t.Fatalf("write users cache: %v", err)
	}
	if _, err := slackcache.Write("default", "channels", json.RawMessage(`{"channels":[{"id":"C123"}]}`)); err != nil {
		t.Fatalf("write channels cache: %v", err)
	}

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), "http://example.invalid", "", []string{"cache", "clear", "users"})
	if err != nil {
		t.Fatalf("cache clear users returned error: %v\nstderr=%s", err, stderr)
	}
	data := envelopeData(t, stdout)
	if data["removed"] != true {
		t.Fatalf("removed = %v, want true", data["removed"])
	}
	if _, ok, _, _ := slackcache.Read("default", "users", 0); ok {
		t.Fatal("users cache still exists")
	}
	if _, ok, _, _ := slackcache.Read("default", "channels", 0); !ok {
		t.Fatal("channels cache was removed")
	}
}

func envelopeData(t *testing.T, stdout string) map[string]any {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("stdout data has unexpected shape:\n%s", stdout)
	}
	return data
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

	root.AddCommand(clicache.NewCommand(runtime))
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
