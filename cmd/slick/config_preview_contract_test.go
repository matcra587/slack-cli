package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	"github.com/matcra587/slack-cli/internal/config"
)

func TestConfigMutationPreviewsPreserveFilesAndMemory(t *testing.T) {
	for _, tc := range []struct {
		name, mode                    string
		args                          []string
		exists, tty, applies, blocked bool
		exit                          int
	}{
		{name: "new init", mode: "json", args: []string{"init", "--profile=Preview", "--dry-run"}},
		{name: "replace init", mode: "compact", args: []string{"init", "--profile=Preview", "--force", "-n"}, exists: true},
		{name: "tty init without prompts", mode: "json", args: []string{"init", "--dry-run"}, tty: true},
		{name: "init preserves overwrite guard", mode: "json", args: []string{"init", "--dry-run"}, exists: true, exit: 4},
		{name: "set channel", mode: "human", args: []string{"set", "workspaces.other.default_channel", "C_NEW", "-n"}, exists: true},
		{name: "unset attribution", mode: "json", args: []string{"unset", "workspaces.other.attribution.enabled", "--dry-run"}, exists: true},
		{name: "canonical default", mode: "compact", args: []string{"set", "default_workspace", " OTHER ", "--dry-run"}, exists: true},
		{name: "invalid boolean", mode: "json", args: []string{"set", "workspaces.other.attribution.enabled", "invalid", "--dry-run"}, exists: true, exit: 4},
		{name: "invalid init text", mode: "json", args: []string{"init", "--attribution-label=invalid\xff", "--dry-run"}, exit: 4},
		{name: "explicit false applies", mode: "json", args: []string{"set", "workspaces.other.default_channel", "C_NEW", "--dry-run=false"}, exists: true, applies: true},
		{name: "removed flag applies", mode: "compact", args: []string{"unset", "workspaces.other.attribution.enabled"}, exists: true, applies: true},
		{name: "failed save preserves memory", mode: "json", args: []string{"set", "workspaces.other.default_channel", "C_NEW"}, exists: true, blocked: true, exit: 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := authTestConfig()
			profile := cfg.Workspaces["other"]
			enabled := true
			profile.Attribution.Enabled = &enabled
			profile.DefaultChannel = "C_KEEP"
			profile.Aliases = map[string]string{"alerts": "C_UNRELATED"}
			cfg.Workspaces["other"] = profile
			path := filepath.Join(t.TempDir(), "nested", "config.toml")
			if tc.exists {
				if err := config.SaveFile(path, cfg); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := os.ReadFile(path)
			prior, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			savePath := path
			if tc.blocked {
				savePath = t.TempDir()
			}
			var out, diagnostic bytes.Buffer
			resolutions := 0
			root, runtime := NewRootCommandWithRuntime(WithConfig(cfg), WithConfigPath(savePath), WithTTY(tc.tty), WithIO(strings.NewReader(""), &out, &diagnostic),
				WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
					resolutions++
					return "", errors.New("synthetic-provider-must-not-run")
				})))
			args := append([]string{"config"}, tc.args...)
			root.SetArgs(append(args, "--output="+tc.mode, "--color=never"))
			runErr := root.ExecuteContext(t.Context())
			if runtime.CancelTimeout != nil {
				runtime.CancelTimeout()
			}
			gotExit := 0
			if failure, ok := errors.AsType[clioutput.CommandError](runErr); ok {
				gotExit = failure.CLIError.ExitCode
			} else if runErr != nil {
				t.Fatal("config command returned an unstructured failure")
			}
			if gotExit != tc.exit || resolutions != 0 {
				t.Fatalf("config exit/provider calls = %d/%d, want %d/0", gotExit, resolutions, tc.exit)
			}
			after, readErr := os.ReadFile(path)
			current, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.applies {
				if !bytes.Equal(before, after) || !bytes.Equal(prior, current) {
					t.Error("preview or failure changed stored or in-memory config")
				}
				if !tc.exists && !errors.Is(readErr, os.ErrNotExist) {
					t.Error("preview created a config file")
				}
				if !tc.exists {
					if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
						t.Error("preview created the config directory")
					}
				}
			} else if bytes.Equal(before, after) || bytes.Equal(prior, current) {
				t.Error("false/removed preview flag did not apply the actual mutation")
			}
			if tc.mode == "human" {
				if diagnostic.Len() != 0 || !strings.Contains(out.String(), "dry_run=true") {
					t.Error("human preview lost dry-run marker or streams")
				}
			} else {
				var frame map[string]any
				if json.Unmarshal(out.Bytes(), &frame) != nil || diagnostic.Len() != 0 {
					t.Fatal("config result is not clean machine JSON")
				}
				data := frame
				if tc.mode == "json" || tc.exit != 0 {
					if frame["ok"] != (tc.exit == 0) {
						t.Fatal("config result lost envelope status")
					}
					if tc.exit == 0 {
						data = frame["data"].(map[string]any)
					}
				}
				if tc.exit == 0 {
					if tc.applies && data["dry_run"] != nil || !tc.applies && data["dry_run"] != true {
						t.Error("config result lost dry-run state")
					}
					if tc.name == "canonical default" && data["value"] != "other" {
						t.Error("preview did not resolve canonical saved default")
					}
				}
			}
		})
	}
}
