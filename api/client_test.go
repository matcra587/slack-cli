package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/matcra587/slack-cli/api"
	"github.com/matcra587/slack-cli/auth"
)

const fixtureToken = "xoxp-synthetic-public-api"

type blockedStore struct{}

func (blockedStore) Get(string, string) (string, error) { panic("unexpected saved credential lookup") }
func (blockedStore) Set(string, string, string) error   { panic("unexpected credential write") }
func (blockedStore) Delete(string, string) error        { panic("unexpected credential deletion") }

type blockedSecretReader struct{}

func (blockedSecretReader) Read(context.Context, string) (string, error) {
	panic("unexpected secret manager lookup")
}

func TestExplicitTokenBypassesAllSavedAuthentication(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.toml")
	t.Setenv("SLICK_CONFIG", missing)
	t.Setenv("SLACK_CLI_CONFIG", missing)
	t.Setenv("SLACK_CLI_TOKEN", "xoxp-synthetic-unselected")
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("token") != fixtureToken {
			t.Error("explicit credential was not used by native SDK")
		}
		_, _ = w.Write([]byte(`{"ok":true,"team_id":"TSYNTHETIC","user_id":"USYNTHETIC"}`))
	}))
	defer server.Close()
	client, err := api.NewClient(api.ClientOptions{
		AuthToken: fixtureToken, ConfigPath: missing, APIURL: server.URL,
		HTTPClient: server.Client(), CredentialStore: blockedStore{}, SecretReader: blockedSecretReader{}, NoThrottle: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("construction contacted Slack")
	}
	identity, err := client.AuthTestContext(t.Context())
	if err != nil || identity.TeamID != "TSYNTHETIC" || calls != 1 {
		t.Fatalf("native SDK request failed: err=%v calls=%d", err, calls)
	}
}

func TestNewClientResolvesSavedWorkspace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("SLICK_API_TEST_TOKEN", fixtureToken)
	t.Setenv("SLACK_CLI_TOKEN", "")
	t.Setenv("SLACK_CLI_TOKEN_FIXTURE", "")
	t.Setenv("SLACK_CLI_DEFAULT_WORKSPACE", "")
	raw := `schema_version="1"
default_workspace="fixture"
[workspaces.fixture]
name="fixture"
team_id="TSYNTHETIC"
token_type="user"
token_ref="env:SLICK_API_TEST_TOKEN"
`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	client, err := api.NewClient(api.ClientOptions{ConfigPath: path, CredentialStore: blockedStore{}})
	if err != nil {
		t.Fatal(err)
	}
	if client.Session().Workspace != "fixture" || client.Session().TeamID != "TSYNTHETIC" {
		t.Fatalf("wrong selected workspace: %v", client.Session())
	}
	if client.SDK() == nil {
		t.Fatal("native SDK was not exposed")
	}
}

func TestNewClientMissingSavedAuthIsClassifiable(t *testing.T) {
	_, err := api.NewClient(api.ClientOptions{ConfigPath: filepath.Join(t.TempDir(), "missing.toml"), CredentialStore: blockedStore{}})
	if !errors.Is(err, auth.ErrNotAuthenticated) {
		t.Fatalf("error = %v, want missing auth classification", err)
	}
}

func TestClientFormattingRedactsCredentials(t *testing.T) {
	opts := api.ClientOptions{AuthToken: fixtureToken, CredentialStore: blockedStore{}}
	client, err := api.NewClient(opts)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]any{"options": opts, "options pointer": &opts, "client": client, "client value": *client} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		for _, output := range []string{string(raw), fmt.Sprintf("%v", value), fmt.Sprintf("%+v", value), fmt.Sprintf("%#v", value)} {
			if strings.Contains(output, fixtureToken) {
				t.Fatalf("%s leaked token", name)
			}
		}
	}
}

func TestNewClientRejectsUnsafeURLsAndLimits(t *testing.T) {
	for _, address := range []string{
		"http://slack.com/api/", "https://user:password@slack.com/api/", "https://slack.com/api/?token=private",
		"https://slack.com/api/#private", "https://slack.com/a/../api/", "https://slack.com/%2e%2e/api/",
		"https:///api/", "file:///tmp/api/", " https://slack.com/api/",
	} {
		t.Run(address, func(t *testing.T) {
			if _, err := api.NewClient(api.ClientOptions{AuthToken: fixtureToken, APIURL: address}); err == nil {
				t.Fatal("unsafe API URL was accepted")
			}
		})
	}
	for _, opts := range []api.ClientOptions{
		{AuthToken: fixtureToken, Timeout: -time.Second},
		{AuthToken: fixtureToken, MaxResponseBytes: -1},
		{AuthToken: fixtureToken, MaxResponseBytes: 1<<63 - 1},
		{AuthToken: "synthetic\r\nInjected: header"},
	} {
		if _, err := api.NewClient(opts); err == nil {
			t.Fatal("invalid client options were accepted")
		}
	}
}

func TestClientPreservesCallerHTTPSettingsWithoutMutation(t *testing.T) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	originalTransport := http.DefaultTransport
	redirects := 0
	clientOptions := &http.Client{
		Transport: originalTransport, Jar: jar, Timeout: 500 * time.Millisecond,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			redirects++
			return errors.New("synthetic redirect policy")
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth.test" {
			http.Redirect(w, r, "/other", http.StatusFound)
			return
		}
		t.Error("redirect was followed")
	}))
	defer server.Close()
	client, err := api.NewClient(api.ClientOptions{AuthToken: fixtureToken, HTTPClient: clientOptions, APIURL: server.URL, NoThrottle: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.AuthTestContext(t.Context())
	if err == nil || redirects != 1 {
		t.Fatalf("redirect policy lost: err=%v callbacks=%d", err, redirects)
	}
	if clientOptions.Transport != originalTransport || clientOptions.Timeout != 500*time.Millisecond || clientOptions.Jar != jar {
		t.Fatal("caller HTTP client was mutated")
	}
}
