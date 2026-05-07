package cache_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/matcra587/slack-cli/internal/cache"
)

func TestCacheRoundTripUsesSlickXDGCacheHome(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", root)

	payload := json.RawMessage(`{"users":[{"id":"U123","name":"matt"}]}`)
	if _, err := cache.Write("default", "users", payload); err != nil {
		t.Fatalf("Write: %v", err)
	}

	path, err := cache.Path("default", "users")
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if want := filepath.Join(root, "slick", "default", "users.json"); path != want {
		t.Fatalf("Path = %q, want %q", path, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat cache file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("cache file mode = %v, want 0600", got)
	}

	entry, ok, stale, err := cache.Read("default", "users", time.Hour)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !ok {
		t.Fatal("Read ok=false, want true")
	}
	if stale {
		t.Fatal("fresh entry reported stale")
	}
	var got, want map[string]any
	if err := json.Unmarshal(entry.Data, &got); err != nil {
		t.Fatalf("decode entry data: %v", err)
	}
	if err := json.Unmarshal(payload, &want); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if !jsonEqual(got, want) {
		t.Fatalf("entry data = %s, want %s", entry.Data, payload)
	}
}

func jsonEqual(got, want any) bool {
	gotBody, err := json.Marshal(got)
	if err != nil {
		return false
	}
	wantBody, err := json.Marshal(want)
	if err != nil {
		return false
	}
	return string(gotBody) == string(wantBody)
}

func TestCacheReportsStaleAndClearRemovesOnlyOneResource(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	if _, err := cache.Write("default", "users", json.RawMessage(`{"users":[]}`)); err != nil {
		t.Fatalf("Write users: %v", err)
	}
	if _, err := cache.Write("default", "channels", json.RawMessage(`{"channels":[]}`)); err != nil {
		t.Fatalf("Write channels: %v", err)
	}

	path, err := cache.Path("default", "users")
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	old := time.Now().Add(-48 * time.Hour).UTC()
	staleEntry := cache.Entry{
		Profile:   "default",
		Resource:  "users",
		FetchedAt: old,
		Data:      json.RawMessage(`{"users":[{"id":"UOLD"}]}`),
	}
	body, err := json.Marshal(staleEntry)
	if err != nil {
		t.Fatalf("marshal stale entry: %v", err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("rewrite stale entry: %v", err)
	}

	entry, ok, stale, err := cache.Read("default", "users", 24*time.Hour)
	if err != nil || !ok {
		t.Fatalf("Read ok=%v err=%v", ok, err)
	}
	if !stale {
		t.Fatalf("entry from %s should be stale", entry.FetchedAt)
	}

	removed, err := cache.Clear("default", "users")
	if err != nil || !removed {
		t.Fatalf("Clear users removed=%v err=%v", removed, err)
	}
	if _, ok, _, _ := cache.Read("default", "users", time.Hour); ok {
		t.Fatal("users cache still exists after clear")
	}
	if _, ok, _, _ := cache.Read("default", "channels", time.Hour); !ok {
		t.Fatal("clearing users removed channels cache")
	}
}

func TestCachePathTraversalSafe(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", root)

	for _, tc := range []struct {
		profile  string
		resource string
	}{
		{profile: "../../etc", resource: "passwd"},
		{profile: "default", resource: "../../etc/passwd"},
		{profile: "default/../../etc", resource: "users"},
	} {
		path, err := cache.Path(tc.profile, tc.resource)
		if err != nil {
			t.Fatalf("Path(%q, %q): %v", tc.profile, tc.resource, err)
		}
		clean := filepath.Clean(path)
		expectedRoot := filepath.Join(root, "slick")
		if !strings.HasPrefix(clean, expectedRoot+string(filepath.Separator)) && clean != expectedRoot {
			t.Fatalf("path escaped cache root: profile=%q resource=%q path=%q root=%q", tc.profile, tc.resource, clean, expectedRoot)
		}
	}
}
