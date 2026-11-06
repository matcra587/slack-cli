package config_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func TestConfigRejectsRawSlackCredentialsBeforeSaving(t *testing.T) {
	for _, tt := range []struct{ name, token string }{
		{"bot", "xoxb-synthetic-private"},
		{"user", "xoxp-synthetic-private"},
		{"rotating bot", "xoxe.xoxb-synthetic-private"},
		{"rotating user", "xoxe.xoxp-synthetic-private"},
		{"refresh", "xoxe-synthetic-private"},
		{"app", "xapp-synthetic-private"},
		{"rotating app", "xoxe.xapp-synthetic-private"},
		{"workflow", "xwfp-synthetic-private"},
		{"whitespace", "  xoxe.xoxp-synthetic-private  "},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{SchemaVersion: "1", DefaultWorkspace: "default", Workspaces: map[string]config.WorkspaceProfile{"default": validWorkspace("default")}}
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := config.SaveFile(path, cfg); err != nil {
				t.Fatal(err)
			}
			prior, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			profile := cfg.Workspaces["default"]
			profile.TokenRef = tt.token
			cfg.Workspaces["default"] = profile
			if err := config.SaveFile(path, cfg); err == nil {
				t.Error("raw Slack credential accepted for persistence")
			} else if strings.Contains(err.Error(), "synthetic-private") {
				t.Error("raw credential appeared in validation error")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(prior, after) {
				t.Fatal("rejected credential changed prior configuration")
			}
			profile.TokenRef = "env:SLICK_SYNTHETIC_TOKEN"
			cfg.Workspaces["default"] = profile
			if err := config.SaveFile(path, cfg); err != nil {
				t.Fatal("valid reference could not recover rejected credential")
			}
			loaded, err := config.LoadFile(path)
			if err != nil || loaded.Workspaces["default"].TokenRef != profile.TokenRef {
				t.Fatal("recovered reference did not round-trip")
			}
		})
	}
}
