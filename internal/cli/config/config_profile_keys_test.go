package config_test

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	cliconfig "github.com/matcra587/slack-cli/internal/cli/config"
	"github.com/matcra587/slack-cli/internal/config"
)

func TestConfigDottedProfileSettings(t *testing.T) {
	for _, name := range []string{"team.prod", "team.attribution", "team.prod.attribution", "a..b", ".leading", "trailing."} {
		for _, field := range []string{
			"default_channel", "attribution.enabled", "attribution.label", "attribution.emoji", "attribution.message",
			"agent_attribution", "agent_label", "agent_emoji", "agent_message",
		} {
			t.Run(name+"/"+field, func(t *testing.T) {
				cfg := &config.Config{SchemaVersion: "1", DefaultWorkspace: "team", Workspaces: map[string]config.WorkspaceProfile{
					"team": {Name: "team", DefaultChannel: "C_KEEP"}, name: {Name: name},
				}}
				path := filepath.Join(t.TempDir(), "config.toml")
				key := "workspaces." + name + "." + field
				value := "review"
				if field == "attribution.enabled" || field == "agent_attribution" {
					value = "false"
				}
				for _, args := range [][]string{{"set", key, value}, {"get", key}, {"unset", key}, {"get", key}} {
					out, diagnostic, err := executeRoot(t, cfg, path, config.NewMemoryCredentialStore(), strings.NewReader(""), false,
						append([]string{"--output=json", "config"}, args...))
					if err != nil || diagnostic != "" {
						t.Fatalf("%s on dotted profile failed: %v", args[0], err)
					}
					var frame struct {
						OK   bool `json:"ok"`
						Data struct {
							Key, Value string
						} `json:"data"`
					}
					if err := json.Unmarshal([]byte(out), &frame); err != nil || !frame.OK || frame.Data.Key != key {
						t.Fatal("dotted profile result lost key or success")
					}
					if args[0] == "set" && frame.Data.Value != value {
						t.Fatal("dotted profile set returned a different value")
					}
					if args[0] == "unset" {
						value = ""
					}
					if args[0] == "get" && frame.Data.Value != value {
						t.Fatal("dotted profile get did not reflect set or unset")
					}
					if cfg.Workspaces["team"].DefaultChannel != "C_KEEP" {
						t.Fatal("dotted profile setting changed its parent-named profile")
					}
				}
				if !strings.HasPrefix(field, "agent_") && !slices.ContainsFunc(cliconfig.KeyCompletions(cfg), func(s string) bool {
					return strings.HasPrefix(s, key+"\t")
				}) {
					t.Fatal("completion omitted the usable dotted profile key")
				}
			})
		}
	}
}
