package main

import (
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func TestWorkspaceListUsesConfigOnly(t *testing.T) {
	stdout, stderr, err := executeAuthRoot(t, authTestConfig(), "", config.NewMemoryCredentialStore(), "http://example.invalid",
		[]string{"workspace", "list"},
	)
	if err != nil {
		t.Fatalf("workspace list returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"name":"default"`) || !strings.Contains(stdout, `"name":"other"`) {
		t.Fatalf("stdout = %s, want configured workspaces", stdout)
	}
}
