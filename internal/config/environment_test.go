package config_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func TestWorkspaceEnvironmentSelectionNeverBecomesStoredPreference(t *testing.T) {
	for _, override := range []string{"Other", "oThEr", "Missing", ""} {
		t.Run("override="+override, func(t *testing.T) {
			t.Setenv("SLACK_CLI_DEFAULT_WORKSPACE", override)
			path := filepath.Join(t.TempDir(), "config.toml")
			prior := &config.Config{SchemaVersion: "1", DefaultWorkspace: "Primary", Workspaces: map[string]config.WorkspaceProfile{"Primary": {Name: "Primary", DefaultChannel: "C_KEEP"}, "Other": {Name: "Other"}}}
			if err := config.SaveFile(path, prior); err != nil {
				t.Fatal(err)
			}
			loaded, err := config.LoadFile(path)
			if err != nil || !reflect.DeepEqual(loaded, prior) {
				t.Fatalf("environment changed loaded preferences: %v", err)
			}
			for _, selector := range []string{"", "Primary"} {
				name, nameErr := loaded.ResolveWorkspaceName(selector)
				profile, profileErr := loaded.ResolveWorkspace(selector)
				if selector == "" && override == "Missing" {
					if nameErr == nil || profileErr == nil {
						t.Fatal("unknown environment selection was accepted")
					}
					continue
				}
				want := "Primary"
				if selector == "" && override != "" {
					want = "Other"
				}
				if nameErr != nil || profileErr != nil || name != want || profile.Name != want {
					t.Fatalf("selection precedence changed: name=%q profile=%q errors=%v/%v", name, profile.Name, nameErr, profileErr)
				}
			}
			loaded.Workspaces["Other"] = config.WorkspaceProfile{Name: "Other", DefaultChannel: "C_NEW"}
			if err := config.SaveFile(path, loaded); err != nil {
				t.Fatal(err)
			}
			t.Setenv("SLACK_CLI_DEFAULT_WORKSPACE", "")
			stored, err := config.LoadFile(path)
			if err != nil || stored.DefaultWorkspace != "Primary" || stored.Workspaces["Other"].DefaultChannel != "C_NEW" {
				t.Fatalf("runtime selection leaked into save: %v", err)
			}
		})
	}
}

func TestWorkspaceEnvironmentDoesNotMaskInvalidStoredDefault(t *testing.T) {
	t.Setenv("SLACK_CLI_DEFAULT_WORKSPACE", "Primary")
	path := filepath.Join(t.TempDir(), "config.toml")
	body := []byte("schema_version='1'\ndefault_workspace='Missing'\n[workspaces.Primary]\nname='Primary'\n")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadFile(path); err == nil {
		t.Fatal("environment concealed an invalid stored default")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(body) {
		t.Fatal("invalid configuration was modified")
	}
}
