package testutil

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type CommandOptions struct {
	Dir     string
	Env     map[string]string
	Stdin   string
	Timeout time.Duration
}

type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Err      error
}

func RunBinary(t testing.TB, binary string, args []string, opts CommandOptions) CommandResult {
	t.Helper()

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = opts.Dir
	cmd.Env = os.Environ()
	for key, value := range opts.Env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	if opts.Stdin != "" {
		cmd.Stdin = strings.NewReader(opts.Stdin)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("command timed out: %v", ctx.Err())
	}

	exitCode := 0
	if err != nil {
		exitCode = -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	return CommandResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
		Err:      err,
	}
}

func WriteExecutable(t testing.TB, name string, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write executable: %v", err)
	}
	return path
}
