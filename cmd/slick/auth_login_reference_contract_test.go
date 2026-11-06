package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	"github.com/matcra587/slack-cli/internal/config"
)

func TestAuthLoginRequiresForceToReplaceOtherCredentialReferences(t *testing.T) {
	for _, tc := range []struct{ name, ref string }{
		{"environment", "env:SLICK_UNUSED_EXISTING"},
		{"secret manager", "op://Synthetic/Fixture/credential"},
		{"other keychain entry", "keychain:slack-cli/external"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := authTestConfig()
			profile := cfg.Workspaces["other"]
			profile.TokenRef = tc.ref
			profile.Aliases = map[string]string{"alerts": "C_KEEP"}
			cfg.Workspaces["other"] = profile
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := config.SaveFile(path, cfg); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			store := &authLoginStore{CredentialStore: config.NewMemoryCredentialStore()}
			const external = `{"access_token":"xoxp-synthetic-existing"}`
			if err := store.CredentialStore.Set("slack-cli", "external", external); err != nil {
				t.Fatal(err)
			}
			var calls, resolutions atomic.Int32
			client := &http.Client{Transport: authStatusTransport(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Method != "POST" || r.URL.Path != "/api/auth.test" || r.PostForm.Get("token") != "xoxp-synthetic-replacement" || len(r.PostForm) != 1 {
					t.Error("login changed validation request or used existing credentials")
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":true,"team_id":"T_NEW","team":"New","user_id":"U_NEW"}`)), Request: r}, nil
			})}
			run := func(force bool) (string, string, error) {
				args := []string{"auth", "login", "--workspace-name=OTHER", "--token-stdin", "--output=json", "--color=never"}
				if force {
					args = append(args, "--force")
				}
				return executeAuthRootWithOptions(t, cfg, path, store, "http://127.0.0.1:1", strings.NewReader("xoxp-synthetic-replacement\n"), false, args,
					func(runtime *RootRuntime) { runtime.HTTPClient = client },
					WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
						resolutions.Add(1)
						return "", errors.New("synthetic-provider-must-not-run")
					})))
			}
			out, diagnostic, runErr := run(false)
			failure, ok := errors.AsType[clioutput.CommandError](runErr)
			if !ok || failure.CLIError.ExitCode != 4 || !strings.Contains(out, "--force") || diagnostic != "" {
				t.Fatal("login replaced a configured credential reference without force")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) || cfg.Workspaces["other"].TokenRef != tc.ref || store.sets != 0 {
				t.Fatal("rejected login changed prior config, reference or credentials")
			}
			out, diagnostic, runErr = run(true)
			if runErr != nil || diagnostic != "" || store.sets != 1 || calls.Load() != 2 || resolutions.Load() != 0 {
				t.Fatal("explicit forced retry failed, replayed or resolved the external provider")
			}
			loaded, err := config.LoadFile(path)
			if err != nil || loaded.Workspaces["other"].TokenRef != "keychain:slack-cli/other" || loaded.Workspaces["other"].TeamID != "T_NEW" || !reflect.DeepEqual(loaded.Workspaces["other"].Aliases, profile.Aliases) || loaded.DefaultWorkspace != "default" || !reflect.DeepEqual(loaded.Workspaces["default"], cfg.Workspaces["default"]) {
				t.Fatal("forced retry lost unrelated settings or did not save the managed reference")
			}
			if value, err := store.Get("slack-cli", "external"); err != nil || value != external {
				t.Fatal("forced retry changed the previous credential source")
			}
			stored, err := store.Get("slack-cli", "other")
			if err != nil {
				t.Fatal("forced retry did not store replacement credentials")
			}
			payload, err := config.DecodeCredential(stored)
			if err != nil || payload.AccessToken != "xoxp-synthetic-replacement" {
				t.Fatal("forced retry stored the wrong credential")
			}
			after, err = os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out+diagnostic+string(after), "xoxp-synthetic-replacement") {
				t.Fatal("login exposed replacement credentials")
			}
		})
	}
}
