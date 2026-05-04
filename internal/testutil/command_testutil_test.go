package testutil_test

import (
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestRunBinaryCapturesStdoutStderrStdinEnvAndExitCode(t *testing.T) {
	script := testutil.WriteExecutable(t, "helper.sh", `#!/bin/sh
read line
echo "stdout:${SLACK_TEST_ENV}:${line}"
echo "stderr:${SLACK_TEST_ENV}" >&2
exit 7
`)

	result := testutil.RunBinary(t, script, nil, testutil.CommandOptions{
		Stdin: "from-stdin\n",
		Env: map[string]string{
			"SLACK_TEST_ENV": "from-env",
		},
	})

	if result.ExitCode != 7 {
		t.Fatalf("ExitCode = %d, want 7", result.ExitCode)
	}
	if got, want := strings.TrimSpace(result.Stdout), "stdout:from-env:from-stdin"; got != want {
		t.Fatalf("Stdout = %q, want %q", got, want)
	}
	if got, want := strings.TrimSpace(result.Stderr), "stderr:from-env"; got != want {
		t.Fatalf("Stderr = %q, want %q", got, want)
	}
}
