package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func TestAuthLogoutReplacesCaseInsensitiveSavedDefault(t *testing.T) {
	t.Setenv("SLICK_CONFIG", filepath.Join(t.TempDir(), "unused.toml"))
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("SLACK_CLI_DEFAULT_WORKSPACE", "")
	for _, savedDefault := range []string{"default", "DEFAULT"} {
		t.Run(savedDefault, func(t *testing.T) {
			other := config.WorkspaceProfile{Name: "Alpha", DefaultChannel: "C_PRESERVED"}
			cfg := &config.Config{
				SchemaVersion: config.SchemaVersion, DefaultWorkspace: savedDefault,
				Workspaces: map[string]config.WorkspaceProfile{
					"default": {Name: "default", TeamID: "T_SYNTHETIC", TokenType: config.TokenTypeBot, TokenRef: "keychain:slack-cli/default"},
					"Alpha":   other,
				},
			}
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := config.SaveFile(path, cfg); err != nil {
				t.Fatal(err)
			}
			memory := config.NewMemoryCredentialStore()
			const payload = "synthetic-preserved-credential"
			if err := memory.Set("slack-cli", "default", payload); err != nil {
				t.Fatal(err)
			}
			store := &logoutReproStore{CredentialStore: memory}
			var out, diagnostic bytes.Buffer
			var resolutions, requests int
			root, runtime := NewRootCommandWithRuntime(WithConfig(cfg), WithConfigPath(path), WithCredentialStore(store), WithTTY(false), WithIO(strings.NewReader(""), &out, &diagnostic),
				WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
					resolutions++
					return "", errors.New("unexpected synthetic credential resolution")
				})), func(runtime *RootRuntime) {
					runtime.HTTPClient = &http.Client{Transport: logoutReproTransport(func(*http.Request) (*http.Response, error) {
						requests++
						return nil, errors.New("unexpected synthetic HTTP request")
					})}
				})
			t.Cleanup(func() {
				if runtime.CancelTimeout != nil {
					runtime.CancelTimeout()
				}
			})
			root.SetArgs([]string{"auth", "logout", "default", "--keep-token", "--output=json", "--color=never"})
			if err := root.ExecuteContext(t.Context()); err != nil {
				t.Fatalf("logout failed to replace a valid case-insensitive default: %v", err)
			}
			loaded, err := config.LoadFile(path)
			if err != nil || loaded.DefaultWorkspace != "Alpha" || len(loaded.Workspaces) != 1 || !reflect.DeepEqual(loaded.Workspaces["Alpha"], other) || !reflect.DeepEqual(cfg, loaded) {
				t.Fatal("logout did not preserve the remaining profile and publish the replacement default")
			}
			var frame struct {
				OK   bool
				Data struct{ Workspace string }
			}
			if json.Unmarshal(out.Bytes(), &frame) != nil || !frame.OK || frame.Data.Workspace != "default" {
				t.Fatal("logout lost its canonical success receipt")
			}
			kept, err := memory.Get("slack-cli", "default")
			if err != nil || kept != payload || resolutions != 0 || requests != 0 || store.gets != 0 || store.deletes != 0 {
				t.Fatal("keep-token accessed a provider, requested Slack or changed the protected credential")
			}
		})
	}
}

type logoutReproStore struct {
	config.CredentialStore
	gets, deletes int
}

func (s *logoutReproStore) Get(service, account string) (string, error) {
	s.gets++
	return s.CredentialStore.Get(service, account)
}

func (s *logoutReproStore) Delete(service, account string) error {
	s.deletes++
	return s.CredentialStore.Delete(service, account)
}

type logoutReproTransport func(*http.Request) (*http.Response, error)

func (f logoutReproTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
