package config

import (
	"errors"
	"fmt"
	"strings"
)

const SchemaVersion = "1"

type TokenType string

const (
	TokenTypeBot  TokenType = "bot"
	TokenTypeUser TokenType = "user"
)

type Config struct {
	SchemaVersion    string                      `toml:"schema_version" json:"schema_version"`
	DefaultWorkspace string                      `toml:"default_workspace" json:"default_workspace"`
	Workspaces       map[string]WorkspaceProfile `toml:"workspaces" json:"workspaces"`
}

type WorkspaceProfile struct {
	Name      string    `toml:"name" json:"name"`
	TeamID    string    `toml:"team_id" json:"team_id"`
	TeamName  string    `toml:"team_name,omitempty" json:"team_name,omitempty"`
	TokenType TokenType `toml:"token_type" json:"token_type"`
	TokenRef  string    `toml:"token_ref" json:"token_ref"`
	// LegacyToken reads the deprecated `token` TOML key. Read only inside migrateTokenRefs.
	LegacyToken      string            `toml:"token" json:"-"`
	DefaultChannel   string            `toml:"default_channel,omitempty" json:"default_channel,omitempty"`
	AgentAttribution *bool             `toml:"agent_attribution,omitempty" json:"agent_attribution,omitempty"`
	AgentLabel       string            `toml:"agent_label,omitempty" json:"agent_label,omitempty"`
	AgentEmoji       string            `toml:"agent_emoji,omitempty" json:"agent_emoji,omitempty"`
	AgentMessage     string            `toml:"agent_message,omitempty" json:"agent_message,omitempty"`
	Attribution      AttributionConfig `toml:"attribution,omitempty" json:"attribution,omitempty"`
	RateLimitTier    string            `toml:"rate_limit_tier,omitempty" json:"rate_limit_tier,omitempty"`
	Aliases          map[string]string `toml:"aliases,omitempty" json:"aliases,omitempty"`
}

type AttributionConfig struct {
	Enabled *bool  `toml:"enabled,omitempty" json:"enabled,omitempty"`
	Message string `toml:"message,omitempty" json:"message,omitempty"`
	Label   string `toml:"label,omitempty" json:"label,omitempty"`
	Emoji   string `toml:"emoji,omitempty" json:"emoji,omitempty"`
}

func (c *Config) Validate() error {
	if c == nil {
		return errors.New("config is required")
	}
	if c.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema_version %q", c.SchemaVersion)
	}
	if c.DefaultWorkspace == "" {
		return errors.New("default workspace is required")
	}
	if len(c.Workspaces) == 0 {
		return errors.New("at least one workspace is required")
	}
	if _, ok := c.Workspaces[c.DefaultWorkspace]; !ok {
		if _, _, ok := c.resolveWorkspaceCaseInsensitive(c.DefaultWorkspace); !ok {
			return fmt.Errorf("default workspace %q is not configured", c.DefaultWorkspace)
		}
	}
	seen := make(map[string]string, len(c.Workspaces))
	for key, workspace := range c.Workspaces {
		normalized := strings.ToLower(key)
		if previous, ok := seen[normalized]; ok {
			return fmt.Errorf("duplicate workspace profile %q conflicts with %q; profile names are case-insensitive", key, previous)
		}
		seen[normalized] = key
		if err := workspace.validate(key); err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) ResolveWorkspace(name string) (WorkspaceProfile, error) {
	if err := c.Validate(); err != nil {
		return WorkspaceProfile{}, err
	}
	// Stored keys are cleaned at the marshal boundary by cleanWorkspaceKeys,
	// but caller-supplied names (flags, env vars, args) arrive raw — trim
	// here so a user typing `--workspace 'default '` still resolves.
	name = strings.TrimSpace(name)
	if name == "" {
		name = c.DefaultWorkspace
	}
	_, workspace, ok := c.resolveWorkspaceCaseInsensitive(name)
	if !ok {
		return WorkspaceProfile{}, fmt.Errorf("workspace %q not found", name)
	}
	return workspace, nil
}

func (c *Config) ResolveWorkspaceName(name string) (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = c.DefaultWorkspace
	}
	if key, _, ok := c.resolveWorkspaceCaseInsensitive(name); ok {
		return key, nil
	}
	return "", fmt.Errorf("workspace %q not found", name)
}

func (c *Config) resolveWorkspaceCaseInsensitive(name string) (string, WorkspaceProfile, bool) {
	if workspace, ok := c.Workspaces[name]; ok {
		return name, workspace, true
	}
	for key, workspace := range c.Workspaces {
		if strings.EqualFold(key, name) {
			return key, workspace, true
		}
	}
	return "", WorkspaceProfile{}, false
}

func (w WorkspaceProfile) validate(key string) error {
	if w.Name == "" {
		return fmt.Errorf("workspace %q name is required", key)
	}
	if w.Name != key {
		return fmt.Errorf("workspace %q name must match map key", key)
	}
	authFields := 0
	if w.TeamID != "" {
		authFields++
	}
	if w.TokenType != "" {
		authFields++
	}
	if w.TokenRef != "" {
		authFields++
	}
	if authFields == 0 {
		return nil
	}
	if authFields != 3 {
		return fmt.Errorf("workspace %q auth fields must be managed together by slick auth", key)
	}
	if w.TokenType != TokenTypeBot && w.TokenType != TokenTypeUser {
		return fmt.Errorf("workspace %q token_type must be bot or user", key)
	}
	if w.TokenRef == "" {
		return fmt.Errorf("workspace %q token keychain reference is required", key)
	}
	if strings.HasPrefix(w.TokenRef, "xoxb-") || strings.HasPrefix(w.TokenRef, "xoxp-") {
		return fmt.Errorf("workspace %q token must be a keychain reference, not a Slack token value", key)
	}
	return nil
}
