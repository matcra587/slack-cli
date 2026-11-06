//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestBinaryAuthLoginPreviewAndInvalidCombination(t *testing.T) {
	binary := buildSlackBinary(t)
	path := writePipeConfig(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "missing-token")
	out, diagnostic, err := runSlackBinary(t, binary, path, "http://127.0.0.1:1", "", "auth", "login", "-w", "DEFAULT", "-f", missing, "-n", "--output=compact", "--color=never")
	var preview map[string]any
	if err != nil || diagnostic != "" || json.Unmarshal([]byte(out), &preview) != nil || len(preview) != 3 || preview["workspace"] != "default" || preview["dry_run"] != true || preview["authenticated"] != false {
		t.Fatal("native login preview read a missing token file or lost compact plan shape")
	}
	out, diagnostic, err = runSlackBinary(t, binary, path, "http://127.0.0.1:1", "", "auth", "login", "--workspace-name=default", "--token-file="+missing, "--oauth-client-id=synthetic-client", "--dry-run", "--output=json", "--color=never")
	var frame map[string]any
	if exitCode(err) != 4 || diagnostic != "" || json.Unmarshal([]byte(out), &frame) != nil || len(frame) != 4 || frame["ok"] != false || frame["data"] != nil || frame["meta"].(map[string]any)["command"] != "auth.login" {
		t.Fatal("native invalid login combination lost validation envelope or streams")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("native login preview or validation changed prior config")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("login preview created its token input")
	}
}
