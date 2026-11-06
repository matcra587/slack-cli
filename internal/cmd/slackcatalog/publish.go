package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type jsonOutput struct {
	path  string
	value any
}
type stagedOutput struct {
	path, replacement, backup string
	identity                  os.FileInfo
}

func writeJSON(path string, value any) error {
	return publishJSON([]jsonOutput{{path, value}}, os.Rename)
}

// publishJSON stages the complete generation before replacing any output. If a
// later rename fails, earlier replacements are restored from same-directory
// backups. Multiple filesystem paths cannot be published atomically; this
// rollback protects ordinary write failures, not process termination or power
// loss between renames. Concurrent generators must use different output paths.
func publishJSON(outputs []jsonOutput, rename func(string, string) error) error {
	staged := make([]stagedOutput, 0, len(outputs))
	defer func() {
		for _, output := range staged {
			_ = os.Remove(output.replacement)
			if output.backup != "" {
				_ = os.Remove(output.backup)
			}
		}
	}()
	seen := map[string]bool{}
	for _, output := range outputs {
		path, err := filepath.Abs(output.path)
		if err != nil {
			return err
		}
		if seen[path] {
			return errors.New("catalog outputs must use distinct paths")
		}
		seen[path] = true
		raw, err := json.MarshalIndent(output.value, "", "  ")
		if err != nil {
			return err
		}
		prior, err := os.Lstat(path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil && !prior.Mode().IsRegular() {
			return errors.New("catalog output must be a regular file or a new path")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return err
		}
		replacement, err := stageFile(path, append(raw, '\n'), 0o644)
		if err != nil {
			return err
		}
		staged = append(staged, stagedOutput{path: path, replacement: replacement})
		current := &staged[len(staged)-1]
		current.identity, err = os.Stat(replacement)
		if err != nil {
			return err
		}
		if prior != nil {
			old, err := os.ReadFile(path) //nolint:gosec // Explicit generator output is backed up before publication.
			if err != nil {
				return err
			}
			current.backup, err = stageFile(path, old, prior.Mode().Perm())
			if err != nil {
				return err
			}
		}
	}
	for index, output := range staged {
		if err := rename(output.replacement, output.path); err != nil {
			failures := []error{err}
			for previous := index - 1; previous >= 0; previous-- {
				old := staged[previous]
				identity, statErr := os.Lstat(old.path)
				if statErr != nil || !os.SameFile(identity, old.identity) {
					staged[previous].backup = "" // Retain the prior generation for manual recovery.
					failures = append(failures, fmt.Errorf("cannot roll back changed catalog output %s; prior generation retained at %s", old.path, old.backup))
					continue
				}
				if old.backup != "" {
					if err := rename(old.backup, old.path); err != nil {
						staged[previous].backup = "" // A failed rollback must not delete the only remaining prior bytes.
						failures = append(failures, fmt.Errorf("rollback failed; prior generation retained at %s: %w", old.backup, err))
					}
				} else if err := os.Remove(old.path); err != nil {
					failures = append(failures, err)
				}
			}
			return errors.Join(failures...)
		}
	}
	return nil
}

func stageFile(path string, raw []byte, mode os.FileMode) (string, error) {
	file, err := os.CreateTemp(filepath.Dir(path), ".slackcatalog-*")
	if err != nil {
		return "", err
	}
	name := file.Name()
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if _, err := file.Write(raw); err != nil {
		return "", err
	}
	if err := file.Chmod(mode); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	ok = true
	return name, nil
}
