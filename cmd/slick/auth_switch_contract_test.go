package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	"github.com/matcra587/slack-cli/internal/config"
)

func TestAuthSwitchPreservesStateAndUsesCanonicalTarget(t *testing.T) {
	for _, tc := range []struct {
		name, target, mode, flag string
		exit                     int
	}{
		{"canonical", " OTHER ", "json", "", 0},
		{"preview long", "other", "json", "--dry-run", 0},
		{"preview short", "other", "compact", "-n", 0},
		{"preview human", "other", "human", "--dry-run", 0},
		{"explicit false", "other", "compact", "--dry-run=false", 0},
		{"removed flag", "other", "human", "", 0},
		{"missing", "absent", "json", "", 4},
		{"save failure", "other", "json", "", 5},
		{"canceled", "other", "json", "", 6},
		{"deadline", "other", "compact", "", 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := authTestConfig()
			profile := cfg.Workspaces["other"]
			profile.Aliases = map[string]string{"release": "C_KEEP"}
			cfg.Workspaces["other"] = profile
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := config.SaveFile(path, cfg); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			prior, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			savePath := path
			if tc.name == "save failure" {
				savePath = filepath.Join(t.TempDir(), "occupied")
				if err := os.Mkdir(savePath, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			ctx := t.Context()
			switch tc.name {
			case "canceled":
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			case "deadline":
				expired, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Minute))
				defer cancel()
				ctx = expired
			}
			var out, diagnostic bytes.Buffer
			root := NewRootCommand(WithConfig(cfg), WithConfigPath(savePath), WithCredentialStore(config.NewMemoryCredentialStore()), WithTTY(false), WithIO(strings.NewReader(""), &out, &diagnostic), WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
				t.Error("local profile switch accessed credentials")
				return "", config.ErrCredentialNotFound
			})), func(runtime *RootRuntime) {
				runtime.HTTPClient = &http.Client{Transport: authStatusTransport(func(*http.Request) (*http.Response, error) {
					t.Error("local profile switch called Slack")
					return nil, errors.New("synthetic unexpected request")
				})}
			})
			args := []string{"auth", "switch", tc.target, "--output=" + tc.mode, "--color=never"}
			if tc.flag != "" {
				args = append(args, tc.flag)
			}
			root.SetArgs(args)
			err = root.ExecuteContext(ctx)
			gotExit := 0
			if failure, ok := errors.AsType[clioutput.CommandError](err); ok {
				gotExit = failure.CLIError.ExitCode
			} else if err != nil {
				t.Fatalf("switch failed before its handler: %v", err)
			}
			if gotExit != tc.exit {
				t.Fatalf("switch exit=%d, want %d", gotExit, tc.exit)
			}
			preview := strings.HasPrefix(tc.name, "preview")
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if tc.exit != 0 || preview {
				current, err := json.Marshal(cfg)
				if err != nil || !bytes.Equal(prior, current) || !bytes.Equal(before, after) {
					t.Error("failed or preview switch changed prior state")
				}
			} else {
				loaded, err := config.LoadFile(path)
				if err != nil || loaded.DefaultWorkspace != "other" || cfg.DefaultWorkspace != "other" || loaded.Workspaces["other"].Aliases["release"] != "C_KEEP" || len(loaded.Workspaces) != 2 {
					t.Error("switch did not persist the canonical target and preserve settings")
				}
			}
			if tc.mode == "human" {
				if out.Len() == 0 || diagnostic.Len() != 0 || (preview && !strings.Contains(out.String(), "dry_run=true")) {
					t.Error("human switch lost preview or streams")
				}
			} else {
				var frame map[string]any
				if json.Unmarshal(out.Bytes(), &frame) != nil || diagnostic.Len() != 0 {
					t.Fatal("switch lost machine JSON or streams")
				}
				data := frame
				if tc.mode == "json" || tc.exit != 0 {
					if frame["ok"] != (tc.exit == 0) || frame["meta"].(map[string]any)["command"] != "auth.switch" || frame["meta"].(map[string]any)["exit_code"] != float64(tc.exit) {
						t.Error("switch lost envelope metadata")
					}
					if tc.exit == 0 {
						if frame["meta"].(map[string]any)["workspace"] != "other" {
							t.Error("switch metadata names the previous workspace")
						}
						data = frame["data"].(map[string]any)
					} else if frame["data"] != nil {
						t.Error("failed switch published success data")
					}
				}
				if tc.exit == 0 && (data["workspace"] != "other" || (preview && data["dry_run"] != true) || (!preview && data["dry_run"] != nil)) {
					t.Error("switch lost canonical data or preview state")
				}
			}
			if tc.name == "save failure" {
				_, _, err := executeAuthRoot(t, cfg, path, config.NewMemoryCredentialStore(), "http://127.0.0.1:1", []string{"auth", "switch", "other", "--output=json"})
				if err != nil || cfg.DefaultWorkspace != "other" {
					t.Error("explicit switch recovery failed")
				}
			}
		})
	}
}
