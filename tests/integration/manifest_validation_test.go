//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBinaryManifestRejectsInvalidDisplayAndCallback(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tc := range []struct{ name, flag, value string }{
		{"name too long", "--name", strings.Repeat("a", 36)},
		{"unicode name too long", "--name", strings.Repeat("é", 36)},
		{"description too long", "--description", strings.Repeat("a", 141)},
		{"long description too short", "--long-description", strings.Repeat("a", 174)},
		{"long description too long", "--long-description", strings.Repeat("a", 4001)},
		{"invalid background", "--background-color", "synthetic-manifest-private-value"},
		{"wrong hex length", "--background-color", "#1234"},
		{"non hex background", "--background-color", "#ggg"},
		{"invalid port", "--callback-port", "synthetic-manifest-private-value"},
		{"negative port", "--callback-port", "-1"},
		{"port too large", "--callback-port", "65536"},
		{"port overflow", "--callback-port", "18446744073709551616"},
	} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				run := manifestFixture(t, binary)
				out, diagnostic, code := run("53221", tc.flag+"="+tc.value, "--output="+mode)
				if code != 4 {
					t.Fatalf("invalid manifest input emitted success: exit=%d", code)
				}
				if strings.Contains(out+diagnostic, "synthetic-manifest-private-value") {
					t.Fatal("manifest validation echoed unreviewed input")
				}
				if mode == "human" {
					if out != "" || diagnostic == "" {
						t.Fatal("manifest validation used wrong human streams")
					}
				} else {
					var frame map[string]json.RawMessage
					var failure struct {
						Meta struct {
							Exit int `json:"exit_code"`
						}
						Errors []struct{ Type string }
					}
					if json.Unmarshal([]byte(out), &frame) != nil || len(frame) != 4 || string(frame["ok"]) != "false" || string(frame["data"]) != "null" || json.Unmarshal([]byte(out), &failure) != nil || failure.Meta.Exit != 4 || len(failure.Errors) != 1 || failure.Errors[0].Type != "validation_error" || diagnostic != "" {
						t.Fatal("manifest validation lost common failure envelope")
					}
				}
				out, diagnostic, code = run("53221", "--output="+mode)
				if code != 0 || diagnostic != "" {
					t.Fatal("manifest failed to recover after invalid input was removed")
				}
				data := manifestJSON(t, out, mode)
				if data.Display.Name != "slack-cli" || data.Display.LongDescription != "" || data.OAuth.Redirects[0] != "http://localhost:53221/callback" {
					t.Fatal("removed manifest arguments persisted in subsequent output")
				}
			})
		}
	}
}

func TestBinaryManifestPreservesDisplayBoundaries(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tc := range []struct{ name, flag, value string }{
		{"name max", "--name", strings.Repeat("é", 35)},
		{"description max", "--description", strings.Repeat("é", 140)},
		{"long description min", "--long-description", strings.Repeat("é", 175)},
		{"long description max", "--long-description", strings.Repeat("é", 4000)},
		{"empty description", "--description", ""},
		{"empty long description", "--long-description", " "},
		{"empty background", "--background-color", ""},
		{"short hex", "--background-color", "#aBc"},
		{"long hex", "--background-color", "#aBc123"},
		{"maximum port", "--callback-port", "65535"},
		{"ephemeral port", "--callback-port", "0"},
	} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				run := manifestFixture(t, binary)
				out, diagnostic, code := run("53221", tc.flag+"="+tc.value, "--output="+mode)
				if code != 0 || diagnostic != "" {
					t.Fatalf("valid manifest boundary rejected: exit=%d", code)
				}
				got := manifestJSON(t, out, mode)
				value := strings.TrimSpace(tc.value)
				switch tc.flag {
				case "--name":
					if got.Display.Name != value {
						t.Fatal("manifest altered valid name")
					}
				case "--description":
					if got.Display.Description != value {
						t.Fatal("manifest altered valid description")
					}
				case "--long-description":
					if got.Display.LongDescription != value {
						t.Fatal("manifest altered valid long description")
					}
				case "--background-color":
					if got.Display.Background != value {
						t.Fatal("manifest altered valid background")
					}
				case "--callback-port":
					url := got.OAuth.Redirects[0]
					if value == "0" {
						if url == "http://localhost:0/callback" || !strings.HasPrefix(url, "http://localhost:") || !strings.HasSuffix(url, "/callback") {
							t.Fatal("manifest lost automatic callback port allocation")
						}
					} else if url != "http://localhost:65535/callback" {
						t.Fatal("manifest altered valid callback port")
					}
				}
			})
		}
	}
}

func TestBinaryManifestCallbackEnvironmentAndExplicitURL(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, mode := range []string{"json", "compact", "human"} {
		t.Run(mode, func(t *testing.T) {
			run := manifestFixture(t, binary)
			_, _, code := run("invalid", "--output="+mode)
			if code != 4 {
				t.Fatal("invalid callback environment emitted success")
			}
			out, diagnostic, code := run("invalid", "--callback-port=invalid", "--redirect-url=https://example.invalid/oauth", "--output="+mode)
			if code != 0 || diagnostic != "" || manifestJSON(t, out, mode).OAuth.Redirects[0] != "https://example.invalid/oauth" {
				t.Fatal("explicit redirect URL lost precedence over unused callback values")
			}
			out, diagnostic, code = run("0", "--output="+mode)
			if code != 0 || diagnostic != "" || manifestJSON(t, out, mode).OAuth.Redirects[0] == "http://localhost:0/callback" {
				t.Fatal("callback environment did not recover with automatic allocation")
			}
		})
	}
}

func TestBinaryManifestPreservesLongAndShortOverrides(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, spelling := range []string{"long", "short"} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(spelling+"/"+mode, func(t *testing.T) {
				run := manifestFixture(t, binary)
				flags := []string{"--name", "--description", "--long-description", "--preset", "--type", "--background-color", "--bot-scope", "--user-scope", "--redirect-url", "--callback-port", "--format"}
				if spelling == "short" {
					flags = []string{"-N", "-d", "-L", "-p", "-t", "-B", "-S", "-U", "-r", "-C", "-f"}
				}
				values := []string{"Release Assistant", "Release notes", strings.Repeat("é", 175), "readonly", "both", "#abc", "chat:write", "users:read", "https://example.invalid/oauth", "invalid", "json"}
				args := []string{"--output=" + mode}
				for i, flag := range flags {
					args = append(args, flag, values[i])
				}
				out, diagnostic, code := run("invalid", args...)
				if code != 0 || diagnostic != "" {
					t.Fatal("valid manifest overrides failed")
				}
				got := manifestJSON(t, out, mode)
				if got.Display.Name != "Release Assistant" || got.Display.Description != "Release notes" || got.Display.LongDescription != values[2] || got.Display.Background != "#abc" || got.OAuth.Redirects[0] != values[8] || len(got.OAuth.Scopes.Bot) != 1 || got.OAuth.Scopes.Bot[0] != "chat:write" || len(got.OAuth.Scopes.User) != 1 || got.OAuth.Scopes.User[0] != "users:read" || !got.OAuth.PKCE || !got.Settings.Rotation {
					t.Fatal("manifest lost an explicit flag value or scope override")
				}
				out, diagnostic, code = run("53221", "--output="+mode)
				if code != 0 || diagnostic != "" {
					t.Fatal("manifest defaults failed after optional arguments were removed")
				}
				got = manifestJSON(t, out, mode)
				if got.Display.Name != "slack-cli" || got.Display.LongDescription != "" || got.Display.Background != "#4A154B" || got.OAuth.Redirects[0] != "http://localhost:53221/callback" || len(got.OAuth.Scopes.Bot) != 0 || len(got.OAuth.Scopes.User) < 2 {
					t.Fatal("removed manifest overrides did not restore defaults")
				}
			})
		}
	}
}

type manifestDocument struct {
	Display struct {
		Name            string `json:"name"`
		Description     string `json:"description"`
		LongDescription string `json:"long_description"`
		Background      string `json:"background_color"`
	} `json:"display_information"`
	OAuth struct {
		Redirects []string `json:"redirect_urls"`
		PKCE      bool     `json:"pkce_enabled"`
		Scopes    struct {
			Bot  []string `json:"bot"`
			User []string `json:"user"`
		} `json:"scopes"`
	} `json:"oauth_config"`
	Settings struct {
		Rotation bool `json:"token_rotation_enabled"`
	} `json:"settings"`
}

func manifestJSON(t *testing.T, out, mode string) manifestDocument {
	t.Helper()
	data := json.RawMessage(out)
	if mode == "json" {
		var frame struct {
			OK   bool
			Data json.RawMessage
		}
		if json.Unmarshal(data, &frame) != nil || !frame.OK {
			t.Fatal("manifest lost success envelope")
		}
		data = frame.Data
	}
	if mode != "human" {
		var payload struct{ Manifest json.RawMessage }
		if json.Unmarshal(data, &payload) != nil {
			t.Fatal("manifest payload is not JSON")
		}
		data = payload.Manifest
	}
	var doc manifestDocument
	if json.Unmarshal(data, &doc) != nil || len(doc.OAuth.Redirects) != 1 {
		t.Fatal("manifest did not contain an importable document")
	}
	return doc
}

func manifestFixture(t *testing.T, binary string) func(string, ...string) (string, string, int) {
	t.Helper()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); w.WriteHeader(503) }))
	t.Cleanup(server.Close)
	path := filepath.Join(t.TempDir(), "config.toml")
	prior := []byte("schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"TFIXTURE\"\ntoken_type=\"user\"\ntoken=\"env:SLICK_MANIFEST_UNUSED_TOKEN\"\n[workspaces.fixture.aliases]\nops=\"C123\"\n")
	if err := os.WriteFile(path, prior, 0o600); err != nil {
		t.Fatal(err)
	}
	return func(port string, args ...string) (string, string, int) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, append([]string{"manifest", "template", "--color=never"}, args...)...)
		cmd.Env = append(isolatedRuntimeEnv(t), "SLICK_CONFIG="+path, "SLACK_CLI_BASE_URL="+server.URL, "SLACK_CLI_CALLBACK_PORT="+port)
		var out, diagnostic strings.Builder
		cmd.Stdout, cmd.Stderr = &out, &diagnostic
		err := cmd.Run()
		after, readErr := os.ReadFile(path)
		if ctx.Err() != nil || requests.Load() != 0 || readErr != nil || string(after) != string(prior) {
			t.Fatal("manifest accessed Slack, changed prior settings or exceeded its deadline")
		}
		return out.String(), diagnostic.String(), exitCode(err)
	}
}
