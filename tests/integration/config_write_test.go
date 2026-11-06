//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func configWriteFixture(t *testing.T, path string) *config.Config {
	t.Helper()
	cfg := &config.Config{SchemaVersion: "1", DefaultWorkspace: "default", Workspaces: map[string]config.WorkspaceProfile{
		"default": {Name: "default", TeamID: "T_SYNTHETIC", TokenType: config.TokenTypeBot, TokenRef: "env:SLICK_UNUSED_TOKEN", DefaultChannel: "C_OLD", Aliases: map[string]string{"alerts": "C_KEEP"}},
		"other":   {Name: "other", DefaultChannel: "C_OTHER"},
	}}
	if err := config.SaveFile(path, cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func runConfigWrite(t *testing.T, binary, path, mode, value string, want int) string {
	t.Helper()
	out, diagnostic, err := runSlackBinary(t, binary, path, "http://127.0.0.1:1", "", "--output="+mode, "--color=never", "config", "set", "workspaces.default.default_channel", value)
	if strings.Contains(out+diagnostic, "SLICK_UNUSED_TOKEN") || strings.Contains(out+diagnostic, "xoxb-test") {
		t.Fatal("config write disclosed an authentication reference or synthetic token")
	}
	if exitCode(err) != want {
		t.Fatalf("config write exit=%d, want %d", exitCode(err), want)
	}
	if mode == "human" {
		if want == 0 && (out == "" || diagnostic != "") || want != 0 && (out != "" || diagnostic == "") {
			t.Fatal("config write used wrong human stream")
		}
		return diagnostic
	}
	if diagnostic != "" || !json.Valid([]byte(out)) {
		t.Fatal("config write lost machine record or used stderr")
	}
	if want != 0 {
		var frame struct {
			OK   bool            `json:"ok"`
			Data json.RawMessage `json:"data"`
			Meta struct {
				Command string `json:"command"`
				Exit    int    `json:"exit_code"`
			} `json:"meta"`
		}
		if err := json.Unmarshal([]byte(out), &frame); err != nil || frame.OK || string(frame.Data) != "null" || frame.Meta.Command != "config.set" || frame.Meta.Exit != want {
			t.Fatal("config write failure lost common envelope")
		}
	}
	return out
}

func TestBinaryConfigWritesProtectImportedFilesAndSymlinks(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, mode := range []string{"json", "compact", "human"} {
		for _, link := range []bool{false, true} {
			name := "regular"
			if link {
				name = "symlink"
			}
			t.Run(mode+"/"+name, func(t *testing.T) {
				dir := t.TempDir()
				path := filepath.Join(dir, "config.toml")
				prior := configWriteFixture(t, path)
				if err := os.Chmod(path, 0o644); err != nil { //nolint:gosec // Synthetic fixture models an imported file with loose permissions.
					t.Fatal(err)
				}
				entry := path
				if link {
					entry = filepath.Join(dir, "link.toml")
					if err := os.Symlink("config.toml", entry); err != nil {
						t.Skipf("symlinks unavailable: %v", err)
					}
				}
				runConfigWrite(t, binary, entry, mode, "C_UPDATED", 0)
				loaded, err := config.LoadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				updated := prior.Workspaces["default"]
				updated.DefaultChannel = "C_UPDATED"
				prior.Workspaces["default"] = updated
				if !reflect.DeepEqual(loaded, prior) {
					t.Fatal("config write changed unrelated preferences, aliases or auth references")
				}
				if runtime.GOOS != "windows" {
					info, err := os.Stat(path)
					if err != nil || info.Mode().Perm() != 0o600 {
						t.Fatal("updated imported configuration is not private")
					}
				}
				if link {
					info, err := os.Lstat(entry)
					if err != nil || info.Mode()&os.ModeSymlink == 0 {
						t.Fatal("config command replaced its symlink")
					}
				}
			})
		}
	}
}

func TestBinaryConfigWriteFailuresPreserveStateAndReportPartialCompletion(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires Unix permission enforcement without root")
	}
	binary := buildSlackBinary(t)
	for _, mode := range []string{"json", "compact", "human"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.toml")
			priorConfig := configWriteFixture(t, path)
			prior, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			chmod := func(path string, mode os.FileMode) {
				t.Helper()
				if err := os.Chmod(path, mode); err != nil {
					t.Fatal(err)
				}
			}
			t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // Restore private directory permissions for cleanup.
			chmod(path, 0o400)
			runConfigWrite(t, binary, path, mode, "C_DENIED", 5)
			chmod(path, 0o600)
			chmod(dir, 0o500)
			runConfigWrite(t, binary, path, mode, "C_DENIED", 5)
			chmod(dir, 0o700)
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, prior) {
				t.Fatal("failed config write damaged prior configuration")
			}
			runConfigWrite(t, binary, path, mode, "C_RECOVERED", 0)
			chmod(dir, 0o300)
			failure := runConfigWrite(t, binary, path, mode, "C_PARTIAL", 5)
			chmod(dir, 0o700)
			if !strings.Contains(failure, "sync directory after rename") {
				t.Fatal("post-replacement failure did not identify uncertain durability")
			}
			loaded, err := config.LoadFile(path)
			updated := priorConfig.Workspaces["default"]
			updated.DefaultChannel = "C_PARTIAL"
			priorConfig.Workspaces["default"] = updated
			if err != nil || !reflect.DeepEqual(loaded, priorConfig) {
				t.Fatalf("post-replacement failure lost completed update: %v", err)
			}
			runConfigWrite(t, binary, path, mode, "C_FINAL", 0)
			loaded, err = config.LoadFile(path)
			updated.DefaultChannel = "C_FINAL"
			priorConfig.Workspaces["default"] = updated
			if err != nil || !reflect.DeepEqual(loaded, priorConfig) {
				t.Fatalf("write did not recover after restoring permissions: %v", err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 || entries[0].Name() != "config.toml" {
				t.Fatal("config write left temporary files")
			}
		})
	}
}
