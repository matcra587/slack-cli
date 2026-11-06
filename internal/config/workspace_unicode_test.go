package config_test

import (
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func TestWorkspaceValidationUsesResolverUnicodeEquivalence(t *testing.T) {
	for _, tc := range []struct{ name, first, second string }{
		{"ascii", "default", "Default"},
		{"long s", "s", "ſ"},
		{"sigma", "σ", "ς"},
		{"kelvin", "K", "K"},
		{"long s suffix", "team-s", "team-ſ"},
		{"sigma suffix", "team.σ", "TEAM.ς"},
		{"cyrillic", "Б", "б"},
		{"accented", "É", "é"},
		{"dotless i", "I", "ı"},
		{"dotted i", "İ", "i"},
		{"sharp s", "ß", "ss"},
		{"normalization", "é", "e\u0301"},
		{"confusable", "a", "а"},
		{"different scripts", "Σ", "S"},
		{"prefix", "k", "kk"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{SchemaVersion: "1", DefaultWorkspace: tc.first, Workspaces: map[string]config.WorkspaceProfile{
				tc.first: {Name: tc.first}, tc.second: {Name: tc.second}, "t": {Name: "t"},
			}}
			wantCollision := strings.EqualFold(tc.first, tc.second)
			var previous string
			for range 25 {
				err := cfg.Validate()
				if wantCollision {
					if err == nil || !strings.Contains(err.Error(), "duplicate workspace profile") {
						t.Fatal("validation accepted names that the resolver treats as equivalent")
					}
					if previous != "" && previous != err.Error() {
						t.Fatal("collision diagnostic depends on map iteration order")
					}
					previous = err.Error()
					if _, err := cfg.ResolveWorkspace(tc.first); err == nil {
						t.Fatal("exact-name lookup bypassed ambiguous configuration validation")
					}
					if _, err := cfg.ResolveWorkspaceName(tc.second); err == nil {
						t.Fatal("name lookup bypassed ambiguous configuration validation")
					}
				} else {
					if err != nil {
						t.Fatalf("validation rejected distinct resolver names: %v", err)
					}
					for _, name := range []string{tc.first, tc.second} {
						profile, err := cfg.ResolveWorkspace(name)
						if err != nil || profile.Name != name {
							t.Fatal("distinct Unicode name resolved to a different profile")
						}
					}
				}
			}
		})
	}
}
