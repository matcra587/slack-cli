package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func TestAPIDiscoveryOutputFailuresUseIOExitWithoutReplay(t *testing.T) {
	for _, command := range []struct {
		name string
		args []string
	}{
		{"methods", []string{"api", "methods", "users."}},
		{"describe", []string{"api", "describe", "users.info"}},
	} {
		for _, mode := range []string{"human", "json", "compact"} {
			for _, failure := range []string{"error", "short", "partial", "transient"} {
				t.Run(command.name+"/"+mode+"/"+failure, func(t *testing.T) {
					writer := &completionFailingWriter{mode: failure}
					var diagnostic bytes.Buffer
					providers := 0
					root, runtime := NewRootCommandWithRuntime(WithConfig(authTestConfig()), WithTTY(false),
						WithIO(strings.NewReader(""), writer, &diagnostic),
						WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
							providers++
							return "", errors.New("synthetic-private-provider")
						})))
					root.SetArgs(append([]string{"--output=" + mode, "--color=never"}, command.args...))
					_, err := executeRoot(t.Context(), root, runtime)
					if runtime.CancelTimeout != nil {
						runtime.CancelTimeout()
					}
					typed, ok := errors.AsType[CommandError](err)
					if !ok || typed.CLIError.ExitCode != 8 || typed.CLIError.Type != "io" || typed.CLIError.Message != "output_write_failed" {
						t.Fatalf("discovery output failure did not return IO8: %v", err)
					}
					if writer.calls != 1 || providers != 0 || strings.Count(diagnostic.String(), "output_write_failed") != 1 {
						t.Fatal("discovery retried output, accessed credentials, or lost its diagnostic")
					}
					if strings.Contains(diagnostic.String()+err.Error(), "synthetic-private") {
						t.Fatal("discovery output failure exposed private diagnostics")
					}
				})
			}
		}
	}
}
