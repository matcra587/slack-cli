package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/matcra587/slack-cli/internal/agent"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestNewRootCommandDefinesPersistentFlags(t *testing.T) {
	cmd := NewRootCommand()
	if cmd.Use != "slack" {
		t.Fatalf("Use = %q, want slack", cmd.Use)
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
