package integration_test

import (
	"encoding/json"
	"testing"
)

func TestQuickstartSmokeBuildSchemaAndDryRuns(t *testing.T) {
	binary := buildSlackBinary(t)
	configPath := writePipeConfig(t)

	stdout, stderr, err := runSlackBinary(t, binary, configPath, "http://example.invalid", "", "agent", "schema", "--compact")
	if err != nil {
		t.Fatalf("agent schema failed: %v\nstderr=%s", err, stderr)
	}
	if err := json.Unmarshal([]byte(stdout), &map[string]any{}); err != nil {
		t.Fatalf("agent schema stdout is not JSON: %v", err)
	}

	for _, args := range [][]string{
		{"message", "send", "--channel", "C123", "--message", "hello", "--dry-run"},
		{"file", "upload", "--channel", "C123", "--file", "-", "--filename", "dry.txt", "--dry-run"},
	} {
		stdout, stderr, err := runSlackBinary(t, binary, configPath, "http://example.invalid", "hello", args...)
		if err != nil {
			t.Fatalf("%v failed: %v\nstdout=%s\nstderr=%s", args, err, stdout, stderr)
		}
		if err := json.Unmarshal([]byte(stdout), &map[string]any{}); err != nil {
			t.Fatalf("%v stdout is not JSON: %v\n%s", args, err, stdout)
		}
	}
}
