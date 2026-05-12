package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	termansi "github.com/gechr/x/ansi"
	"github.com/matcra587/slack-cli/internal/agent"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestTruncateText(t *testing.T) {
	for _, tc := range []struct {
		name       string
		input      string
		limit      int
		wantExact  string
		checkExact bool
	}{
		// UTF-8: each 🌍 is 4 bytes; old byte-slicing at limit-3 would corrupt the rune boundary.
		{name: "utf8_multibyte", input: "Hello 🌍🌍🌍🌍", limit: 10},
		// Empty input must pass through unchanged.
		{name: "empty_input", input: "", limit: 300, wantExact: "", checkExact: true},
		// limit < tail width ("..."=3 cells): xansi.Truncate returns ""; old code returned value[:limit].
		{name: "limit_less_than_tail", input: "hello", limit: 2, wantExact: "", checkExact: true},
		// Input shorter than limit: no truncation.
		{name: "short_input", input: "hi", limit: 300, wantExact: "hi", checkExact: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateText(tc.input, tc.limit)
			if !utf8.ValidString(got) {
				t.Fatalf("truncateText(%q, %d) = %q: invalid UTF-8", tc.input, tc.limit, got)
			}
			if tc.checkExact && got != tc.wantExact {
				t.Fatalf("truncateText(%q, %d) = %q, want %q", tc.input, tc.limit, got, tc.wantExact)
			}
			if termansi.StringWidth(got) > tc.limit {
				t.Fatalf("truncateText(%q, %d) = %q: display width %d exceeds limit", tc.input, tc.limit, got, termansi.StringWidth(got))
			}
		})
	}
}

func TestMain(m *testing.M) {
	clearAmbientAgentEnvironment()
	os.Exit(m.Run())
}

func clearAmbientAgentEnvironment() {
	for _, key := range agent.KnownEnvVars() {
		_ = os.Unsetenv(key)
	}
}

func TestNewRootCommandDefinesPersistentFlags(t *testing.T) {
	cmd := NewRootCommand()
	if cmd.Use != "slick" {
		t.Fatalf("Use = %q, want slick", cmd.Use)
	}

	for _, name := range []string{
		"workspace",
		"output",
		"no-throttle",
	} {
		if cmd.PersistentFlags().Lookup(name) == nil {
			t.Fatalf("persistent flag %q is missing", name)
		}
	}
}

func TestDefaultConfigPathUsesXDGConfigDirAndEnvOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "custom.toml")
	t.Setenv("SLICK_CONFIG", override)
	if got := defaultConfigPath(); got != override {
		t.Fatalf("defaultConfigPath SLICK_CONFIG override = %q, want %q", got, override)
	}

	t.Setenv("SLICK_CONFIG", "")
	t.Setenv("SLACK_CLI_CONFIG", override)
	if got := defaultConfigPath(); got != override {
		t.Fatalf("defaultConfigPath legacy SLACK_CLI_CONFIG override = %q, want %q", got, override)
	}

	overrideDir := t.TempDir()
	t.Setenv("SLACK_CLI_CONFIG_DIR", overrideDir)
	t.Setenv("SLICK_CONFIG", "$SLACK_CLI_CONFIG_DIR/custom.toml")
	if got, want := defaultConfigPath(), filepath.Join(overrideDir, "custom.toml"); got != want {
		t.Fatalf("defaultConfigPath expanded override = %q, want %q", got, want)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SLICK_CONFIG", "~/custom.toml")
	if got, want := defaultConfigPath(), filepath.Join(home, "custom.toml"); got != want {
		t.Fatalf("defaultConfigPath home override = %q, want %q", got, want)
	}

	t.Setenv("SLICK_CONFIG", "")
	t.Setenv("SLACK_CLI_CONFIG", "")
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	got := defaultConfigPath()
	want := filepath.Join(configHome, "slick", "config.toml")
	if got != want {
		t.Fatalf("defaultConfigPath = %q, want %q", got, want)
	}

	t.Setenv("XDG_CONFIG_HOME", "")
	home = t.TempDir()
	t.Setenv("HOME", home)
	got = defaultConfigPath()
	want = filepath.Join(home, ".config", "slick", "config.toml")
	if got != want {
		t.Fatalf("defaultConfigPath XDG fallback = %q, want %q", got, want)
	}
}

func TestNewRootCommandDefinesVisibleShortFlags(t *testing.T) {
	root := NewRootCommand()
	var missing []string
	var visit func(*cobra.Command)
	visit = func(cmd *cobra.Command) {
		cmd.LocalNonPersistentFlags().VisitAll(func(flag *pflag.Flag) {
			if flag.Hidden || flag.Shorthand != "" || flag.Name == "help" {
				return
			}
			// Attribution overrides are long-form only; they're niche
			// per-call overrides and the long names are self-documenting.
			// --attribution (force-on) is the symmetric partner of -z
			// --no-attribution and stays long-form for the same reason.
			if flag.Name == "attribution" || strings.HasPrefix(flag.Name, "attribution-") {
				return
			}
			missing = append(missing, cmd.CommandPath()+" --"+flag.Name)
		})
		if cmd == root {
			cmd.PersistentFlags().VisitAll(func(flag *pflag.Flag) {
				if flag.Hidden || flag.Shorthand != "" || flag.Name == "help" {
					return
				}
				missing = append(missing, cmd.CommandPath()+" --"+flag.Name)
			})
		}
		for _, child := range cmd.Commands() {
			visit(child)
		}
	}
	visit(root)
	if len(missing) > 0 {
		t.Fatalf("visible flags missing shorthand: %s", strings.Join(missing, ", "))
	}
}

func TestNewRootCommandUsesLookupForDiscovery(t *testing.T) {
	cmd := NewRootCommand()
	if lookup, _, err := cmd.Find([]string{"lookup"}); err != nil || lookup.Name() != "lookup" {
		t.Fatalf("root command missing lookup command: cmd=%v err=%v", lookup, err)
	}
	for _, child := range cmd.Commands() {
		switch child.Name() {
		case "channel", "dm", "user":
			t.Fatalf("root command exposes %q; use slick lookup channel/user and slick message send --user", child.CommandPath())
		}
	}
}

func TestNewRootCommandHidesDeferredCommandSurfaces(t *testing.T) {
	cmd := NewRootCommand()
	for _, name := range []string{"react", "reply"} {
		child := findDirectChild(cmd, name)
		if child == nil {
			t.Fatalf("root command missing promoted command %q", name)
		}
		if child.Hidden {
			t.Fatalf("root command %q is hidden; it should be public", name)
		}
	}
	for _, name := range []string{"file"} {
		child := findDirectChild(cmd, name)
		if child == nil {
			t.Fatalf("root command missing hidden command %q", name)
		}
		if !child.Hidden {
			t.Fatalf("root command %q is visible; it should stay hidden for now", name)
		}
	}
	for _, name := range []string{"reaction", "thread"} {
		if child := findDirectChild(cmd, name); child != nil {
			t.Fatalf("root command exposes legacy %q; use slick react or slick reply", child.CommandPath())
		}
	}
	if search := findDirectChild(cmd, "search"); search != nil {
		t.Fatalf("root command exposes %q; message search should live under lookup messages", search.CommandPath())
	}

	lookup := findDirectChild(cmd, "lookup")
	if lookup == nil {
		t.Fatal("root command missing lookup command")
	}
	messages := findDirectChild(lookup, "messages")
	if messages == nil {
		t.Fatal("lookup command missing messages command")
	}
	if messages.Hidden {
		t.Fatal("lookup messages is hidden; it should be public now that search scope requirements are documented")
	}
}

func findDirectChild(cmd *cobra.Command, name string) *cobra.Command {
	for _, child := range cmd.Commands() {
		if child.Name() == name {
			return child
		}
	}
	return nil
}

func TestNewCommandContextResolvesWorkspaceAndOutputMode(t *testing.T) {
	cfg := &config.Config{
		SchemaVersion:    "1",
		DefaultWorkspace: "default",
		Workspaces: map[string]config.WorkspaceProfile{
			"default": {
				Name:      "default",
				TeamID:    "T123",
				TokenType: config.TokenTypeBot,
				TokenRef:  "keychain:slack-cli/default",
			},
		},
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	ctx, attribution, err := NewCommandContext(RootOptions{
		Config: cfg,
		Output: OutputFlags{
			Output: clioutput.OutputJSON,
		},
		Stdout: stdout,
		Stderr: stderr,
		IsTTY:  true,
	})
	if err != nil {
		t.Fatalf("NewCommandContext returned error: %v", err)
	}
	if ctx.Workspace != "default" {
		t.Fatalf("Workspace = %q, want default", ctx.Workspace)
	}
	if ctx.Mode != RenderModeEnvelope {
		t.Fatalf("Mode = %v, want RenderModeEnvelope", ctx.Mode)
	}
	if attribution.Enabled {
		t.Fatalf("Attribution enabled without agent trigger: %#v", attribution)
	}
}

func TestNewCommandContextUsesNestedProfileAttributionConfig(t *testing.T) {
	clearAgentEnvironment(t)
	profileOn := true
	cfg := &config.Config{
		SchemaVersion:    "1",
		DefaultWorkspace: "default",
		Workspaces: map[string]config.WorkspaceProfile{
			"default": {
				Name:      "default",
				TeamID:    "T123",
				TokenType: config.TokenTypeBot,
				TokenRef:  "keychain:slack-cli/default",
				Attribution: config.AttributionConfig{
					Enabled: &profileOn,
					Message: "Sent from config",
					Emoji:   ":rocket:",
				},
			},
		},
	}

	ctx, attribution, err := NewCommandContext(RootOptions{
		Config: cfg,
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
		IsTTY:  true,
	})
	if err != nil {
		t.Fatalf("NewCommandContext returned error: %v", err)
	}
	if !attribution.Enabled {
		t.Fatal("Attribution Enabled = false, want profile attribution to force enabled")
	}
	if attribution.Message != "Sent from config" {
		t.Fatalf("Attribution Message = %q, want config message", attribution.Message)
	}
	if attribution.Emoji != ":rocket:" {
		t.Fatalf("Attribution Emoji = %q, want config emoji", attribution.Emoji)
	}
	if ctx.Mode != RenderModePlain {
		t.Fatalf("profile-attributed TTY mode = %v, want RenderModePlain", ctx.Mode)
	}
}

func TestNewCommandContextProfileAttributionUsesSlackCLIMessageUntilAgentDetected(t *testing.T) {
	clearAgentEnvironment(t)
	profileOn := true
	cfg := &config.Config{
		SchemaVersion:    "1",
		DefaultWorkspace: "default",
		Workspaces: map[string]config.WorkspaceProfile{
			"default": {
				Name:      "default",
				TeamID:    "T123",
				TokenType: config.TokenTypeBot,
				TokenRef:  "keychain:slack-cli/default",
				Attribution: config.AttributionConfig{
					Enabled: &profileOn,
				},
			},
		},
	}

	ctx, attribution, err := NewCommandContext(RootOptions{
		Config: cfg,
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
		IsTTY:  true,
	})
	if err != nil {
		t.Fatalf("NewCommandContext returned error: %v", err)
	}
	if !attribution.Enabled {
		t.Fatal("Attribution Enabled = false, want profile attribution enabled")
	}
	if attribution.Message != "Sent via slick" {
		t.Fatalf("Attribution Message = %q, want slack-cli message", attribution.Message)
	}
	if ctx.Mode != RenderModePlain {
		t.Fatalf("profile-attributed TTY mode = %v, want RenderModePlain", ctx.Mode)
	}

	t.Setenv("CLAUDE_CODE", "1")
	ctx, attribution, err = NewCommandContext(RootOptions{
		Config: cfg,
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
		IsTTY:  true,
	})
	if err != nil {
		t.Fatalf("NewCommandContext with agent env returned error: %v", err)
	}
	if attribution.Message != "Sent via slick (agent mode)" {
		t.Fatalf("Attribution Message = %q, want agent-mode suffix", attribution.Message)
	}
	if ctx.Mode != RenderModeEnvelope {
		t.Fatalf("agent-detected TTY mode = %v, want RenderModeEnvelope", ctx.Mode)
	}
}

func TestWriteWorkspacesPopulatedSliceGoesViaTableRenderer(t *testing.T) {
	ctx, stdout, stderr := newOutputTestContext(RenderModePlain)

	workspaces := []config.WorkspaceProfile{
		{
			Name:      "default",
			TeamID:    "T8KQ42P9D",
			TeamName:  "Example",
			TokenType: config.TokenTypeBot,
			TokenRef:  "keychain:slack-cli/default",
		},
	}

	if err := ctx.WriteWorkspaces("workspace.list", workspaces, nil); err != nil {
		t.Fatalf("WriteWorkspaces returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	got := stdout.String()
	if strings.Contains(got, "token=") || strings.Contains(got, "token_ref=") {
		t.Fatalf("stdout = %q, WriteWorkspaces populated path must not emit per-row events — only table renderer output", got)
	}
	if !strings.Contains(got, "default") {
		t.Fatalf("stdout = %q, want workspace name in table output", got)
	}
}

func clearAgentEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range agent.KnownEnvVars() {
		t.Setenv(key, "")
	}
}

func TestNewCommandContextNestedProfileAttributionOptOutBeatsAgentEnv(t *testing.T) {
	t.Setenv("CLAUDE_CODE", "1")
	profileOff := false
	cfg := &config.Config{
		SchemaVersion:    "1",
		DefaultWorkspace: "default",
		Workspaces: map[string]config.WorkspaceProfile{
			"default": {
				Name:      "default",
				TeamID:    "T123",
				TokenType: config.TokenTypeBot,
				TokenRef:  "keychain:slack-cli/default",
				Attribution: config.AttributionConfig{
					Enabled: &profileOff,
				},
			},
		},
	}

	ctx, attribution, err := NewCommandContext(RootOptions{
		Config: cfg,
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
		IsTTY:  true,
	})
	if err != nil {
		t.Fatalf("NewCommandContext returned error: %v", err)
	}
	if attribution.Enabled {
		t.Fatalf("Attribution enabled despite profile opt-out: %#v", attribution)
	}
	if ctx.Mode != RenderModeEnvelope {
		t.Fatalf("agent-detected TTY mode = %v, want RenderModeEnvelope", ctx.Mode)
	}
}

func TestNewCommandContextWiresAgentDetection(t *testing.T) {
	t.Setenv("CLAUDE_CODE", "1")

	ctx, attribution, err := NewCommandContext(RootOptions{
		Output:      OutputFlags{},
		Attribution: AttributionFlags{},
		Stdout:      &bytes.Buffer{},
		Stderr:      &bytes.Buffer{},
		IsTTY:       true,
	})
	if err != nil {
		t.Fatalf("NewCommandContext returned error: %v", err)
	}
	if !attribution.Enabled {
		t.Fatal("Attribution Enabled = false, want true")
	}
	if attribution.Category != agent.CategoryAI {
		t.Fatalf("Attribution Category = %q, want AI", attribution.Category)
	}
	if ctx.Mode != RenderModeEnvelope {
		t.Fatalf("agent TTY mode = %v, want RenderModeEnvelope", ctx.Mode)
	}
}

func TestCommandSurfacesConfigLoadError(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(`
schema_version = "1"
default_workspace = "default"

[workspaces.default]
name = "default"

[workspaces.Default]
name = "Default"
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := NewRootCommand(
		WithConfigPath(configPath),
		WithCredentialStore(config.NewMemoryCredentialStore()),
		WithIO(strings.NewReader(""), stdout, stderr),
		WithTTY(false),
	)
	cmd.SetArgs([]string{"message", "send", "--channel", "C123", "--message", "hello"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute returned nil error, want config load error")
	}
	if !strings.Contains(stderr.String(), "duplicate workspace profile") {
		t.Fatalf("stderr = %q, want duplicate workspace profile error", stderr.String())
	}
}

func TestCommandSurfacesMissingConfigWithInitRemediation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(home, ".config", "slick", "missing.toml")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := NewRootCommand(
		WithConfigPath(configPath),
		WithCredentialStore(config.NewMemoryCredentialStore()),
		WithIO(strings.NewReader(""), stdout, stderr),
		WithTTY(false),
	)
	cmd.SetArgs([]string{"message", "send", "--channel", "C123", "--message", "hello"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute returned nil error, want missing config error")
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	for _, want := range []string{"config file not found", "~/.config/slick/missing.toml", "slick config init"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
	if strings.Contains(stderr.String(), home) {
		t.Fatalf("stderr = %q, should not expose absolute home path %q", stderr.String(), home)
	}
	if strings.Contains(stderr.String(), "open ") {
		t.Fatalf("stderr = %q, should not expose raw open error", stderr.String())
	}
}

func TestConfigInitWorksWhenDefaultConfigIsMissing(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd := NewRootCommand(
		WithConfigPath(configPath),
		WithCredentialStore(config.NewMemoryCredentialStore()),
		WithIO(strings.NewReader(""), stdout, stderr),
		WithTTY(false),
	)
	cmd.SetArgs([]string{
		"config", "init",
		"--profile", "default",
		"--default-channel", "C123",
	})

	err := cmd.Execute()
	if err != nil {
		t.Fatalf("config init returned error: %v\nstderr=%s", err, stderr.String())
	}
	if stderr.String() != "" {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("config init did not write %s: %v", configPath, err)
	}
}
