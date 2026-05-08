package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func TestLoadFileReadsWorkspaceProfilesAndAliases(t *testing.T) {
	path := writeConfig(t, `
schema_version = "1"
default_workspace = "default"

[workspaces.default]
name = "default"
team_id = "T123"
team_name = "Example"
token_type = "bot"
token = "keychain:slack-cli/default"
default_channel = "C123"
agent_attribution = true
agent_label = "agent mode"
agent_emoji = ":robot_face:"
agent_message = "Sent from local automation"
rate_limit_tier = "auto"

[workspaces.default.attribution]
enabled = false
label = "nested automation"
message = "Sent from nested automation"
emoji = ":sparkles:"

[workspaces.default.aliases]
alerts = "C123"
ops = "C456"
`)

	cfg, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile returned error: %v", err)
	}

	if cfg.SchemaVersion != "1" {
		t.Fatalf("SchemaVersion = %q, want 1", cfg.SchemaVersion)
	}
	if cfg.DefaultWorkspace != "default" {
		t.Fatalf("DefaultWorkspace = %q, want default", cfg.DefaultWorkspace)
	}

	workspace, err := cfg.ResolveWorkspace("")
	if err != nil {
		t.Fatalf("ResolveWorkspace returned error: %v", err)
	}
	if workspace.TeamID != "T123" {
		t.Fatalf("TeamID = %q, want T123", workspace.TeamID)
	}
	if workspace.TokenRef != "keychain:slack-cli/default" {
		t.Fatalf("TokenRef = %q, want keychain ref", workspace.TokenRef)
	}
	if got := workspace.Aliases["alerts"]; got != "C123" {
		t.Fatalf("alerts alias = %q, want C123", got)
	}
	settings := workspace.AgentSettings()
	if settings.Attribution {
		t.Fatal("AgentSettings Attribution = true, want nested enabled=false override")
	}
	if settings.Label != "nested automation" {
		t.Fatalf("AgentSettings Label = %q, want nested label override", settings.Label)
	}
	if settings.Message != "Sent from nested automation" {
		t.Fatalf("AgentSettings Message = %q, want nested message override", settings.Message)
	}
	if settings.Emoji != ":sparkles:" {
		t.Fatalf("AgentSettings Emoji = %q, want nested emoji override", settings.Emoji)
	}
}

func TestLoadFileLetsEnvironmentOverrideDefaultWorkspace(t *testing.T) {
	path := writeConfig(t, `
schema_version = "1"
default_workspace = "default"

[workspaces.default]
name = "default"
team_id = "T111"
token_type = "bot"
token = "keychain:slack-cli/default"

[workspaces.ci]
name = "ci"
team_id = "T222"
token_type = "user"
token = "keychain:slack-cli/ci"
`)
	t.Setenv("SLACK_CLI_DEFAULT_WORKSPACE", "ci")

	cfg, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile returned error: %v", err)
	}

	workspace, err := cfg.ResolveWorkspace("")
	if err != nil {
		t.Fatalf("ResolveWorkspace returned error: %v", err)
	}
	if workspace.Name != "ci" {
		t.Fatalf("resolved workspace = %q, want ci", workspace.Name)
	}
}

func TestSaveFileRoundTripsWithoutTokenValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	disabled := false
	cfg := &config.Config{
		SchemaVersion:    "1",
		DefaultWorkspace: "default",
		Workspaces: map[string]config.WorkspaceProfile{
			"default": {
				Name:           "default",
				TeamID:         "T123",
				TeamName:       "Example",
				TokenType:      config.TokenTypeBot,
				TokenRef:       "keychain:slack-cli/default",
				DefaultChannel: "C123",
				Attribution: config.AttributionConfig{
					Enabled: &disabled,
					Message: "Sent from tests",
					Emoji:   ":test_tube:",
				},
				Aliases: map[string]string{
					"alerts": "C123",
				},
			},
		},
	}

	if err := config.SaveFile(path, cfg); err != nil {
		t.Fatalf("SaveFile returned error: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("saved config is empty")
	}
	if strings.Contains(string(raw), "xoxb-") || strings.Contains(string(raw), "xoxp-") {
		t.Fatalf("saved config contains token value: %s", raw)
	}
	for _, fragment := range []string{
		`schema_version = "1"`,
		`default_workspace = "default"`,
		"[workspaces.default]\nname = \"default\"",
		"[workspaces.default.attribution]\nenabled = false\nmessage = \"Sent from tests\"",
		"[workspaces.default.aliases]\nalerts = \"C123\"",
	} {
		if !strings.Contains(string(raw), fragment) {
			t.Fatalf("saved config = %s, want fragment %q", raw, fragment)
		}
	}
	for _, fragment := range []string{"[workspaces]\n", "  [workspaces.default]", "    name ="} {
		if strings.Contains(string(raw), fragment) {
			t.Fatalf("saved config = %s, did not want encoder-style fragment %q", raw, fragment)
		}
	}
	for _, legacy := range []string{"agent_attribution", "agent_label", "agent_emoji", "agent_message"} {
		if strings.Contains(string(raw), legacy) {
			t.Fatalf("saved config = %s, did not want legacy key %q in new writes", raw, legacy)
		}
	}

	loaded, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile returned error: %v", err)
	}
	if loaded.Workspaces["default"].TokenRef != "keychain:slack-cli/default" {
		t.Fatalf("round-tripped token ref = %q", loaded.Workspaces["default"].TokenRef)
	}
}

func TestSaveFileOmitsAuthFieldsForPreferencesOnlyWorkspace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := &config.Config{
		SchemaVersion:    "1",
		DefaultWorkspace: "default",
		Workspaces: map[string]config.WorkspaceProfile{
			"default": {
				Name:           "default",
				DefaultChannel: "C123",
			},
		},
	}

	if err := config.SaveFile(path, cfg); err != nil {
		t.Fatalf("SaveFile returned error: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	for _, forbidden := range []string{"team_id", "team_name", "token_type", "token ="} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("saved config contains auth-owned field %q: %s", forbidden, raw)
		}
	}
	if _, err := config.LoadFile(path); err != nil {
		t.Fatalf("LoadFile returned error for preferences-only config: %v", err)
	}
}

func TestLoadFileMigratesMissingSchemaVersionToCurrentVersion(t *testing.T) {
	path := writeConfig(t, `
default_workspace = "default"

[workspaces.default]
name = "default"
team_id = "T123"
token_type = "bot"
token = "keychain:slack-cli/default"
`)

	cfg, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile returned error: %v", err)
	}
	if cfg.SchemaVersion != config.SchemaVersion {
		t.Fatalf("SchemaVersion = %q, want migrated current version", cfg.SchemaVersion)
	}
}

func TestLoadFileRejectsUnsupportedFutureSchemaVersionThroughMigrationPath(t *testing.T) {
	path := writeConfig(t, `
schema_version = "999"
default_workspace = "default"

[workspaces.default]
name = "default"
team_id = "T123"
token_type = "bot"
token = "keychain:slack-cli/default"
`)

	_, err := config.LoadFile(path)
	if err == nil {
		t.Fatal("LoadFile returned nil error, want unsupported migration failure")
	}
	if !strings.Contains(err.Error(), "no migration path") {
		t.Fatalf("error = %v, want no migration path", err)
	}
}

func TestLoadFileAcceptsLegacyTokenKeyAndMigratesToTokenRef(t *testing.T) {
	path := writeConfig(t, `
schema_version = "1"
default_workspace = "default"

[workspaces.default]
name = "default"
team_id = "T123"
token_type = "bot"
token = "keychain:slack-cli/default"
`)

	cfg, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile returned error for legacy token key: %v", err)
	}

	workspace, err := cfg.ResolveWorkspace("")
	if err != nil {
		t.Fatalf("ResolveWorkspace returned error: %v", err)
	}
	if workspace.TokenRef != "keychain:slack-cli/default" {
		t.Fatalf("TokenRef = %q, want keychain ref migrated from legacy token key", workspace.TokenRef)
	}
}

func TestLoadFilePreferTokenRefOverLegacyToken(t *testing.T) {
	path := writeConfig(t, `
schema_version = "1"
default_workspace = "default"

[workspaces.default]
name = "default"
team_id = "T123"
token_type = "bot"
token = "keychain:slack-cli/legacy"
token_ref = "keychain:slack-cli/current"
`)

	cfg, err := config.LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile returned error when both token keys present: %v", err)
	}

	workspace, err := cfg.ResolveWorkspace("")
	if err != nil {
		t.Fatalf("ResolveWorkspace returned error: %v", err)
	}
	if workspace.TokenRef != "keychain:slack-cli/current" {
		t.Fatalf("TokenRef = %q, want token_ref to take precedence over legacy token", workspace.TokenRef)
	}
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	return path
}
