package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func TestAuthLogoutWarnsWhenConfiguredTokenCannotBeResolved(t *testing.T) {
	for _, tc := range []struct {
		name                             string
		configured, providerError, empty bool
	}{
		{"provider failure", true, true, false},
		{"empty token", true, false, true},
		{"healthy", true, false, false},
		{"local profile", false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const private = "synthetic-private-logout-provider-detail"
			const token = "xoxb-synthetic-logout-warning"
			path := filepath.Join(t.TempDir(), "config.toml")
			t.Setenv("SLICK_CONFIG", path)
			t.Setenv("SLACK_CLI_DEFAULT_WORKSPACE", "")
			t.Setenv("SLACK_CLI_TOKEN", "")
			t.Setenv("SLACK_CLI_TOKEN_DEFAULT", "")
			profile := config.WorkspaceProfile{Name: "default", DefaultChannel: "C_KEEP"}
			if tc.configured {
				profile.TokenRef = "keychain:slack-cli/default"
				profile.TokenType = config.TokenTypeBot
				profile.TeamID = "T_SYNTHETIC"
			}
			cfg := &config.Config{SchemaVersion: config.SchemaVersion, DefaultWorkspace: "default", Workspaces: map[string]config.WorkspaceProfile{"default": profile}}
			if err := config.SaveFile(path, cfg); err != nil {
				t.Fatal(err)
			}
			memory := config.NewMemoryCredentialStore()
			if err := memory.Set("slack-cli", "default", token); err != nil {
				t.Fatal(err)
			}
			resolutions, requests := 0, 0
			var out, diagnostic bytes.Buffer
			options := []RootOption{WithConfig(cfg), WithConfigPath(path), WithCredentialStore(memory), WithTTY(false), WithSlackBaseURL("https://fixture.invalid"), WithIO(strings.NewReader(""), &out, &diagnostic), WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
				resolutions++
				if tc.providerError {
					return "", errors.New(private)
				}
				if tc.empty {
					return "", nil
				}
				return token, nil
			})), func(runtime *RootRuntime) {
				runtime.HTTPClient = &http.Client{Transport: logoutWarningTransport(func(r *http.Request) (*http.Response, error) {
					requests++
					if tc.name != "healthy" || requests != 1 || r.Method != http.MethodPost || r.URL.Path != "/api/auth.revoke" {
						t.Fatal("logout attempted an unexpected revocation")
					}
					if err := r.ParseForm(); err != nil {
						t.Fatal(err)
					}
					if r.PostForm.Get("token") != token || len(r.PostForm) != 1 {
						t.Fatal("logout changed revocation authentication")
					}
					return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":true,"revoked":true}`)), Request: r}, nil
				})}
			}}
			run := func() {
				t.Helper()
				out.Reset()
				diagnostic.Reset()
				root, runtime := NewRootCommandWithRuntime(options...)
				defer func() {
					if runtime.CancelTimeout != nil {
						runtime.CancelTimeout()
					}
				}()
				root.SetArgs([]string{"auth", "logout", "default", "--output=json", "--color=never", "--no-throttle"})
				if err := root.ExecuteContext(t.Context()); err != nil {
					t.Fatalf("local logout failed: %v", err)
				}
				var frame struct {
					OK   bool
					Data struct{ Authenticated bool }
				}
				if json.Unmarshal(out.Bytes(), &frame) != nil || !frame.OK || frame.Data.Authenticated {
					t.Fatal("logout lost its successful cleanup receipt")
				}
				if strings.Contains(out.String()+diagnostic.String(), private) || strings.Contains(out.String()+diagnostic.String(), token) {
					t.Fatal("logout disclosed credential or provider detail")
				}
			}
			run()
			warn := tc.providerError || tc.empty
			if strings.Contains(diagnostic.String(), "could not resolve token for revocation; proceeding with local cleanup") != warn {
				t.Error("logout did not accurately warn about skipped revocation")
			}
			wantResolutions, wantRequests := 0, 0
			if tc.configured {
				wantResolutions = 1
			}
			if tc.name == "healthy" {
				wantRequests = 1
			}
			if resolutions != wantResolutions || requests != wantRequests {
				t.Error("logout resolved an unconfigured credential or requested an unexpected mutation")
			}
			if _, err := memory.Get("slack-cli", "default"); !errors.Is(err, config.ErrCredentialNotFound) {
				t.Fatal("logout did not remove the local credential")
			}
			loaded, err := config.LoadFile(path)
			if err != nil || !reflect.DeepEqual(loaded, cfg) || loaded.Workspaces["default"].DefaultChannel != "C_KEEP" || loaded.Workspaces["default"].TokenRef != "" {
				t.Fatal("logout did not preserve preferences and clear authentication")
			}
			run()
			if diagnostic.Len() != 0 || resolutions != wantResolutions || requests != wantRequests {
				t.Error("repeated logout warned or retried revocation for a local profile")
			}
		})
	}
}

type logoutWarningTransport func(*http.Request) (*http.Response, error)

func (f logoutWarningTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
