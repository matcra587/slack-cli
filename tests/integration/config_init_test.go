//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func TestBinaryConfigInitRejectsArgumentsBeforeWriting(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"positional", []string{"unexpected"}},
		{"long force", []string{"--force", "unexpected"}},
		{"short force", []string{"-F", "unexpected"}},
		{"literal argument", []string{"--force", "--", "--profile=other"}},
	} {
		for _, existing := range []bool{false, true} {
			state := "missing"
			if existing {
				state = "existing"
			}
			for _, mode := range []string{"json", "compact", "human"} {
				t.Run(tc.name+"/"+state+"/"+mode, func(t *testing.T) {
					dir := t.TempDir()
					path := filepath.Join(dir, "config.toml")
					prior := []byte("schema_version=\"1\"\ndefault_workspace=\"keep\"\n[workspaces.keep]\nname=\"keep\"\ndefault_channel=\"C_KEEP\"\n[workspaces.keep.aliases]\nalerts=\"C_ALERTS\"\n")
					if existing {
						if err := os.WriteFile(path, prior, 0o600); err != nil {
							t.Fatal(err)
						}
					}
					args := append([]string{"--output=" + mode, "--color=never", "config", "init"}, tc.args...)
					out, diagnostic, err := runSlackBinary(t, binary, path, "http://127.0.0.1:1", "", args...)
					if exitCode(err) != 4 {
						t.Fatalf("unexpected init argument must fail validation before writing: exit=%d", exitCode(err))
					}
					if mode == "human" {
						if out != "" || !strings.Contains(diagnostic, "unknown command") {
							t.Fatal("argument error lost human diagnostic or used wrong stream")
						}
					} else {
						var frame struct {
							OK   bool            `json:"ok"`
							Data json.RawMessage `json:"data"`
							Meta struct {
								Command string `json:"command"`
								Exit    int    `json:"exit_code"`
							} `json:"meta"`
						}
						if err := json.Unmarshal([]byte(out), &frame); err != nil || frame.OK || string(frame.Data) != "null" || frame.Meta.Command != "config.init" || frame.Meta.Exit != 4 || diagnostic != "" || !strings.Contains(out, "validation_error") {
							t.Fatal("argument error lost machine failure contract")
						}
					}
					after, err := os.ReadFile(path)
					if existing {
						if err != nil || !bytes.Equal(prior, after) {
							t.Fatal("rejected init changed existing configuration")
						}
					} else if !os.IsNotExist(err) {
						t.Fatal("rejected init created a configuration")
					}
					entries, err := os.ReadDir(dir)
					want := 0
					if existing {
						want = 1
					}
					if err != nil || len(entries) != want {
						t.Fatal("rejected init left unexpected files")
					}
				})
			}
		}
	}
}

func TestBinaryConfigInitFlagsAndOverwriteLifecycle(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tc := range []struct {
		name, profile string
		args          []string
	}{
		{"defaults", "default", nil},
		{"long flags", "Staging", []string{"--profile= Staging ", "--default-channel= C_TEST ", "--attribution-enabled=false", "--attribution-label= Review ", "--attribution-emoji= :eyes: ", "--attribution-message= Ready ", "--force=false"}},
		{"short flags", "Staging", []string{"-p", " Staging ", "-c", " C_TEST ", "-A=false", "-l", " Review ", "-e", " :eyes: ", "-m", " Ready ", "-F=false"}},
	} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "new", "config.toml")
				args := append([]string{"--output=" + mode, "--color=never", "config", "init"}, tc.args...)
				out, diagnostic, err := runSlackBinary(t, binary, path, "http://127.0.0.1:1", "", args...)
				if err != nil || diagnostic != "" || out == "" {
					t.Fatalf("valid initialization failed: %v", err)
				}
				cfg, err := config.LoadFile(path)
				if err != nil || cfg.DefaultWorkspace != tc.profile || len(cfg.Workspaces) != 1 {
					t.Fatalf("initialized unexpected profiles: %v", err)
				}
				profile := cfg.Workspaces[tc.profile]
				if profile.TokenRef != "" || profile.TokenType != "" || profile.TeamID != "" {
					t.Fatal("preferences initialization persisted authentication fields")
				}
				if profile.Attribution.Enabled == nil {
					t.Fatal("initialization omitted attribution default")
				}
				if tc.name == "defaults" {
					if !*profile.Attribution.Enabled || profile.DefaultChannel != "" || profile.Attribution.Label != "" {
						t.Fatal("default preferences changed")
					}
				} else if *profile.Attribution.Enabled || profile.DefaultChannel != "C_TEST" || profile.Attribution.Label != "Review" || profile.Attribution.Emoji != ":eyes:" || profile.Attribution.Message != "Ready" {
					t.Fatal("initialization ignored or failed to trim a preference flag")
				}
				prior, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if runtime.GOOS != "windows" {
					info, err := os.Stat(path)
					if err != nil || info.Mode().Perm() != 0o600 {
						t.Fatal("new configuration is not private")
					}
				}
				_, _, err = runSlackBinary(t, binary, path, "http://127.0.0.1:1", "", args...)
				if exitCode(err) != 4 {
					t.Fatal("initialization overwrote existing configuration without force")
				}
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(prior, after) {
					t.Fatal("refused overwrite changed previous configuration")
				}
				force := "--force"
				if tc.name == "short flags" {
					force = "-F"
				}
				_, _, err = runSlackBinary(t, binary, path, "http://127.0.0.1:1", "", "--output="+mode, "--color=never", "config", "init", force)
				if err != nil {
					t.Fatalf("explicit overwrite failed: %v", err)
				}
				cfg, err = config.LoadFile(path)
				if err != nil || cfg.DefaultWorkspace != "default" || len(cfg.Workspaces) != 1 {
					t.Fatalf("explicit overwrite retained previous profile: %v", err)
				}
				profile = cfg.Workspaces["default"]
				if profile.DefaultChannel != "" || profile.Attribution.Label != "" || profile.Attribution.Emoji != "" || profile.Attribution.Message != "" || profile.Attribution.Enabled == nil || !*profile.Attribution.Enabled {
					t.Fatal("omitted preferences survived explicit reset")
				}
			})
		}
	}
}
