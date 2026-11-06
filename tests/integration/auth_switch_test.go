//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func TestBinaryAuthSwitchPreviewAndNextProcess(t *testing.T) {
	binary := buildSlackBinary(t)
	path, prior := defaultSelectorFixture(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out, diagnostic, err := runSlackBinary(t, binary, path, "http://127.0.0.1:1", "", "-n", "auth", "switch", " OTHER ", "--output=compact", "--color=never")
	var preview struct {
		Workspace string `json:"workspace"`
		DryRun    bool   `json:"dry_run"`
	}
	if err != nil || diagnostic != "" || json.Unmarshal([]byte(out), &preview) != nil || preview.Workspace != "Other" || !preview.DryRun {
		t.Fatal("built-binary switch preview lost canonical target or output")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("built-binary switch preview changed config")
	}
	out, diagnostic, err = runSlackBinary(t, binary, path, "http://127.0.0.1:1", "", "auth", "switch", "OTHER", "--output=json", "--color=never")
	var frame struct {
		OK   bool
		Meta struct{ Workspace string }
		Data struct{ Workspace string }
	}
	if err != nil || diagnostic != "" || json.Unmarshal([]byte(out), &frame) != nil || !frame.OK || frame.Meta.Workspace != "Other" || frame.Data.Workspace != "Other" {
		t.Fatal("built-binary switch lost canonical result metadata")
	}
	loaded, err := config.LoadFile(path)
	if err != nil || loaded.DefaultWorkspace != "Other" || !reflect.DeepEqual(loaded.Workspaces, prior.Workspaces) {
		t.Fatal("built-binary switch changed unrelated settings")
	}
	out, diagnostic, err = runSlackBinary(t, binary, path, "http://127.0.0.1:1", "", "workspace", "list", "--output=json", "--color=never")
	if err != nil || diagnostic != "" || json.Unmarshal([]byte(out), &frame) != nil || !frame.OK || frame.Meta.Workspace != "Other" {
		t.Fatal("next CLI process did not select the switched default")
	}
}
