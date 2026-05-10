package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

func LoadFile(path string) (*Config, error) {
	cfg := &Config{}
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, fmt.Errorf("loading config file: %w", err)
	}

	migrateTokenRefs(cfg)
	applyEnv(cfg)
	cleanWorkspaceKeys(cfg)
	if err := Migrate(cfg); err != nil {
		return nil, err
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// cleanWorkspaceKeys trims surrounding whitespace from workspace map
// keys, the embedded profile names, and the DefaultWorkspace selector.
// Callers (LoadFile/SaveFile, mutation helpers) rely on this to ensure
// downstream lookups never need to TrimSpace again.
func cleanWorkspaceKeys(cfg *Config) {
	if cfg == nil {
		return
	}
	cfg.DefaultWorkspace = strings.TrimSpace(cfg.DefaultWorkspace)
	if len(cfg.Workspaces) == 0 {
		return
	}
	cleaned := make(map[string]WorkspaceProfile, len(cfg.Workspaces))
	for key, workspace := range cfg.Workspaces {
		trimmed := strings.TrimSpace(key)
		workspace.Name = strings.TrimSpace(workspace.Name)
		cleaned[trimmed] = workspace
	}
	cfg.Workspaces = cleaned
}

// migrateTokenRefs upgrades workspaces that still use the legacy "token" TOML
// key to the current "token_ref" key. LegacyToken is always zeroed so it cannot
// round-trip back into TOML via SaveFile.
func migrateTokenRefs(cfg *Config) {
	if cfg == nil {
		return
	}
	for key, workspace := range cfg.Workspaces {
		if workspace.TokenRef == "" && workspace.LegacyToken != "" {
			workspace.TokenRef = workspace.LegacyToken
		}
		workspace.LegacyToken = ""
		cfg.Workspaces[key] = workspace
	}
}

func Migrate(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}
	switch cfg.SchemaVersion {
	case "":
		cfg.SchemaVersion = SchemaVersion
		return nil
	case SchemaVersion:
		return nil
	default:
		return fmt.Errorf("no migration path from schema_version %q to %q", cfg.SchemaVersion, SchemaVersion)
	}
}

func SaveFile(path string, cfg *Config) error {
	cleanWorkspaceKeys(cfg)
	if err := cfg.Validate(); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	if err := os.WriteFile(path, []byte(encodeConfigTOML(cfg)), 0o600); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}
	return nil
}

func encodeConfigTOML(cfg *Config) string {
	var b strings.Builder
	writeTOMLString(&b, "schema_version", cfg.SchemaVersion)
	writeTOMLString(&b, "default_workspace", cfg.DefaultWorkspace)

	names := make([]string, 0, len(cfg.Workspaces))
	for name := range cfg.Workspaces {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		workspace := cfg.Workspaces[name]
		b.WriteString("\n")
		writeTOMLTable(&b, "workspaces", name)
		writeTOMLString(&b, "name", workspace.Name)
		writeOptionalTOMLString(&b, "team_id", workspace.TeamID)
		writeOptionalTOMLString(&b, "team_name", workspace.TeamName)
		writeOptionalTOMLString(&b, "token_type", string(workspace.TokenType))
		writeOptionalTOMLString(&b, "token_ref", workspace.TokenRef)
		writeOptionalTOMLString(&b, "default_channel", workspace.DefaultChannel)
		writeOptionalTOMLString(&b, "rate_limit_tier", workspace.RateLimitTier)

		attribution := canonicalAttributionConfig(workspace)
		if attribution.Enabled != nil || attribution.Message != "" || attribution.Label != "" || attribution.Emoji != "" {
			b.WriteString("\n")
			writeTOMLTable(&b, "workspaces", name, "attribution")
			if attribution.Enabled != nil {
				writeTOMLBool(&b, "enabled", *attribution.Enabled)
			}
			writeOptionalTOMLString(&b, "message", attribution.Message)
			writeOptionalTOMLString(&b, "label", attribution.Label)
			writeOptionalTOMLString(&b, "emoji", attribution.Emoji)
		}

		if len(workspace.Aliases) > 0 {
			b.WriteString("\n")
			writeTOMLTable(&b, "workspaces", name, "aliases")
			aliases := make([]string, 0, len(workspace.Aliases))
			for alias := range workspace.Aliases {
				aliases = append(aliases, alias)
			}
			sort.Strings(aliases)
			for _, alias := range aliases {
				writeTOMLString(&b, tomlKey(alias), workspace.Aliases[alias])
			}
		}
	}
	return b.String()
}

func canonicalAttributionConfig(workspace WorkspaceProfile) AttributionConfig {
	attribution := workspace.Attribution
	if attribution.Enabled == nil && workspace.AgentAttribution != nil {
		enabled := *workspace.AgentAttribution
		attribution.Enabled = &enabled
	}
	if attribution.Label == "" {
		attribution.Label = workspace.AgentLabel
	}
	if attribution.Emoji == "" {
		attribution.Emoji = workspace.AgentEmoji
	}
	if attribution.Message == "" {
		attribution.Message = workspace.AgentMessage
	}
	return attribution
}

func writeTOMLTable(b *strings.Builder, path ...string) {
	b.WriteString("[")
	for i, part := range path {
		if i > 0 {
			b.WriteString(".")
		}
		b.WriteString(tomlKey(part))
	}
	b.WriteString("]\n")
}

func writeTOMLString(b *strings.Builder, key, value string) {
	b.WriteString(key)
	b.WriteString(" = ")
	b.WriteString(strconv.Quote(value))
	b.WriteString("\n")
}

func writeOptionalTOMLString(b *strings.Builder, key, value string) {
	if value == "" {
		return
	}
	writeTOMLString(b, key, value)
}

func writeTOMLBool(b *strings.Builder, key string, value bool) {
	b.WriteString(key)
	b.WriteString(" = ")
	b.WriteString(strconv.FormatBool(value))
	b.WriteString("\n")
}

func tomlKey(value string) string {
	if value == "" {
		return strconv.Quote(value)
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			continue
		}
		return strconv.Quote(value)
	}
	return value
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("SLACK_CLI_DEFAULT_WORKSPACE"); v != "" {
		cfg.DefaultWorkspace = v
	}
}
