package cache_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/cache"
)

func TestCacheProfilePathsAreDistinctBoundedAndPortable(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", root)
	seen := map[string]string{}
	for _, name := range []string{"default", "prod.eu", "team_1", "team-1", "a/b", `a\b`, "a_b", "a..b", ".", "..", "", "_", "~reserved", "team.", "team", "CON", "con.txt", "NUL", "COM1", "LPT9.log", "space name", "colon:name", strings.Repeat("界", 100), strings.Repeat("x", 260)} {
		t.Run(name, func(t *testing.T) {
			path, err := cache.Path(name, "users")
			if err != nil {
				t.Fatal(err)
			}
			if prior, ok := seen[path]; ok {
				t.Fatalf("distinct profile names %q and %q share a path", prior, name)
			}
			seen[path] = name
			component := filepath.Base(filepath.Dir(path))
			switch name {
			case "CON", "con.txt", "NUL", "COM1", "LPT9.log":
				if component == name {
					t.Fatal("Windows device name was used as a cache directory")
				}
			}
			if len(component) > 200 || strings.ContainsAny(component, `/\ :<>"|?*`) || strings.HasSuffix(component, ".") || filepath.Dir(filepath.Dir(path)) != filepath.Join(root, "slick") {
				t.Fatal("profile path is unbounded, nonportable, or escaped its root")
			}
			again, err := cache.Path(name, "users")
			if err != nil || again != path {
				t.Fatal("profile cache path is not stable")
			}
			if component != name {
				literal, err := cache.Path(component, "users")
				if err != nil || literal == path {
					t.Fatal("generated directory name aliases a literal profile name")
				}
			}
		})
	}
	for _, name := range []string{"default", "prod.eu", "team_1", "team-1"} {
		path, err := cache.Path(name, "users")
		if err != nil || path != filepath.Join(root, "slick", name, "users.json") {
			t.Fatal("ordinary profile cache layout changed")
		}
	}
}

func TestCacheUnsafeProfileLeavesLegacyAmbiguousDirectoryAlone(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", root)
	legacy := filepath.Join(root, "slick", "a_b", "users.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatal(err)
	}
	prior := []byte(`{"profile":"a/b","resource":"users","data":{"users":[]}}`)
	if err := os.WriteFile(legacy, prior, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, sweep := range []bool{false, true} {
		if _, err := cache.Write("a/b", "users", json.RawMessage(`{"users":[{"id":"U1"}]}`)); err != nil {
			t.Fatal(err)
		}
		if sweep {
			if _, err := cache.ClearProfile("a/b"); err != nil {
				t.Fatal(err)
			}
		} else if _, err := cache.Clear("a/b", "users"); err != nil {
			t.Fatal(err)
		}
		body, err := os.ReadFile(legacy)
		if err != nil || !bytes.Equal(body, prior) {
			t.Fatal("cache write or clearing changed the ambiguous legacy directory")
		}
	}
}
