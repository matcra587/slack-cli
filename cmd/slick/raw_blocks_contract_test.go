package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	"github.com/matcra587/slack-cli/internal/config"
)

func TestRawBlockInputRejectsNonArraysAndEmptyNewMessagesBeforeCredentials(t *testing.T) {
	t.Setenv("SLICK_CONFIG", filepath.Join(t.TempDir(), "unused.toml"))
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	file := filepath.Join(t.TempDir(), "artifact.txt")
	if err := os.WriteFile(file, []byte("artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, route := range []struct {
		name    string
		args    []string
		emptyOK bool
	}{
		{"send", []string{"message", "send", "--channel=C123"}, false},
		{"scheduled", []string{"message", "send", "--channel=C123", "--schedule=90m"}, false},
		{"edit", []string{"message", "edit", "--channel=C123", "--timestamp=1746284582.123456"}, true},
		{"reply", []string{"reply", "--channel=C123", "--parent=1746284500.000001"}, false},
		{"upload", []string{"file", "upload", "--channel=C123", "--file=" + file}, true},
	} {
		for _, payload := range []string{`{}`, `null`, `"synthetic-unreviewed-input"`, `[null]`, `[{"type":"context","elements":[{"type":"synthetic-unreviewed-input"}]}]`, " ", "", `[]`} {
			if payload == `[]` && route.emptyOK {
				continue
			}
			for _, mode := range []string{"json", "compact", "human"} {
				// Every route/payload pair keeps precredential and state guards in JSON.
				// Send covers each payload's failure streams and privacy in other modes.
				if mode != "json" && route.name != "send" {
					continue
				}
				t.Run(route.name+"/"+payload+"/"+mode, func(t *testing.T) {
					var out, diagnostic bytes.Buffer
					var resolutions atomic.Int32
					cfg := workspaceConfig(config.TokenTypeUser)
					before, err := json.Marshal(cfg)
					if err != nil {
						t.Fatal(err)
					}
					root := NewRootCommand(WithConfig(cfg), WithIO(strings.NewReader(""), &out, &diagnostic), WithTTY(false), WithSlackBaseURL("http://127.0.0.1:1"), WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
						resolutions.Add(1)
						return "", errors.New("synthetic-private-provider")
					})))
					args := append(append([]string{}, route.args...), "--blocks", "--message="+payload, "--no-attribution", "--output="+mode, "--no-throttle")
					root.SetArgs(args)
					err = root.Execute()
					commandErr, ok := errors.AsType[clioutput.CommandError](err)
					if !ok || commandErr.CLIError.ExitCode != 4 || resolutions.Load() != 0 {
						t.Fatalf("invalid raw input reached credentials or changed exit: err=%v resolutions=%d", err, resolutions.Load())
					}
					if mode == "human" {
						if out.Len() != 0 || diagnostic.Len() == 0 {
							t.Fatal("human validation used wrong stream")
						}
					} else {
						var frame map[string]json.RawMessage
						if err := json.Unmarshal(out.Bytes(), &frame); err != nil {
							t.Fatal(err)
						}
						if len(frame) != 4 || string(frame["ok"]) != "false" || string(frame["data"]) != "null" || diagnostic.Len() != 0 {
							t.Fatal("raw input failure changed machine envelope")
						}
					}
					if strings.Contains(out.String()+diagnostic.String(), "synthetic-unreviewed-input") || strings.Contains(out.String()+diagnostic.String(), "synthetic-private-provider") {
						t.Fatal("raw input/provider data entered diagnostics")
					}
					after, err := json.Marshal(cfg)
					if err != nil || !bytes.Equal(before, after) {
						t.Fatal("invalid raw input changed workspace settings")
					}
				})
			}
		}
	}
}

func TestRawEmptyBlockEditPreviewShowsClearWithoutReplacingText(t *testing.T) {
	t.Setenv("SLICK_CONFIG", filepath.Join(t.TempDir(), "unused.toml"))
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, mode := range []string{"json", "compact"} {
		t.Run(mode, func(t *testing.T) {
			var out, diagnostic bytes.Buffer
			var resolutions atomic.Int32
			root := NewRootCommand(WithConfig(workspaceConfig(config.TokenTypeUser)), WithIO(strings.NewReader(""), &out, &diagnostic), WithTTY(false), WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
				resolutions.Add(1)
				return "", errors.New("unexpected credential access")
			})))
			root.SetArgs([]string{"message", "edit", "--channel=C123", "--timestamp=1746284582.123456", "--blocks", "--message=[]", "--no-attribution", "--dry-run", "--output=" + mode})
			if err := root.Execute(); err != nil || diagnostic.Len() != 0 || resolutions.Load() != 0 {
				t.Fatalf("raw empty preview failed or resolved credentials: %v", err)
			}
			data := out.Bytes()
			if mode == "json" {
				var frame struct {
					Data json.RawMessage `json:"data"`
				}
				if err := json.Unmarshal(data, &frame); err != nil {
					t.Fatal(err)
				}
				data = frame.Data
			}
			var result struct {
				DryRun  bool                       `json:"dry_run"`
				Message map[string]json.RawMessage `json:"message"`
			}
			if err := json.Unmarshal(data, &result); err != nil {
				t.Fatal(err)
			}
			if !result.DryRun || string(result.Message["blocks"]) != "[]" {
				t.Fatal("preview lost explicit clear intent")
			}
			if _, exists := result.Message["text"]; exists {
				t.Fatal("preview invented replacement text")
			}
		})
	}
}
