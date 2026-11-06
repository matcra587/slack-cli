//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func TestBinaryConfigPreviewsDoNotWriteState(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tc := range []struct {
		name, mode string
		args       []string
		missing    bool
	}{
		{"init prefix", "json", []string{"--dry-run", "config", "init", "--profile=Preview"}, true},
		{"set suffix", "compact", []string{"config", "set", "workspaces.default.default_channel", "C_NEW", "-n"}, false},
		{"unset prefix", "human", []string{"-n", "config", "unset", "workspaces.default.default_channel"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "nested", "config.toml")
			if !tc.missing {
				cfg := &config.Config{SchemaVersion: "1", DefaultWorkspace: "default", Workspaces: map[string]config.WorkspaceProfile{"default": {Name: "default", DefaultChannel: "C_KEEP"}}}
				if err := config.SaveFile(path, cfg); err != nil {
					t.Fatal(err)
				}
			}
			prior, _ := os.ReadFile(path)
			args := append(append([]string{}, tc.args...), "--output="+tc.mode, "--color=never")
			out, diagnostic, err := runSlackBinary(t, binary, path, "http://127.0.0.1:1", "", args...)
			if err != nil || diagnostic != "" {
				t.Fatal("native config preview failed or wrote diagnostics")
			}
			after, readErr := os.ReadFile(path)
			if !bytes.Equal(prior, after) || tc.missing && !errors.Is(readErr, os.ErrNotExist) {
				t.Error("native preview changed config")
			}
			if tc.missing {
				if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
					t.Error("native preview created config directory")
				}
			}
			if tc.mode == "human" {
				if !strings.Contains(out, "dry_run=true") {
					t.Error("human preview omitted dry-run marker")
				}
			} else {
				var data map[string]any
				if json.Unmarshal([]byte(out), &data) != nil {
					t.Fatal("native preview is not JSON")
				}
				if tc.mode == "json" {
					if data["ok"] != true {
						t.Fatal("native preview lost success envelope")
					}
					data = data["data"].(map[string]any)
				}
				if data["dry_run"] != true {
					t.Error("native preview omitted dry-run marker")
				}
			}
		})
	}
}
