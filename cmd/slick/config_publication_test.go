//go:build unix

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	"github.com/matcra587/slack-cli/internal/config"
)

func TestConfigPublicationFailureKeepsRuntimeAndCredentialsAligned(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires Unix directory permission enforcement")
	}
	for _, tc := range []struct {
		name   string
		args   []string
		update func(*config.Config)
		login  bool
	}{
		{"new login", []string{"auth", "login", "--workspace-name=New", "--token-stdin"}, func(c *config.Config) {
			c.Workspaces["New"] = config.WorkspaceProfile{Name: "New", TeamID: "T_NEW", TeamName: "New Team", TokenType: config.TokenTypeUser, TokenRef: "keychain:slack-cli/New"}
		}, true},
		{"replace login", []string{"auth", "login", "--workspace-name=default", "--token-stdin", "--force"}, func(c *config.Config) {
			p := c.Workspaces["default"]
			p.TeamID = "T_NEW"
			p.TeamName = "New Team"
			p.TokenType = config.TokenTypeUser
			c.Workspaces["default"] = p
		}, true},
		{"switch", []string{"auth", "switch", "other"}, func(c *config.Config) { c.DefaultWorkspace = "other" }, false},
		{"logout", []string{"auth", "logout", "default"}, func(c *config.Config) {
			p := c.Workspaces["default"]
			p.TeamID = ""
			p.TeamName = ""
			p.TokenType = ""
			p.TokenRef = ""
			c.Workspaces["default"] = p
		}, false},
		{"init", []string{"config", "init", "--profile=Fresh", "--force"}, func(c *config.Config) {
			c.DefaultWorkspace = "Fresh"
			enabled := true
			c.Workspaces = map[string]config.WorkspaceProfile{"Fresh": {Name: "Fresh", Attribution: config.AttributionConfig{Enabled: &enabled}}}
		}, false},
		{"set", []string{"config", "set", "workspaces.default.default_channel", "C_NEXT"}, func(c *config.Config) {
			p := c.Workspaces["default"]
			p.DefaultChannel = "C_NEXT"
			c.Workspaces["default"] = p
		}, false},
		{"unset", []string{"config", "unset", "workspaces.default.default_channel"}, func(c *config.Config) {
			p := c.Workspaces["default"]
			p.DefaultChannel = ""
			c.Workspaces["default"] = p
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.toml")
			t.Setenv("SLICK_CONFIG", path)
			t.Setenv("SLACK_CLI_DEFAULT_WORKSPACE", "")
			cfg := &config.Config{SchemaVersion: config.SchemaVersion, DefaultWorkspace: "default", Workspaces: map[string]config.WorkspaceProfile{
				"default": {Name: "default", TeamID: "T_OLD", TokenType: config.TokenTypeBot, TokenRef: "keychain:slack-cli/default", DefaultChannel: "C_DEFAULT", Aliases: map[string]string{"release": "C_KEEP"}},
				"other":   {Name: "other", DefaultChannel: "C_OTHER"},
			}}
			if err := config.SaveFile(path, cfg); err != nil {
				t.Fatal(err)
			}
			want, err := config.LoadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			tc.update(want)
			memory := config.NewMemoryCredentialStore()
			const oldToken = "xoxb-synthetic-publication-old"
			const newToken = "xoxp-synthetic-publication-new"
			if err := memory.Set("slack-cli", "default", oldToken); err != nil {
				t.Fatal(err)
			}
			store := &publicationStore{CredentialStore: memory}
			var out, diagnostic bytes.Buffer
			requests, resolutions := 0, 0
			root, runtime := NewRootCommandWithRuntime(WithConfig(cfg), WithConfigPath(path), WithCredentialStore(store), WithTTY(false), WithSlackBaseURL("https://fixture.invalid"), WithIO(strings.NewReader(newToken+"\n"), &out, &diagnostic), WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
				resolutions++
				return oldToken, nil
			})), func(runtime *RootRuntime) {
				runtime.HTTPClient = &http.Client{Transport: publicationTransport(func(r *http.Request) (*http.Response, error) {
					requests++
					if !tc.login || requests != 1 || r.Method != http.MethodPost || r.URL.Path != "/api/auth.test" {
						t.Fatal("unexpected request during config publication")
					}
					if err := r.ParseForm(); err != nil {
						t.Fatal(err)
					}
					if len(r.PostForm) != 1 || r.PostForm.Get("token") != newToken {
						t.Fatal("login changed validation authentication")
					}
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":true,"team_id":"T_NEW","team":"New Team","user_id":"U_SYNTHETIC","unreviewed_secret":"synthetic-unreviewed-publication"}`)), Request: r}, nil
				})}
			})
			defer func() {
				if runtime.CancelTimeout != nil {
					runtime.CancelTimeout()
				}
			}()
			if err := os.Chmod(dir, 0o300); err != nil { //nolint:gosec // Synthetic write/search-only directory forces a post-rename sync failure.
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // Restore private fixture directory access for cleanup.
			root.SetArgs(append(append([]string{}, tc.args...), "--output=json", "--color=never", "--no-throttle"))
			err = root.ExecuteContext(t.Context())
			if chmodErr := os.Chmod(dir, 0o700); chmodErr != nil { //nolint:gosec // Restore private fixture directory access after the failure.
				t.Fatal(chmodErr)
			}
			failure, ok := errors.AsType[clioutput.CommandError](err)
			if !ok || failure.CLIError.ExitCode != 5 {
				t.Fatalf("publication did not report runtime failure: %v", err)
			}
			loaded, err := config.LoadFile(path)
			if err != nil || !reflect.DeepEqual(loaded, want) {
				t.Fatal("fixture did not replace the expected config before failing directory sync")
			}
			if !reflect.DeepEqual(runtime.Config, loaded) {
				t.Error("published config and runtime state diverged")
			}
			account := "default"
			if tc.name == "new login" {
				account = "New"
			}
			stored, err := memory.Get("slack-cli", account)
			if tc.login {
				credential, decodeErr := config.DecodeCredential(stored)
				if err != nil || decodeErr != nil || credential.AccessToken != newToken || store.sets != 1 || store.deletes != 0 {
					t.Error("published config and saved credential diverged")
				}
			} else if err != nil || stored != oldToken || store.sets != 0 || store.deletes != 0 {
				t.Error("failed publication performed credential cleanup")
			}
			if resolutions != 0 || requests != boolCount(tc.login) {
				t.Error("failed publication requested revocation or retried a mutation")
			}
			var frame struct {
				OK     bool
				Data   json.RawMessage
				Errors []struct{ Details map[string]any }
				Meta   struct {
					ExitCode int `json:"exit_code"`
				}
			}
			if json.Unmarshal(out.Bytes(), &frame) != nil || frame.OK || string(frame.Data) != "null" || frame.Meta.ExitCode != 5 || len(frame.Errors) != 1 || diagnostic.Len() != 0 {
				t.Fatal("publication failure lost its machine envelope")
			}
			if frame.Errors[0].Details["config_published"] != true || frame.Errors[0].Details["durability_confirmed"] != false {
				t.Error("publication failure hid the completed replacement")
			}
			if tc.name == "logout" && frame.Errors[0].Details["credential_cleanup_attempted"] != false {
				t.Error("logout did not report skipped credential cleanup")
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{oldToken, newToken, "synthetic-unreviewed-publication"} {
				if strings.Contains(string(raw)+out.String()+diagnostic.String(), secret) {
					t.Error("publication disclosed or persisted secret material")
				}
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Error("replacement config lost private permissions")
			}
			// Recovery uses a fresh real command and the state actually published on disk.
			var recovered bytes.Buffer
			next := NewRootCommand(WithConfigPath(path), WithCredentialStore(store), WithTTY(false), WithIO(strings.NewReader(""), &recovered, &diagnostic))
			next.SetArgs([]string{"config", "set", "workspaces." + loaded.DefaultWorkspace + ".default_channel", "C_RECOVERED", "--output=json"})
			if err := next.ExecuteContext(t.Context()); err != nil {
				t.Fatalf("write did not recover after permission repair: %v", err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 {
				t.Error("publication left temporary files")
			}
		})
	}
}

type publicationStore struct {
	config.CredentialStore
	sets, deletes int
}

func (s *publicationStore) Set(service, account, value string) error {
	s.sets++
	return s.CredentialStore.Set(service, account, value)
}

func (s *publicationStore) Delete(service, account string) error {
	s.deletes++
	return s.CredentialStore.Delete(service, account)
}

type publicationTransport func(*http.Request) (*http.Response, error)

func (f publicationTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}
