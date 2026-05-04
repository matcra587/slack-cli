package config_test

import (
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func TestValidateRejectsInvalidWorkspaceReferences(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
		want string
	}{
		{
			name: "unknown default workspace",
			cfg: &config.Config{
				SchemaVersion:    "1",
				DefaultWorkspace: "missing",
				Workspaces: map[string]config.WorkspaceProfile{
					"default": validWorkspace("default"),
				},
			},
			want: "default workspace",
		},
		{
			name: "workspace name does not match map key",
			cfg: &config.Config{
				SchemaVersion:    "1",
				DefaultWorkspace: "default",
				Workspaces: map[string]config.WorkspaceProfile{
					"default": validWorkspace("other"),
				},
			},
			want: "name",
		},
		{
			name: "duplicate workspace profiles by case",
			cfg: &config.Config{
				SchemaVersion:    "1",
				DefaultWorkspace: "default",
				Workspaces: map[string]config.WorkspaceProfile{
					"default": validWorkspace("default"),
					"Default": validWorkspace("Default"),
				},
			},
			want: "duplicate workspace profile",
		},
		{
			name: "partial auth token type",
			cfg: &config.Config{
				SchemaVersion:    "1",
				DefaultWorkspace: "default",
				Workspaces: map[string]config.WorkspaceProfile{
					"default": {
						Name:      "default",
						TokenType: config.TokenTypeUser,
					},
				},
			},
			want: "auth fields",
		},
		{
			name: "invalid token type",
			cfg: &config.Config{
				SchemaVersion:    "1",
				DefaultWorkspace: "default",
				Workspaces: map[string]config.WorkspaceProfile{
					"default": func() config.WorkspaceProfile {
						workspace := validWorkspace("default")
						workspace.TokenType = "legacy"
						return workspace
					}(),
				},
			},
			want: "token_type",
		},
		{
			name: "raw bot token in config",
			cfg: &config.Config{
				SchemaVersion:    "1",
				DefaultWorkspace: "default",
				Workspaces: map[string]config.WorkspaceProfile{
					"default": func() config.WorkspaceProfile {
						workspace := validWorkspace("default")
						workspace.TokenRef = "xoxb-secret"
						return workspace
					}(),
				},
			},
			want: "keychain",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if err == nil {
				t.Fatal("Validate returned nil error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate error = %q, want substring %q", err.Error(), tt.want)
			}
		})
	}
}

func TestValidateAllowsPreferencesOnlyWorkspace(t *testing.T) {
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

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate returned error for preferences-only workspace: %v", err)
	}
}

func TestResolveWorkspaceUsesExplicitNameOrDefault(t *testing.T) {
	cfg := &config.Config{
		SchemaVersion:    "1",
		DefaultWorkspace: "default",
		Workspaces: map[string]config.WorkspaceProfile{
			"default": validWorkspace("default"),
			"ci":      validWorkspace("ci"),
		},
	}

	workspace, err := cfg.ResolveWorkspace("ci")
	if err != nil {
		t.Fatalf("ResolveWorkspace returned error: %v", err)
	}
	if workspace.Name != "ci" {
		t.Fatalf("explicit workspace = %q, want ci", workspace.Name)
	}

	workspace, err = cfg.ResolveWorkspace("")
	if err != nil {
		t.Fatalf("ResolveWorkspace default returned error: %v", err)
	}
	if workspace.Name != "default" {
		t.Fatalf("default workspace = %q, want default", workspace.Name)
	}
}

func TestResolveWorkspaceMatchesProfileCaseInsensitively(t *testing.T) {
	cfg := &config.Config{
		SchemaVersion:    "1",
		DefaultWorkspace: "Default",
		Workspaces: map[string]config.WorkspaceProfile{
			"Default": validWorkspace("Default"),
		},
	}

	workspace, err := cfg.ResolveWorkspace("default")
	if err != nil {
		t.Fatalf("ResolveWorkspace returned error: %v", err)
	}
	if workspace.Name != "Default" {
		t.Fatalf("workspace = %q, want existing profile spelling Default", workspace.Name)
	}
}

func validWorkspace(name string) config.WorkspaceProfile {
	return config.WorkspaceProfile{
		Name:      name,
		TeamID:    "T123",
		TokenType: config.TokenTypeBot,
		TokenRef:  "keychain:slack-cli/" + name,
	}
}
