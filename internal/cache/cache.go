// Package cache stores cheap Slack metadata as per-profile JSON files.
package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	xfs "github.com/gechr/x/fs"
	"github.com/gechr/x/shell"
)

const DefaultTTL = 24 * time.Hour

type Entry struct {
	Profile   string          `json:"profile"`
	Resource  string          `json:"resource"`
	FetchedAt time.Time       `json:"fetched_at"`
	Data      json.RawMessage `json:"data"`
}

func Path(profile, resource string) (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, sanitize(profile), sanitize(resource)+".json")
	if !xfs.IsWithin(root, path) {
		return "", fmt.Errorf("cache path escaped root")
	}
	return path, nil
}

func Root() (string, error) {
	dir, err := shell.XDGCacheHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "slick"), nil
}

func Read(profile, resource string, ttl time.Duration) (entry Entry, ok, stale bool, err error) {
	path, err := Path(profile, resource)
	if err != nil {
		return Entry{}, false, false, err
	}
	body, err := os.ReadFile(path) //nolint:gosec // Path builds a sanitized path constrained under the XDG cache root.
	if err != nil {
		if os.IsNotExist(err) {
			return Entry{}, false, false, nil
		}
		return Entry{}, false, false, fmt.Errorf("read cache %s: %w", path, err)
	}
	if err := json.Unmarshal(body, &entry); err != nil {
		return Entry{}, false, false, fmt.Errorf("decode cache %s: %w", path, err)
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return entry, true, time.Since(entry.FetchedAt) > ttl, nil
}

func Write(profile, resource string, data json.RawMessage) (Entry, error) {
	path, err := Path(profile, resource)
	if err != nil {
		return Entry{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Entry{}, err
	}
	entry := Entry{
		Profile:   profile,
		Resource:  resource,
		FetchedAt: time.Now().UTC(),
		Data:      data,
	}
	body, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return Entry{}, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return Entry{}, err
	}
	defer func() {
		_ = os.Remove(tmp.Name())
	}()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return Entry{}, err
	}
	if err := tmp.Close(); err != nil {
		return Entry{}, err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

func Clear(profile, resource string) (bool, error) {
	path, err := Path(profile, resource)
	if err != nil {
		return false, err
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// ClearProfile removes every cache file for the given profile and returns
// the resource names that were removed (file basenames sans .json), in the
// order the directory listed them.
func ClearProfile(profile string) ([]string, error) {
	root, err := Root()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, sanitize(profile))
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var removed []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			return removed, err
		}
		removed = append(removed, strings.TrimSuffix(name, ".json"))
	}
	return removed, nil
}

func sanitize(name string) string {
	if name == "" {
		return "_"
	}
	return strings.NewReplacer("/", "_", "\\", "_", "..", "_").Replace(name)
}
