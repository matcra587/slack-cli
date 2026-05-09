package token_test

import (
	"context"
	"strings"
	"testing"
	"time"

	clitoken "github.com/matcra587/slack-cli/internal/cli/token"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

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

	token, err := (clitoken.CredentialTokenResolver{Store: store}).ResolveToken(context.Background(), config.WorkspaceProfile{
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

	token, err := (clitoken.CredentialTokenResolver{}).ResolveToken(context.Background(), config.WorkspaceProfile{
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

	token, err := (clitoken.CredentialTokenResolver{Store: store}).ResolveToken(context.Background(), config.WorkspaceProfile{
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
	token, err = (clitoken.CredentialTokenResolver{Store: store}).ResolveToken(context.Background(), config.WorkspaceProfile{
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

func TestCredentialTokenResolverRejectsLegacyRawSecret(t *testing.T) {
	store := config.NewMemoryCredentialStore()
	if err := store.Set("slack-cli", "default", "xoxb-secret"); err != nil {
		t.Fatalf("store raw credential: %v", err)
	}

	_, err := (clitoken.CredentialTokenResolver{Store: store}).ResolveToken(context.Background(), config.WorkspaceProfile{
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
	reader := clitoken.SecretReaderFunc(func(_ context.Context, ref string) (string, error) {
		if ref != "op://Slack CLI/test/credential" {
			t.Fatalf("1Password ref = %q", ref)
		}
		return secret, nil
	})

	token, err := (clitoken.CredentialTokenResolver{SecretReader: reader}).ResolveToken(context.Background(), config.WorkspaceProfile{
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

	token, err := (clitoken.CredentialTokenResolver{
		Store:        store,
		SlackBaseURL: server.BaseURL(),
		Now:          func() time.Time { return now },
	}).ResolveToken(context.Background(), config.WorkspaceProfile{
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

func TestCredentialTokenResolverRefreshPropagatesCancelledContext(t *testing.T) {
	// Verify that a canceled context propagates through resolveStoredCredential into oauthRefreshToken.
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

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled

	_, err = (clitoken.CredentialTokenResolver{
		Store: store,
		Now:   func() time.Time { return now },
	}).ResolveToken(ctx, config.WorkspaceProfile{
		Name:     "default",
		TokenRef: "keychain:slack-cli/default",
	})
	if err == nil {
		t.Fatal("expected error from canceled context, got nil")
	}
}
