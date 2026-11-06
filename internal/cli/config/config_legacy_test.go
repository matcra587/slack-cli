package config_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func TestConfigLegacyAttributionReadsPreserveWhitespace(t *testing.T) {
	for _, field := range []string{"label", "emoji", "message"} {
		for _, tc := range []struct{ name, nested, want string }{
			{"fallback", "", "legacy"},
			{"whitespace", " ", " "},
			{"override", "nested", "nested"},
		} {
			t.Run(field+"/"+tc.name, func(t *testing.T) {
				cfg := &config.Config{SchemaVersion: "1", DefaultWorkspace: "default", Workspaces: map[string]config.WorkspaceProfile{
					"default": {
						Name: "default", AgentLabel: "legacy", AgentEmoji: "legacy", AgentMessage: "legacy",
						Attribution: config.AttributionConfig{Label: tc.nested, Emoji: tc.nested, Message: tc.nested},
					},
				}}
				path := filepath.Join(t.TempDir(), "config.toml")
				for _, key := range []string{"attribution." + field, "agent_" + field} {
					out, diagnostic, err := executeRoot(t, cfg, path, config.NewMemoryCredentialStore(), strings.NewReader(""), false,
						[]string{"--output=json", "config", "get", "workspaces.default." + key})
					var frame struct {
						OK   bool                   `json:"ok"`
						Data struct{ Value string } `json:"data"`
					}
					if err != nil || diagnostic != "" || json.Unmarshal([]byte(out), &frame) != nil || !frame.OK || frame.Data.Value != tc.want {
						t.Fatal("get changed configured whitespace or legacy precedence")
					}
				}
				out, diagnostic, err := executeRoot(t, cfg, path, config.NewMemoryCredentialStore(), strings.NewReader(""), false,
					[]string{"--output=json", "config", "list"})
				var frame struct {
					Data struct {
						Settings []struct{ Key, Value string }
					} `json:"data"`
				}
				if err != nil || diagnostic != "" || json.Unmarshal([]byte(out), &frame) != nil {
					t.Fatal("list failed for configured legacy values")
				}
				found := false
				for _, setting := range frame.Data.Settings {
					if setting.Key == "workspaces.default.attribution."+field {
						found = true
						if setting.Value != tc.want {
							t.Fatal("list changed configured whitespace or legacy precedence")
						}
					}
				}
				if !found {
					t.Fatal("list omitted configured attribution field")
				}
			})
		}
	}
}
