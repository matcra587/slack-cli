//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBinaryCompletionScriptWriteFailuresAndRecovery(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("/dev/full fixture requires Linux")
	}
	binary := buildSlackBinary(t)
	// TestCompletionScriptsPreserveBytesAndReturnIOWithoutReplay owns the
	// shell/route/mode cross-product; these rows retain actual OS pipe failures.
	for _, tc := range []struct{ name, shell, route, mode string }{
		{"public fish json", "fish", "public", "json"},
		{"hidden fish compact", "fish", "hidden", "compact"},
		{"public bash human", "bash", "public", "human"},
		{"hidden zsh json", "zsh", "hidden", "json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writePipeConfig(t)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			args := []string{"--output=" + tc.mode, "--color=never"}
			if tc.route == "public" {
				args = append(args, "completion", tc.shell)
			} else {
				args = append(args, "--print-completion", "--@shell="+tc.shell)
			}
			run := func(full bool) (string, string, error) {
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, binary, args...)
				cmd.Env = append(isolatedRuntimeEnv(t), "SLICK_CONFIG="+path)
				var stdout, stderr bytes.Buffer
				cmd.Stdout = &stdout
				cmd.Stderr = &stderr
				if full {
					dst, err := os.OpenFile("/dev/full", os.O_WRONLY, 0)
					if err != nil {
						t.Fatal(err)
					}
					defer func() {
						if err := dst.Close(); err != nil {
							t.Error(err)
						}
					}()
					cmd.Stdout = dst
				}
				err := cmd.Run()
				if ctx.Err() != nil {
					t.Fatal("script fixture timed out")
				}
				return stdout.String(), stderr.String(), err
			}
			_, diagnostic, err := run(true)
			if exitCode(err) != 8 || strings.Count(diagnostic, "output_write_failed") != 1 || !strings.Contains(diagnostic, "exit_code=8") {
				t.Fatalf("script destination failure lost IO8: %v; %s", err, diagnostic)
			}
			healthy, diagnostic, err := run(false)
			if err != nil || diagnostic != "" || !strings.Contains(healthy, "slick") || strings.HasPrefix(healthy, "{") {
				t.Fatalf("script recovery did not execute successfully: %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("script output changed configuration")
			}
		})
	}
}

func TestBinaryCompletionScriptsReachAllShells(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tc := range []struct{ shell, marker string }{
		{"bash", "slick"},
		{"elvish", "slick"},
		{"fish", "complete -c slick"},
		{"nu", "slick"},
		{"pwsh", "slick"},
		{"zsh", "slick"},
	} {
		t.Run(tc.shell, func(t *testing.T) {
			config := writePipeConfig(t)
			before, err := os.ReadFile(config)
			if err != nil {
				t.Fatal(err)
			}
			out, diagnostic, err := runSlackBinary(t, binary, config, "http://127.0.0.1:1", "", "completion", tc.shell, "--output=json", "--color=never")
			if err != nil || diagnostic != "" || !strings.Contains(out, tc.marker) || strings.HasPrefix(out, "{") {
				t.Fatalf("executable completion generator did not return a raw script: %v", err)
			}
			after, err := os.ReadFile(config)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("script output changed configuration")
			}
		})
	}
}
