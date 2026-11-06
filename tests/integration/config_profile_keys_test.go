//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func TestBinaryConfigKeysSupportDottedProfiles(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, mode := range []string{"json", "compact", "human"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			cfg := &config.Config{SchemaVersion: "1", DefaultWorkspace: "team", Workspaces: map[string]config.WorkspaceProfile{}}
			for _, name := range []string{"team", "team.prod", "team.prod.attribution"} {
				cfg.Workspaces[name] = config.WorkspaceProfile{
					Name: name, TeamID: "T_SYNTHETIC", TokenType: config.TokenTypeBot,
					TokenRef: "env:SLICK_SYNTHETIC_UNUSED", DefaultChannel: "C_KEEP",
					Aliases: map[string]string{"alerts": "C_ALERTS"},
				}
			}
			if err := config.SaveFile(path, cfg); err != nil {
				t.Fatal(err)
			}
			run := func(want int, args ...string) string {
				t.Helper()
				args = append([]string{"--output=" + mode, "--color=never", "config"}, args...)
				out, diagnostic, err := runSlackBinary(t, binary, path, "http://127.0.0.1:1", "", args...)
				if exitCode(err) != want {
					t.Fatalf("config key command %v exit=%d, want %d", args[3:], exitCode(err), want)
				}
				if mode == "human" {
					if want != 0 && (out != "" || diagnostic == "") || want == 0 && (out == "" || diagnostic != "") {
						t.Fatal("config key command used wrong human stream")
					}
				} else if mode == "compact" && want == 0 {
					if !json.Valid([]byte(out)) || diagnostic != "" {
						t.Fatal("compact config success lost its data record")
					}
				} else {
					var frame struct {
						OK   bool            `json:"ok"`
						Data json.RawMessage `json:"data"`
						Meta struct {
							Command string `json:"command"`
							Exit    int    `json:"exit_code"`
						} `json:"meta"`
					}
					command := args[3]
					if command == "rm" || command == "remove" {
						command = "unset"
					}
					if err := json.Unmarshal([]byte(out), &frame); err != nil || frame.OK != (want == 0) || frame.Meta.Command != "config."+command || frame.Meta.Exit != want || diagnostic != "" {
						t.Fatal("config key command lost machine envelope")
					}
					if want != 0 && string(frame.Data) != "null" {
						t.Fatal("config failure returned success data")
					}
				}
				if mode == "human" && want != 0 {
					return diagnostic
				}
				return out
			}
			listed := run(0, "list")
			for _, profile := range []string{"team.prod", "team.prod.attribution"} {
				for i, field := range []string{"default_channel", "attribution.label", "attribution.enabled"} {
					key := "workspaces." + profile + "." + field
					if !strings.Contains(listed, key) {
						t.Fatal("list omitted a dotted profile setting")
					}
					value := "C_UPDATED"
					switch field {
					case "attribution.label":
						value = "Release review"
					case "attribution.enabled":
						value = "false"
					}
					run(0, "set", key, value)
					out := run(0, "get", key)
					if !strings.Contains(out, value) {
						t.Fatal("get did not return the updated dotted profile setting")
					}
					loaded, err := config.LoadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					for name, prior := range cfg.Workspaces {
						if name != profile && !reflect.DeepEqual(loaded.Workspaces[name], prior) {
							t.Fatal("editing dotted profile changed a different profile")
						}
					}
					before := loaded.Workspaces[profile]
					if before.TokenRef != cfg.Workspaces[profile].TokenRef || !reflect.DeepEqual(before.Aliases, cfg.Workspaces[profile].Aliases) {
						t.Fatal("editing preference changed authentication or aliases")
					}
					run(0, []string{"unset", "rm", "remove"}[i], key)
					out = run(0, "get", key)
					if mode != "human" {
						data := []byte(out)
						var frame struct {
							Data json.RawMessage `json:"data"`
						}
						if mode == "json" {
							if err := json.Unmarshal(data, &frame); err != nil {
								t.Fatal(err)
							}
							data = frame.Data
						}
						var got struct{ Value string }
						if err := json.Unmarshal(data, &got); err != nil || got.Value != "" {
							t.Fatal("unset left previous value on dotted profile")
						}
					} else if !strings.Contains(out, "(unset)") {
						t.Fatal("unset did not display an empty setting")
					}
					cfg, err = config.LoadFile(path)
					if err != nil {
						t.Fatal(err)
					}
				}
				prior, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				for _, args := range [][]string{
					{"set", "workspaces." + profile + ".token_ref", "env:REPLACEMENT"},
					{"unset", "workspaces." + profile + ".team_id"},
					{"set", "workspaces." + profile + ".attribution.enabled", "invalid"},
				} {
					failure := run(4, args...)
					if strings.HasSuffix(args[1], ".attribution.enabled") {
						if !strings.Contains(failure, "must be true or false") {
							t.Fatal("invalid boolean failed for a different reason")
						}
					} else if !strings.Contains(failure, "auth-owned") {
						t.Fatal("auth field failed for a different reason")
					}
					after, err := os.ReadFile(path)
					if err != nil || !bytes.Equal(prior, after) {
						t.Fatal("rejected dotted profile change altered configuration")
					}
				}
			}
		})
	}
}
