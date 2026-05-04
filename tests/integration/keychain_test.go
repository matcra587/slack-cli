package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func TestFakeCredentialBackendStoresAndRemovesTokenWithoutTOMLLeak(t *testing.T) {
	store := config.NewMemoryCredentialStore()
	secret, err := config.EncodeCredential(config.CredentialPayload{AccessToken: "xoxb-secret"})
	if err != nil {
		t.Fatalf("encode credential: %v", err)
	}
	if err := store.Set("slack-cli", "default", secret); err != nil {
		t.Fatalf("store token: %v", err)
	}
	stored, err := store.Get("slack-cli", "default")
	if err != nil {
		t.Fatalf("stored credential err=%v", err)
	}
	if stored == "xoxb-secret" {
		t.Fatal("stored credential is raw token")
	}
	credential, err := config.DecodeCredential(stored)
	if err != nil {
		t.Fatalf("decode credential: %v", err)
	}
	if credential.AccessToken != "xoxb-secret" {
		t.Fatalf("access token = %q, want xoxb-secret", credential.AccessToken)
	}

	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := &config.Config{
		SchemaVersion:    config.SchemaVersion,
		DefaultWorkspace: "default",
		Workspaces: map[string]config.WorkspaceProfile{
			"default": {Name: "default", TeamID: "T123", TokenType: config.TokenTypeBot, TokenRef: "keychain:slack-cli/default"},
		},
	}
	if err := config.SaveFile(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "xoxb-secret") {
		t.Fatalf("config leaked token: %s", string(raw))
	}
	if err := store.Delete("slack-cli", "default"); err != nil {
		t.Fatalf("delete token: %v", err)
	}
	if _, err := store.Get("slack-cli", "default"); err == nil {
		t.Fatal("token still present after delete")
	}
}
