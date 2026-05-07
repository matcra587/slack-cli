package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/matcra587/slack-cli/internal/agent"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

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
		"json",
		"plain",
		"compact",
		"raw",
		"agent",
		"no-agent-attribution",
		"agent-label",
		"agent-emoji",
		"agent-message",
		"no-throttle",
	} {
		if cmd.PersistentFlags().Lookup(name) == nil {
			t.Fatalf("persistent flag %q is missing", name)
		}
	}
	for _, name := range []string{"agent-attribution-mode", "agent-color"} {
		if cmd.PersistentFlags().Lookup(name) != nil {
			t.Fatalf("persistent flag %q should not exist", name)
		}
	}
}

func TestDefaultConfigPathUsesXDGConfigDirAndEnvOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "custom.toml")
	t.Setenv("SLACK_CLI_CONFIG", override)
	if got := defaultConfigPath(); got != override {
		t.Fatalf("defaultConfigPath override = %q, want %q", got, override)
	}

	overrideDir := t.TempDir()
	t.Setenv("SLACK_CLI_CONFIG_DIR", overrideDir)
	t.Setenv("SLACK_CLI_CONFIG", "$SLACK_CLI_CONFIG_DIR/custom.toml")
	if got, want := defaultConfigPath(), filepath.Join(overrideDir, "custom.toml"); got != want {
		t.Fatalf("defaultConfigPath expanded override = %q, want %q", got, want)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SLACK_CLI_CONFIG", "~/custom.toml")
	if got, want := defaultConfigPath(), filepath.Join(home, "custom.toml"); got != want {
		t.Fatalf("defaultConfigPath home override = %q, want %q", got, want)
	}

	t.Setenv("SLACK_CLI_CONFIG", "")
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	got := defaultConfigPath()
	want := filepath.Join(configHome, "slack-cli", "config.toml")
	if got != want {
		t.Fatalf("defaultConfigPath = %q, want %q", got, want)
	}

	t.Setenv("XDG_CONFIG_HOME", "")
	home = t.TempDir()
	t.Setenv("HOME", home)
	got = defaultConfigPath()
	want = filepath.Join(home, ".config", "slack-cli", "config.toml")
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

func TestExecuteRejectsMutuallyExclusiveOutputModes(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "json plain", args: []string{"--json", "--plain", "version"}},
		{name: "json compact", args: []string{"--json", "--compact", "version"}},
		{name: "json raw", args: []string{"--json", "--raw", "version"}},
		{name: "plain compact", args: []string{"--plain", "--compact", "version"}},
		{name: "plain raw", args: []string{"--plain", "--raw", "version"}},
		{name: "compact raw", args: []string{"--compact", "--raw", "version"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}
			cmd := NewRootCommand(WithConfig(nil), WithIO(strings.NewReader(""), stdout, stderr), WithTTY(false))
			cmd.SetArgs(tt.args)

			err := cmd.Execute()
			if err == nil {
				t.Fatal("Execute returned nil error, want output-mode validation error")
			}
			var commandErr CommandError
			if !errors.As(err, &commandErr) {
				t.Fatalf("Execute error = %T %[1]v, want CommandError", err)
			}
			if commandErr.CLIError.Type != ErrorTypeValidation || commandErr.CLIError.ExitCode != ExitCodeValidation {
				t.Fatalf("CLIError = %#v, want validation exit 4", commandErr.CLIError)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			if !strings.Contains(stderr.String(), `"type":"validation_error"`) {
				t.Fatalf("stderr = %q, want structured validation_error", stderr.String())
			}
		})
	}
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
			JSON: true,
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
	if ctx.Mode != OutputModeJSON {
		t.Fatalf("Mode = %q, want json", ctx.Mode)
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
	if ctx.Mode != OutputModePlain {
		t.Fatalf("profile-attributed TTY mode = %q, want plain", ctx.Mode)
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
	if ctx.Mode != OutputModePlain {
		t.Fatalf("profile-attributed TTY mode = %q, want plain", ctx.Mode)
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
	if ctx.Mode != OutputModeJSON {
		t.Fatalf("agent-detected TTY mode = %q, want json", ctx.Mode)
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
	if ctx.Mode != OutputModeJSON {
		t.Fatalf("agent-detected TTY mode = %q, want json even when attribution is disabled", ctx.Mode)
	}
}

func TestNewCommandContextWiresAgentDetection(t *testing.T) {
	t.Setenv("CLAUDE_CODE", "1")

	ctx, attribution, err := NewCommandContext(RootOptions{
		Output: OutputFlags{},
		Agent:  AgentFlags{},
		Stdout: &bytes.Buffer{},
		Stderr: &bytes.Buffer{},
		IsTTY:  true,
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
	if ctx.Mode != OutputModeJSON {
		t.Fatalf("agent TTY mode = %q, want JSON", ctx.Mode)
	}
}

func TestCredentialTokenResolverPrefersRuntimeEnvOverrides(t *testing.T) {
	store := config.NewMemoryCredentialStore()
	secret, err := config.EncodeCredential(config.CredentialPayload{AccessToken: "xoxb-configured"})
	if err != nil {
		t.Fatalf("encode credential: %v", err)
	}
	if err := store.Set("slack-cli", "default", secret); err != nil {
		t.Fatalf("store credential: %v", err)
	}
	t.Setenv("SLACK_CLI_TOKEN_DEFAULT", "xoxb-profile-env")
	t.Setenv("SLACK_CLI_TOKEN", "xoxb-global-env")

	token, err := (CredentialTokenResolver{Store: store}).ResolveToken(config.WorkspaceProfile{
		Name:      "default",
		TeamID:    "T123",
		TokenType: config.TokenTypeBot,
		TokenRef:  "keychain:slack-cli/default",
	})
	if err != nil {
		t.Fatalf("ResolveToken returned error: %v", err)
	}
	if token != "xoxb-profile-env" {
		t.Fatalf("token = %q, want profile-specific env override", token)
	}
}

func TestCredentialTokenResolverUsesNormalizedProfileEnvName(t *testing.T) {
	t.Setenv("SLACK_CLI_TOKEN_PROD_ENV", "xoxp-profile-env")

	token, err := (CredentialTokenResolver{}).ResolveToken(config.WorkspaceProfile{
		Name:      "prod-env",
		TeamID:    "T123",
		TokenType: config.TokenTypeUser,
		TokenRef:  "keychain:slack-cli/prod-env",
	})
	if err != nil {
		t.Fatalf("ResolveToken returned error: %v", err)
	}
	if token != "xoxp-profile-env" {
		t.Fatalf("token = %q, want normalized profile env token", token)
	}
}

func TestCredentialTokenResolverFallsBackThroughEnvPrecedence(t *testing.T) {
	store := config.NewMemoryCredentialStore()
	secret, err := config.EncodeCredential(config.CredentialPayload{AccessToken: "xoxb-configured"})
	if err != nil {
		t.Fatalf("encode credential: %v", err)
	}
	if err := store.Set("slack-cli", "default", secret); err != nil {
		t.Fatalf("store credential: %v", err)
	}
	t.Setenv("SLACK_CLI_TOKEN_DEFAULT", "")
	t.Setenv("SLACK_CLI_TOKEN", "xoxb-global-env")

	token, err := (CredentialTokenResolver{Store: store}).ResolveToken(config.WorkspaceProfile{
		Name:      "default",
		TeamID:    "T123",
		TokenType: config.TokenTypeBot,
		TokenRef:  "keychain:slack-cli/default",
	})
	if err != nil {
		t.Fatalf("ResolveToken returned error: %v", err)
	}
	if token != "xoxb-global-env" {
		t.Fatalf("token = %q, want global env token after empty profile env", token)
	}

	t.Setenv("SLACK_CLI_TOKEN", "")
	token, err = (CredentialTokenResolver{Store: store}).ResolveToken(config.WorkspaceProfile{
		Name:      "default",
		TeamID:    "T123",
		TokenType: config.TokenTypeBot,
		TokenRef:  "keychain:slack-cli/default",
	})
	if err != nil {
		t.Fatalf("ResolveToken returned error after env fallback: %v", err)
	}
	if token != "xoxb-configured" {
		t.Fatalf("token = %q, want configured credential after empty env values", token)
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

func TestCredentialTokenResolverRejectsLegacyRawSecret(t *testing.T) {
	store := config.NewMemoryCredentialStore()
	if err := store.Set("slack-cli", "default", "xoxb-secret"); err != nil {
		t.Fatalf("store raw credential: %v", err)
	}

	_, err := (CredentialTokenResolver{Store: store}).ResolveToken(config.WorkspaceProfile{
		Name:      "default",
		TeamID:    "T123",
		TokenType: config.TokenTypeBot,
		TokenRef:  "keychain:slack-cli/default",
	})
	if err == nil {
		t.Fatal("ResolveToken accepted legacy raw secret")
	}
	if !strings.Contains(err.Error(), "structured credential") {
		t.Fatalf("ResolveToken error = %q, want structured credential", err.Error())
	}
}

func TestCredentialTokenResolverReadsStructuredOnePasswordSecret(t *testing.T) {
	secret, err := config.EncodeCredential(config.CredentialPayload{AccessToken: "xoxp-from-1password"})
	if err != nil {
		t.Fatalf("encode credential: %v", err)
	}
	old := readOnePasswordSecret
	readOnePasswordSecret = func(ref string) (string, error) {
		if ref != "op://Slack CLI/test/credential" {
			t.Fatalf("1Password ref = %q", ref)
		}
		return secret, nil
	}
	t.Cleanup(func() { readOnePasswordSecret = old })

	token, err := (CredentialTokenResolver{}).ResolveToken(config.WorkspaceProfile{
		Name:      "test",
		TeamID:    "T123",
		TokenType: config.TokenTypeUser,
		TokenRef:  "op://Slack CLI/test/credential",
	})
	if err != nil {
		t.Fatalf("ResolveToken returned error: %v", err)
	}
	if token != "xoxp-from-1password" {
		t.Fatalf("token = %q, want xoxp-from-1password", token)
	}
}

func TestCredentialTokenResolverRefreshesExpiringKeychainCredential(t *testing.T) {
	now := time.Date(2026, 5, 3, 20, 10, 0, 0, time.UTC)
	expiresAt := now.Add(time.Minute)
	store := config.NewMemoryCredentialStore()
	secret, err := config.EncodeCredential(config.CredentialPayload{
		AccessToken:  "xoxp-old",
		RefreshToken: "refresh-old",
		ExpiresAt:    &expiresAt,
		ClientID:     "C123",
	})
	if err != nil {
		t.Fatalf("encode credential: %v", err)
	}
	if err := store.Set("slack-cli", "default", secret); err != nil {
		t.Fatalf("store credential: %v", err)
	}
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"oauth.v2.access": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("grant_type"); got != "refresh_token" {
				t.Fatalf("grant_type = %q, want refresh_token", got)
			}
			if got := req.Form.Get("refresh_token"); got != "refresh-old" {
				t.Fatalf("refresh_token = %q, want refresh-old", got)
			}
			if got := req.Form.Get("client_id"); got != "C123" {
				t.Fatalf("client_id = %q, want C123", got)
			}
			if _, ok := req.Form["client_secret"]; ok {
				t.Fatalf("client_secret was sent, want omitted for PKCE refresh")
			}
			return testutil.JSONResponse(`{"ok":true,"authed_user":{"access_token":"xoxp-new","refresh_token":"refresh-new","expires_in":7200,"token_type":"user"}}`)
		},
	})
	defer server.Close()

	token, err := (CredentialTokenResolver{
		Store:        store,
		SlackBaseURL: server.BaseURL(),
		Now:          func() time.Time { return now },
	}).ResolveToken(config.WorkspaceProfile{
		Name:      "default",
		TeamID:    "T123",
		TokenType: config.TokenTypeUser,
		TokenRef:  "keychain:slack-cli/default",
	})
	if err != nil {
		t.Fatalf("ResolveToken returned error: %v", err)
	}
	if token != "xoxp-new" {
		t.Fatalf("token = %q, want xoxp-new", token)
	}
	updatedSecret, err := store.Get("slack-cli", "default")
	if err != nil {
		t.Fatalf("get updated credential: %v", err)
	}
	updated, err := config.DecodeCredential(updatedSecret)
	if err != nil {
		t.Fatalf("decode updated credential: %v", err)
	}
	if updated.AccessToken != "xoxp-new" || updated.RefreshToken != "refresh-new" {
		t.Fatalf("updated credential = %#v, want refreshed tokens", updated)
	}
	if updated.ExpiresAt == nil || !updated.ExpiresAt.Equal(now.Add(2*time.Hour)) {
		t.Fatalf("updated ExpiresAt = %v, want %v", updated.ExpiresAt, now.Add(2*time.Hour))
	}
}
