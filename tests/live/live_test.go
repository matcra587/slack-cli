//go:build live

// Package live_test contains tests that exercise slick against a real Slack
// workspace. Run with:
//
//	mise run test:live
//
// Required environment:
//
//	SLICK_LIVE_WORKSPACE - workspace profile name from your slick config
//	SLICK_LIVE_CHANNEL   - channel ID (Cxxxxxxx) the test bot can post to
//
// The tests build the slick binary, post real messages tagged with a run ID,
// verify Slack accepted them, and clean up by deleting/un-reacting the same
// targets. A failed test that escapes cleanup leaves residue tagged with the
// run ID for manual triage.
package live_test

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLiveMessageSendAndDelete(t *testing.T) {
	env := requireLiveEnv(t)
	binary := buildSlackBinary(t)
	runID := newRunID(t)

	body := fmt.Sprintf("phase 12 live test message — run id %s", runID)
	stdout, stderr, err := runSlick(t, binary, env, "",
		"message", "send",
		"--workspace", env.workspace,
		"--channel", env.channel,
		"--message", body,
		"--json",
	)
	if err != nil {
		t.Fatalf("message send failed: %v\nstderr=%s", err, stderr)
	}

	envelope := decodeEnvelope(t, stdout)
	channel := mustString(t, envelope, "data", "message", "channel")
	timestamp := mustString(t, envelope, "data", "message", "ts")
	if channel == "" || timestamp == "" {
		t.Fatalf("missing channel/ts in envelope: %s", stdout)
	}

	t.Cleanup(func() {
		_, cleanupStderr, cleanupErr := runSlick(t, binary, env, "",
			"message", "delete",
			"--workspace", env.workspace,
			"--channel", channel,
			"--timestamp", timestamp,
			"--force",
			"--json",
		)
		if cleanupErr != nil {
			t.Logf("cleanup delete failed (run id %s, ts %s): %v\nstderr=%s",
				runID, timestamp, cleanupErr, cleanupStderr)
		}
	})
}

func TestLiveReactionAddAndRemove(t *testing.T) {
	env := requireLiveEnv(t)
	binary := buildSlackBinary(t)
	runID := newRunID(t)

	body := fmt.Sprintf("phase 12 live reaction test — run id %s", runID)
	sendOut, sendErr, err := runSlick(t, binary, env, "",
		"message", "send",
		"--workspace", env.workspace,
		"--channel", env.channel,
		"--message", body,
		"--json",
	)
	if err != nil {
		t.Fatalf("parent message send failed: %v\nstderr=%s", err, sendErr)
	}
	envelope := decodeEnvelope(t, sendOut)
	channel := mustString(t, envelope, "data", "message", "channel")
	timestamp := mustString(t, envelope, "data", "message", "ts")

	t.Cleanup(func() {
		_, cleanupStderr, cleanupErr := runSlick(t, binary, env, "",
			"message", "delete",
			"--workspace", env.workspace,
			"--channel", channel,
			"--timestamp", timestamp,
			"--force",
			"--json",
		)
		if cleanupErr != nil {
			t.Logf("cleanup delete failed (run id %s, ts %s): %v\nstderr=%s",
				runID, timestamp, cleanupErr, cleanupStderr)
		}
	})

	const emoji = "white_check_mark"
	_, reactStderr, err := runSlick(t, binary, env, "",
		"react", "add",
		"--workspace", env.workspace,
		"--channel", channel,
		"--timestamp", timestamp,
		"--emoji", emoji,
		"--json",
	)
	if err != nil {
		t.Fatalf("react add failed: %v\nstderr=%s", err, reactStderr)
	}

	_, removeStderr, err := runSlick(t, binary, env, "",
		"react", "remove",
		"--workspace", env.workspace,
		"--channel", channel,
		"--timestamp", timestamp,
		"--emoji", emoji,
		"--json",
	)
	if err != nil {
		t.Fatalf("react remove failed: %v\nstderr=%s", err, removeStderr)
	}
}

type liveEnv struct {
	workspace string
	channel   string
}

func requireLiveEnv(t *testing.T) liveEnv {
	t.Helper()
	workspace := os.Getenv("SLICK_LIVE_WORKSPACE")
	channel := os.Getenv("SLICK_LIVE_CHANNEL")
	if workspace == "" || channel == "" {
		t.Skip("set SLICK_LIVE_WORKSPACE and SLICK_LIVE_CHANNEL to run live tests")
	}
	if !strings.HasPrefix(channel, "C") && !strings.HasPrefix(channel, "G") && !strings.HasPrefix(channel, "D") {
		t.Fatalf("SLICK_LIVE_CHANNEL=%q does not look like a Slack channel ID", channel)
	}
	return liveEnv{workspace: workspace, channel: channel}
}

func buildSlackBinary(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	binary := filepath.Join(t.TempDir(), "slick")
	cmd := exec.Command("go", "build", "-o", binary, "./cmd/slick")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build failed: %v\n%s", err, string(out))
	}
	return binary
}

func runSlick(t *testing.T, binary string, _ liveEnv, stdin string, args ...string) (string, string, error) {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Env = os.Environ()
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func repoRoot(t *testing.T) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func newRunID(t *testing.T) string {
	t.Helper()
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return hex.EncodeToString(b[:])
}

func decodeEnvelope(t *testing.T, stdout string) map[string]any {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("envelope decode: %v\nstdout=%s", err, stdout)
	}
	return envelope
}

func mustString(t *testing.T, m map[string]any, path ...string) string {
	t.Helper()
	current := any(m)
	for _, key := range path {
		obj, ok := current.(map[string]any)
		if !ok {
			t.Fatalf("path %v: expected map at %q, got %T", path, key, current)
		}
		current = obj[key]
	}
	if current == nil {
		return ""
	}
	s, ok := current.(string)
	if !ok {
		t.Fatalf("path %v: expected string, got %T", path, current)
	}
	return s
}
