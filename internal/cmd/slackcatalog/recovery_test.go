package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFailedRollbackRetainsPriorBytesForRecovery(t *testing.T) {
	directory := t.TempDir()
	first, second := filepath.Join(directory, "methods.json"), filepath.Join(directory, "sdk.json")
	if err := os.WriteFile(first, []byte("prior generation"), 0o640); err != nil {
		t.Fatal(err)
	}
	calls := 0
	err := publishJSON([]jsonOutput{{first, map[string]bool{"new": true}}, {second, map[string]bool{"new": true}}}, func(source, target string) error {
		calls++
		if calls >= 2 {
			return errors.New("fixture publication and rollback blocked")
		}
		return os.Rename(source, target)
	})
	if err == nil || !strings.Contains(err.Error(), "prior generation retained at") {
		t.Fatalf("missing recovery diagnostic: %v", err)
	}
	files, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	recovered := false
	for _, file := range files {
		if !strings.HasPrefix(file.Name(), ".slackcatalog-") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(directory, file.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if string(body) == "prior generation" {
			recovered = true
			info, _ := file.Info()
			if info.Mode().Perm() != 0o640 {
				t.Fatal("recovery backup permissions changed")
			}
		}
	}
	if !recovered {
		t.Fatal("failed rollback deleted prior generation")
	}
}
