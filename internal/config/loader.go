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

	applyEnv(cfg)
	if err := Migrate(cfg); err != nil {
		return nil, err
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
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
		writeOptionalTOMLString(&b, "token", workspace.TokenRef)
		writeOptionalTOMLString(&b, "default_channel", workspace.DefaultChannel)
		if workspace.AgentAttribution != nil {
			writeTOMLBool(&b, "agent_attribution", *workspace.AgentAttribution)
		}
		writeOptionalTOMLString(&b, "agent_label", workspace.AgentLabel)
		writeOptionalTOMLString(&b, "agent_emoji", workspace.AgentEmoji)
		writeOptionalTOMLString(&b, "agent_message", workspace.AgentMessage)
		writeOptionalTOMLString(&b, "rate_limit_tier", workspace.RateLimitTier)

		if workspace.Attribution.Message != "" || workspace.Attribution.Label != "" || workspace.Attribution.Emoji != "" {
			b.WriteString("\n")
			writeTOMLTable(&b, "workspaces", name, "attribution")
			writeOptionalTOMLString(&b, "message", workspace.Attribution.Message)
			writeOptionalTOMLString(&b, "label", workspace.Attribution.Label)
			writeOptionalTOMLString(&b, "emoji", workspace.Attribution.Emoji)
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

func writeTOMLString(b *strings.Builder, key string, value string) {
	b.WriteString(key)
	b.WriteString(" = ")
	b.WriteString(strconv.Quote(value))
	b.WriteString("\n")
}

func writeOptionalTOMLString(b *strings.Builder, key string, value string) {
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
