//go:build unix

package config_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func TestSaveFileReportsPublicationOutcome(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires Unix directory permission enforcement")
	}
	for _, tc := range []struct {
		name              string
		fileMode, dirMode os.FileMode
		failed, published bool
	}{
		{"success", 0o600, 0o700, false, false},
		{"read-only file", 0o400, 0o700, true, false},
		{"read-only directory", 0o600, 0o500, true, false},
		{"published", 0o600, 0o300, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.toml")
			cfg := &config.Config{SchemaVersion: config.SchemaVersion, DefaultWorkspace: "default", Workspaces: map[string]config.WorkspaceProfile{"default": {Name: "default", DefaultChannel: "C_PRIOR"}}}
			if err := config.SaveFile(path, cfg); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			profile := cfg.Workspaces["default"]
			profile.DefaultChannel = "C_NEW"
			cfg.Workspaces["default"] = profile
			if err := os.Chmod(path, tc.fileMode); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(dir, tc.dirMode); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // Restore private fixture directory access for cleanup.
			err = config.SaveFile(path, cfg)
			if (err != nil) != tc.failed || config.SaveWasPublished(err) != tc.published {
				t.Errorf("incorrect save publication outcome: published=%v failed=%v", config.SaveWasPublished(err), err != nil)
			}
			if tc.failed && !errors.Is(err, os.ErrPermission) {
				t.Error("save lost the filesystem error cause")
			}
			if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // Restore private fixture directory access after the failure.
				t.Fatal(err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if tc.failed && !tc.published {
				if !bytes.Equal(before, after) {
					t.Error("unpublished write damaged prior config")
				}
			} else if bytes.Equal(before, after) || !bytes.Contains(after, []byte("C_NEW")) {
				t.Error("published config was lost")
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 || entries[0].Name() != "config.toml" {
				t.Error("save left a temporary file or removed unrelated state")
			}
			if err := os.Chmod(path, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := config.SaveFile(path, cfg); err != nil {
				t.Fatalf("save did not recover: %v", err)
			}
		})
	}
}
