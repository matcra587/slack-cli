//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestBinaryCacheClearPreservesUnrelatedFilesAndProfiles(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, mode := range []string{"json", "compact", "human"} {
		t.Run(mode, func(t *testing.T) {
			home := t.TempDir()
			configPath := filepath.Join(home, "config.toml")
			configBody := []byte("schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\n[workspaces.other]\nname=\"other\"\n")
			cacheDir := filepath.Join(home, "cache", "slick", "fixture")
			preserved := map[string][]byte{
				configPath:                                                   configBody,
				filepath.Join(cacheDir, "preserve.toml"):                     []byte("keep=true\n"),
				filepath.Join(cacheDir, "users.json.backup"):                 []byte("keep-backup"),
				filepath.Join(cacheDir, ".users.json.partial"):               []byte("keep-temporary"),
				filepath.Join(cacheDir, "nested.json", "keep.txt"):           []byte("keep-directory"),
				filepath.Join(home, "cache", "slick", "other", "users.json"): []byte("keep-other-profile"),
			}
			for path, body := range preserved {
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, body, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range []string{"users", "channels", "future-metadata"} {
				if err := os.WriteFile(filepath.Join(cacheDir, name+".json"), []byte(`{"data":{}}`), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			run := func(extra ...string) map[string]json.RawMessage {
				t.Helper()
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				args := append([]string{"cache", "clear", "--output=" + mode, "--color=never", "--timeout=2s"}, extra...)
				cmd := exec.CommandContext(ctx, binary, args...)
				cmd.Env = []string{"HOME=" + home, "XDG_CACHE_HOME=" + filepath.Join(home, "cache"), "SLICK_CONFIG=" + configPath, "SLACK_CLI_BASE_URL=http://127.0.0.1:1"}
				var out, diagnostic bytes.Buffer
				cmd.Stdout, cmd.Stderr = &out, &diagnostic
				if err := cmd.Run(); err != nil || diagnostic.Len() != 0 || out.Len() == 0 || ctx.Err() != nil {
					t.Fatalf("local cache clear failed: %v", err)
				}
				if mode == "human" {
					if strings.Contains(out.String(), "preserve.toml") || strings.Contains(out.String(), "backup") {
						t.Fatal("cache clear reported unrelated files as removed resources")
					}
					return nil
				}
				var frame map[string]json.RawMessage
				if err := json.Unmarshal(out.Bytes(), &frame); err != nil {
					t.Fatal(err)
				}
				data := frame
				if mode == "json" {
					if len(frame) != 4 || string(frame["ok"]) != "true" || string(frame["errors"]) != "[]" {
						t.Fatal("cache clear lost the success envelope")
					}
					if err := json.Unmarshal(frame["data"], &data); err != nil {
						t.Fatal(err)
					}
				}
				if string(data["profile"]) != `"fixture"` {
					t.Fatal("cache clear changed selected profile")
				}
				return data
			}
			data := run("users")
			if mode != "human" && (string(data["cleared"]) != "true" || string(data["resource"]) != `"users"`) {
				t.Fatal("single clear lost acknowledgement")
			}
			if _, err := os.Stat(filepath.Join(cacheDir, "users.json")); !os.IsNotExist(err) {
				t.Fatal("single clear did not remove its resource")
			}
			if _, err := os.Stat(filepath.Join(cacheDir, "channels.json")); err != nil {
				t.Fatal("single clear removed another resource")
			}
			data = run("users")
			if mode != "human" && string(data["cleared"]) != "false" {
				t.Fatal("missing single resource did not report cleared=false")
			}
			data = run()
			if mode != "human" {
				var resources []string
				if err := json.Unmarshal(data["resources"], &resources); err != nil || !reflect.DeepEqual(resources, []string{"channels", "future-metadata"}) {
					t.Fatal("sweep changed owned resource names or ordering")
				}
			}
			for path, want := range preserved {
				body, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(body, want) {
					t.Fatalf("cache clear changed unrelated state: %s", filepath.Base(path))
				}
			}
			data = run()
			if mode != "human" && data["resources"] != nil {
				t.Fatal("empty sweep did not omit resources")
			}
		})
	}
}
