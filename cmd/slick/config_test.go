package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func TestConfigInitWritesPreferencesOnlyConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")

	stdout, stderr, err := executeAuthRoot(t, nil, configPath, config.NewMemoryCredentialStore(), "http://example.invalid",
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
	root := NewRootCommand()
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
	for _, name := range []string{"agent-attribution", "agent-label", "agent-emoji", "agent-message"} {
		flag := initCmd.Flags().Lookup(name)
		if flag == nil {
			t.Fatalf("legacy config init alias %q is missing", name)
		}
		if !flag.Hidden {
			t.Fatalf("legacy config init alias %q is visible; docs/schema should expose attribution.* flags", name)
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

	_, stderr, err := executeAuthRoot(t, nil, configPath, config.NewMemoryCredentialStore(), "http://example.invalid",
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

	_, stderr, err := executeAuthRoot(t, nil, configPath, config.NewMemoryCredentialStore(), "http://example.invalid",
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

	stdout, stderr, err := executeAuthRootWithInput(t, nil, configPath, config.NewMemoryCredentialStore(), "http://example.invalid",
		lineReader("n\n"),
		true,
		[]string{"config", "init"},
	)
	if err != nil {
		t.Fatalf("config init returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stderr, "Overwrite existing config?") {
		t.Fatalf("stderr prompts = %q, want overwrite prompt", stderr)
	}
	if !strings.Contains(stdout, "written=false") {
		t.Fatalf("stdout = %s, want unchanged result", stdout)
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

	stdout, stderr, err := executeAuthRootWithInput(t, nil, configPath, config.NewMemoryCredentialStore(), "http://example.invalid",
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

	stdout, stderr, err := executeAuthRoot(t, nil, configPath, config.NewMemoryCredentialStore(), "http://example.invalid",
		[]string{"config", "path"},
	)
	if err != nil {
		t.Fatalf("config path returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, configPath) {
		t.Fatalf("stdout = %s, want config path", stdout)
	}

	stdout, stderr, err = executeAuthRoot(t, nil, configPath, config.NewMemoryCredentialStore(), "http://example.invalid",
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

	stdout, stderr, err = executeAuthRoot(t, nil, configPath, config.NewMemoryCredentialStore(), "http://example.invalid",
		[]string{"config", "get", "workspaces.default.default_channel", "--compact"},
	)
	if err != nil {
		t.Fatalf("config get returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"value":"C7N2Q8L4P"`) {
		t.Fatalf("stdout = %s, want default channel value", stdout)
	}

	_, stderr, err = executeAuthRoot(t, nil, configPath, config.NewMemoryCredentialStore(), "http://example.invalid",
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

	_, stderr, err = executeAuthRoot(t, nil, configPath, config.NewMemoryCredentialStore(), "http://example.invalid",
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
