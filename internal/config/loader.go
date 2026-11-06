package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
	xfilepath "github.com/gechr/x/filepath"
)

func LoadFile(path string) (*Config, error) {
	cfg := &Config{}
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, fmt.Errorf("loading config file: %w", err)
	}

	migrateTokenRefs(cfg)
	if err := Migrate(cfg); err != nil {
		return nil, err
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
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
	if err := cfg.Validate(); err != nil {
		return err
	}
	body, err := encodeConfigTOML(cfg)
	if err != nil {
		return fmt.Errorf("encoding config file: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	writePath, err := configWritePath(path)
	if err != nil {
		return fmt.Errorf("resolving config destination: %w", err)
	}
	if err := writeConfigFile(writePath, []byte(body)); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}
	return nil
}

// configWritePath follows leaf symlinks without replacing them. Unlike strict
// path resolution, it accepts a missing final target, as os.WriteFile did.
func configWritePath(path string) (string, error) {
	for range 255 {
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			return path, nil
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			if !info.Mode().IsRegular() {
				return "", fmt.Errorf("config destination is not a regular file")
			}
			// Keep the existing file's write-access requirement even though
			// replacing its directory entry would bypass its read-only mode.
			file, err := os.OpenFile(path, os.O_WRONLY, 0) //nolint:gosec // Probe write access to the caller-selected config without truncating it.
			if err != nil {
				return "", err
			}
			if err := file.Close(); err != nil {
				return "", err
			}
			return path, nil
		}
		target, err := os.Readlink(path)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(target) {
			parent, err := xfilepath.Resolve(filepath.Dir(path))
			if err != nil {
				return "", err
			}
			target = filepath.Join(parent, target)
		}
		path = target
	}
	return "", fmt.Errorf("too many configuration symlinks")
}

// configTOMLWriter keeps the explicit persisted-field allowlist and layout,
// while delegating quoted literals to the same codec used to read the file.
type configTOMLWriter struct {
	strings.Builder
	err error
}

func (b *configTOMLWriter) quoted(value string) string {
	if b.err != nil {
		return ""
	}
	if !utf8.ValidString(value) {
		b.err = fmt.Errorf("config strings must be valid UTF-8")
		return ""
	}
	var literal strings.Builder
	if err := toml.NewEncoder(&literal).Encode(map[string]string{"value": value}); err != nil {
		b.err = err
		return ""
	}
	quoted, prefix := strings.CutPrefix(literal.String(), "value = ")
	quoted, suffix := strings.CutSuffix(quoted, "\n")
	if !prefix || !suffix || !strings.HasPrefix(quoted, `"`) || !strings.HasSuffix(quoted, `"`) {
		b.err = fmt.Errorf("unexpected TOML string encoding")
		return ""
	}
	return quoted
}

func encodeConfigTOML(cfg *Config) (string, error) {
	var b configTOMLWriter
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
				writeTOMLString(&b, tomlKey(&b, alias), workspace.Aliases[alias])
			}
		}
	}
	return b.String(), b.err
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

func writeTOMLTable(b *configTOMLWriter, path ...string) {
	b.WriteString("[")
	for i, part := range path {
		if i > 0 {
			b.WriteString(".")
		}
		b.WriteString(tomlKey(b, part))
	}
	b.WriteString("]\n")
}

func writeTOMLString(b *configTOMLWriter, key, value string) {
	b.WriteString(key)
	b.WriteString(" = ")
	b.WriteString(b.quoted(value))
	b.WriteString("\n")
}

func writeOptionalTOMLString(b *configTOMLWriter, key, value string) {
	if value == "" {
		return
	}
	writeTOMLString(b, key, value)
}

func writeTOMLBool(b *configTOMLWriter, key string, value bool) {
	b.WriteString(key)
	b.WriteString(" = ")
	b.WriteString(strconv.FormatBool(value))
	b.WriteString("\n")
}

func tomlKey(b *configTOMLWriter, value string) string {
	if value == "" {
		return b.quoted(value)
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			continue
		}
		return b.quoted(value)
	}
	return value
}
