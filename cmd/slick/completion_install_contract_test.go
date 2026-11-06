package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	clicompletion "github.com/matcra587/slack-cli/internal/cli/completion"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/spf13/cobra"
)

func runCompletionInstallFixture(t *testing.T, cfg *config.Config, shell, mode string, out, diagnostic io.Writer) (*cobra.Command, error) {
	t.Helper()
	root, rt := NewRootCommandWithRuntime(WithConfig(cfg), WithTTY(false), WithSlackBaseURL("http://127.0.0.1:1"), WithIO(strings.NewReader(""), out, diagnostic), WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
		t.Error("completion installation accessed a credential provider")
		return "", errors.New("synthetic-private-install-provider")
	})))
	root.SetArgs([]string{"--install-completion", "--@shell=" + shell, "--output=" + mode, "--color=never"})
	err := root.ExecuteContext(t.Context())
	if rt.CancelTimeout != nil {
		rt.CancelTimeout()
	}
	return root, err
}

func TestCompletionInstallLifecyclePreservesScriptsAndNeighbors(t *testing.T) {
	for _, shell := range []struct{ name, base, path string }{
		{"bash", "data", "bash-completion/completions/slick"}, {"zsh", "data", "zsh/site-functions/_slick"}, {"fish", "config", "fish/completions/slick.fish"}, {"nu", "data", "nushell/vendor/autoload/slick.nu"}, {"pwsh", "config", "powershell/completions/slick.ps1"}, {"elvish", "config", "elvish/lib/completions/slick.elv"},
	} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(shell.name+"/"+mode, func(t *testing.T) {
				base := t.TempDir()
				configHome, dataHome := filepath.Join(base, "config"), filepath.Join(base, "data")
				t.Setenv("XDG_CONFIG_HOME", configHome)
				t.Setenv("XDG_DATA_HOME", dataHome)
				home := configHome
				if shell.base == "data" {
					home = dataHome
				}
				path := filepath.Join(home, filepath.FromSlash(shell.path))
				cfg := completionProfileConfig()
				before, err := json.Marshal(cfg)
				if err != nil {
					t.Fatal(err)
				}
				install := func(wantMode os.FileMode) {
					t.Helper()
					var out, diagnostic bytes.Buffer
					root, err := runCompletionInstallFixture(t, cfg, shell.name, mode, &out, &diagnostic)
					if err != nil {
						t.Fatalf("install failed: %v", err)
					}
					assertCompletionAction(t, out.String(), diagnostic.String(), mode, "install", shell.name, path, true, false, false)
					var expected bytes.Buffer
					if err := clicompletion.Generator(root).Print(&expected, shell.name); err != nil {
						t.Fatal(err)
					}
					actual, err := os.ReadFile(path)
					if err != nil || expected.Len() == 0 || !bytes.Equal(actual, expected.Bytes()) {
						t.Fatal("installed script differs from pinned generator bytes")
					}
					info, err := os.Stat(path)
					if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != wantMode) {
						t.Fatal("installation changed expected file permissions")
					}
					entries, err := os.ReadDir(filepath.Dir(path))
					if err != nil {
						t.Fatal(err)
					}
					for _, entry := range entries {
						if strings.HasPrefix(entry.Name(), "."+filepath.Base(path)+".") {
							t.Fatal("installation left a temporary file")
						}
					}
				}
				install(0o600)
				if err := os.Chmod(filepath.Dir(path), 0o700); err != nil { //nolint:gosec // This private fixture directory needs owner traversal.
					t.Fatal(err)
				}
				unrelated := filepath.Join(filepath.Dir(path), "unrelated.settings")
				neighbor := []byte("synthetic unrelated settings\n")
				if err := os.WriteFile(unrelated, neighbor, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("synthetic externally changed script\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, 0o640); err != nil { //nolint:gosec // Synthetic script tests preservation of an existing group-read mode.
					t.Fatal(err)
				}
				install(0o640)
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				install(0o600)
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
				occupant := filepath.Join(path, "keep.settings")
				if err := os.WriteFile(occupant, neighbor, 0o600); err != nil {
					t.Fatal(err)
				}
				var out, diagnostic bytes.Buffer
				_, err = runCompletionInstallFixture(t, cfg, shell.name, mode, &out, &diagnostic)
				typed, ok := errors.AsType[CommandError](err)
				if !ok || typed.CLIError.ExitCode != 5 {
					t.Fatal("non-file drift did not return a filesystem failure")
				}
				if mode == "human" {
					if out.Len() != 0 || diagnostic.Len() == 0 {
						t.Fatal("human filesystem failure used wrong stream")
					}
				} else {
					var frame struct {
						OK     bool
						Data   json.RawMessage
						Errors []struct {
							Type string
							Exit int `json:"exit_code"`
						}
					}
					if json.Unmarshal(out.Bytes(), &frame) != nil || frame.OK || string(frame.Data) != "null" || len(frame.Errors) != 1 || frame.Errors[0].Type != "server_error" || frame.Errors[0].Exit != 5 || diagnostic.Len() != 0 {
						t.Fatal("filesystem failure lost common envelope")
					}
				}
				saved, err := os.ReadFile(occupant)
				if err != nil || !bytes.Equal(saved, neighbor) {
					t.Fatal("failed installation changed unrelated directory contents")
				}
				if err := os.Remove(occupant); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				install(0o600)
				saved, err = os.ReadFile(unrelated)
				if err != nil || !bytes.Equal(saved, neighbor) {
					t.Fatal("installation changed neighboring settings")
				}
				parentInfo, parentErr := os.Stat(filepath.Dir(path))
				if parentErr != nil || (runtime.GOOS != "windows" && parentInfo.Mode().Perm() != 0o700) {
					t.Fatal("installation widened existing parent permissions")
				}
				for _, changed := range []bool{true, false} {
					out, diagnostic, err := executeTestRoot(t, cfg, "http://127.0.0.1:1", "", []string{"--uninstall-completion", "--@shell=" + shell.name, "--output=" + mode, "--color=never"})
					if err != nil {
						t.Fatal(err)
					}
					assertCompletionAction(t, out, diagnostic, mode, "uninstall", shell.name, path, changed, false, false)
					if _, err := os.Stat(path); !os.IsNotExist(err) {
						t.Fatal("completion removal did not remove only the selected entry")
					}
					neighborAfter, err := os.ReadFile(unrelated)
					if err != nil || !bytes.Equal(neighbor, neighborAfter) {
						t.Fatal("completion removal changed neighboring settings")
					}
				}
				after, err := json.Marshal(cfg)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatal("installation changed slick profile settings")
				}
			})
		}
	}
}

func TestCompletionInstallStatusFailureDoesNotRetryOrLosePublishedScript(t *testing.T) {
	for _, mode := range []string{"json", "compact", "human"} {
		for _, failure := range []string{"error", "short", "partial", "transient"} {
			t.Run(mode+"/"+failure, func(t *testing.T) {
				base := t.TempDir()
				t.Setenv("XDG_CONFIG_HOME", base)
				out := &completionFailingWriter{mode: failure}
				var diagnostic bytes.Buffer
				root, err := runCompletionInstallFixture(t, completionProfileConfig(), "fish", mode, out, &diagnostic)
				typed, ok := errors.AsType[CommandError](err)
				if !ok || typed.CLIError.ExitCode != 8 || typed.CLIError.Message != "output_write_failed" || out.calls != 1 || strings.Count(diagnostic.String(), "output_write_failed") != 1 || strings.Contains(err.Error(), "synthetic-private") {
					t.Fatal("failed status write lost IO8, retried a destination, or leaked details")
				}
				var expected bytes.Buffer
				if err := clicompletion.Generator(root).Print(&expected, "fish"); err != nil {
					t.Fatal(err)
				}
				saved, err := os.ReadFile(filepath.Join(base, "fish", "completions", "slick.fish"))
				if err != nil || !bytes.Equal(saved, expected.Bytes()) {
					t.Fatal("lost status removed or corrupted an already published script")
				}
				var nextOut, nextDiagnostic bytes.Buffer
				if _, err := runCompletionInstallFixture(t, completionProfileConfig(), "fish", mode, &nextOut, &nextDiagnostic); err != nil || nextOut.Len() == 0 || nextDiagnostic.Len() != 0 {
					t.Fatalf("explicit status recovery failed: %v", err)
				}
			})
		}
	}
}

func TestCompletionInstallCancellationDoesNotCreateResources(t *testing.T) {
	for _, mode := range []string{"json", "compact", "human"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config")
			t.Setenv("XDG_CONFIG_HOME", path)
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			var out, diagnostic bytes.Buffer
			root, rt := NewRootCommandWithRuntime(WithConfig(completionProfileConfig()), WithTTY(false), WithIO(strings.NewReader(""), &out, &diagnostic))
			root.SetArgs([]string{"--install-completion", "--@shell=fish", "--output=" + mode, "--color=never"})
			err := root.ExecuteContext(ctx)
			if rt.CancelTimeout != nil {
				rt.CancelTimeout()
			}
			typed, ok := errors.AsType[CommandError](err)
			if !ok || typed.CLIError.ExitCode != 6 {
				t.Fatalf("canceled installation did not return cancellation: %v", err)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("canceled installation created resources")
			}
			if mode == "human" {
				if out.Len() != 0 || diagnostic.Len() == 0 {
					t.Fatal("human cancellation used wrong stream")
				}
			} else {
				var frame struct {
					OK     bool
					Data   json.RawMessage
					Errors []struct {
						Exit int `json:"exit_code"`
					}
				}
				if diagnostic.Len() != 0 || json.Unmarshal(out.Bytes(), &frame) != nil || frame.OK || string(frame.Data) != "null" || len(frame.Errors) != 1 || frame.Errors[0].Exit != 6 {
					t.Fatal("machine cancellation lost failure envelope")
				}
			}
		})
	}
}

func TestCompletionInstallationRetainsActionPriorityAndRemoval(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"install before print", []string{"--install-completion", "--print-completion"}, "install"},
		{"install before uninstall", []string{"--install-completion", "--uninstall-completion"}, "install"},
		{"dynamic before install", []string{"--@complete=workspace", "--install-completion", "--print-completion", "--uninstall-completion"}, "dynamic"},
		{"disabled install", []string{"--install-completion=false", "--print-completion"}, "script"},
		{"removed install", nil, "discovery"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", dir)
			args := append([]string{"--output=json", "--color=never", "--@shell=fish"}, tc.args...)
			out, diagnostic, err := executeTestRoot(t, completionProfileConfig(), "http://127.0.0.1:1", "", args)
			if err != nil {
				t.Fatalf("completion action invocation failed: %v", err)
			}
			info, statErr := os.Stat(filepath.Join(dir, "fish", "completions", "slick.fish"))
			if tc.want == "install" {
				if statErr != nil || !info.Mode().IsRegular() || diagnostic != "" {
					t.Fatal("higher-priority install action lost")
				}
				assertCompletionAction(t, out, diagnostic, "json", "install", "fish", filepath.Join(dir, "fish", "completions", "slick.fish"), true, false, false)
				return
			}
			if !os.IsNotExist(statErr) || diagnostic != "" {
				t.Fatal("non-install action created a script or changed diagnostics")
			}
			switch tc.want {
			case "dynamic":
				if !strings.Contains(out, "default\n") || strings.Contains(out, "complete -c slick") {
					t.Fatal("dynamic action lost priority")
				}
			case "script":
				if !strings.Contains(out, "complete -c slick") {
					t.Fatal("disabling install did not restore print")
				}
			case "discovery":
				if !strings.Contains(out, `"schema"`) {
					t.Fatal("removing install did not restore discovery")
				}
			}
		})
	}
}
