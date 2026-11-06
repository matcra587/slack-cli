//go:build integration

package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func legacyAttributionFixture(t *testing.T, nested bool) (string, *config.Config) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	body := "schema_version=\"1\"\ndefault_workspace=\"default\"\n[workspaces.default]\nname=\"default\"\nteam_id=\"T_SYNTHETIC\"\ntoken_type=\"bot\"\ntoken_ref=\"env:SLACK_TEST_TOKEN\"\ndefault_channel=\"C_KEEP\"\nagent_attribution=false\nagent_label=\"Old label\"\nagent_emoji=\":eyes:\"\nagent_message=\"Old note\"\n[workspaces.default.aliases]\nalerts=\"C_ALERTS\"\n[workspaces.other]\nname=\"other\"\ndefault_channel=\"C_OTHER\"\n"
	if nested {
		body += "[workspaces.default.attribution]\nenabled=true\nlabel=\"New label\"\nemoji=\":rocket:\"\nmessage=\"New note\"\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, cfg
}

func runLegacyConfig(t *testing.T, binary, path, mode string, args ...string) json.RawMessage {
	t.Helper()
	out, diagnostic, err := runSlackBinary(t, binary, path, "http://127.0.0.1:1", "", append([]string{"--output=" + mode, "--color=never", "config"}, args...)...)
	if err != nil || diagnostic != "" || out == "" {
		t.Fatalf("legacy config command %s failed: %v", args[0], err)
	}
	if strings.Contains(out, "xoxb-test") || strings.Contains(out, "env:SLACK_TEST_TOKEN") {
		t.Fatal("configuration command disclosed credentials or references")
	}
	if mode != "json" {
		if mode == "compact" && !json.Valid([]byte(out)) {
			t.Fatal("compact configuration result is not JSON")
		}
		return json.RawMessage(out)
	}
	var frame struct {
		OK     bool                     `json:"ok"`
		Data   json.RawMessage          `json:"data"`
		Meta   struct{ Command string } `json:"meta"`
		Errors []json.RawMessage        `json:"errors"`
	}
	command := args[0]
	if command == "rm" || command == "remove" {
		command = "unset"
	}
	if err := json.Unmarshal([]byte(out), &frame); err != nil || !frame.OK || frame.Meta.Command != "config."+command || len(frame.Errors) != 0 {
		t.Fatal("legacy configuration result lost its success envelope")
	}
	return frame.Data
}

func assertLegacyValue(t *testing.T, mode string, data json.RawMessage, want string) {
	t.Helper()
	if mode == "human" {
		if want == "" {
			want = "(unset)"
		}
		if !strings.Contains(string(data), want) {
			t.Fatal("get did not report effective attribution value")
		}
		return
	}
	var result struct{ Value string }
	if err := json.Unmarshal(data, &result); err != nil || result.Value != want {
		t.Fatal("get did not report effective attribution value")
	}
}

func TestBinaryConfigReadsEffectiveLegacyAttribution(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, nested := range []bool{false, true} {
		name := "legacy"
		if nested {
			name = "nested"
		}
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(name+"/"+mode, func(t *testing.T) {
				path, cfg := legacyAttributionFixture(t, nested)
				prior, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				settings := cfg.Workspaces["default"].AgentSettings()
				for field, value := range map[string]string{"label": settings.Label, "emoji": settings.Emoji, "message": settings.Message} {
					for _, key := range []string{"attribution." + field, "agent_" + field} {
						assertLegacyValue(t, mode, runLegacyConfig(t, binary, path, mode, "get", "workspaces.default."+key), value)
					}
				}
				data := runLegacyConfig(t, binary, path, mode, "list")
				if mode == "human" {
					for _, value := range []string{settings.Label, settings.Emoji, settings.Message} {
						if !strings.Contains(string(data), value) {
							t.Fatal("list omitted an effective legacy attribution value")
						}
					}
				} else {
					var result struct {
						Settings []struct{ Key, Value string }
					}
					if err := json.Unmarshal(data, &result); err != nil {
						t.Fatal(err)
					}
					values := map[string]string{}
					for _, setting := range result.Settings {
						values[setting.Key] = setting.Value
					}
					for field, want := range map[string]string{"label": settings.Label, "emoji": settings.Emoji, "message": settings.Message} {
						if values["workspaces.default.attribution."+field] != want {
							t.Fatal("list omitted an effective legacy attribution value")
						}
					}
				}
				after, err := os.ReadFile(path)
				if err != nil || string(after) != string(prior) {
					t.Fatal("reading legacy preferences rewrote configuration")
				}
			})
		}
	}
}

func TestBinaryConfigAttributionRemovalDoesNotRestoreLegacyValues(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, nested := range []bool{false, true} {
		shape := "legacy"
		if nested {
			shape = "nested"
		}
		for i, field := range []string{"label", "emoji", "message"} {
			for _, action := range []string{"unset", "empty", "replace", "whitespace"} {
				for _, mode := range []string{"json", "compact", "human"} {
					t.Run(shape+"/"+field+"/"+action+"/"+mode, func(t *testing.T) {
						path, prior := legacyAttributionFixture(t, nested)
						profile := prior.Workspaces["default"]
						want := profile.AgentSettings()
						value := ""
						switch action {
						case "replace":
							value = "Replacement"
						case "whitespace":
							value = "  padded  "
						}
						key := "workspaces.default.attribution." + field
						var result json.RawMessage
						if action == "unset" {
							result = runLegacyConfig(t, binary, path, mode, []string{"unset", "rm", "remove"}[i], key)
						} else {
							result = runLegacyConfig(t, binary, path, mode, "set", key, value)
						}
						if mode != "human" {
							var data struct{ Path, Key, Value string }
							if err := json.Unmarshal(result, &data); err != nil || data.Path != path || data.Key != key || data.Value != value {
								t.Fatal("mutation result did not describe the requested attribution change")
							}
						}
						switch field {
						case "label":
							want.Label = value
						case "emoji":
							want.Emoji = value
						case "message":
							want.Message = value
						}
						loaded, err := config.LoadFile(path)
						if err != nil {
							t.Fatal(err)
						}
						after := loaded.Workspaces["default"]
						if after.AgentSettings() != want {
							t.Fatal("removed attribution value returned after reload or changed another setting")
						}
						if after.TokenRef != profile.TokenRef || after.TeamID != profile.TeamID || after.TokenType != profile.TokenType || after.DefaultChannel != profile.DefaultChannel || !reflect.DeepEqual(after.Aliases, profile.Aliases) || !reflect.DeepEqual(loaded.Workspaces["other"], prior.Workspaces["other"]) {
							t.Fatal("attribution mutation changed unrelated configuration")
						}
						if after.AgentLabel != "" || after.AgentEmoji != "" || after.AgentMessage != "" || after.AgentAttribution != nil {
							t.Fatal("saving retained legacy attribution fields")
						}
						assertLegacyValue(t, mode, runLegacyConfig(t, binary, path, mode, "get", key), value)
						assertLegacyValue(t, mode, runLegacyConfig(t, binary, path, mode, "get", "workspaces.default.agent_"+field), value)
					})
				}
			}
		}
	}
}
