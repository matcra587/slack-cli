package main

import (
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func TestRootSchemaCommandIsNotRegistered(t *testing.T) {
	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), "http://example.invalid",
		"",
		[]string{"schema"},
	)
	if err == nil {
		t.Fatalf("schema unexpectedly succeeded\nstdout=%s\nstderr=%s", stdout, stderr)
	}
	if !strings.Contains(err.Error(), `unknown command "schema"`) {
		t.Fatalf("err = %v, want unknown schema command\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
}
