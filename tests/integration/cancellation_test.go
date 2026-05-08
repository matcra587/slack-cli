package integration_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestBinaryTimeoutFlagErrorsOnHungServer verifies that --timeout fires and
// returns exit code 7 within a reasonable wall-clock window even when the
// Slack server hangs indefinitely.
func TestBinaryTimeoutFlagErrorsOnHungServer(t *testing.T) {
	binary := buildSlackBinary(t)
	configPath := writePipeConfig(t)

	released := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth.test" {
			writeJSON(w, `{"ok":true,"user_id":"U123"}`)
			return
		}
		// Hang until the test releases it (simulates a slow Slack endpoint).
		<-released
	}))
	t.Cleanup(func() {
		close(released)
		server.Close()
	})

	start := time.Now()
	cmd := exec.Command(binary,
		"--timeout", "200ms",
		"history", "list", "--channel", "C123",
	)
	cmd.Env = append(os.Environ(),
		"SLACK_CLI_CONFIG="+configPath,
		"SLACK_CLI_BASE_URL="+server.URL,
		"SLACK_TEST_TOKEN=xoxb-test",
	)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("command succeeded, want timeout failure; stdout=%s", stdout.String())
	}
	// Should fail fast — well under 2 seconds even with OS scheduling noise.
	if elapsed > 2*time.Second {
		t.Fatalf("command took %v, want < 2s for 200ms timeout", elapsed)
	}
	// Exit code 7 = ExitCodeTimeout.
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitError, got %T: %v", err, err)
	}
	if exitErr.ExitCode() != 7 {
		t.Fatalf("exit code = %d, want 7 (timeout); stderr=%s", exitErr.ExitCode(), stderr.String())
	}
	// stderr should carry the timeout error envelope.
	if !strings.Contains(stderr.String(), `"type":"timeout"`) {
		t.Fatalf("stderr = %q, want type:timeout", stderr.String())
	}
}

// TestBinarySignalCancelsCancellableSlackCall sends SIGINT to a running slick
// process that is blocked on a hung Slack endpoint. It verifies the process
// terminates quickly (signal propagated through signal.NotifyContext) with
// exit code 6 (ExitCodeCanceled).
func TestBinarySignalCancelsCancellableSlackCall(t *testing.T) {
	binary := buildSlackBinary(t)
	configPath := writePipeConfig(t)

	released := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth.test" {
			writeJSON(w, `{"ok":true,"user_id":"U123"}`)
			return
		}
		<-released
	}))
	t.Cleanup(func() {
		close(released)
		server.Close()
	})

	// Use a long --timeout so the signal, not the timeout, triggers termination.
	cmd := exec.Command(binary,
		"--timeout", "30s",
		"history", "list", "--channel", "C123",
	)
	cmd.Env = append(os.Environ(),
		"SLACK_CLI_CONFIG="+configPath,
		"SLACK_CLI_BASE_URL="+server.URL,
		"SLACK_TEST_TOKEN=xoxb-test",
	)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start command: %v", err)
	}

	// Let the command reach the hung Slack call before signaling.
	time.Sleep(100 * time.Millisecond)

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("send SIGINT: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		// Process exited — signal was received and the in-flight call canceled.
		// Exit code 6 = ExitCodeCanceled.
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("expected ExitError after SIGINT, got %T: %v", err, err)
		}
		if exitErr.ExitCode() != 6 {
			t.Fatalf("exit code = %d, want 6 (canceled); stderr=%s", exitErr.ExitCode(), stderr.String())
		}
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("process did not exit within 3s after SIGINT")
	}
}
