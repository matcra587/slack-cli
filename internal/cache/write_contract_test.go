package cache_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/matcra587/slack-cli/internal/cache"
)

func TestCacheWriteReplacesSymlinkWithoutChangingItsTarget(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	path, err := cache.Path("fixture", "users")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "untouched.json")
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := cache.Write("fixture", "users", json.RawMessage(`{"users":[]}`)); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("cache replacement lost its private regular file: %v", err)
	}
	body, err := os.ReadFile(target)
	if err != nil || string(body) != "original" {
		t.Fatal("cache write followed its destination symlink")
	}
	if _, ok, stale, err := cache.Read("fixture", "users", time.Hour); err != nil || !ok || stale {
		t.Fatalf("replacement cache unreadable: ok=%v stale=%v error=%v", ok, stale, err)
	}
}

func TestCacheWritePublicationFailureCleansTemporaryFileAndPreservesNeighbors(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	path, err := cache.Path("fixture", "users")
	if err != nil {
		t.Fatal(err)
	}
	// A nonempty directory prevents atomic replacement on every supported OS.
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	neighbor := filepath.Join(path, "keep")
	if err := os.WriteFile(neighbor, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Write("fixture", "users", json.RawMessage(`{"users":[]}`)); err == nil {
		t.Fatal("cache write replaced a nonempty directory")
	}
	body, err := os.ReadFile(neighbor)
	if err != nil || string(body) != "original" {
		t.Fatal("failed cache write changed its destination")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		t.Fatalf("failed cache write left temporary files: %v", err)
	}
}
