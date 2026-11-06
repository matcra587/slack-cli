package agent_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	cliagent "github.com/matcra587/slack-cli/internal/cli/agent"
	"github.com/matcra587/slack-cli/internal/cli/clitheme"
	"github.com/matcra587/slack-cli/internal/cli/runtime/runtimetest"
)

func TestAgentExportDryRunPreservesFiles(t *testing.T) {
	for _, mode := range []string{"json", "compact", "human"} {
		t.Run(mode, func(t *testing.T) {
			t.Chdir(t.TempDir())
			target := filepath.Join(t.TempDir(), "skills")
			stdout := executeIsolatedExport(t, []string{"agent", "export", "--dir", target, "--format=agent-skill", "--dry-run", "--output=" + mode})
			if _, err := os.Stat(target); !os.IsNotExist(err) {
				t.Fatalf("dry run created target: %v", err)
			}
			if mode == "human" {
				if !strings.Contains(stdout, "dry run") {
					t.Fatalf("preview not marked: %s", stdout)
				}
			} else {
				var result struct {
					DryRun bool     `json:"dry_run"`
					Paths  []string `json:"paths"`
				}
				if mode == "json" {
					var env struct {
						Data json.RawMessage `json:"data"`
					}
					if err := json.Unmarshal([]byte(stdout), &env); err != nil {
						t.Fatal(err)
					}
					stdout = string(env.Data)
				}
				if err := json.Unmarshal([]byte(stdout), &result); err != nil {
					t.Fatal(err)
				}
				if !result.DryRun || !slices.Equal(result.Paths, expectedSlickExportPaths()) {
					t.Fatalf("incorrect preview: %s", stdout)
				}
			}
			if err := os.MkdirAll(filepath.Join(target, "slick-auth-setup"), 0o700); err != nil {
				t.Fatal(err)
			}
			prior := filepath.Join(target, "slick-auth-setup", "SKILL.md")
			if err := os.WriteFile(prior, []byte("prior state"), 0o600); err != nil {
				t.Fatal(err)
			}
			executeIsolatedExport(t, []string{"agent", "export", "--dir", target, "--format=agent-skill", "--dry-run", "--output=" + mode})
			content, err := os.ReadFile(prior)
			if err != nil || string(content) != "prior state" {
				t.Fatalf("preview changed prior state: %q %v", content, err)
			}
		})
	}
}

func TestAgentExportExpandsDirectory(t *testing.T) {
	for _, raw := range []string{"~/skills", "$SLICK_EXPORT_TARGET"} {
		t.Run(raw, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", home)
			t.Chdir(t.TempDir())
			target := filepath.Join(home, "skills")
			t.Setenv("SLICK_EXPORT_TARGET", target)
			for _, mode := range []string{"json", "compact", "human"} {
				executeIsolatedExport(t, []string{"agent", "export", "--dir", raw, "--format=agent-skill", "--output=" + mode})
				for _, rel := range expectedSlickExportPaths() {
					if _, err := os.Stat(filepath.Join(target, rel)); err != nil {
						t.Fatalf("expanded artifact missing: %v", err)
					}
				}
			}
		})
	}
}

func TestAgentExportPartialFailureAndRecovery(t *testing.T) {
	for _, mode := range []string{"json", "compact", "human"} {
		t.Run(mode, func(t *testing.T) {
			t.Chdir(t.TempDir())
			target := t.TempDir()
			blocker := filepath.Join(target, "slick-cache-metadata")
			if err := os.WriteFile(blocker, []byte("unrelated state"), 0o600); err != nil {
				t.Fatal(err)
			}
			runtime, stdout, stderr := runtimetest.NewRuntime(t, runtimetest.Options{Token: "xoxb-synthetic-export-fixture", Theme: clitheme.Default()})
			root := runtimetest.NewRoot(runtime, stdout, stderr)
			root.AddCommand(cliagent.NewCommand(runtime))
			out, diagnostic, err := runtimetest.Run(t, root, []string{"agent", "export", "--dir", target, "--format=agent-skill", "--output=" + mode}, stdout, stderr)
			if err == nil {
				t.Fatal("partial write unexpectedly succeeded")
			}
			if mode != "human" {
				if diagnostic != "" {
					t.Fatalf("machine stderr: %s", diagnostic)
				}
				var env struct {
					Errors []struct {
						ExitCode int `json:"exit_code"`
						Details  struct {
							Completed []string `json:"completed_paths"`
						} `json:"details"`
					} `json:"errors"`
				}
				if err := json.Unmarshal([]byte(out), &env); err != nil {
					t.Fatal(err)
				}
				if len(env.Errors) != 1 || env.Errors[0].ExitCode != 5 || !slices.Equal(env.Errors[0].Details.Completed, []string{"slick-auth-setup/SKILL.md"}) {
					t.Fatalf("partial progress lost or misclassified: %s", out)
				}
			}
			content, err := os.ReadFile(blocker)
			if err != nil || string(content) != "unrelated state" {
				t.Fatalf("blocker changed: %q %v", content, err)
			}
			if _, err := os.Stat(filepath.Join(target, "slick-auth-setup", "SKILL.md")); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(blocker); err != nil {
				t.Fatal(err)
			}
			executeIsolatedExport(t, []string{"agent", "export", "--dir", target, "--format=agent-skill", "--output=" + mode})
			for _, rel := range expectedSlickExportPaths() {
				if _, err := os.Stat(filepath.Join(target, rel)); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
