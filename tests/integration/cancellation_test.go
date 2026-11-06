//go:build integration

package integration_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestBinaryTimeoutFlagErrorsOnHungServer verifies the CLI's own timeout,
// independently of the outer process deadline that contains a broken guard.
func TestBinaryTimeoutFlagErrorsOnHungServer(t *testing.T) {
	binary := buildSlackBinary(t)
	configPath := writePipeConfig(t)
	server := heldHistoryServer(t, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	start := time.Now()
	cmd := exec.CommandContext(ctx, binary,
		"--timeout", "200ms",
		"history", "list", "--channel", "C123",
	)
	cmd.WaitDelay = time.Second
	cmd.Env = append(isolatedRuntimeEnv(t),
		"SLACK_CLI_CONFIG="+configPath,
		"SLACK_CLI_BASE_URL="+server.URL,
		"SLACK_TEST_TOKEN=xoxb-test",
	)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	elapsed := time.Since(start)
	if ctx.Err() != nil {
		t.Fatalf("independent outer deadline fired; CLI --timeout did not terminate the hung request: %v", ctx.Err())
	}
	if err == nil {
		t.Fatalf("command succeeded, want timeout failure; stdout=%s", stdout.String())
	}
	if elapsed > 2*time.Second {
		t.Fatalf("command took %v, want < 2s for 200ms timeout", elapsed)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %T: %v", err, err)
	}
	if exitErr.ExitCode() != 7 {
		t.Fatalf("exit code = %d, want 7 (timeout); stderr=%s", exitErr.ExitCode(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `"type":"timeout"`) {
		t.Fatalf("stdout = %q, want type:timeout", stdout.String())
	}
}

// TestBinarySignalCancelsCancellableSlackCall sends SIGINT only after the held
// endpoint proves the process has started its cancellable Slack operation.
func TestBinarySignalCancelsCancellableSlackCall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("real SIGINT delivery is not supported by os.Process.Signal on Windows")
	}
	binary := buildSlackBinary(t)
	configPath := writePipeConfig(t)
	ready := make(chan struct{})
	server := heldHistoryServer(t, ready)
	ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
	defer cancel()
	// Keep the CLI timeout longer than the independent process deadline so only
	// SIGINT can produce the expected exit6 during this observation window.
	cmd := exec.CommandContext(ctx, binary,
		"--timeout", "30s",
		"history", "list", "--channel", "C123",
	)
	cmd.WaitDelay = time.Second
	cmd.Env = append(isolatedRuntimeEnv(t),
		"SLACK_CLI_CONFIG="+configPath,
		"SLACK_CLI_BASE_URL="+server.URL,
		"SLACK_TEST_TOKEN=xoxb-test",
	)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start command: %v", err)
	}
	var waitErr error
	done := make(chan struct{})
	go func() { waitErr = cmd.Wait(); close(done) }()
	// A closed broadcast channel supports both the assertion and cleanup waits.
	// Unlike receiving a single error value twice, cleanup cannot consume or wait
	// forever for an already-consumed completion notification.
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-done
	})
	select {
	case <-ready:
	case <-done:
		t.Fatalf("process exited before reaching the held endpoint: %v; stderr=%s", waitErr, stderr.String())
	case <-time.After(2 * time.Second):
		t.Fatal("process never reached the held history endpoint before SIGINT")
	case <-ctx.Done():
		t.Fatalf("outer deadline fired before the endpoint became ready: %v", ctx.Err())
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("send SIGINT: %v", err)
	}
	select {
	case <-done:
		if ctx.Err() != nil {
			t.Fatalf("outer deadline fired instead of SIGINT cancellation: %v", ctx.Err())
		}
		var exitErr *exec.ExitError
		if !errors.As(waitErr, &exitErr) {
			t.Fatalf("expected ExitError after SIGINT, got %T: %v", waitErr, waitErr)
		}
		if exitErr.ExitCode() != 6 {
			t.Fatalf("exit code = %d, want 6 (canceled); stderr=%s", exitErr.ExitCode(), stderr.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("process did not exit within 3s after SIGINT")
	case <-ctx.Done():
		t.Fatalf("outer deadline fired instead of SIGINT cancellation: %v", ctx.Err())
	}
}

func heldHistoryServer(t *testing.T, ready chan struct{}) *httptest.Server {
	t.Helper()
	released := make(chan struct{})
	var reached sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth.test":
			writeJSON(w, `{"ok":true,"user_id":"U123"}`)
		case "/api/conversations.history":
			if ready != nil {
				reached.Do(func() { close(ready) })
			}
			<-released
		default:
			t.Errorf("unexpected cancellation fixture path %s", r.URL.Path)
			http.Error(w, "unexpected fixture request", http.StatusInternalServerError)
		}
	}))
	t.Cleanup(func() { close(released); server.Close() })
	return server
}
