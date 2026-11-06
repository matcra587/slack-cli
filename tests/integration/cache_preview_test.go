//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBinaryCachePreviewsNeedNoCredentialsAndKeepFiles(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tc := range []struct {
		name, mode, resource string
		args                 []string
	}{
		{"users", "json", "users", []string{"cache", "users", "--refresh", "--dry-run"}},
		{"channels", "compact", "channels", []string{"-n", "cache", "channels"}},
		{"clear", "human", "all", []string{"--dry-run", "cache", "clear"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			configPath := filepath.Join(home, "config.toml")
			cacheRoot := filepath.Join(home, "cache", "slick")
			files := map[string][]byte{configPath: []byte("schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\n")}
			if tc.name == "clear" {
				files[filepath.Join(cacheRoot, "fixture", "users.json")] = []byte(`{"data":{}}`)
				files[filepath.Join(cacheRoot, "fixture", "channels.json")] = []byte(`{"data":{}}`)
				files[filepath.Join(cacheRoot, "other", "users.json")] = []byte(`{"keep":true}`)
			}
			for path, body := range files {
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, body, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			args := append(append([]string{}, tc.args...), "--output="+tc.mode, "--color=never", "--timeout=2s")
			cmd := exec.CommandContext(ctx, binary, args...)
			cmd.Env = []string{"HOME=" + home, "XDG_CACHE_HOME=" + filepath.Join(home, "cache"), "SLICK_CONFIG=" + configPath, "SLACK_CLI_BASE_URL=http://127.0.0.1:1"}
			var out, diagnostic bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &diagnostic
			if err := cmd.Run(); err != nil || diagnostic.Len() != 0 || ctx.Err() != nil {
				t.Fatal("native cache preview required credentials or failed")
			}
			for path, prior := range files {
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(prior, after) {
					t.Error("native cache preview changed fixture files")
				}
			}
			if tc.name != "clear" {
				if _, err := os.Stat(cacheRoot); !errors.Is(err, os.ErrNotExist) {
					t.Error("native cache preview created directories")
				}
			}
			if tc.mode == "human" {
				if !strings.Contains(out.String(), "dry_run=true") || !strings.Contains(out.String(), "resource=all") {
					t.Error("native human preview lost its plan")
				}
			} else {
				var data map[string]any
				if json.Unmarshal(out.Bytes(), &data) != nil {
					t.Fatal("native cache preview is not JSON")
				}
				if tc.mode == "json" {
					if data["ok"] != true {
						t.Fatal("native cache preview lost success envelope")
					}
					data = data["data"].(map[string]any)
				}
				if data["profile"] != "fixture" || data["resource"] != tc.resource || data["dry_run"] != true {
					t.Error("native cache preview lost target profile/resource")
				}
			}
		})
	}
}
