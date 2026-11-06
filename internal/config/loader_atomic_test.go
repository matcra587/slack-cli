package config_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func atomicConfig() *config.Config {
	return &config.Config{SchemaVersion: "1", DefaultWorkspace: "default", Workspaces: map[string]config.WorkspaceProfile{
		"default": {Name: "default", DefaultChannel: "C_UPDATED", Aliases: map[string]string{"alerts": "C_KEEP"}},
	}}
}

func TestSaveFileProtectsExistingConfiguration(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file permissions")
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("prior"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveFile(path, atomicConfig()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("updated configuration must have mode 0600")
	}
	loaded, err := config.LoadFile(path)
	if err != nil || loaded.Workspaces["default"].DefaultChannel != "C_UPDATED" {
		t.Fatalf("replacement is not a complete readable configuration: %v", err)
	}
}

func TestSaveFilePreservesParentDirectoryPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix directory permissions")
	}
	for _, existing := range []bool{false, true} {
		name, want := "missing", os.FileMode(0o700)
		if existing {
			name, want = "existing", 0o755
		}
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "config")
			expected := want
			if existing {
				if err := os.Mkdir(dir, want); err != nil {
					t.Fatal(err)
				}
				info, err := os.Stat(dir)
				if err != nil {
					t.Fatal(err)
				}
				expected = info.Mode().Perm()
			}
			if err := config.SaveFile(filepath.Join(dir, "config.toml"), atomicConfig()); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(dir)
			if err != nil || info.Mode().Perm() != expected {
				t.Fatal("saving changed an existing parent mode or created a loose directory")
			}
		})
	}
}

func TestSaveFilePreservesSymlinks(t *testing.T) {
	for _, tc := range []struct {
		name          string
		missingTarget bool
		parentAlias   bool
		absolute      bool
	}{
		{name: "existing target"},
		{name: "absolute target", absolute: true},
		{name: "missing target", missingTarget: true},
		{name: "relative target under symlinked parent", parentAlias: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "physical", "nested")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(root, "physical", "target.toml")
			if !tc.missingTarget {
				if err := os.WriteFile(target, []byte("prior"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			link := filepath.Join(dir, "config.toml")
			linkTarget := "../target.toml"
			if tc.absolute {
				linkTarget = target
			}
			if err := os.Symlink(linkTarget, link); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			entry := filepath.Join(dir, "entry.toml")
			if err := os.Symlink("config.toml", entry); err != nil {
				t.Fatal(err)
			}
			if tc.parentAlias {
				alias := filepath.Join(root, "alias")
				if err := os.Symlink(dir, alias); err != nil {
					t.Fatal(err)
				}
				entry = filepath.Join(alias, "entry.toml")
			}
			if err := config.SaveFile(entry, atomicConfig()); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{entry, link} {
				info, err := os.Lstat(name)
				if err != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Fatal("saving replaced a configuration symlink")
				}
			}
			loaded, err := config.LoadFile(target)
			if err != nil || loaded.Workspaces["default"].DefaultChannel != "C_UPDATED" {
				t.Fatalf("saving did not update the symlink target: %v", err)
			}
			if _, err := os.Stat(filepath.Join(root, "target.toml")); !os.IsNotExist(err) {
				t.Fatal("relative link target resolved against a lexical parent")
			}
		})
	}
}

func TestSaveFileFailuresPreservePriorState(t *testing.T) {
	for _, name := range []string{"invalid config", "directory target", "missing link target parent", "symlink cycle"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "config.toml")
			cfg := atomicConfig()
			prior := []byte("keep prior state")
			sentinel := path
			switch name {
			case "invalid config":
				cfg.SchemaVersion = "unsupported"
			case "directory target":
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
				sentinel = filepath.Join(path, "keep.txt")
			case "missing link target parent":
				if err := os.Symlink("missing/target.toml", path); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				sentinel = filepath.Join(root, "keep.txt")
			case "symlink cycle":
				if err := os.Symlink("config.toml", path); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
				sentinel = filepath.Join(root, "keep.txt")
			}
			if err := os.WriteFile(sentinel, prior, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := config.SaveFile(path, cfg); err == nil {
				t.Fatal("invalid destination or configuration unexpectedly saved")
			}
			after, err := os.ReadFile(sentinel)
			if err != nil || !bytes.Equal(after, prior) {
				t.Fatal("failed save changed prior state")
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".config.toml.") || entry.Name() == "missing" {
					t.Fatal("failed save left a temporary file or created a missing symlink target directory")
				}
			}
		})
	}
}
