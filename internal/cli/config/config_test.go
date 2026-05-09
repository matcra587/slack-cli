package config_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/matcra587/slack-cli/internal/agent"
	cliconfig "github.com/matcra587/slack-cli/internal/cli/config"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/spf13/cobra"
)

func TestMain(m *testing.M) {
	for _, key := range agent.KnownEnvVars() {
		_ = os.Unsetenv(key)
	}
	os.Exit(m.Run())
}

func TestConfigInitWritesPreferencesOnlyConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")

	stdout, stderr, err := executeRoot(t, nil, configPath, config.NewMemoryCredentialStore(), strings.NewReader(""), false,
		[]string{
			"config", "init",
			"--profile", "default",
			"--default-channel", "C7N2Q8L4P",
			"--attribution-emoji", ":rocket:",
			"--attribution-message", "Sent from deploy automation",
		},
	)
	if err != nil {
		t.Fatalf("config init returned error: %v\nstderr=%s", err, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, `"command":"config.init"`) {
		t.Fatalf("stdout = %s, want config.init envelope", stdout)
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(raw), "xox") {
		t.Fatalf("config contains plaintext token: %s", raw)
	}
	if strings.Contains(string(raw), "agent_attribution") {
		t.Fatalf("config contains legacy attribution key: %s", raw)
	}
	if !strings.Contains(string(raw), "[workspaces.default.attribution]\nenabled = true") {
		t.Fatalf("config = %s, want canonical attribution.enabled write", raw)
	}
	for _, forbidden := range []string{"team_id", "team_name", "token_type", "token ="} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("config contains auth-owned field %q: %s", forbidden, raw)
		}
	}
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	profile := cfg.Workspaces["default"]
	if cfg.DefaultWorkspace != "default" || profile.Name != "default" || profile.DefaultChannel != "C7N2Q8L4P" {
		t.Fatalf("config = %#v, want initialized preferences-only default profile", cfg)
	}
	settings := profile.AgentSettings()
	if settings.Emoji != ":rocket:" || settings.Message != "Sent from deploy automation" {
		t.Fatalf("AgentSettings = %#v, want custom emoji/message", settings)
	}
}

func TestConfigInitExposesCanonicalAttributionFlags(t *testing.T) {
	root := buildTestRoot(nil, "", "", config.NewMemoryCredentialStore(), strings.NewReader(""), false, &bytes.Buffer{}, &bytes.Buffer{})
	initCmd, _, err := root.Find([]string{"config", "init"})
	if err != nil {
		t.Fatalf("find config init: %v", err)
	}
	for _, name := range []string{"attribution-enabled", "attribution-label", "attribution-emoji", "attribution-message"} {
		flag := initCmd.Flags().Lookup(name)
		if flag == nil {
			t.Fatalf("config init flag %q is missing", name)
		}
		if flag.Hidden {
			t.Fatalf("config init flag %q is hidden; canonical attribution flags must be discoverable", name)
		}
	}
	aliases := map[string]string{
		"agent-attribution": "attribution-enabled",
		"agent-label":       "attribution-label",
		"agent-emoji":       "attribution-emoji",
		"agent-message":     "attribution-message",
	}
	for alias, canonical := range aliases {
		if got := initCmd.LocalFlags().Lookup(alias); got != nil {
			t.Errorf("legacy alias %q should not be a local pflag on config init; use SetNormalizeFunc instead", alias)
		}
		if got := initCmd.LocalFlags().Lookup(canonical); got == nil {
			t.Errorf("canonical local flag %q missing after alias %q check", canonical, alias)
		}
	}
}

func TestConfigRejectsAuthOwnedKeys(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	cfg := &config.Config{
		SchemaVersion:    config.SchemaVersion,
		DefaultWorkspace: "default",
		Workspaces: map[string]config.WorkspaceProfile{
			"default": {Name: "default"},
		},
	}
	if err := config.SaveFile(configPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	_, stderr, err := executeRoot(t, nil, configPath, config.NewMemoryCredentialStore(), strings.NewReader(""), false,
		[]string{
			"config", "set",
			"workspaces.default.token",
			"env:SLACK_CLI_TOKEN",
		},
	)
	if err == nil {
		t.Fatal("config set returned nil error, want auth-owned key rejection")
	}
	if !strings.Contains(stderr, "auth settings are managed by slick auth") {
		t.Fatalf("stderr = %q, want auth-owned key validation", stderr)
	}
}

func TestConfigInitRefusesExistingConfigWithoutForce(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte("sentinel"), 0o600); err != nil {
		t.Fatalf("write sentinel config: %v", err)
	}

	_, stderr, err := executeRoot(t, nil, configPath, config.NewMemoryCredentialStore(), strings.NewReader(""), false,
		[]string{
			"config", "init",
			"--profile", "default",
		},
	)
	if err == nil {
		t.Fatal("config init returned nil error, want overwrite protection")
	}
	if !strings.Contains(stderr, "config already exists") {
		t.Fatalf("stderr = %q, want config already exists", stderr)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(raw) != "sentinel" {
		t.Fatalf("config changed to %q, want sentinel preserved", raw)
	}
}

func TestConfigInitTTYPromptsBeforeOverwritingExistingConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte("sentinel"), 0o600); err != nil {
		t.Fatalf("write sentinel config: %v", err)
	}

	stdout, stderr, err := executeRoot(t, nil, configPath, config.NewMemoryCredentialStore(), lineReader("n\n"), true,
		[]string{"config", "init"},
	)
	if err != nil {
		t.Fatalf("config init returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stderr, "Overwrite existing config?") {
		t.Fatalf("stderr prompts = %q, want overwrite prompt", stderr)
	}
	if strings.Contains(stdout, "written=false") {
		t.Fatalf("stdout = %s, should omit false written field", stdout)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(raw) != "sentinel" {
		t.Fatalf("config changed to %q, want sentinel preserved", raw)
	}
}

func TestConfigInitTTYUsesHuhForm(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")

	stdout, stderr, err := executeRoot(t, nil, configPath, config.NewMemoryCredentialStore(),
		lineReader("default\nC7N2Q8L4P\ny\n:rocket:\nSent from config init\n"),
		true,
		[]string{"config", "init"},
	)
	if err != nil {
		t.Fatalf("config init returned error: %v\nstderr=%s", err, stderr)
	}
	for _, fragment := range []string{"Profile name", "Default message channel", "Attribution message"} {
		if !strings.Contains(stderr, fragment) {
			t.Fatalf("stderr prompts = %q, want fragment %q", stderr, fragment)
		}
	}
	for _, fragment := range []string{"Workspace ID", "Token reference", "Token type"} {
		if strings.Contains(stderr, fragment) {
			t.Fatalf("stderr prompts = %q, did not want auth prompt fragment %q", stderr, fragment)
		}
	}
	if !strings.Contains(stdout, "written=true") {
		t.Fatalf("stdout = %s, want written result", stdout)
	}
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	profile := cfg.Workspaces["default"]
	if profile.DefaultChannel != "C7N2Q8L4P" || profile.TeamID != "" || profile.TokenRef != "" {
		t.Fatalf("profile = %#v, want form values", profile)
	}
	if profile.Attribution.Emoji != ":rocket:" || profile.Attribution.Message != "Sent from config init" {
		t.Fatalf("attribution = %#v, want form attribution", profile.Attribution)
	}
}

func TestConfigPathListGetSetUnset(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	cfg := workspaceConfig(config.TokenTypeUser)
	cfg.Workspaces["default"] = config.WorkspaceProfile{
		Name:           "default",
		DefaultChannel: "C7N2Q8L4P",
	}
	if err := config.SaveFile(configPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	stdout, stderr, err := executeRoot(t, nil, configPath, config.NewMemoryCredentialStore(), strings.NewReader(""), false,
		[]string{"config", "path"},
	)
	if err != nil {
		t.Fatalf("config path returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, configPath) {
		t.Fatalf("stdout = %s, want config path", stdout)
	}

	stdout, stderr, err = executeRoot(t, nil, configPath, config.NewMemoryCredentialStore(), strings.NewReader(""), false,
		[]string{"config", "list", "--compact"},
	)
	if err != nil {
		t.Fatalf("config list returned error: %v\nstderr=%s", err, stderr)
	}
	var listed map[string]any
	if err := json.Unmarshal([]byte(stdout), &listed); err != nil {
		t.Fatalf("config list stdout is not JSON: %v\n%s", err, stdout)
	}
	if listed["default_workspace"] != "default" {
		t.Fatalf("config list = %#v, want default workspace", listed)
	}
	settings, ok := listed["settings"].([]any)
	if !ok {
		t.Fatalf("config list settings = %#v, want array", listed["settings"])
	}
	if !configListIncludesKey(settings, "workspaces.default.attribution.enabled") {
		t.Fatalf("config list settings = %#v, want attribution.enabled", settings)
	}

	stdout, stderr, err = executeRoot(t, nil, configPath, config.NewMemoryCredentialStore(), strings.NewReader(""), false,
		[]string{"config", "get", "workspaces.default.default_channel", "--compact"},
	)
	if err != nil {
		t.Fatalf("config get returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"value":"C7N2Q8L4P"`) {
		t.Fatalf("stdout = %s, want default channel value", stdout)
	}

	_, stderr, err = executeRoot(t, nil, configPath, config.NewMemoryCredentialStore(), strings.NewReader(""), false,
		[]string{"config", "set", "workspaces.default.attribution.message", "Sent from config"},
	)
	if err != nil {
		t.Fatalf("config set returned error: %v\nstderr=%s", err, stderr)
	}
	loaded, err := config.LoadFile(configPath)
	if err != nil {
		t.Fatalf("load after set: %v", err)
	}
	if loaded.Workspaces["default"].Attribution.Message != "Sent from config" {
		t.Fatalf("attribution message = %q, want set value", loaded.Workspaces["default"].Attribution.Message)
	}

	_, stderr, err = executeRoot(t, nil, configPath, config.NewMemoryCredentialStore(), strings.NewReader(""), false,
		[]string{"config", "unset", "workspaces.default.attribution.message"},
	)
	if err != nil {
		t.Fatalf("config unset returned error: %v\nstderr=%s", err, stderr)
	}
	loaded, err = config.LoadFile(configPath)
	if err != nil {
		t.Fatalf("load after unset: %v", err)
	}
	if loaded.Workspaces["default"].Attribution.Message != "" {
		t.Fatalf("attribution message = %q, want unset", loaded.Workspaces["default"].Attribution.Message)
	}
}

func configListIncludesKey(settings []any, key string) bool {
	for _, setting := range settings {
		entry, ok := setting.(map[string]any)
		if ok && entry["key"] == key {
			return true
		}
	}
	return false
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

type lineByLineReader struct {
	lines []string
}

func lineReader(value string) *lineByLineReader {
	return &lineByLineReader{lines: strings.SplitAfter(value, "\n")}
}

func (r *lineByLineReader) Read(p []byte) (int, error) {
	if len(r.lines) == 0 {
		return 0, io.EOF
	}
	line := r.lines[0]
	r.lines = r.lines[1:]
	return copy(p, line), nil
}

func buildTestRoot(cfg *config.Config, configPath, baseURL string, store config.CredentialStore, stdin io.Reader, isTTY bool, stdout, stderr *bytes.Buffer) *cobra.Command {
	runtime := &cliruntime.RootRuntime{
		Stdin:           stdin,
		Stdout:          stdout,
		Stderr:          stderr,
		IsTTY:           isTTY,
		Now:             func() time.Time { return time.Date(2026, 5, 3, 13, 8, 0, 0, time.UTC) },
		RequestID:       func() string { return "test-request" },
		ConfigPath:      configPath,
		CredentialStore: store,
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

	root.AddCommand(cliconfig.NewCommand(runtime))
	return root
}

func executeRoot(t *testing.T, cfg *config.Config, configPath string, store config.CredentialStore, stdin io.Reader, isTTY bool, args []string) (string, string, error) {
	t.Helper()
	stdoutBuf := &bytes.Buffer{}
	stderrBuf := &bytes.Buffer{}
	cmd := buildTestRoot(cfg, configPath, "http://example.invalid", store, stdin, isTTY, stdoutBuf, stderrBuf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdoutBuf.String(), stderrBuf.String(), err
}
