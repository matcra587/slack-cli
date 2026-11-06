package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gechr/clog"
)

func TestColorNeverKeepsConfigPathsPlain(t *testing.T) {
	var stdout, stderr bytes.Buffer
	path := filepath.Join(t.TempDir(), "preferences.toml")
	root, runtime := NewRootCommandWithRuntime(WithConfig(nil), WithConfigPath(path), WithIO(strings.NewReader(""), &stdout, &stderr))
	root.SetArgs([]string{"config", "path", "--output=human", "--color=never"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if runtime.ColorMode != clog.ColorNever {
		t.Fatalf("color flag resolved to %v, want never", runtime.ColorMode)
	}
	if strings.Contains(stdout.String(), "\x1b") || !strings.Contains(stdout.String(), "preferences.toml") {
		t.Fatalf("no-color output contains terminal escapes or loses the filename: %q", stdout.String())
	}
}
