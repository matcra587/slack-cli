//go:build linux

package config_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/matcra587/slack-cli/internal/config"
)

func TestSaveFileInterruptedWritePreservesState(t *testing.T) {
	const prior = "synthetic prior configuration"
	if os.Getenv("SLICK_TEST_LIMITED_CONFIG_WRITE") == "1" {
		path := filepath.Join(os.Getenv("SLICK_TEST_CONFIG_DIR"), "config.toml")
		signal.Ignore(syscall.SIGXFSZ)
		var original syscall.Rlimit
		if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &original); err != nil {
			t.Fatal(err)
		}
		limited := original
		limited.Cur = 512
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limited); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &original); err != nil {
				t.Error(err)
			}
		}()
		cfg := atomicConfig()
		profile := cfg.Workspaces["default"]
		profile.Attribution.Message = strings.Repeat("synthetic fixture ", 1000)
		cfg.Workspaces["default"] = profile
		if err := config.SaveFile(path, cfg); !errors.Is(err, syscall.EFBIG) {
			t.Fatalf("size-limited configuration write must fail with EFBIG: %v", err)
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(after, []byte(prior)) {
			t.Fatal("failed write changed prior configuration")
		}
		entries, err := os.ReadDir(filepath.Dir(path))
		if err != nil || len(entries) != 1 || entries[0].Name() != "config.toml" {
			t.Fatal("failed write left a partial temporary configuration")
		}
		return
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(prior), 0o600); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "-test.run=^TestSaveFileInterruptedWritePreservesState$", "-test.count=1")
	cmd.Env = []string{"HOME=" + dir, "SLICK_TEST_LIMITED_CONFIG_WRITE=1", "SLICK_TEST_CONFIG_DIR=" + dir}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("limited-write fixture failed: %v\n%s", err, output)
	}
}
