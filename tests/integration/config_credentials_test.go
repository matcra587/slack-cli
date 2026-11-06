//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBinaryConfigRejectsRawCredentialsAndRecovers(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tc := range []struct {
		name, token, mode string
		args              []string
	}{
		{"rotating read", "xoxe.xoxp-synthetic-private", "json", []string{"config", "list"}},
		{"app write", "xapp-synthetic-private", "compact", []string{"config", "set", "workspaces.default.default_channel", "C_UPDATED"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			fixture := "schema_version=\"1\"\ndefault_workspace=\"default\"\n[workspaces.default]\nname=\"default\"\nteam_id=\"T_FIXTURE\"\ntoken_type=\"user\"\ntoken_ref=\"" + tc.token + "\"\ndefault_channel=\"C_KEEP\"\n"
			if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
				t.Fatal(err)
			}
			args := append([]string{"--output=" + tc.mode, "--color=never"}, tc.args...)
			out, diagnostic, runErr := runSlackBinary(t, binary, path, "http://127.0.0.1:1", "", args...)
			var exit *exec.ExitError
			var frame struct {
				OK   bool
				Meta struct {
					Command string
					Exit    int `json:"exit_code"`
				}
				Errors []struct{ Message string }
			}
			if !errors.As(runErr, &exit) || exit.ExitCode() != 5 || json.Unmarshal([]byte(out), &frame) != nil || frame.OK || frame.Meta.Exit != 5 || frame.Meta.Command != "config."+tc.args[1] || diagnostic != "" {
				t.Fatal("raw credential did not produce a safe config failure envelope")
			}
			if len(frame.Errors) != 1 || !strings.Contains(frame.Errors[0].Message, "credential reference") {
				t.Fatal("command failed for a reason other than rejecting a raw credential")
			}
			if strings.Contains(out+diagnostic, "synthetic-private") {
				t.Fatal("raw credential appeared in output")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, []byte(fixture)) {
				t.Fatal("rejected credential changed existing config")
			}
			// Repair the fixture explicitly, then retry the actual command.
			fixed := strings.Replace(fixture, tc.token, "env:SLICK_UNUSED_SYNTHETIC", 1)
			if err := os.WriteFile(path, []byte(fixed), 0o600); err != nil {
				t.Fatal(err)
			}
			_, diagnostic, runErr = runSlackBinary(t, binary, path, "http://127.0.0.1:1", "", args...)
			if runErr != nil || diagnostic != "" {
				t.Fatal("config command did not recover after explicit reference repair")
			}
		})
	}
}
