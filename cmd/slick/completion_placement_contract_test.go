package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func TestCompletionActionsCannotRunMutatingSubcommands(t *testing.T) {
	file := filepath.Join(t.TempDir(), "fixture.txt")
	if err := os.WriteFile(file, []byte("Release notes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commands := [][]string{
		{"status", "set", "--text=PR review"},
		{"status", "clear"},
		{"message", "send", "--channel=C123", "--message=PR review", "--no-attribution"},
		{"message", "send", "--channel=C123", "--message=PR review", "--schedule=1h", "--no-attribution"},
		{"message", "edit", "--channel=C123", "--timestamp=1746284582.000001", "--message=PR review", "--no-attribution"},
		{"message", "delete", "--channel=C123", "--timestamp=1746284582.000001", "--force"},
		{"message", "scheduled", "delete", "--channel=C123", "--scheduled-id=Q1"},
		{"reply", "--channel=C123", "--parent=1746284582.000001", "--message=PR review", "--no-attribution"},
		{"react", "add", "--channel=C123", "--timestamp=1746284582.000001", "--emoji=eyes"},
		{"react", "remove", "--channel=C123", "--timestamp=1746284582.000001", "--emoji=eyes"},
		{"file", "upload", "--channel=C123", "--file=" + file, "--no-attribution"},
	}
	for _, args := range commands {
		for _, flag := range []string{"--print-completion", "--install-completion", "--uninstall-completion", "--@complete=workspace"} {
			for _, mode := range []string{"json", "compact", "human"} {
				// Every mutation route keeps every active completion action. Status set
				// covers the shared placement-error protocol in the other output modes.
				if mode != "json" && (args[0] != "status" || args[1] != "set") {
					continue
				}
				t.Run(args[0]+"/"+args[1]+"/"+flag+"/"+mode, func(t *testing.T) {
					input := append([]string{}, args...)
					input = append(input, flag, "--@shell=fish", "--output="+mode, "--color=never")
					out, diagnostic, resolutions, err := runCompletionPlacement(t, input)
					if resolutions != 0 {
						t.Fatal("misplaced completion action accessed credentials")
					}
					typed, ok := errors.AsType[CommandError](err)
					if !ok || typed.CLIError.ExitCode != 4 {
						t.Fatal("misplaced completion action did not fail validation before command execution")
					}
					if strings.Contains(out+diagnostic, "synthetic-private-shell") {
						t.Fatal("completion placement error exposed unreviewed flag value")
					}
					if mode == "human" {
						if out != "" || diagnostic == "" {
							t.Fatal("completion placement used wrong human failure stream")
						}
					} else {
						var frame struct {
							OK   bool
							Data json.RawMessage
							Meta struct {
								Exit int `json:"exit_code"`
							}
							Errors []struct{ Type string }
						}
						if json.Unmarshal([]byte(out), &frame) != nil || frame.OK || string(frame.Data) != "null" || frame.Meta.Exit != 4 || len(frame.Errors) != 1 || frame.Errors[0].Type != "validation_error" || diagnostic != "" {
							t.Fatal("completion placement lost common failure envelope")
						}
					}
				})
			}
		}
		t.Run(args[0]+"/"+args[1]+"/inactive", func(t *testing.T) {
			input := append([]string{}, args...)
			input = append(input, "--print-completion=false", "--install-completion=false", "--uninstall-completion=false", "--@complete=", "--@shell=fish", "--dry-run", "--output=json", "--color=never")
			out, diagnostic, resolutions, err := runCompletionPlacement(t, input)
			if err != nil || diagnostic != "" || resolutions != 0 || !strings.Contains(out, `"ok":true`) {
				t.Fatalf("inactive completion flags blocked normal command preview: %v", err)
			}
		})
	}
}

func TestCompletionGeneratedPositionalContextRemainsRootAction(t *testing.T) {
	for _, args := range [][]string{{"--@complete=workspace"}, {"--@complete=workspace", "--", "status", "set"}, {"--@complete=workspace", "--", "config", "get"}} {
		out, diagnostic, resolutions, err := runCompletionPlacement(t, args)
		if err != nil || diagnostic != "" || resolutions != 0 || strings.TrimSpace(out) != "default" {
			t.Fatalf("generated root completion invocation failed: %v", err)
		}
	}
}

func runCompletionPlacement(t *testing.T, args []string) (string, string, int, error) {
	t.Helper()
	cfg := workspaceConfig(config.TokenTypeUser)
	before, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var out, diagnostic bytes.Buffer
	resolutions := 0
	root, rt := NewRootCommandWithRuntime(WithConfig(cfg), WithTTY(false), WithSlackBaseURL("http://127.0.0.1:1"), WithIO(strings.NewReader(""), &out, &diagnostic), WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
		resolutions++
		return "xoxp-synthetic-completion-placement", nil
	})))
	root.SetArgs(append(append([]string{}, args...), "--timeout=250ms", "--no-throttle"))
	err = root.ExecuteContext(t.Context())
	if rt.CancelTimeout != nil {
		rt.CancelTimeout()
	}
	after, marshalErr := json.Marshal(cfg)
	if marshalErr != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("completion placement changed configuration")
	}
	return out.String(), diagnostic.String(), resolutions, err
}

func TestCompletionPlacementErrorDoesNotExposeShellValue(t *testing.T) {
	for _, mode := range []string{"json", "compact", "human"} {
		t.Run(mode, func(t *testing.T) {
			out, diagnostic, resolutions, err := runCompletionPlacement(t, []string{"status", "set", "--text=PR review", "--print-completion", "--@shell=synthetic-private-shell", "--output=" + mode, "--color=never"})
			typed, ok := errors.AsType[CommandError](err)
			if !ok || typed.CLIError.ExitCode != 4 || resolutions != 0 || strings.Contains(out+diagnostic, "synthetic-private-shell") {
				t.Fatal("completion placement lost validation/privacy guard")
			}
		})
	}
}
