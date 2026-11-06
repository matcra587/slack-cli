package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/matcra587/slack-cli/internal/config"
)

type completionActionFrame struct {
	Action      string `json:"action"`
	Shell       string `json:"shell"`
	Path        string `json:"path"`
	Changed     bool   `json:"changed"`
	DryRun      bool   `json:"dry_run"`
	WouldChange bool   `json:"would_change"`
}

func assertCompletionAction(t *testing.T, out, diagnostic, mode, action, shell, path string, changed, dryRun, wouldChange bool) {
	t.Helper()
	if diagnostic != "" {
		t.Fatal("successful completion action wrote stderr")
	}
	if mode == "human" {
		if out == "" || strings.HasPrefix(out, "{") || !strings.Contains(out, path) {
			t.Fatal("human completion action lost text or selected destination")
		}
		return
	}
	raw := json.RawMessage(out)
	if mode == "json" {
		var frame struct {
			OK     bool
			Meta   struct{ Command string }
			Data   json.RawMessage
			Errors []json.RawMessage
		}
		var keys map[string]json.RawMessage
		if json.Unmarshal(raw, &keys) != nil || len(keys) != 4 || json.Unmarshal(raw, &frame) != nil || !frame.OK || frame.Meta.Command != "slick" || frame.Errors == nil || len(frame.Errors) != 0 {
			t.Fatal("completion action lost common success envelope")
		}
		raw = frame.Data
	}
	var data completionActionFrame
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 6 || json.Unmarshal(raw, &data) != nil || data.Action != action || data.Shell != shell || data.Path != path || data.Changed != changed || data.DryRun != dryRun || data.WouldChange != wouldChange {
		t.Fatal("completion action returned wrong result fields or state")
	}
}

func TestCompletionActionsUseSharedSuccessProtocol(t *testing.T) {
	for _, mode := range []string{"json", "compact", "human"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", dir)
			path := filepath.Join(dir, "fish", "completions", "slick.fish")
			for _, tc := range []struct {
				action  string
				changed bool
			}{{"install", true}, {"uninstall", true}, {"uninstall", false}} {
				action := tc.action
				flag := "--" + action + "-completion"
				out, diagnostic, err := executeTestRoot(t, completionProfileConfig(), "http://127.0.0.1:1", "", []string{flag, "--@shell=fish", "--output=" + mode, "--color=never"})
				if err != nil {
					t.Fatalf("completion action failed: %v", err)
				}
				if action == "uninstall" {
					_, statErr := os.Stat(path)
					if !os.IsNotExist(statErr) {
						t.Fatal("uninstall retained completion entry")
					}
				}
				assertCompletionAction(t, out, diagnostic, mode, action, "fish", path, tc.changed, false, false)
			}
		})
	}
}

func runCompletionActionFixture(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var out, diagnostic bytes.Buffer
	root, rt := NewRootCommandWithRuntime(WithConfig(completionProfileConfig()), WithTTY(false), WithIO(strings.NewReader(""), &out, &diagnostic), WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
		t.Error("completion action accessed credentials")
		return "", errors.New("synthetic-private-completion-provider")
	})))
	root.SetArgs(append([]string{"--@shell=fish", "--color=never"}, args...))
	err := root.ExecuteContext(t.Context())
	if rt.CancelTimeout != nil {
		rt.CancelTimeout()
	}
	return out.String(), diagnostic.String(), err
}

func TestCompletionDryRunDoesNotCreateOrModifyResources(t *testing.T) {
	for _, mode := range []string{"json", "compact", "human"} {
		t.Run(mode, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "new-config")
			t.Setenv("XDG_CONFIG_HOME", dir)
			path := filepath.Join(dir, "fish", "completions", "slick.fish")
			for _, action := range []string{"install", "uninstall"} {
				out, diagnostic, err := runCompletionActionFixture(t, "--"+action+"-completion", "--dry-run", "--output="+mode)
				if err != nil {
					t.Fatalf("missing-resource dry run failed: %v", err)
				}
				assertCompletionAction(t, out, diagnostic, mode, action, "fish", path, false, true, action == "install")
				if _, err := os.Stat(dir); !os.IsNotExist(err) {
					t.Fatal("dry run created directories or a completion file")
				}
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			prior := []byte("synthetic imported script\n")
			if err := os.WriteFile(path, prior, 0o600); err != nil {
				t.Fatal(err)
			}
			for _, action := range []string{"install", "uninstall"} {
				out, diagnostic, err := runCompletionActionFixture(t, "--"+action+"-completion", "--dry-run", "--output="+mode)
				if err != nil {
					t.Fatalf("existing-resource dry run failed: %v", err)
				}
				assertCompletionAction(t, out, diagnostic, mode, action, "fish", path, false, true, true)
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(prior, after) {
					t.Fatal("dry run changed an existing script")
				}
			}
		})
	}
}

func TestCompletionRemovalPreservesSymlinkTargets(t *testing.T) {
	for _, kind := range []string{"relative", "absolute", "dangling", "cycle"} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				dir := t.TempDir()
				t.Setenv("XDG_CONFIG_HOME", dir)
				path := filepath.Join(dir, "fish", "completions", "slick.fish")
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(dir, "owned-target.fish")
				prior := []byte("synthetic owner-managed script\n")
				if kind == "relative" || kind == "absolute" {
					if err := os.WriteFile(target, prior, 0o600); err != nil {
						t.Fatal(err)
					}
				}
				link := target
				if kind == "relative" {
					var err error
					link, err = filepath.Rel(filepath.Dir(path), target)
					if err != nil {
						t.Fatal(err)
					}
				}
				if kind == "cycle" {
					link = "slick.fish"
				}
				if err := os.Symlink(link, path); err != nil {
					t.Skipf("symlink fixture unavailable: %v", err)
				}
				for _, step := range []struct{ dry, changed, would bool }{{true, false, true}, {false, true, false}, {false, false, false}} {
					args := []string{"--uninstall-completion", "--output=" + mode}
					if step.dry {
						args = append(args, "--dry-run")
					}
					out, diagnostic, err := runCompletionActionFixture(t, args...)
					if err != nil {
						t.Fatalf("symlink removal failed: %v", err)
					}
					assertCompletionAction(t, out, diagnostic, mode, "uninstall", "fish", path, step.changed, step.dry, step.would)
					info, err := os.Lstat(path)
					if step.dry {
						if err != nil || info.Mode()&os.ModeSymlink == 0 {
							t.Fatal("dry run changed symlink")
						}
					} else if !os.IsNotExist(err) {
						t.Fatal("uninstall retained completion entry")
					}
					if kind == "relative" || kind == "absolute" {
						after, err := os.ReadFile(target)
						if err != nil || !bytes.Equal(prior, after) {
							t.Fatal("uninstall modified or deleted symlink target")
						}
					}
				}
			})
		}
	}
}

func TestCompletionRemovalRejectsDirectoryDriftAndRecovers(t *testing.T) {
	for _, mode := range []string{"json", "compact", "human"} {
		for _, occupied := range []bool{false, true} {
			t.Run(fmt.Sprint(occupied)+"/"+mode, func(t *testing.T) {
				dir := t.TempDir()
				t.Setenv("XDG_CONFIG_HOME", dir)
				path := filepath.Join(dir, "fish", "completions", "slick.fish")
				if err := os.MkdirAll(path, 0o700); err != nil {
					t.Fatal(err)
				}
				marker := filepath.Join(path, "owner.settings")
				prior := []byte("synthetic unrelated settings\n")
				if occupied {
					if err := os.WriteFile(marker, prior, 0o600); err != nil {
						t.Fatal(err)
					}
				}
				for _, dry := range []bool{false, true} {
					args := []string{"--uninstall-completion", "--output=" + mode}
					if dry {
						args = append(args, "--dry-run")
					}
					out, diagnostic, err := runCompletionActionFixture(t, args...)
					typed, ok := errors.AsType[CommandError](err)
					if !ok || typed.CLIError.ExitCode != 5 {
						t.Fatal("directory drift did not return filesystem failure")
					}
					if info, err := os.Stat(path); err != nil || !info.IsDir() {
						t.Fatal("uninstall deleted an unrelated directory")
					}
					if occupied {
						after, err := os.ReadFile(marker)
						if err != nil || !bytes.Equal(prior, after) {
							t.Fatal("uninstall changed directory contents")
						}
					}
					if mode == "human" {
						if out != "" || diagnostic == "" {
							t.Fatal("human removal failure used wrong stream")
						}
					} else {
						var frame struct {
							OK   bool
							Data json.RawMessage
						}
						if json.Unmarshal([]byte(out), &frame) != nil || frame.OK || string(frame.Data) != "null" || diagnostic != "" {
							t.Fatal("removal failure lost common envelope")
						}
					}
				}
				if occupied {
					if err := os.Remove(marker); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				out, diagnostic, err := runCompletionActionFixture(t, "--uninstall-completion", "--output="+mode)
				if err != nil {
					t.Fatal(err)
				}
				assertCompletionAction(t, out, diagnostic, mode, "uninstall", "fish", path, false, false, false)
			})
		}
	}
}

func TestCompletionRemovalPermissionFailurePreservesPriorState(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("fixture needs POSIX permissions enforced for an unprivileged process")
	}
	for _, mode := range []string{"json", "compact", "human"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", dir)
			path := filepath.Join(dir, "fish", "completions", "slick.fish")
			parent := filepath.Dir(path)
			if err := os.MkdirAll(parent, 0o700); err != nil {
				t.Fatal(err)
			}
			prior := []byte("synthetic prior completion\n")
			if err := os.WriteFile(path, prior, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(parent, 0o500); err != nil { //nolint:gosec // Private directory fixture permits reads/traversal but denies deletion.
				t.Fatal(err)
			}
			defer func() { _ = os.Chmod(parent, 0o700) }() //nolint:gosec // Restore traversal/write permission for fixture cleanup.
			_, _, err := runCompletionActionFixture(t, "--uninstall-completion", "--output="+mode)
			typed, ok := errors.AsType[CommandError](err)
			if !ok || typed.CLIError.ExitCode != 5 {
				t.Fatal("permission failure was swallowed or misclassified")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(prior, after) {
				t.Fatal("failed removal changed prior state")
			}
			if err := os.Chmod(parent, 0o700); err != nil { //nolint:gosec // Owner-only fixture directory needs traversal.
				t.Fatal(err)
			}
			out, diagnostic, err := runCompletionActionFixture(t, "--uninstall-completion", "--output="+mode)
			if err != nil {
				t.Fatalf("explicit permission recovery failed: %v", err)
			}
			assertCompletionAction(t, out, diagnostic, mode, "uninstall", "fish", path, true, false, false)
		})
	}
}

func TestCompletionRemovalLostResultDoesNotRetryOrRestoreEntry(t *testing.T) {
	for _, mode := range []string{"json", "compact", "human"} {
		for _, failure := range []string{"error", "short", "partial", "transient"} {
			t.Run(mode+"/"+failure, func(t *testing.T) {
				dir := t.TempDir()
				t.Setenv("XDG_CONFIG_HOME", dir)
				path := filepath.Join(dir, "fish", "completions", "slick.fish")
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("synthetic prior script\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				writer := &completionFailingWriter{mode: failure}
				var diagnostic bytes.Buffer
				root, rt := NewRootCommandWithRuntime(WithConfig(completionProfileConfig()), WithTTY(false), WithIO(strings.NewReader(""), writer, &diagnostic))
				root.SetArgs([]string{"--uninstall-completion", "--@shell=fish", "--output=" + mode, "--color=never"})
				err := root.ExecuteContext(t.Context())
				if rt.CancelTimeout != nil {
					rt.CancelTimeout()
				}
				typed, ok := errors.AsType[CommandError](err)
				if !ok || typed.CLIError.ExitCode != 8 || writer.calls != 1 || strings.Count(diagnostic.String(), "output_write_failed") != 1 || strings.Contains(err.Error(), "synthetic-private") {
					t.Fatal("lost removal result retried output/mutation or lost safe IO8")
				}
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatal("failed result restored or retained an already removed entry")
				}
				out, diag, err := runCompletionActionFixture(t, "--uninstall-completion", "--output="+mode)
				if err != nil {
					t.Fatal(err)
				}
				assertCompletionAction(t, out, diag, mode, "uninstall", "fish", path, false, false, false)
			})
		}
	}
}

func TestCompletionActionsPreserveSelectedWorkspaceMetadata(t *testing.T) {
	for _, tc := range []struct{ name, env, flag, want string }{
		{"saved default", "", "", "Other"}, {"environment", "a.b", "", "a.b"}, {"explicit", "a.b", "DEFAULT", "default"}, {"unknown selector", "", "missing", "missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("SLACK_CLI_DEFAULT_WORKSPACE", tc.env)
			cfg := completionProfileConfig()
			cfg.DefaultWorkspace = "Other"
			before, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"--uninstall-completion", "--@shell=fish", "--output=json", "--color=never"}
			if tc.flag != "" {
				args = append(args, "--workspace="+tc.flag)
			}
			out, diag, err := executeTestRoot(t, cfg, "http://127.0.0.1:1", "", args)
			if err != nil || diag != "" {
				t.Fatal("local completion metadata required credentials or failed")
			}
			var metadata struct {
				Workspace string `json:"workspace"`
				Command   string `json:"command"`
				Timestamp string `json:"timestamp"`
				RequestID string `json:"request_id"`
			}
			var envelope struct {
				OK   bool
				Meta json.RawMessage
			}
			if json.Unmarshal([]byte(out), &envelope) != nil || !envelope.OK || json.Unmarshal(envelope.Meta, &metadata) != nil || metadata.Workspace != tc.want || metadata.Command != "slick" || metadata.RequestID == "" {
				t.Fatal("completion action lost canonical success metadata")
			}
			if _, err := time.Parse(time.RFC3339, metadata.Timestamp); err != nil {
				t.Fatal("completion metadata timestamp is not RFC3339")
			}
			after, err := json.Marshal(cfg)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("runtime workspace selection changed stored settings")
			}
		})
	}
}

func TestCompletionRemovalCancellationPreservesEntry(t *testing.T) {
	for _, mode := range []string{"json", "compact", "human"} {
		for _, dry := range []bool{false, true} {
			t.Run(fmt.Sprint(dry)+"/"+mode, func(t *testing.T) {
				dir := t.TempDir()
				t.Setenv("XDG_CONFIG_HOME", dir)
				path := filepath.Join(dir, "fish", "completions", "slick.fish")
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				prior := []byte("synthetic prior completion\n")
				if err := os.WriteFile(path, prior, 0o600); err != nil {
					t.Fatal(err)
				}
				var out, diagnostic bytes.Buffer
				root, rt := NewRootCommandWithRuntime(WithConfig(completionProfileConfig()), WithTTY(false), WithIO(strings.NewReader(""), &out, &diagnostic))
				args := []string{"--uninstall-completion", "--@shell=fish", "--output=" + mode, "--color=never"}
				if dry {
					args = append(args, "--dry-run")
				}
				root.SetArgs(args)
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				err := root.ExecuteContext(ctx)
				if rt.CancelTimeout != nil {
					rt.CancelTimeout()
				}
				typed, ok := errors.AsType[CommandError](err)
				if !ok || typed.CLIError.ExitCode != 6 {
					t.Fatal("canceled removal did not return cancellation")
				}
				saved, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(prior, saved) {
					t.Fatal("canceled removal changed prior state")
				}
			})
		}
	}
}
