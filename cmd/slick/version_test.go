package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestVersionCommandOutputsClogJSON(t *testing.T) {
	stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", []string{"version"})
	if err != nil {
		t.Fatalf("version returned error: %v\nstderr=%s", err, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, `"command":"version"`) {
		t.Fatalf("stdout = %s, want version envelope", stdout)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	data := envelope["data"].(map[string]any)
	if data["version"] == "" {
		t.Fatalf("data = %#v, want version", data)
	}
}

func TestVersionCommandPlainMatchesPDCShape(t *testing.T) {
	stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", []string{"--plain", "version"})
	if err != nil {
		t.Fatalf("version returned error: %v\nstderr=%s", err, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	for _, fragment := range []string{
		"slick dev",
		"commit",
		"branch",
		"built",
		"built by",
	} {
		if !strings.Contains(stdout, fragment) {
			t.Fatalf("stdout = %q, want fragment %q", stdout, fragment)
		}
	}
	if strings.Contains(stdout, "data=") {
		t.Fatalf("stdout = %q, want rendered version text only", stdout)
	}
}
