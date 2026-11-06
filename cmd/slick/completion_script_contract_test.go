package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	clicompletion "github.com/matcra587/slack-cli/internal/cli/completion"
	"github.com/matcra587/slack-cli/internal/config"
)

func TestCompletionScriptsPreserveBytesAndReturnIOWithoutReplay(t *testing.T) {
	for _, shell := range []string{"bash", "elvish", "fish", "nu", "pwsh", "zsh"} {
		for _, route := range []string{"public", "hidden"} {
			for _, mode := range []string{"json", "compact", "human"} {
				for _, failure := range []string{"error", "short", "partial", "transient"} {
					// Every shell and route keeps its byte/protocol guard. Fish exercises
					// all writer failures and output overrides through the shared writer.
					if shell != "fish" && (mode != "json" || failure != "error") {
						continue
					}
					t.Run(shell+"/"+route+"/"+mode+"/"+failure, func(t *testing.T) {
						cfg := completionProfileConfig()
						providers := 0
						resolver := TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
							providers++
							return "", errors.New("synthetic-private-script-provider")
						})
						writer := &completionFailingWriter{mode: failure}
						var diagnostic bytes.Buffer
						root, runtime := NewRootCommandWithRuntime(WithConfig(cfg), WithTokenResolver(resolver), WithTTY(false), WithIO(strings.NewReader(""), writer, &diagnostic))
						args := []string{"--output=" + mode, "--color=never"}
						if route == "public" {
							args = append(args, "completion", shell)
						} else {
							args = append(args, "--print-completion", "--@shell="+shell)
						}
						root.SetArgs(args)
						err := root.ExecuteContext(t.Context())
						if runtime.CancelTimeout != nil {
							runtime.CancelTimeout()
						}
						typed, ok := errors.AsType[CommandError](err)
						if !ok || typed.CLIError.ExitCode != 8 || typed.CLIError.Type != "io" || typed.CLIError.Message != "output_write_failed" {
							t.Fatalf("script destination failure did not return IO8: %v", err)
						}
						if writer.calls != 1 || strings.Count(diagnostic.String(), "output_write_failed") != 1 || strings.Contains(diagnostic.String(), "synthetic-private") || strings.Contains(err.Error(), "synthetic-private") || providers != 0 {
							t.Fatal("script failure replayed output, exposed private diagnostics, or resolved credentials")
						}
						var healthy, secondDiagnostic bytes.Buffer
						next, nextRuntime := NewRootCommandWithRuntime(WithConfig(cfg), WithTokenResolver(resolver), WithTTY(false), WithIO(strings.NewReader(""), &healthy, &secondDiagnostic))
						next.SetArgs(args)
						if err := next.ExecuteContext(t.Context()); err != nil {
							t.Fatalf("explicit recovery failed: %v", err)
						}
						if nextRuntime.CancelTimeout != nil {
							nextRuntime.CancelTimeout()
						}
						var expected bytes.Buffer
						if err := clicompletion.Generator(next).Print(&expected, shell); err != nil {
							t.Fatal(err)
						}
						if expected.Len() == 0 || !bytes.Equal(healthy.Bytes(), expected.Bytes()) || secondDiagnostic.Len() != 0 || providers != 0 {
							t.Fatal("successful script changed bytes/protocol or accessed credentials")
						}
					})
				}
			}
		}
	}
}

func TestHiddenCompletionPrintPreservesDynamicPriority(t *testing.T) {
	stdout, stderr, err := executeTestRoot(t, completionProfileConfig(), "http://example.invalid", "", []string{"--@complete=workspace", "--print-completion", "--@shell=fish"})
	if err != nil || stderr != "" || !strings.Contains(stdout, "default\n") || strings.Contains(stdout, "complete -c slick") {
		t.Fatal("script print displaced dynamic completion")
	}
}

func TestUnsupportedCompletionShellDoesNotWriteScript(t *testing.T) {
	writer := &completionFailingWriter{mode: "error"}
	var diagnostic bytes.Buffer
	root, runtime := NewRootCommandWithRuntime(WithConfig(completionProfileConfig()), WithTTY(false), WithIO(strings.NewReader(""), writer, &diagnostic))
	root.SetArgs([]string{"--print-completion", "--@shell=unsupported-fixture", "--color=never"})
	err := root.ExecuteContext(t.Context())
	if runtime.CancelTimeout != nil {
		runtime.CancelTimeout()
	}
	if err == nil || !strings.Contains(err.Error(), "unsupported shell") || writer.calls != 0 || diagnostic.Len() != 0 {
		t.Fatal("unsupported shell wrote a partial script or lost validation error")
	}
}

func TestHiddenCompletionPrintShellDetectionAndRemoval(t *testing.T) {
	t.Setenv("COMPLETE_SHELL", "fish")
	for _, tc := range []struct {
		name   string
		args   []string
		script bool
	}{
		{"detected", []string{"--print-completion"}, true},
		{"empty shell", []string{"--print-completion", "--@shell="}, true},
		{"disabled", []string{"--print-completion=false", "--output=json"}, false},
		{"removed", []string{"--output=json"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, err := executeTestRoot(t, completionProfileConfig(), "http://example.invalid", "", tc.args)
			if err != nil || stderr != "" {
				t.Fatalf("completion flag invocation failed: %v", err)
			}
			if tc.script {
				if !strings.Contains(stdout, "complete -c slick") || strings.HasPrefix(stdout, "{") {
					t.Fatal("shell detection did not select fish script")
				}
			} else {
				var envelope struct {
					OK   bool `json:"ok"`
					Data struct {
						Schema json.RawMessage `json:"schema"`
					} `json:"data"`
				}
				if json.Unmarshal([]byte(stdout), &envelope) != nil || !envelope.OK || len(envelope.Data.Schema) == 0 {
					t.Fatal("removing print flag did not restore discovery envelope")
				}
			}
		})
	}
}
