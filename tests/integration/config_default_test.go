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

func defaultSelectorFixture(t *testing.T) (string, *config.Config) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := &config.Config{SchemaVersion: "1", DefaultWorkspace: "Primary", Workspaces: map[string]config.WorkspaceProfile{}}
	for _, name := range []string{"Primary", "Other", "s", "σ", "é", "İ", "i"} {
		cfg.Workspaces[name] = config.WorkspaceProfile{Name: name, TeamID: "T_SYNTHETIC", TokenType: config.TokenTypeBot, TokenRef: "env:SLICK_UNUSED_TOKEN", DefaultChannel: "C_KEEP", Aliases: map[string]string{"alerts": "C_ALERTS"}}
	}
	if err := config.SaveFile(path, cfg); err != nil {
		t.Fatal(err)
	}
	return path, cfg
}

func TestBinaryDefaultWorkspaceUsesCanonicalSelectors(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tc := range []struct{ name, selector, canonical string }{
		{"lower", "primary", "Primary"},
		{"upper", "OTHER", "Other"},
		{"padded", "  pRiMaRy\t", "Primary"},
		{"long s", "ſ", "s"},
		{"sigma", "ς", "σ"},
		{"accented", "É", "é"},
		{"dotted i", "İ", "İ"},
		{"latin i", "I", "i"},
	} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				path, prior := defaultSelectorFixture(t)
				data := runLegacyConfig(t, binary, path, mode, "set", "default_workspace", tc.selector)
				if mode == "human" {
					if !strings.Contains(string(data), "value="+tc.canonical) {
						t.Fatal("default-workspace result did not report canonical stored name")
					}
				} else {
					var result struct{ Key, Value string }
					if json.Unmarshal(data, &result) != nil || result.Key != "default_workspace" || result.Value != tc.canonical {
						t.Fatal("default-workspace result did not report canonical stored name")
					}
				}
				loaded, err := config.LoadFile(path)
				if err != nil || loaded.DefaultWorkspace != tc.canonical || !reflect.DeepEqual(loaded.Workspaces, prior.Workspaces) {
					t.Fatalf("default-workspace update lost canonical name or changed profile settings: %v", err)
				}
				assertLegacyValue(t, mode, runLegacyConfig(t, binary, path, mode, "get", "default_workspace"), tc.canonical)
				out, diagnostic, err := runSlackBinary(t, binary, path, "http://127.0.0.1:1", "", "--output="+mode, "--color=never", "workspace", "list")
				if err != nil || diagnostic != "" {
					t.Fatalf("new default did not work for the next process: %v", err)
				}
				if mode == "json" {
					var frame struct{ Meta struct{ Workspace string } }
					if json.Unmarshal([]byte(out), &frame) != nil || frame.Meta.Workspace != tc.canonical {
						t.Fatal("subsequent command selected a different default profile")
					}
				}
			})
		}
	}
}

func TestBinaryInvalidDefaultWorkspacePreservesState(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tc := range []struct{ name, value string }{
		{"empty", ""},
		{"blank", " \t "},
		{"unknown", "Missing"},
		{"normalization is distinct", "E\u0301"},
		{"different script", "а"},
	} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				path, _ := defaultSelectorFixture(t)
				prior, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				out, diagnostic, err := runSlackBinary(t, binary, path, "http://127.0.0.1:1", "", "--output="+mode, "--color=never", "config", "set", "default_workspace", tc.value)
				if exitCode(err) != 4 {
					t.Fatal("invalid default-workspace value did not fail validation")
				}
				if mode == "human" {
					if out != "" || diagnostic == "" {
						t.Fatal("invalid default used wrong human stream")
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
					if json.Unmarshal([]byte(out), &frame) != nil || frame.OK || string(frame.Data) != "null" || frame.Meta.Command != "config.set" || frame.Meta.Exit != 4 || diagnostic != "" {
						t.Fatal("invalid default lost common machine failure envelope")
					}
				}
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(after, prior) {
					t.Fatal("invalid default-workspace value changed configuration")
				}
			})
		}
	}
}
