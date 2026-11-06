//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func TestBinaryConfigTextRoundTripsThroughTOML(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tc := range []struct{ name, value string }{
		{"bell", "left\aright"},
		{"vertical tab", "left\vright"},
		{"delete", "left\x7fright"},
		{"literal escapes", `literal \a and \u0007`},
		{"newline", "line\nnext"},
		{"tab", "left\tright"},
		{"unicode", "café 日本語"},
		{"quotes", "quoted \"text\"\\path"},
	} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "config.toml")
				prior := configWriteFixture(t, path)
				key := "workspaces.default.attribution.message"
				runLegacyConfig(t, binary, path, mode, "set", key, tc.value)
				data := runLegacyConfig(t, binary, path, mode, "get", key)
				if mode != "human" {
					var result struct{ Key, Value string }
					if err := json.Unmarshal(data, &result); err != nil || result.Key != key || result.Value != tc.value {
						t.Fatal("machine output changed the configured text")
					}
				}
				loaded, err := config.LoadFile(path)
				if err != nil {
					t.Fatalf("configuration became unreadable after a successful update: %v", err)
				}
				profile := prior.Workspaces["default"]
				profile.Attribution.Message = tc.value
				prior.Workspaces["default"] = profile
				if !reflect.DeepEqual(loaded, prior) {
					t.Fatal("saving changed text or unrelated settings")
				}
			})
		}
	}
}

func TestBinaryConfigInvalidUTF8FailsWithoutWriting(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows argv uses UTF-16")
	}
	binary := buildSlackBinary(t)
	bad := "PRIVATE_INPUT_CANARY" + string([]byte{0xff})
	for _, target := range []struct{ name, long, short string }{
		{"set value", "", ""},
		{"set key", "", ""},
		{"init profile", "--profile", "-p"},
		{"init channel", "--default-channel", "-c"},
		{"init label", "--attribution-label", "-l"},
		{"init emoji", "--attribution-emoji", "-e"},
		{"init message", "--attribution-message", "-m"},
	} {
		for _, existing := range []bool{false, true} {
			if target.long == "" && !existing {
				continue
			}
			state := "missing"
			if existing {
				state = "existing"
			}
			flags := []string{target.long}
			if target.short != "" {
				flags = append(flags, target.short)
			}
			for _, flag := range flags {
				form := flag
				if form == "" {
					form = "args"
				}
				for _, mode := range []string{"json", "compact", "human"} {
					t.Run(target.name+"/"+state+"/"+form+"/"+mode, func(t *testing.T) {
						root := t.TempDir()
						path := filepath.Join(root, "nested", "config.toml")
						var prior []byte
						if existing {
							configWriteFixture(t, path)
							var err error
							prior, err = os.ReadFile(path)
							if err != nil {
								t.Fatal(err)
							}
						}
						args := []string{"set", "workspaces.default.attribution.message", bad}
						command := "config.set"
						if target.name == "set key" {
							args = []string{"set", bad, "ignored"}
						} else if target.long != "" {
							command = "config.init"
							args = []string{"init", "--force", flag, bad}
						}
						out, diagnostic, err := runSlackBinary(t, binary, path, "http://127.0.0.1:1", "", append([]string{"--output=" + mode, "--color=never", "config"}, args...)...)
						if exitCode(err) != 4 || !strings.Contains(out+diagnostic, "UTF-8") || strings.Contains(out+diagnostic, "PRIVATE_INPUT_CANARY") {
							t.Fatal("invalid UTF-8 did not fail validation safely")
						}
						if mode == "human" {
							if out != "" || diagnostic == "" {
								t.Fatal("invalid text used the wrong human stream")
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
							if json.Unmarshal([]byte(out), &frame) != nil || frame.OK || string(frame.Data) != "null" || frame.Meta.Command != command || frame.Meta.Exit != 4 || diagnostic != "" {
								t.Fatal("invalid text lost common machine failure envelope")
							}
						}
						if prior != nil {
							after, err := os.ReadFile(path)
							if err != nil || !bytes.Equal(after, prior) {
								t.Fatal("invalid text changed prior configuration")
							}
						} else if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
							t.Fatal("invalid text created config directories")
						}
					})
				}
			}
		}
	}
}
