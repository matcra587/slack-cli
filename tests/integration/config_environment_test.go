//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/matcra587/slack-cli/internal/config"
)

func runConfigEnvironment(t *testing.T, binary, path, mode, override string, args ...string) (string, string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	argv := append([]string{"--output=" + mode, "--color=never"}, args...)
	cmd := exec.CommandContext(ctx, binary, argv...)
	cmd.Env = append(isolatedRuntimeEnv(t), "SLICK_CONFIG="+path, "SLACK_CLI_DEFAULT_WORKSPACE="+override)
	var out, diagnostic bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &diagnostic
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatal("fixture command timed out")
	}
	return out.String(), diagnostic.String(), err
}

func TestBinaryConfigEditsNeverPersistEnvironmentSelection(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, override := range []string{"Other", "Missing"} {
		for _, mode := range []string{"json", "compact", "human"} {
			for _, action := range []string{"get", "list", "set", "unset", "default", "invalid"} {
				t.Run(override+"/"+mode+"/"+action, func(t *testing.T) {
					path, prior := defaultSelectorFixture(t)
					before, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					args := []string{"config"}
					switch action {
					case "get":
						args = append(args, "get", "default_workspace")
					case "list":
						args = append(args, "list")
					case "set":
						args = append(args, "set", "workspaces.Primary.default_channel", "C_NEW")
					case "unset":
						args = append(args, "unset", "workspaces.Other.default_channel")
					case "default":
						args = append(args, "set", "default_workspace", "oThEr")
					case "invalid":
						args = append(args, "set", "default_workspace", "Unknown")
					}
					out, diagnostic, err := runConfigEnvironment(t, binary, path, mode, override, args...)
					if action == "invalid" {
						if exitCode(err) != 4 {
							t.Fatalf("invalid edit did not fail validation: %v", err)
						}
						if mode == "human" {
							if out != "" || diagnostic == "" {
								t.Fatal("wrong human error streams")
							}
						} else {
							var frame struct {
								OK   bool
								Data json.RawMessage
								Meta struct {
									Command string
									Exit    int `json:"exit_code"`
								}
							}
							if json.Unmarshal([]byte(out), &frame) != nil || frame.OK || string(frame.Data) != "null" || frame.Meta.Command != "config.set" || frame.Meta.Exit != 4 || diagnostic != "" {
								t.Fatal("wrong machine error envelope")
							}
						}
					} else if err != nil || diagnostic != "" {
						t.Fatalf("environment blocked local config operation: %v", err)
					}
					if action == "get" && !strings.Contains(out, "Primary") {
						t.Fatal("config get returned runtime override instead of saved default")
					}
					if action == "list" && (!strings.Contains(out, "default_workspace") || !strings.Contains(out, "Primary")) {
						t.Fatal("config list lost stored default")
					}
					wantDefault := "Primary"
					switch action {
					case "set":
						p := prior.Workspaces["Primary"]
						p.DefaultChannel = "C_NEW"
						prior.Workspaces["Primary"] = p
					case "unset":
						p := prior.Workspaces["Other"]
						p.DefaultChannel = ""
						prior.Workspaces["Other"] = p
					case "default":
						wantDefault = "Other"
					}
					loaded, err := config.LoadFile(path)
					if err != nil || loaded.DefaultWorkspace != wantDefault || !reflect.DeepEqual(loaded.Workspaces, prior.Workspaces) {
						t.Fatalf("edit persisted override or lost unrelated settings: %v", err)
					}
					if action == "get" || action == "list" || action == "invalid" {
						after, err := os.ReadFile(path)
						if err != nil || !bytes.Equal(after, before) {
							t.Fatal("read or rejected edit modified file")
						}
					}
					out, diagnostic, err = runConfigEnvironment(t, binary, path, mode, "", "config", "get", "default_workspace")
					if err != nil || diagnostic != "" || !strings.Contains(out, wantDefault) {
						t.Fatal("saved default was not preserved for the next process")
					}
				})
			}
		}
	}
}

func TestBinaryWorkspaceEnvironmentPrecedence(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, mode := range []string{"json", "compact", "human"} {
		for _, tc := range []struct {
			name, override, flag, want string
			exit                       int
		}{
			{"stored", "", "", "Primary", 0},
			{"environment", "oThEr", "", "Other", 0},
			{"explicit wins", "Other", "Primary", "Primary", 0},
			{"explicit recovers", "Missing", "Primary", "Primary", 0},
			{"unknown environment", "Missing", "", "Missing", 4},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				path, _ := defaultSelectorFixture(t)
				before, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				args := []string{"workspace", "list"}
				if tc.flag != "" {
					args = append(args, "--workspace="+tc.flag)
				}
				out, diagnostic, err := runConfigEnvironment(t, binary, path, mode, tc.override, args...)
				if exitCode(err) != tc.exit {
					t.Fatalf("selection exit=%d want=%d", exitCode(err), tc.exit)
				}
				if tc.exit == 0 && diagnostic != "" {
					t.Fatal("successful selection wrote diagnostics")
				}
				if mode == "json" || (tc.exit != 0 && mode == "compact") {
					var frame struct {
						OK   bool
						Meta struct{ Workspace string }
					}
					if json.Unmarshal([]byte(out), &frame) != nil || frame.OK != (tc.exit == 0) || frame.Meta.Workspace != tc.want {
						t.Fatalf("selection metadata does not identify %s", tc.want)
					}
				}
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(after, before) {
					t.Fatal("selection changed stored preferences")
				}
			})
		}
	}
}
