package config_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func codecConfig() *config.Config {
	disabled := false
	return &config.Config{SchemaVersion: "1", DefaultWorkspace: "default", Workspaces: map[string]config.WorkspaceProfile{
		"default": {
			Name: "default", TeamID: "T_SYNTHETIC", TeamName: "Example", TokenType: config.TokenTypeBot,
			TokenRef: "env:SLICK_UNUSED_TOKEN", DefaultChannel: "C_KEEP", RateLimitTier: "tier3",
			Attribution: config.AttributionConfig{Enabled: &disabled, Label: "Review", Emoji: ":eyes:", Message: "Release note"},
			Aliases:     map[string]string{"alerts": "C_ALERTS"},
		},
	}}
}

func TestSaveFileQuotedKeysAndValuesPreserveState(t *testing.T) {
	cfg := codecConfig()
	profile := cfg.Workspaces["default"]
	profile.Name = "profile.with spaces"
	profile.TeamName = "Workspace \"sample\"\\name"
	profile.Attribution.Message = "café 日本語\nLiteral \\a and \\u0007"
	profile.Aliases = map[string]string{"": "C_EMPTY", "ops.with spaces": "C_DOT", "line\nbreak": "C_LINE", "quoted\"name": "C_QUOTE", "slash\\name": "C_SLASH", "日本語": "C_UNICODE"}
	cfg.DefaultWorkspace = profile.Name
	cfg.Workspaces = map[string]config.WorkspaceProfile{profile.Name: profile}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := config.SaveFile(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.LoadFile(path)
	if err != nil || !reflect.DeepEqual(loaded, cfg) {
		t.Fatalf("saving changed quoted keys or values: %v", err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.SaveFile(path, loaded); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatal("repeated serialization changed the configuration layout")
	}
}

func TestSaveFileControlCharactersRoundTrip(t *testing.T) {
	for _, control := range append([]byte{127}, []byte("\x00\x01\x02\x03\x04\x05\x06\x07\x08\x09\x0a\x0b\x0c\x0d\x0e\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f")...) {
		t.Run(fmt.Sprintf("U%04X", control), func(t *testing.T) {
			cfg := codecConfig()
			profile := cfg.Workspaces["default"]
			profile.Name = "profile" + string(control)
			profile.TeamName = "team" + string(control)
			profile.Attribution.Message = "left" + string(control) + "right"
			profile.Aliases = map[string]string{"alias" + string(control): "C_ALIAS"}
			cfg.DefaultWorkspace = profile.Name
			cfg.Workspaces = map[string]config.WorkspaceProfile{profile.Name: profile}
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := config.SaveFile(path, cfg); err != nil {
				t.Fatal(err)
			}
			loaded, err := config.LoadFile(path)
			if err != nil || !reflect.DeepEqual(loaded, cfg) {
				t.Fatalf("saved control character did not round-trip as TOML: %v", err)
			}
		})
	}
}

func TestSaveFileInvalidUTF8PreservesPriorState(t *testing.T) {
	for _, field := range []string{"value", "profile", "alias key", "alias value"} {
		for _, existing := range []bool{false, true} {
			state := "missing"
			if existing {
				state = "existing"
			}
			t.Run(field+"/"+state, func(t *testing.T) {
				root := t.TempDir()
				path := filepath.Join(root, "nested", "config.toml")
				prior := []byte("prior configuration")
				if existing {
					if err := os.Mkdir(filepath.Dir(path), 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, prior, 0o600); err != nil {
						t.Fatal(err)
					}
				}
				cfg := codecConfig()
				profile := cfg.Workspaces["default"]
				bad := "synthetic" + string([]byte{0xff})
				switch field {
				case "value":
					profile.Attribution.Message = bad
				case "profile":
					cfg.Workspaces[bad] = config.WorkspaceProfile{Name: bad}
				case "alias key":
					profile.Aliases[bad] = "C_ALIAS"
				case "alias value":
					profile.Aliases["alerts"] = bad
				}
				cfg.Workspaces["default"] = profile
				err := config.SaveFile(path, cfg)
				if err == nil || !strings.Contains(err.Error(), "UTF-8") || strings.Contains(err.Error(), "synthetic") {
					t.Fatal("invalid UTF-8 was accepted or disclosed in diagnostics")
				}
				if existing {
					after, err := os.ReadFile(path)
					if err != nil || !bytes.Equal(after, prior) {
						t.Fatal("invalid input damaged prior configuration")
					}
				} else if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
					t.Fatal("invalid input created a configuration directory")
				}
			})
		}
	}
}

func TestSaveFileNeverPersistsReadOnlyLegacyCredential(t *testing.T) {
	cfg := codecConfig()
	profile := cfg.Workspaces["default"]
	profile.LegacyToken = "xoxb-synthetic-must-not-be-persisted"
	cfg.Workspaces["default"] = profile
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := config.SaveFile(path, cfg); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil || bytes.Contains(body, []byte(profile.LegacyToken)) || bytes.Contains(body, []byte("token =")) {
		t.Fatal("read-only legacy credential entered persisted state")
	}
	if cfg.Workspaces["default"].LegacyToken != profile.LegacyToken {
		t.Fatal("saving modified the caller's credential state")
	}
}
