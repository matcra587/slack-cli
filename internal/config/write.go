package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	xos "github.com/gechr/x/os"
)

// SaveWasPublished reports whether a failed SaveFile replaced the destination.
// Such a failure leaves the new config in place; only crash durability is unconfirmed.
func SaveWasPublished(err error) bool {
	_, ok := errors.AsType[*publishedSaveError](err)
	return ok
}

type publishedSaveError struct{ cause error }

func (e *publishedSaveError) Error() string {
	return fmt.Sprintf("failed to sync directory after rename: %v", e.cause)
}

func (e *publishedSaveError) Unwrap() error { return e.cause }

// The shared AtomicWrite helper reports both pre-rename and post-rename errors
// without distinguishing their outcome. Authentication needs that distinction
// to keep the saved credential paired with the config actually published.
func writeConfigFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("creating config temporary file: %w", err)
	}
	tmpName := tmp.Name()
	published := false
	defer func() {
		if !published {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing config temporary file: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("protecting config temporary file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("syncing config temporary file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing config temporary file: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replacing config: %w", err)
	}
	published = true
	// Match the shared helper's platform policy: directory sync is Unix-only.
	if xos.IsUnix() {
		directory, err := os.Open(dir) //nolint:gosec // Sync the caller-selected config directory after the completed rename.
		if err != nil {
			return &publishedSaveError{cause: err}
		}
		defer func() { _ = directory.Close() }()
		if err := directory.Sync(); err != nil {
			return &publishedSaveError{cause: err}
		}
	}
	return nil
}
