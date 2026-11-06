//go:build integration

package integration_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func TestBinaryAuthLogoutKeepTokenPreservesPreferences(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, flag := range []string{"--keep-token", "-K"} {
		t.Run(flag, func(t *testing.T) {
			path, prior := defaultSelectorFixture(t)
			enabled := false
			profile := prior.Workspaces["Other"]
			profile.DefaultChannel, profile.Aliases = "", nil
			profile.Attribution.Enabled = &enabled
			prior.Workspaces["Other"] = profile
			if err := config.SaveFile(path, prior); err != nil {
				t.Fatal(err)
			}
			out, diagnostic, err := runSlackBinary(t, binary, path, "http://127.0.0.1:1", "", "auth", "logout", " OTHER ", flag, "--output=json", "--color=never")
			var frame struct {
				OK   bool
				Meta struct{ Workspace string }
				Data struct {
					Workspace     string
					Authenticated bool
				}
			}
			if err != nil || diagnostic != "" || json.Unmarshal([]byte(out), &frame) != nil || !frame.OK || frame.Meta.Workspace != "Other" || frame.Data.Workspace != "Other" || frame.Data.Authenticated {
				t.Fatal("keep-token logout lost canonical target, result or streams")
			}
			prior.Workspaces["Other"] = config.WorkspaceProfile{Name: "Other", Attribution: config.AttributionConfig{Enabled: &enabled}}
			loaded, err := config.LoadFile(path)
			if err != nil || !reflect.DeepEqual(loaded, prior) {
				t.Fatal("keep-token logout discarded explicit preference or changed unrelated state")
			}
		})
	}
}
