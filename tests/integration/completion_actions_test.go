//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBinaryCompletionActionLifecyclesAndProtocols(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, shell := range []struct{ name, base, path string }{
		{"bash", "data", "bash-completion/completions/slick"}, {"zsh", "data", "zsh/site-functions/_slick"}, {"fish", "config", "fish/completions/slick.fish"}, {"nu", "data", "nushell/vendor/autoload/slick.nu"}, {"pwsh", "config", "powershell/completions/slick.ps1"}, {"elvish", "config", "elvish/lib/completions/slick.elv"},
	} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(shell.name+"/"+mode, func(t *testing.T) {
				dir := t.TempDir()
				configHome, dataHome := filepath.Join(dir, "config"), filepath.Join(dir, "data")
				home := configHome
				if shell.base == "data" {
					home = dataHome
				}
				path := filepath.Join(home, filepath.FromSlash(shell.path))
				cfg := writePipeConfig(t)
				before, err := os.ReadFile(cfg)
				if err != nil {
					t.Fatal(err)
				}
				run := func(args []string) ([]byte, []byte, error) {
					ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
					defer cancel()
					cmd := exec.CommandContext(ctx, binary, args...)
					cmd.Env = append(isolatedRuntimeEnv(t), "SLICK_CONFIG="+cfg, "XDG_CONFIG_HOME="+configHome, "XDG_DATA_HOME="+dataHome)
					var out, diagnostic bytes.Buffer
					cmd.Stdout = &out
					cmd.Stderr = &diagnostic
					err := cmd.Run()
					if ctx.Err() != nil {
						t.Fatal("native completion action timed out")
					}
					return out.Bytes(), diagnostic.Bytes(), err
				}
				public, diagnostic, err := run([]string{"completion", shell.name})
				if err != nil || len(diagnostic) != 0 || len(public) == 0 {
					t.Fatal("public script generation did not execute")
				}
				// Root print and install initialize the same built-in help flag.
				expected, diagnostic, err := run([]string{"--print-completion", "--@shell=" + shell.name, "--output=" + mode, "--color=never"})
				if err != nil || len(diagnostic) != 0 || len(expected) == 0 {
					t.Fatal("root script generation did not execute")
				}
				for _, step := range []struct {
					action              string
					dry, changed, would bool
				}{{"install", true, false, true}, {"install", false, true, false}, {"uninstall", true, false, true}, {"uninstall", false, true, false}, {"uninstall", false, false, false}} {
					args := []string{"--" + step.action + "-completion", "--@shell=" + shell.name, "--output=" + mode, "--color=never"}
					if step.dry {
						args = append(args, "--dry-run")
					}
					out, diagnostic, err := run(args)
					if err != nil || len(diagnostic) != 0 {
						t.Fatalf("native completion action failed: %v", err)
					}
					if mode == "human" {
						if len(out) == 0 || out[0] == '{' || !bytes.Contains(out, []byte(filepath.Base(path))) {
							t.Fatal("native human completion status lost text/path")
						}
					} else {
						raw := json.RawMessage(out)
						if mode == "json" {
							var frame struct {
								OK     bool
								Meta   struct{ Command string }
								Data   json.RawMessage
								Errors []json.RawMessage
							}
							var fields map[string]json.RawMessage
							if json.Unmarshal(raw, &fields) != nil || len(fields) != 4 || json.Unmarshal(raw, &frame) != nil || !frame.OK || frame.Meta.Command != "slick" || frame.Errors == nil || len(frame.Errors) != 0 {
								t.Fatal("native completion action lost common envelope")
							}
							raw = frame.Data
						}
						var data struct {
							Action, Shell, Path string
							Changed             bool
							DryRun              bool `json:"dry_run"`
							WouldChange         bool `json:"would_change"`
						}
						var fields map[string]json.RawMessage
						if json.Unmarshal(raw, &fields) != nil || len(fields) != 6 || json.Unmarshal(raw, &data) != nil || data.Action != step.action || data.Shell != shell.name || data.Path != path || data.Changed != step.changed || data.DryRun != step.dry || data.WouldChange != step.would {
							t.Fatal("native completion action returned incorrect state fields")
						}
					}
					if step.action == "install" && step.dry {
						if _, err := os.Stat(configHome); !os.IsNotExist(err) {
							t.Fatal("native dry run created config directories")
						}
						if _, err := os.Stat(dataHome); !os.IsNotExist(err) {
							t.Fatal("native dry run created data directories")
						}
					}
					if step.action == "install" && !step.dry || step.action == "uninstall" && step.dry {
						saved, err := os.ReadFile(path)
						if err != nil || !bytes.Equal(saved, expected) {
							i := 0
							for i < len(saved) && i < len(expected) && saved[i] == expected[i] {
								i++
							}
							t.Fatalf("native installed bytes differ: read=%v byte=%d got=%q want=%q", err, i, saved[i:min(i+180, len(saved))], expected[i:min(i+180, len(expected))])
						}
					}
					if step.action == "uninstall" && !step.dry {
						if _, err := os.Lstat(path); !os.IsNotExist(err) {
							t.Fatal("native uninstall retained completion entry")
						}
					}
					saved, err := os.ReadFile(cfg)
					if err != nil || !bytes.Equal(before, saved) {
						t.Fatal("completion action changed slick configuration")
					}
					if strings.Contains(string(out), "xoxb-test") {
						t.Fatal("completion result exposed fixture credential")
					}
				}
			})
		}
	}
}

func TestBinaryInvalidCompletionShellDoesNotExposeArgumentOrMutate(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, action := range []string{"install", "uninstall", "print"} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(action+"/"+mode, func(t *testing.T) {
				dir := t.TempDir()
				home := filepath.Join(dir, "new-config")
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				const private = "xoxp-synthetic-private-completion-shell"
				cmd := exec.CommandContext(ctx, binary, "--"+action+"-completion", "--@shell="+private, "--output="+mode, "--color=never")
				cmd.Env = append(isolatedRuntimeEnv(t), "SLICK_CONFIG="+writePipeConfig(t), "XDG_CONFIG_HOME="+home)
				var out, diagnostic bytes.Buffer
				cmd.Stdout = &out
				cmd.Stderr = &diagnostic
				err := cmd.Run()
				if ctx.Err() != nil || exitCode(err) != 4 || strings.Contains(out.String()+diagnostic.String(), private) || !strings.Contains(out.String()+diagnostic.String(), "unsupported shell") {
					t.Fatal("invalid completion shell lost safe validation failure")
				}
				if _, err := os.Stat(home); !os.IsNotExist(err) {
					t.Fatal("invalid shell created resources")
				}
				if mode == "human" {
					if out.Len() != 0 || diagnostic.Len() == 0 {
						t.Fatal("human shell validation used wrong stream")
					}
				} else {
					var frame struct {
						OK   bool
						Data json.RawMessage
					}
					if json.Unmarshal(out.Bytes(), &frame) != nil || frame.OK || string(frame.Data) != "null" || diagnostic.Len() != 0 {
						t.Fatal("machine shell validation lost common failure envelope")
					}
				}
			})
		}
	}
}
