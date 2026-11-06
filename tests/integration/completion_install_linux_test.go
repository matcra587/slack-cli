//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestCompletionFileSizeLimitHelper(t *testing.T) {
	if os.Getenv("SLICK_TEST_FSIZE_LIMIT_HELPER") != "1" {
		return
	}
	marker := slices.Index(os.Args, "--")
	if marker < 0 || marker+1 >= len(os.Args) {
		os.Exit(200)
	}
	signal.Ignore(syscall.SIGXFSZ)
	if err := unix.Setrlimit(unix.RLIMIT_FSIZE, &unix.Rlimit{Cur: 512, Max: 512}); err != nil {
		os.Exit(200)
	}
	args := os.Args[marker+1:]
	if err := unix.Exec(args[0], args, os.Environ()); err != nil {
		os.Exit(200)
	}
}

func TestBinaryCompletionFailedUpdatePreservesPriorScript(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, kind := range []string{"regular", "absolute link", "relative link", "relative link with parent alias"} {
		t.Run(kind, func(t *testing.T) {
			base := t.TempDir()
			configHome := filepath.Join(base, "config")
			realConfig := configHome
			if kind == "relative link with parent alias" {
				configHome = filepath.Join(base, "alias-zone", "config")
				if err := os.MkdirAll(filepath.Dir(configHome), 0o700); err != nil {
					t.Fatal(err)
				}
				realConfig = filepath.Join(base, "target-zone", "deep", "real-config")
				if err := os.Symlink(realConfig, configHome); err != nil {
					t.Fatal(err)
				}
			}
			parent := filepath.Join(realConfig, "fish", "completions")
			if err := os.MkdirAll(parent, 0o700); err != nil {
				t.Fatal(err)
			}
			entry := filepath.Join(configHome, "fish", "completions", "slick.fish")
			priorPath := entry
			linked := kind != "regular"
			if linked {
				priorPath = filepath.Join(base, "shared", "prior.fish")
				if kind == "relative link with parent alias" {
					priorPath = filepath.Join(base, "target-zone", "shared", "prior.fish")
				}
				if err := os.MkdirAll(filepath.Dir(priorPath), 0o700); err != nil {
					t.Fatal(err)
				}
				target := priorPath
				if kind != "absolute link" {
					var err error
					target, err = filepath.Rel(parent, priorPath)
					if err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Symlink(target, entry); err != nil {
					t.Fatal(err)
				}
			}
			prior := []byte("# synthetic prior completion\n" + strings.Repeat("prior-state\n", 400))
			if err := os.WriteFile(priorPath, prior, 0o640); err != nil {
				t.Fatal(err)
			}
			unrelated := filepath.Join(parent, "unrelated.settings")
			settings := []byte("synthetic unrelated settings\n")
			if err := os.WriteFile(unrelated, settings, 0o600); err != nil {
				t.Fatal(err)
			}
			snapshot := func(path string) []string {
				t.Helper()
				entries, err := os.ReadDir(path)
				if err != nil {
					t.Fatal(err)
				}
				names := make([]string, 0, len(entries))
				for _, entry := range entries {
					names = append(names, entry.Name())
				}
				return names
			}
			dirs := map[string][]string{parent: snapshot(parent), filepath.Dir(priorPath): snapshot(filepath.Dir(priorPath))}
			cfg := writePipeConfig(t)
			configBefore, err := os.ReadFile(cfg)
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"--install-completion", "--@shell=fish", "--output=json", "--color=never"}
			run := func(limited bool) ([]byte, []byte, error) {
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				var cmd *exec.Cmd
				if limited {
					helperArgs := append([]string{"-test.run=^TestCompletionFileSizeLimitHelper$", "--", binary}, args...)
					cmd = exec.CommandContext(ctx, os.Args[0], helperArgs...)
				} else {
					cmd = exec.CommandContext(ctx, binary, args...)
				}
				cmd.Env = append(isolatedRuntimeEnv(t), "SLICK_CONFIG="+cfg, "XDG_CONFIG_HOME="+configHome)
				if limited {
					cmd.Env = append(cmd.Env, "SLICK_TEST_FSIZE_LIMIT_HELPER=1")
				}
				var out, diagnostic bytes.Buffer
				cmd.Stdout = &out
				cmd.Stderr = &diagnostic
				err := cmd.Run()
				if ctx.Err() != nil || exitCode(err) == 200 {
					t.Fatal("file-size fixture did not execute slick successfully")
				}
				return out.Bytes(), diagnostic.Bytes(), err
			}
			out, diagnostic, runErr := run(true)
			if runErr == nil {
				t.Fatal("file-size guard did not cause a write failure")
			}
			after, err := os.ReadFile(priorPath)
			if err != nil || !bytes.Equal(prior, after) {
				t.Fatal("failed installation destroyed the prior completion script")
			}
			var frame struct {
				OK     bool
				Data   json.RawMessage
				Errors []struct {
					Type     string
					ExitCode int `json:"exit_code"`
				}
			}
			if exitCode(runErr) != 5 || json.Unmarshal(out, &frame) != nil || frame.OK || string(frame.Data) != "null" || len(frame.Errors) != 1 || frame.Errors[0].Type != "server_error" || frame.Errors[0].ExitCode != 5 || len(diagnostic) != 0 {
				t.Fatal("failed installation lost filesystem error class or machine envelope")
			}
			for dir, before := range dirs {
				if !reflect.DeepEqual(before, snapshot(dir)) {
					t.Fatal("failed installation left a temporary file or changed neighboring entries")
				}
			}
			out, diagnostic, runErr = run(false)
			var published struct {
				OK   bool
				Data struct {
					Action  string
					Changed bool
				}
			}
			if runErr != nil || len(diagnostic) != 0 || json.Unmarshal(out, &published) != nil || !published.OK || published.Data.Action != "install" || !published.Data.Changed {
				t.Fatalf("explicit installation recovery failed: %v", runErr)
			}
			after, err = os.ReadFile(priorPath)
			if err != nil || !bytes.Contains(after, []byte("complete -c slick")) || bytes.Equal(prior, after) {
				t.Fatal("recovery did not publish a complete fish script")
			}
			info, err := os.Lstat(entry)
			if err != nil || (info.Mode()&os.ModeSymlink != 0) != linked {
				t.Fatal("installation changed configured symlink behavior")
			}
			targetInfo, err := os.Stat(priorPath)
			if err != nil || targetInfo.Mode().Perm() != 0o640 {
				t.Fatal("installation changed prior script permissions")
			}
			saved, err := os.ReadFile(unrelated)
			if err != nil || !bytes.Equal(saved, settings) {
				t.Fatal("installation changed unrelated settings")
			}
			saved, err = os.ReadFile(cfg)
			if err != nil || !bytes.Equal(saved, configBefore) {
				t.Fatal("installation changed slick configuration")
			}
			for dir, before := range dirs {
				if !reflect.DeepEqual(before, snapshot(dir)) {
					t.Fatal("successful installation left temporary files or changed neighbors")
				}
			}
		})
	}
}

func TestBinaryCompletionRejectsNonRegularDestinationWithoutBlocking(t *testing.T) {
	binary := buildSlackBinary(t)
	home := t.TempDir()
	path := filepath.Join(home, "fish", "completions", "slick.fish")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "--install-completion", "--@shell=fish", "--output=json", "--color=never")
	cmd.Env = append(isolatedRuntimeEnv(t), "XDG_CONFIG_HOME="+home, "SLICK_CONFIG="+writePipeConfig(t))
	var out, diagnostic bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &diagnostic
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatal("non-regular completion destination caused blocking file access")
	}
	var frame struct {
		OK     bool
		Errors []struct {
			Exit int `json:"exit_code"`
		}
	}
	if exitCode(err) != 5 || json.Unmarshal(out.Bytes(), &frame) != nil || frame.OK || len(frame.Errors) != 1 || frame.Errors[0].Exit != 5 || diagnostic.Len() != 0 {
		t.Fatal("non-regular destination lost filesystem failure contract")
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatal("installation replaced the non-regular destination")
	}
}
