package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func TestHelpWriteFailuresReturnIOWithoutReplay(t *testing.T) {
	for _, tc := range []struct {
		name      string
		args      []string
		humanOnly bool
	}{
		{"root flag", []string{"--help"}, false},
		{"short flag", []string{"-h"}, false},
		{"leaf", []string{"history", "list", "--help"}, false},
		{"help command", []string{"help", "history", "list"}, false},
		{"custom manifest", []string{"manifest", "template", "--help"}, false},
		{"completion group", []string{"completion", "--help"}, false},
		{"root implicit", nil, true},
		{"group implicit", []string{"history"}, true},
	} {
		for _, mode := range []string{"json", "compact", "human"} {
			if tc.humanOnly && mode != "human" {
				continue
			}
			for _, failure := range []string{"error", "short", "partial", "transient"} {
				// Every help route keeps each applicable output override. Full writer
				// coverage belongs to the shared wrapper, custom callback and implicit help.
				allWriterFailures := mode == "human" && (tc.name == "root flag" ||
					tc.name == "custom manifest" || tc.name == "root implicit" || tc.name == "group implicit")
				if failure != "error" && !allWriterFailures {
					continue
				}
				t.Run(tc.name+"/"+mode+"/"+failure, func(t *testing.T) {
					writer := &completionFailingWriter{mode: failure}
					var diagnostic bytes.Buffer
					providers := 0
					root, runtime := NewRootCommandWithRuntime(WithConfig(completionProfileConfig()), WithTTY(false), WithIO(strings.NewReader(""), writer, &diagnostic), WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
						providers++
						return "", errors.New("synthetic-private-help-provider")
					})))
					args := append([]string{"--color=never", "--output=" + mode}, tc.args...)
					root.SetArgs(args)
					_, err := executeRoot(t.Context(), root, runtime)
					if runtime.CancelTimeout != nil {
						runtime.CancelTimeout()
					}
					typed, ok := errors.AsType[CommandError](err)
					if !ok || typed.CLIError.ExitCode != 8 || typed.CLIError.Type != "io" || typed.CLIError.Message != "output_write_failed" {
						t.Fatalf("help failed to return safe IO8: %v", err)
					}
					if writer.calls != 1 || strings.Count(diagnostic.String(), "output_write_failed") != 1 || strings.Contains(diagnostic.String()+err.Error(), "synthetic-private") || providers != 0 {
						t.Fatal("help failure replayed a write, leaked private data, or accessed credentials")
					}
					var healthy, nextDiagnostic bytes.Buffer
					root.SetOut(&healthy)
					root.SetErr(&nextDiagnostic)
					runtime.Stderr = &nextDiagnostic
					root.SetArgs(args)
					_, err = executeRoot(t.Context(), root, runtime)
					if runtime.CancelTimeout != nil {
						runtime.CancelTimeout()
					}
					if err != nil || nextDiagnostic.Len() != 0 || !strings.Contains(healthy.String(), "Usage") || strings.HasPrefix(healthy.String(), "{") || providers != 0 {
						t.Fatalf("explicit help recovery failed or changed protocol: %v", err)
					}
					if tc.name == "custom manifest" && !strings.Contains(healthy.String(), "Messaging Template User Scopes") {
						t.Fatal("custom manifest sections lost")
					}
				})
			}
		}
	}
}

func TestHelpBothOutputStreamsCanFailWithoutReplay(t *testing.T) {
	stdout, stderr := &completionFailingWriter{mode: "error"}, &completionFailingWriter{mode: "error"}
	root, runtime := NewRootCommandWithRuntime(WithConfig(completionProfileConfig()), WithTTY(false), WithIO(strings.NewReader(""), stdout, stderr))
	root.SetArgs([]string{"--help", "--color=never"})
	_, err := executeRoot(t.Context(), root, runtime)
	typed, ok := errors.AsType[CommandError](err)
	if !ok || typed.CLIError.ExitCode != 8 || stdout.calls != 1 || stderr.calls != 1 {
		t.Fatal("diagnostic failure replayed help or lost IO8")
	}
}
