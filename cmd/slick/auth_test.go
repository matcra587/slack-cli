package main

import (
	"fmt"
	"image/color"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	clibtheme "github.com/gechr/clib/theme"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestAuthLoginStoresTokenReferenceNotRawToken(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"auth.test": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"team_id":"T123","team":"Test Workspace","user_id":"U123"}`)
		},
	})
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.toml")
	store := config.NewMemoryCredentialStore()
	stdout, stderr, err := executeAuthRootWithInput(t, nil, configPath, store, server.BaseURL(),
		strings.NewReader("xoxb-secret\n"),
		false,
		[]string{"--workspace", "default", "auth", "login", "--method", "token", "--token-stdin", "--team-id", "T123", "--team-name", "Test Workspace"},
	)
	if err != nil {
		t.Fatalf("auth login returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"workspace":"default"`) {
		t.Fatalf("stdout = %s, want workspace", stdout)
	}
	secret, err := store.Get("slack-cli", "default")
	if err != nil {
		t.Fatalf("stored credential err=%v", err)
	}
	if secret == "xoxb-secret" {
		t.Fatal("stored credential is the raw token")
	}
	credential, err := config.DecodeCredential(secret)
	if err != nil {
		t.Fatalf("decode stored credential: %v", err)
	}
	if credential.AccessToken != "xoxb-secret" {
		t.Fatalf("stored access token = %q, want xoxb-secret", credential.AccessToken)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(raw), "xoxb-secret") {
		t.Fatalf("config leaked raw token: %s", string(raw))
	}
	if !strings.Contains(string(raw), "keychain:slack-cli/default") {
		t.Fatalf("config missing keychain reference: %s", string(raw))
	}
}

func TestAuthLoginTokenDerivesWorkspaceMetadataFromAuthTest(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"auth.test": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"team_id":"TDERIVED","team":"Derived Workspace","user_id":"U123"}`)
		},
	})
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.toml")
	store := config.NewMemoryCredentialStore()
	_, stderr, err := executeAuthRootWithInput(t, nil, configPath, store, server.BaseURL(),
		strings.NewReader("xoxp-secret\n"),
		false,
		[]string{"--workspace", "derived", "auth", "login", "--method", "token", "--token-stdin"},
	)
	if err != nil {
		t.Fatalf("auth login returned error: %v\nstderr=%s", err, stderr)
	}
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	profile := cfg.Workspaces["derived"]
	if profile.TeamID != "TDERIVED" || profile.TeamName != "Derived Workspace" {
		t.Fatalf("profile metadata = %#v, want derived team id/name", profile)
	}
}

func TestAuthLoginTokenFileTrimsTrailingNewline(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"auth.test": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"team_id":"TFILE","team":"File Workspace","user_id":"U123"}`)
		},
	})
	defer server.Close()

	tokenPath := filepath.Join(t.TempDir(), "slack-token.txt")
	if err := os.WriteFile(tokenPath, []byte("xoxb-file-secret\n"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	configPath := filepath.Join(t.TempDir(), "config.toml")
	store := config.NewMemoryCredentialStore()
	_, stderr, err := executeAuthRoot(t, nil, configPath, store, server.BaseURL(),
		[]string{"--workspace", "file-profile", "auth", "login", "--method", "token", "--token-file", tokenPath},
	)
	if err != nil {
		t.Fatalf("auth login returned error: %v\nstderr=%s", err, stderr)
	}
	secret, err := store.Get("slack-cli", "file-profile")
	if err != nil {
		t.Fatalf("stored credential err=%v", err)
	}
	credential, err := config.DecodeCredential(secret)
	if err != nil {
		t.Fatalf("decode stored credential: %v", err)
	}
	if credential.AccessToken != "xoxb-file-secret" {
		t.Fatalf("stored access token = %q, want token without trailing newline", credential.AccessToken)
	}
}

func TestAuthLoginTokenFileExpandsShellPath(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"auth.test": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"team_id":"T123","team":"Test Workspace","user_id":"U123"}`)
		},
	})
	defer server.Close()

	home := t.TempDir()
	t.Setenv("HOME", home)
	tokenPath := filepath.Join(home, "slack-token.txt")
	if err := os.WriteFile(tokenPath, []byte("xoxb-file-secret\n"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	configPath := filepath.Join(t.TempDir(), "config.toml")
	store := config.NewMemoryCredentialStore()
	_, stderr, err := executeAuthRoot(t, nil, configPath, store, server.BaseURL(),
		[]string{"--workspace", "default", "auth", "login", "--method", "token", "--token-file", "~/slack-token.txt", "--team-id", "T123", "--team-name", "Test Workspace"},
	)
	if err != nil {
		t.Fatalf("auth login returned error: %v\nstderr=%s", err, stderr)
	}
	secret, err := store.Get("slack-cli", "default")
	if err != nil {
		t.Fatalf("stored credential err=%v", err)
	}
	credential, err := config.DecodeCredential(secret)
	if err != nil {
		t.Fatalf("decode stored credential: %v", err)
	}
	if credential.AccessToken != "xoxb-file-secret" {
		t.Fatalf("stored token = %q, want expanded token file content", credential.AccessToken)
	}
}

func TestAuthLoginTokenEnvReadsEnvironmentVariableName(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"auth.test": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"team_id":"TENV","team":"Env Workspace","user_id":"U123"}`)
		},
	})
	defer server.Close()

	t.Setenv("SLACK_CLI_TOKEN_TEST_PROFILE", "xoxp-env-secret")
	configPath := filepath.Join(t.TempDir(), "config.toml")
	store := config.NewMemoryCredentialStore()
	_, stderr, err := executeAuthRoot(t, nil, configPath, store, server.BaseURL(),
		[]string{"--workspace", "env-profile", "auth", "login", "--method", "token", "--token-env", "SLACK_CLI_TOKEN_TEST_PROFILE"},
	)
	if err != nil {
		t.Fatalf("auth login returned error: %v\nstderr=%s", err, stderr)
	}
	secret, err := store.Get("slack-cli", "env-profile")
	if err != nil {
		t.Fatalf("stored credential err=%v", err)
	}
	credential, err := config.DecodeCredential(secret)
	if err != nil {
		t.Fatalf("decode stored credential: %v", err)
	}
	if credential.AccessToken != "xoxp-env-secret" {
		t.Fatalf("stored access token = %q, want env token", credential.AccessToken)
	}
}

func TestAuthLoginTokenSourcesAreMutuallyExclusive(t *testing.T) {
	tokenPath := filepath.Join(t.TempDir(), "slack-token.txt")
	if err := os.WriteFile(tokenPath, []byte("xoxb-file-secret\n"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	_, stderr, err := executeAuthRootWithInput(t, nil, filepath.Join(t.TempDir(), "config.toml"), config.NewMemoryCredentialStore(), "http://example.invalid",
		strings.NewReader("xoxb-stdin-secret\n"),
		false,
		[]string{"--workspace", "default", "auth", "login", "--method", "token", "--token-stdin", "--token-file", tokenPath},
	)
	if err == nil {
		t.Fatal("auth login returned nil error, want mutually exclusive token source failure")
	}
	if !strings.Contains(stderr, "token source flags are mutually exclusive") {
		t.Fatalf("stderr = %q, want token source mutual exclusion", stderr)
	}
}

func TestAuthLoginDoesNotExposeRawTokenFlag(t *testing.T) {
	root := NewRootCommand()
	authCmd, _, err := root.Find([]string{"auth"})
	if err != nil {
		t.Fatalf("find auth command: %v", err)
	}
	loginCmd, _, err := authCmd.Find([]string{"login"})
	if err != nil {
		t.Fatalf("find auth login command: %v", err)
	}
	if loginCmd.Flags().Lookup("token") != nil {
		t.Fatal("auth login exposes --token; raw token values must not be accepted in argv")
	}
	for _, name := range []string{"token-stdin", "token-file", "token-env", "method", "oauth-client-id"} {
		if loginCmd.Flags().Lookup(name) == nil {
			t.Fatalf("auth login missing --%s", name)
		}
	}
}

func TestAuthLoginPreservesConfigManagedProfilePreferences(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"auth.test": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"team_id":"TAUTH","team":"Auth Workspace","user_id":"U123"}`)
		},
	})
	defer server.Close()

	attribution := false
	cfg := &config.Config{
		SchemaVersion:    config.SchemaVersion,
		DefaultWorkspace: "default",
		Workspaces: map[string]config.WorkspaceProfile{
			"default": {
				Name:           "default",
				DefaultChannel: "C7N2Q8L4P",
				Attribution: config.AttributionConfig{
					Enabled: &attribution,
					Emoji:   ":rocket:",
					Message: "Sent from config",
				},
			},
		},
	}
	configPath := filepath.Join(t.TempDir(), "config.toml")
	store := config.NewMemoryCredentialStore()

	_, stderr, err := executeAuthRootWithInput(t, cfg, configPath, store, server.BaseURL(),
		strings.NewReader("xoxp-secret\n"),
		false,
		[]string{"--workspace", "default", "auth", "login", "--method", "token", "--token-stdin"},
	)
	if err != nil {
		t.Fatalf("auth login returned error: %v\nstderr=%s", err, stderr)
	}

	loaded, err := config.LoadFile(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	profile := loaded.Workspaces["default"]
	if profile.TeamID != "TAUTH" || profile.TokenRef != "keychain:slack-cli/default" {
		t.Fatalf("profile auth fields = %#v, want auth metadata", profile)
	}
	if profile.DefaultChannel != "C7N2Q8L4P" {
		t.Fatalf("DefaultChannel = %q, want preserved", profile.DefaultChannel)
	}
	if profile.Attribution.Enabled == nil || *profile.Attribution.Enabled {
		t.Fatalf("Attribution.Enabled = %#v, want preserved false", profile.Attribution.Enabled)
	}
	if profile.Attribution.Emoji != ":rocket:" || profile.Attribution.Message != "Sent from config" {
		t.Fatalf("Attribution = %#v, want preserved config preferences", profile.Attribution)
	}
}

func TestAuthLoginReusesExistingProfileCaseInsensitively(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"auth.test": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"team_id":"TAUTH","team":"Auth Workspace","user_id":"U123"}`)
		},
	})
	defer server.Close()

	cfg := &config.Config{
		SchemaVersion:    config.SchemaVersion,
		DefaultWorkspace: "Default",
		Workspaces: map[string]config.WorkspaceProfile{
			"Default": {Name: "Default", DefaultChannel: "C7N2Q8L4P"},
		},
	}
	configPath := filepath.Join(t.TempDir(), "config.toml")
	store := config.NewMemoryCredentialStore()

	_, stderr, err := executeAuthRootWithInput(t, cfg, configPath, store, server.BaseURL(),
		strings.NewReader("xoxp-secret\n"),
		false,
		[]string{"--workspace", "default", "auth", "login", "--method", "token", "--token-stdin"},
	)
	if err != nil {
		t.Fatalf("auth login returned error: %v\nstderr=%s", err, stderr)
	}
	if _, ok := cfg.Workspaces["default"]; ok {
		t.Fatalf("created duplicate lower-case profile: %#v", cfg.Workspaces)
	}
	profile := cfg.Workspaces["Default"]
	if profile.TeamID != "TAUTH" || profile.DefaultChannel != "C7N2Q8L4P" {
		t.Fatalf("profile = %#v, want auth merged into existing profile", profile)
	}
	if _, err := store.Get("slack-cli", "Default"); err != nil {
		t.Fatalf("credential stored under existing profile name: %v", err)
	}
	if _, err := store.Get("slack-cli", "default"); err == nil {
		t.Fatalf("credential stored under duplicate lower-case profile")
	}
}

func TestAuthLoginRequiresForceToOverwriteAuthenticatedProfile(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"auth.test": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"team_id":"TNEW","team":"New Workspace","user_id":"U123"}`)
		},
	})
	defer server.Close()

	cfg := &config.Config{
		SchemaVersion:    config.SchemaVersion,
		DefaultWorkspace: "default",
		Workspaces: map[string]config.WorkspaceProfile{
			"default": {
				Name:           "default",
				TeamID:         "TOLD",
				TeamName:       "Old Workspace",
				TokenType:      config.TokenTypeBot,
				TokenRef:       "keychain:slack-cli/default",
				DefaultChannel: "C7N2Q8L4P",
			},
		},
	}
	configPath := filepath.Join(t.TempDir(), "config.toml")
	store := config.NewMemoryCredentialStore()
	oldSecret, err := config.EncodeCredential(config.CredentialPayload{AccessToken: "xoxb-old"})
	if err != nil {
		t.Fatalf("encode old credential: %v", err)
	}
	if err := store.Set("slack-cli", "default", oldSecret); err != nil {
		t.Fatalf("store old credential: %v", err)
	}

	stdout, stderr, err := executeAuthRootWithInput(t, cfg, configPath, store, server.BaseURL(),
		strings.NewReader("xoxp-new\n"),
		false,
		[]string{"--workspace", "default", "auth", "login", "--method", "token", "--token-stdin"},
	)
	if err == nil {
		t.Fatal("auth login returned nil error, want force validation")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "--force") {
		t.Fatalf("stderr = %q, want --force guidance", stderr)
	}
	secret, err := store.Get("slack-cli", "default")
	if err != nil {
		t.Fatalf("get credential: %v", err)
	}
	credential, err := config.DecodeCredential(secret)
	if err != nil {
		t.Fatalf("decode credential: %v", err)
	}
	if credential.AccessToken != "xoxb-old" {
		t.Fatalf("credential access token = %q, want old token preserved", credential.AccessToken)
	}
	if cfg.Workspaces["default"].TeamID != "TOLD" {
		t.Fatalf("profile team id = %q, want old profile preserved", cfg.Workspaces["default"].TeamID)
	}
}

func TestAuthLoginForceOverwritesAuthFieldsAndPreservesPreferences(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"auth.test": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"team_id":"TNEW","team":"New Workspace","user_id":"U123"}`)
		},
	})
	defer server.Close()

	attributionEnabled := true
	cfg := &config.Config{
		SchemaVersion:    config.SchemaVersion,
		DefaultWorkspace: "default",
		Workspaces: map[string]config.WorkspaceProfile{
			"default": {
				Name:           "default",
				TeamID:         "TOLD",
				TeamName:       "Old Workspace",
				TokenType:      config.TokenTypeBot,
				TokenRef:       "keychain:slack-cli/default",
				DefaultChannel: "C7N2Q8L4P",
				Attribution: config.AttributionConfig{
					Enabled: &attributionEnabled,
					Message: "Sent from profile",
				},
			},
		},
	}
	configPath := filepath.Join(t.TempDir(), "config.toml")
	store := config.NewMemoryCredentialStore()
	oldSecret, err := config.EncodeCredential(config.CredentialPayload{AccessToken: "xoxb-old"})
	if err != nil {
		t.Fatalf("encode old credential: %v", err)
	}
	if err := store.Set("slack-cli", "default", oldSecret); err != nil {
		t.Fatalf("store old credential: %v", err)
	}

	_, stderr, err := executeAuthRootWithInput(t, cfg, configPath, store, server.BaseURL(),
		strings.NewReader("xoxp-new\n"),
		false,
		[]string{"--workspace", "default", "auth", "login", "--method", "token", "--token-stdin", "--force"},
	)
	if err != nil {
		t.Fatalf("auth login returned error: %v\nstderr=%s", err, stderr)
	}
	profile := cfg.Workspaces["default"]
	if profile.TeamID != "TNEW" || profile.TeamName != "New Workspace" || profile.TokenType != config.TokenTypeUser {
		t.Fatalf("profile auth fields = %#v, want new auth metadata", profile)
	}
	if profile.DefaultChannel != "C7N2Q8L4P" || profile.Attribution.Message != "Sent from profile" {
		t.Fatalf("profile preferences = %#v, want preserved preferences", profile)
	}
	secret, err := store.Get("slack-cli", "default")
	if err != nil {
		t.Fatalf("get credential: %v", err)
	}
	credential, err := config.DecodeCredential(secret)
	if err != nil {
		t.Fatalf("decode credential: %v", err)
	}
	if credential.AccessToken != "xoxp-new" {
		t.Fatalf("credential access token = %q, want force-written token", credential.AccessToken)
	}
}

func TestAuthLoginOAuthLocalFlowUsesPKCEAndStoresUserToken(t *testing.T) {
	redirectURL := localOAuthTestRedirectURL(t)
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"oauth.v2.access": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("client_id"); got != "C123" {
				t.Fatalf("client_id = %q, want C123", got)
			}
			if _, ok := req.Form["client_secret"]; ok {
				t.Fatalf("client_secret was sent, want omitted for PKCE local flow")
			}
			if got := req.Form.Get("code"); got != "oauth-code" {
				t.Fatalf("code = %q, want oauth-code", got)
			}
			if got := req.Form.Get("redirect_uri"); got != redirectURL {
				t.Fatalf("redirect_uri = %q, want %q", got, redirectURL)
			}
			if got := req.Form.Get("code_verifier"); got == "" {
				t.Fatal("code_verifier was empty, want PKCE verifier")
			}
			return testutil.JSONResponse(`{"ok":true,"authed_user":{"id":"U123","access_token":"xoxe.xoxp-oauth","token_type":"user","scope":"chat:write"},"team":{"id":"TOAUTH","name":"OAuth Workspace"}}`)
		},
	})
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.toml")
	store := config.NewMemoryCredentialStore()
	stdout, stderr, err := executeAuthRootWithOptions(t, nil, configPath, store, server.BaseURL(),
		strings.NewReader(""),
		false,
		[]string{"auth", "login", "--workspace-name", "oauth-profile", "--auth-method", "oauth", "--client-id", "C123", "--oauth-redirect-url", redirectURL},
		WithOAuthTimeout(2*time.Second),
		WithURLOpener(oauthTestCallbackOpener(t, "oauth-code")),
	)
	if err != nil {
		t.Fatalf("auth login returned error: %v\nstderr=%s", err, stderr)
	}
	secret, err := store.Get("slack-cli", "oauth-profile")
	if err != nil {
		t.Fatalf("stored credential err=%v", err)
	}
	credential, err := config.DecodeCredential(secret)
	if err != nil {
		t.Fatalf("decode stored credential: %v", err)
	}
	if credential.AccessToken != "xoxe.xoxp-oauth" {
		t.Fatalf("stored access token = %q, want xoxe.xoxp-oauth", credential.AccessToken)
	}
	for _, fragment := range []string{
		`"authenticated":true`,
		`"token_type":"user"`,
		`"team_id":"TOAUTH"`,
		`"team_name":"OAuth Workspace"`,
	} {
		if !strings.Contains(stdout, fragment) {
			t.Fatalf("stdout = %q, want fragment %q", stdout, fragment)
		}
	}
	cfg, err := config.LoadFile(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	profile := cfg.Workspaces["oauth-profile"]
	if profile.TeamID != "TOAUTH" || profile.TeamName != "OAuth Workspace" || profile.TokenType != config.TokenTypeUser {
		t.Fatalf("profile = %#v, want OAuth workspace user profile", profile)
	}
}

func TestAuthLoginOAuthDefaultRedirectUsesOSAssignedPort(t *testing.T) {
	t.Setenv("SLACK_CLI_CALLBACK_PORT", "")
	if got := defaultOAuthRedirectURL(); got != "http://localhost:0/callback" {
		t.Fatalf("defaultOAuthRedirectURL = %q, want OS-assigned port redirect", got)
	}
}

func TestAuthLoginOAuthPortZeroUsesAssignedListenerPort(t *testing.T) {
	var exchangedRedirect string
	var openedRedirect string
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"oauth.v2.access": func(req testutil.SlackRequest) testutil.SlackResponse {
			exchangedRedirect = req.Form.Get("redirect_uri")
			if strings.Contains(exchangedRedirect, ":0/") {
				t.Fatalf("redirect_uri = %q, want assigned listener port", exchangedRedirect)
			}
			if !strings.HasPrefix(exchangedRedirect, "http://localhost:") {
				t.Fatalf("redirect_uri = %q, want localhost redirect", exchangedRedirect)
			}
			return testutil.JSONResponse(`{"ok":true,"authed_user":{"id":"U123","access_token":"xoxe.xoxp-oauth","token_type":"user","scope":"chat:write"},"team":{"id":"TOAUTH","name":"OAuth Workspace"}}`)
		},
	})
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.toml")
	store := config.NewMemoryCredentialStore()
	_, stderr, err := executeAuthRootWithOptions(t, nil, configPath, store, server.BaseURL(),
		strings.NewReader(""),
		false,
		[]string{"auth", "login", "--workspace-name", "oauth-profile", "--auth-method", "oauth", "--client-id", "C123", "--oauth-redirect-url", "http://localhost:0/callback"},
		WithOAuthTimeout(2*time.Second),
		WithURLOpener(func(authorizeURL string) error {
			parsed, err := url.Parse(authorizeURL)
			if err != nil {
				return err
			}
			openedRedirect = parsed.Query().Get("redirect_uri")
			return oauthTestCallbackOpener(t, "oauth-code")(authorizeURL)
		}),
	)
	if err != nil {
		t.Fatalf("auth login returned error: %v\nstderr=%s", err, stderr)
	}
	if openedRedirect == "" || exchangedRedirect == "" {
		t.Fatalf("openedRedirect=%q exchangedRedirect=%q, want populated redirects", openedRedirect, exchangedRedirect)
	}
	if openedRedirect != exchangedRedirect {
		t.Fatalf("authorize redirect = %q, exchange redirect = %q", openedRedirect, exchangedRedirect)
	}
}

func TestAuthLoginOAuthDefaultRedirectHonorsCallbackPortEnv(t *testing.T) {
	port := localOAuthTestPort(t)
	t.Setenv("SLACK_CLI_CALLBACK_PORT", port)
	wantRedirect := "http://localhost:" + port + "/callback"
	var exchangedRedirect string
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"oauth.v2.access": func(req testutil.SlackRequest) testutil.SlackResponse {
			exchangedRedirect = req.Form.Get("redirect_uri")
			return testutil.JSONResponse(`{"ok":true,"authed_user":{"id":"U123","access_token":"xoxe.xoxp-oauth","token_type":"user","scope":"chat:write"},"team":{"id":"TOAUTH","name":"OAuth Workspace"}}`)
		},
	})
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.toml")
	store := config.NewMemoryCredentialStore()
	_, stderr, err := executeAuthRootWithOptions(t, nil, configPath, store, server.BaseURL(),
		strings.NewReader(""),
		false,
		[]string{"auth", "login", "--workspace-name", "oauth-profile", "--auth-method", "oauth", "--client-id", "C123"},
		WithOAuthTimeout(2*time.Second),
		WithURLOpener(oauthTestCallbackOpener(t, "oauth-code")),
	)
	if err != nil {
		t.Fatalf("auth login returned error: %v\nstderr=%s", err, stderr)
	}
	if exchangedRedirect != wantRedirect {
		t.Fatalf("redirect_uri = %q, want %q", exchangedRedirect, wantRedirect)
	}
}

func TestAuthLoginOAuthTTYOutputsClogFields(t *testing.T) {
	redirectURL := localOAuthTestRedirectURL(t)
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"oauth.v2.access": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"authed_user":{"id":"U123","access_token":"xoxe.xoxp-oauth","token_type":"user","scope":"chat:write"},"team":{"id":"TOAUTH","name":"OAuth Workspace"}}`)
		},
	})
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.toml")
	store := config.NewMemoryCredentialStore()
	stdout, stderr, err := executeAuthRootWithOptions(t, nil, configPath, store, server.BaseURL(),
		strings.NewReader(""),
		true,
		[]string{"auth", "login", "--workspace-name", "oauth-profile", "--auth-method", "oauth", "--client-id", "C123", "--oauth-redirect-url", redirectURL},
		WithOAuthTimeout(2*time.Second),
		WithURLOpener(oauthTestCallbackOpener(t, "oauth-code")),
	)
	if err != nil {
		t.Fatalf("auth login returned error: %v\nstderr=%s", err, stderr)
	}
	if strings.Contains(stdout, "{") {
		t.Fatalf("stdout = %q, want clog human output, not JSON", stdout)
	}
	for _, fragment := range []string{"INF", "workspace=oauth-profile", "authenticated=true", "token_type=user", "team_id=TOAUTH", `team_name="OAuth Workspace"`} {
		if !strings.Contains(stdout, fragment) {
			t.Fatalf("stdout = %q, want fragment %q", stdout, fragment)
		}
	}
}

func TestAuthLoginOAuthLocalFlowStoresRefreshableCredential(t *testing.T) {
	redirectURL := localOAuthTestRedirectURL(t)
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"oauth.v2.access": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("client_id"); got != "C123" {
				t.Fatalf("client_id = %q, want C123", got)
			}
			if _, ok := req.Form["client_secret"]; ok {
				t.Fatalf("client_secret was sent, want omitted for PKCE local flow")
			}
			if got := req.Form.Get("code"); got != "oauth-code" {
				t.Fatalf("code = %q, want oauth-code", got)
			}
			if got := req.Form.Get("redirect_uri"); got != redirectURL {
				t.Fatalf("redirect_uri = %q, want callback %q", got, redirectURL)
			}
			return testutil.JSONResponse(`{"ok":true,"authed_user":{"id":"U123","access_token":"xoxp-oauth","refresh_token":"refresh-1","expires_in":3600,"token_type":"user","scope":"chat:write"},"team":{"id":"TOAUTH","name":"OAuth Workspace"}}`)
		},
	})
	defer server.Close()

	now := time.Date(2026, 5, 3, 20, 10, 0, 0, time.UTC)
	configPath := filepath.Join(t.TempDir(), "config.toml")
	store := config.NewMemoryCredentialStore()
	stdout, stderr, err := executeAuthRootWithOptions(t, nil, configPath, store, server.BaseURL(),
		strings.NewReader(""),
		false,
		[]string{"auth", "login", "--workspace-name", "oauth-profile", "--auth-method", "oauth", "--client-id", "C123", "--oauth-redirect-url", redirectURL},
		WithNow(func() time.Time { return now }),
		WithOAuthTimeout(2*time.Second),
		WithURLOpener(oauthTestCallbackOpener(t, "oauth-code")),
	)
	if err != nil {
		t.Fatalf("auth login returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"token_type":"user"`) {
		t.Fatalf("stdout = %s, want user token type", stdout)
	}

	secret, err := store.Get("slack-cli", "oauth-profile")
	if err != nil {
		t.Fatalf("stored credential err=%v", err)
	}
	credential, err := config.DecodeCredential(secret)
	if err != nil {
		t.Fatalf("decode stored credential: %v", err)
	}
	if credential.AccessToken != "xoxp-oauth" || credential.RefreshToken != "refresh-1" || credential.ClientID != "C123" {
		t.Fatalf("credential = %#v, want access token, refresh token, and client id", credential)
	}
	if credential.ExpiresAt == nil || !credential.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("ExpiresAt = %v, want %v", credential.ExpiresAt, now.Add(time.Hour))
	}
}

func TestAuthLoginInteractiveOAuthStartsLocalCallbackAndStoresCredential(t *testing.T) {
	redirectURL := localOAuthTestRedirectURL(t)
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"oauth.v2.access": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("client_id"); got != "C123" {
				t.Fatalf("client_id = %q, want C123", got)
			}
			if _, ok := req.Form["client_secret"]; ok {
				t.Fatalf("client_secret was sent, want omitted for PKCE local flow")
			}
			if got := req.Form.Get("code"); got != "oauth-code" {
				t.Fatalf("code = %q, want oauth-code", got)
			}
			if got := req.Form.Get("redirect_uri"); got != redirectURL {
				t.Fatalf("redirect_uri = %q, want %q", got, redirectURL)
			}
			return testutil.JSONResponse(`{"ok":true,"authed_user":{"id":"U123","access_token":"xoxp-oauth","refresh_token":"refresh-1","expires_in":3600,"token_type":"user","scope":"chat:write"},"team":{"id":"TOAUTH","name":"OAuth Workspace"}}`)
		},
	})
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.toml")
	store := config.NewMemoryCredentialStore()
	var openedURL string
	stdout, stderr, err := executeAuthRootWithOptions(t, nil, configPath, store, server.BaseURL(),
		lineReader("oauth-profile\n1\nC123\n"+redirectURL+"\n"),
		true,
		[]string{"auth", "login"},
		WithURLOpener(func(target string) error {
			openedURL = target
			return oauthTestCallbackOpener(t, "oauth-code")(target)
		}),
		WithOAuthTimeout(2*time.Second),
	)
	if err != nil {
		t.Fatalf("auth login returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(openedURL, "/oauth/v2/authorize?") || !strings.Contains(openedURL, url.QueryEscape(redirectURL)) {
		t.Fatalf("openedURL = %q, want Slack authorize URL with configured redirect", openedURL)
	}
	if strings.Contains(stderr, "OAuth callback URL") {
		t.Fatalf("stderr = %q, callback URL should be handled by local listener", stderr)
	}
	if !strings.Contains(stdout, "authenticated=true") {
		t.Fatalf("stdout = %s, want authenticated login result", stdout)
	}
	secret, err := store.Get("slack-cli", "oauth-profile")
	if err != nil {
		t.Fatalf("stored credential err=%v", err)
	}
	credential, err := config.DecodeCredential(secret)
	if err != nil {
		t.Fatalf("decode stored credential: %v", err)
	}
	if credential.AccessToken != "xoxp-oauth" || credential.RefreshToken != "refresh-1" {
		t.Fatalf("credential = %#v, want OAuth access and refresh token", credential)
	}
}

func TestAuthLoginOAuthBadClientSecretExplainsPKCESetup(t *testing.T) {
	redirectURL := localOAuthTestRedirectURL(t)
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"oauth.v2.access": func(req testutil.SlackRequest) testutil.SlackResponse {
			if _, ok := req.Form["client_secret"]; ok {
				t.Fatalf("client_secret was sent, want omitted for PKCE local flow")
			}
			return testutil.JSONResponse(`{"ok":false,"error":"bad_client_secret"}`)
		},
	})
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.toml")
	store := config.NewMemoryCredentialStore()
	_, stderr, err := executeAuthRootWithOptions(t, nil, configPath, store, server.BaseURL(),
		strings.NewReader(""),
		false,
		[]string{"auth", "login", "--workspace-name", "oauth-profile", "--auth-method", "oauth", "--client-id", "C123", "--oauth-redirect-url", redirectURL},
		WithOAuthTimeout(2*time.Second),
		WithURLOpener(oauthTestCallbackOpener(t, "oauth-code")),
	)
	if err == nil {
		t.Fatal("auth login returned nil error, want PKCE setup error")
	}
	for _, fragment := range []string{"PKCE", "oauth_config.pkce_enabled", "client secret"} {
		if !strings.Contains(stderr, fragment) {
			t.Fatalf("stderr = %q, want fragment %q", stderr, fragment)
		}
	}
}

func TestAuthLoginOAuthTimeoutReportsRedirectURLAsField(t *testing.T) {
	redirectURL := localOAuthTestRedirectURL(t)
	configPath := filepath.Join(t.TempDir(), "config.toml")
	store := config.NewMemoryCredentialStore()

	stdout, stderr, err := executeAuthRootWithOptions(t, nil, configPath, store, "http://example.invalid",
		strings.NewReader(""),
		true,
		[]string{"auth", "login", "--workspace-name", "oauth-profile", "--auth-method", "oauth", "--client-id", "C123", "--oauth-redirect-url", redirectURL},
		WithOAuthTimeout(time.Millisecond),
		WithURLOpener(func(string) error { return nil }),
	)
	if err == nil {
		t.Fatal("auth login returned nil error, want oauth timeout")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	for _, fragment := range []string{
		"oauth flow timed out waiting",
		"redirect_url=" + redirectURL,
		"type=auth_failure",
		"exit_code=1",
	} {
		if !strings.Contains(stderr, fragment) {
			t.Fatalf("stderr = %q, want fragment %q", stderr, fragment)
		}
	}
	if strings.Contains(stderr, "waiting for "+redirectURL) {
		t.Fatalf("stderr = %q, redirect URL should be a structured field, not message text", stderr)
	}
}

func TestAuthLoginOAuthRequiresClientIDBeforeOpeningBrowser(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	store := config.NewMemoryCredentialStore()
	opened := false
	_, stderr, err := executeAuthRootWithOptions(t, nil, configPath, store, "http://example.invalid",
		strings.NewReader(""),
		false,
		[]string{"auth", "login", "--workspace-name", "oauth-profile", "--auth-method", "oauth"},
		WithURLOpener(func(string) error {
			opened = true
			return nil
		}),
	)
	if err == nil {
		t.Fatal("auth login returned nil error, want missing client secret")
	}
	if opened {
		t.Fatal("browser opener was called before client id validation")
	}
	if !strings.Contains(stderr, "oauth client id is required") {
		t.Fatalf("stderr = %q, want client id validation", stderr)
	}
}

func TestOAuthRedirectURLAllowsOnlyLocalHTTP(t *testing.T) {
	if _, err := oauthRedirectURL("http://127.0.0.1:53682/callback"); err != nil {
		t.Fatalf("oauthRedirectURL rejected local http redirect URL: %v", err)
	}
	if _, err := oauthRedirectURL("http://localhost:45682/callback"); err != nil {
		t.Fatalf("oauthRedirectURL rejected localhost http redirect URL: %v", err)
	}
	if _, err := oauthRedirectURL("http://example.com/callback"); err == nil {
		t.Fatal("oauthRedirectURL accepted non-local http redirect URL")
	}
	if !strings.HasPrefix(defaultOAuthRedirectURL(), "http://localhost:") {
		t.Fatalf("defaultOAuthRedirectURL = %q, want local http URL", defaultOAuthRedirectURL())
	}
}

func TestAuthLoginPromptsForMissingFieldsInTTY(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"auth.test": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"team_id":"T123","team":"Test Workspace","user_id":"U123"}`)
		},
	})
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.toml")
	store := config.NewMemoryCredentialStore()
	stdout, stderr, err := executeAuthRootWithInput(t, nil, configPath, store, server.BaseURL(),
		lineReader("default\n2\nxoxb-secret\n"),
		true,
		[]string{"auth", "login"},
	)
	if err != nil {
		t.Fatalf("auth login returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stderr, "Profile name") || !strings.Contains(stderr, "Slack token") {
		t.Fatalf("stderr prompts = %q, want profile and token prompts", stderr)
	}
	if strings.Contains(stderr, "Workspace name") {
		t.Fatalf("stderr prompts = %q, want Profile name label instead of Workspace name", stderr)
	}
	if strings.Contains(stderr, "Workspace ID") || strings.Contains(stderr, "Team ID") {
		t.Fatalf("stderr prompts = %q, workspace id should be derived instead of prompted", stderr)
	}
	if strings.Contains(stderr, "Workspace display name") {
		t.Fatalf("stderr prompts = %q, workspace display name should be derived", stderr)
	}
	if !strings.Contains(stdout, "default") {
		t.Fatalf("stdout = %s, want login result", stdout)
	}
	secret, err := store.Get("slack-cli", "default")
	if err != nil {
		t.Fatalf("stored credential err=%v", err)
	}
	credential, err := config.DecodeCredential(secret)
	if err != nil {
		t.Fatalf("decode stored credential: %v", err)
	}
	if credential.AccessToken != "xoxb-secret" {
		t.Fatalf("stored access token = %q, want xoxb-secret", credential.AccessToken)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(raw), "xoxb-secret") {
		t.Fatalf("config leaked raw token: %s", string(raw))
	}
}

func TestAuthLoginTTYValidationUsesHumanClogError(t *testing.T) {
	stdout, stderr, err := executeAuthRootWithInput(t, nil, filepath.Join(t.TempDir(), "config.toml"), config.NewMemoryCredentialStore(), "",
		strings.NewReader(""),
		true,
		[]string{"auth", "login"},
	)
	if err == nil {
		t.Fatal("auth login returned nil error, want validation error")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want no data on validation error", stdout)
	}
	if strings.Contains(stderr, `"errors"`) || strings.Contains(stderr, `{"`) {
		t.Fatalf("stderr = %q, want human clog diagnostic instead of JSON", stderr)
	}
	if !strings.Contains(stderr, "workspace-name is required") {
		t.Fatalf("stderr = %q, want validation message", stderr)
	}
}

func TestAuthLoginInteractivePathUsesHuhForm(t *testing.T) {
	raw, err := os.ReadFile("auth.go")
	if err != nil {
		t.Fatalf("read auth.go: %v", err)
	}
	if !strings.Contains(string(raw), `"charm.land/huh/v2"`) {
		t.Fatalf("auth.go does not import huh for interactive login")
	}
}

func TestAuthLoginFieldHelpExplainsProvisioningInputs(t *testing.T) {
	help := authLoginFieldHelp()
	tests := map[string][]string{
		"workspace":      {"--workspace", "default"},
		"auth_method":    {"OAuth", "token"},
		"token":          {"Slack issues tokens through an app install", "xoxp-", "acts as you", "xoxb-", "acts as the app bot", "keychain"},
		"team_id":        {"https://app.slack.com/client/T8KQ42P9D/C7N2Q8L4P", "T8KQ42P9D", "auth.test"},
		"client_id":      {"OAuth client ID", "Slack app"},
		"oauth_redirect": {"Local HTTP redirect URL", "Slack app", "OAuth settings"},
	}
	for key, fragments := range tests {
		got := help[key]
		if got.Description == "" || got.Placeholder == "" {
			t.Fatalf("%s help = %#v, want description and placeholder", key, got)
		}
		for _, fragment := range fragments {
			if !strings.Contains(got.Description+" "+got.Placeholder, fragment) {
				t.Fatalf("%s help = %#v, want fragment %q", key, got, fragment)
			}
		}
	}
}

func TestAuthLoginFormUsesClibThemeAdapter(t *testing.T) {
	raw, err := os.ReadFile("auth.go")
	if err != nil {
		t.Fatalf("read auth.go: %v", err)
	}
	content := string(raw)
	for _, fragment := range []string{
		`github.com/gechr/clib/theme`,
		"authLoginHuhTheme",
		"WithTheme(authLoginHuhTheme",
	} {
		if !strings.Contains(content, fragment) {
			t.Fatalf("auth.go missing theme-aware fragment %q", fragment)
		}
	}
}

func TestAuthLoginHuhThemeUsesClibSemanticColors(t *testing.T) {
	th := clibtheme.Default().With(
		clibtheme.WithHelpCommand(lipgloss.NewStyle().Foreground(lipgloss.Color("#123456"))),
		clibtheme.WithHelpDim(lipgloss.NewStyle().Foreground(lipgloss.Color("#654321"))),
		clibtheme.WithHelpFlag(lipgloss.NewStyle().Foreground(lipgloss.Color("#fedcba"))),
		clibtheme.WithHelpPlaceholder(lipgloss.NewStyle().Foreground(lipgloss.Color("#abcdef"))),
	)

	got := authLoginHuhTheme(th).Theme(false)
	assertSameColor(t, "#123456", got.Focused.Title.GetForeground())
	assertSameColor(t, "#654321", got.Focused.Description.GetForeground())
	assertSameColor(t, "#123456", got.Focused.TextInput.Prompt.GetForeground())
	assertSameColor(t, "#abcdef", got.Focused.TextInput.Placeholder.GetForeground())
}

func TestOAuthCallbackHandlerWritesStyledHTML(t *testing.T) {
	resultCh := make(chan oauthCallbackResult, 1)
	handler := oauthCallbackHandler("state-1", "/callback", resultCh)
	req := httptest.NewRequest(http.MethodGet, "/callback?code=abc&state=state-1", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{`style="font-family:sans-serif"`, "Authorisation successful"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body = %q, want %q", body, want)
		}
	}
}

func TestDefaultOpenURLSplitsBrowserCommand(t *testing.T) {
	argsPath := filepath.Join(t.TempDir(), "browser-args.txt")
	opener := testutil.WriteExecutable(t, "browser.sh", fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$@" > %q
`, argsPath))
	t.Setenv("BROWSER", opener+" --profile test")

	if err := defaultOpenURL("https://example.com/oauth"); err != nil {
		t.Fatalf("defaultOpenURL returned error: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		raw, err := os.ReadFile(argsPath)
		if err == nil {
			if got, want := string(raw), "--profile\ntest\nhttps://example.com/oauth\n"; got != want {
				t.Fatalf("browser args = %q, want %q", got, want)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("browser args were not written: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAuthLoginHuhColorLeavesUnsetClibColorUnset(t *testing.T) {
	if got := authLoginHuhColor(lipgloss.NoColor{}); got != nil {
		t.Fatalf("unset clib color converted to %v, want nil", got)
	}
}

func TestAuthStatusSwitchAndLogout(t *testing.T) {
	cfg := authTestConfig()
	configPath := filepath.Join(t.TempDir(), "config.toml")
	store := config.NewMemoryCredentialStore()
	defaultSecret, err := config.EncodeCredential(config.CredentialPayload{AccessToken: "xoxb-secret"})
	if err != nil {
		t.Fatalf("encode default credential: %v", err)
	}
	if err := store.Set("slack-cli", "default", defaultSecret); err != nil {
		t.Fatalf("store token: %v", err)
	}
	otherSecret, err := config.EncodeCredential(config.CredentialPayload{AccessToken: "xoxp-secret"})
	if err != nil {
		t.Fatalf("encode other credential: %v", err)
	}
	if err := store.Set("slack-cli", "other", otherSecret); err != nil {
		t.Fatalf("store other token: %v", err)
	}
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"auth.test": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"team_id":"T123","team":"Test Workspace","user_id":"U123"}`)
		},
	})
	defer server.Close()

	stdout, stderr, err := executeAuthRoot(t, cfg, configPath, store, server.BaseURL(),
		[]string{"auth", "status"},
	)
	if err != nil {
		t.Fatalf("auth status returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"authenticated":true`) {
		t.Fatalf("stdout = %s, want authenticated", stdout)
	}
	if !strings.Contains(stdout, `"validation_state":"valid"`) {
		t.Fatalf("stdout = %s, want valid token state", stdout)
	}

	_, stderr, err = executeAuthRoot(t, cfg, configPath, store, server.BaseURL(),
		[]string{"auth", "switch", "other"},
	)
	if err != nil {
		t.Fatalf("auth switch returned error: %v\nstderr=%s", err, stderr)
	}

	_, stderr, err = executeAuthRoot(t, cfg, configPath, store, server.BaseURL(),
		[]string{"auth", "logout", "default"},
	)
	if err != nil {
		t.Fatalf("auth logout returned error: %v\nstderr=%s", err, stderr)
	}
	if _, err := store.Get("slack-cli", "default"); err == nil {
		t.Fatal("token still present after logout")
	}
	if _, ok := cfg.Workspaces["default"]; ok {
		t.Fatalf("default workspace profile still present after logout: %#v", cfg.Workspaces)
	}
	if cfg.DefaultWorkspace != "other" {
		t.Fatalf("default workspace = %q, want other after logout", cfg.DefaultWorkspace)
	}
}

func TestAuthLogoutPreservesConfigManagedProfilePreferences(t *testing.T) {
	attribution := false
	cfg := &config.Config{
		SchemaVersion:    config.SchemaVersion,
		DefaultWorkspace: "default",
		Workspaces: map[string]config.WorkspaceProfile{
			"default": {
				Name:             "default",
				TeamID:           "T123",
				TeamName:         "Auth Workspace",
				TokenType:        config.TokenTypeUser,
				TokenRef:         "keychain:slack-cli/default",
				DefaultChannel:   "C7N2Q8L4P",
				AgentAttribution: &attribution,
				Attribution: config.AttributionConfig{
					Emoji:   ":rocket:",
					Message: "Sent from config",
				},
			},
		},
	}
	configPath := filepath.Join(t.TempDir(), "config.toml")
	store := config.NewMemoryCredentialStore()
	secret, err := config.EncodeCredential(config.CredentialPayload{AccessToken: "xoxp-secret"})
	if err != nil {
		t.Fatalf("encode credential: %v", err)
	}
	if err := store.Set("slack-cli", "default", secret); err != nil {
		t.Fatalf("store token: %v", err)
	}

	_, stderr, err := executeAuthRoot(t, cfg, configPath, store, "http://example.invalid",
		[]string{"auth", "logout", "default"},
	)
	if err != nil {
		t.Fatalf("auth logout returned error: %v\nstderr=%s", err, stderr)
	}
	if _, err := store.Get("slack-cli", "default"); err == nil {
		t.Fatal("token still present after logout")
	}
	profile, ok := cfg.Workspaces["default"]
	if !ok {
		t.Fatalf("default profile was deleted, want config-managed preferences preserved")
	}
	if profile.TeamID != "" || profile.TeamName != "" || profile.TokenType != "" || profile.TokenRef != "" {
		t.Fatalf("profile auth fields = %#v, want cleared", profile)
	}
	if profile.DefaultChannel != "C7N2Q8L4P" || profile.Attribution.Emoji != ":rocket:" || profile.Attribution.Message != "Sent from config" {
		t.Fatalf("profile preferences = %#v, want preserved", profile)
	}
}

func TestMessageCommandReportsUnauthenticatedWorkspaceProfile(t *testing.T) {
	cfg := &config.Config{
		SchemaVersion:    config.SchemaVersion,
		DefaultWorkspace: "default",
		Workspaces: map[string]config.WorkspaceProfile{
			"default": {
				Name:           "default",
				DefaultChannel: "C7N2Q8L4P",
			},
		},
	}

	_, stderr, err := executeAuthRoot(t, cfg, filepath.Join(t.TempDir(), "config.toml"), config.NewMemoryCredentialStore(), "http://example.invalid",
		[]string{"message", "send", "--channel", "C7N2Q8L4P", "--message", "hello"},
	)
	if err == nil {
		t.Fatal("message send returned nil error, want unauthenticated workspace failure")
	}
	if !strings.Contains(stderr, "workspace default is not authenticated") {
		t.Fatalf("stderr = %q, want unauthenticated workspace guidance", stderr)
	}
}

func TestAuthStatusTreatsLegacyRawCredentialAsUnauthenticated(t *testing.T) {
	cfg := &config.Config{
		SchemaVersion:    config.SchemaVersion,
		DefaultWorkspace: "default",
		Workspaces: map[string]config.WorkspaceProfile{
			"default": {Name: "default", TeamID: "T123", TokenType: config.TokenTypeUser, TokenRef: "keychain:slack-cli/default"},
		},
	}
	configPath := filepath.Join(t.TempDir(), "config.toml")
	store := config.NewMemoryCredentialStore()
	if err := store.Set("slack-cli", "default", "xoxp-legacy-raw-token"); err != nil {
		t.Fatalf("store legacy token: %v", err)
	}

	stdout, stderr, err := executeAuthRoot(t, cfg, configPath, store, "http://example.invalid",
		[]string{"auth", "status"},
	)
	if err != nil {
		t.Fatalf("auth status returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"authenticated":false`) {
		t.Fatalf("stdout = %s, want unauthenticated for legacy raw credential", stdout)
	}
	if !strings.Contains(stdout, `"validation_state":"invalid"`) {
		t.Fatalf("stdout = %s, want invalid validation state", stdout)
	}
	if !strings.Contains(stdout, "legacy plaintext credential") {
		t.Fatalf("stdout = %s, want legacy plaintext credential guidance", stdout)
	}
}

func TestAuthStatusUsesRuntimeEnvTokenOverride(t *testing.T) {
	t.Setenv("SLACK_CLI_TOKEN_DEFAULT", "xoxb-env-override")
	cfg := &config.Config{
		SchemaVersion:    config.SchemaVersion,
		DefaultWorkspace: "default",
		Workspaces: map[string]config.WorkspaceProfile{
			"default": {Name: "default", TeamID: "T123", TokenType: config.TokenTypeBot, TokenRef: "keychain:slack-cli/default"},
		},
	}
	configPath := filepath.Join(t.TempDir(), "config.toml")
	store := config.NewMemoryCredentialStore()
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"auth.test": func(req testutil.SlackRequest) testutil.SlackResponse {
			token := req.Header.Get("Authorization")
			if token == "" {
				token = req.Form.Get("token")
			}
			if !strings.Contains(token, "xoxb-env-override") {
				t.Fatalf("auth token = %q, want env override token", token)
			}
			return testutil.JSONResponse(`{"ok":true,"team_id":"T123","team":"Test Workspace","user_id":"U123"}`)
		},
	})
	defer server.Close()

	stdout, stderr, err := executeAuthRoot(t, cfg, configPath, store, server.BaseURL(),
		[]string{"auth", "status"},
	)
	if err != nil {
		t.Fatalf("auth status returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"authenticated":true`) || !strings.Contains(stdout, `"validation_state":"valid"`) {
		t.Fatalf("stdout = %s, want env override valid auth status", stdout)
	}
}

func assertSameColor(t *testing.T, want string, got color.Color) {
	t.Helper()
	r, g, b, _ := got.RGBA()
	gotHex := fmt.Sprintf("#%02x%02x%02x", uint8(r>>8), uint8(g>>8), uint8(b>>8))
	if gotHex != want {
		t.Fatalf("color = %s, want %s", gotHex, want)
	}
}

func localOAuthTestRedirectURL(t *testing.T) string {
	t.Helper()
	return "http://127.0.0.1:" + localOAuthTestPort(t) + "/callback"
}

func localOAuthTestPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate local OAuth port: %v", err)
	}
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("split local OAuth port: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("close local OAuth port probe: %v", err)
	}
	return port
}

func oauthTestCallbackOpener(t *testing.T, code string) func(string) error {
	t.Helper()
	return func(authorizeURL string) error {
		parsed, err := url.Parse(authorizeURL)
		if err != nil {
			t.Errorf("parse authorize URL: %v", err)
			return err
		}
		redirectURI := parsed.Query().Get("redirect_uri")
		state := parsed.Query().Get("state")
		if redirectURI == "" || state == "" {
			t.Errorf("authorize URL missing redirect_uri or state: %s", authorizeURL)
			return nil
		}
		go func() {
			target, err := url.Parse(redirectURI)
			if err != nil {
				t.Errorf("parse redirect URI: %v", err)
				return
			}
			query := target.Query()
			query.Set("code", code)
			query.Set("state", state)
			target.RawQuery = query.Encode()
			resp, err := http.Get(target.String())
			if err != nil {
				t.Errorf("send OAuth callback: %v", err)
				return
			}
			_ = resp.Body.Close()
		}()
		return nil
	}
}

func executeAuthRoot(t *testing.T, cfg *config.Config, configPath string, store config.CredentialStore, baseURL string, args []string) (string, string, error) {
	t.Helper()
	return executeAuthRootWithInput(t, cfg, configPath, store, baseURL, strings.NewReader(""), false, args)
}

func executeAuthRootWithInput(t *testing.T, cfg *config.Config, configPath string, store config.CredentialStore, baseURL string, stdin io.Reader, isTTY bool, args []string) (string, string, error) {
	t.Helper()
	return executeAuthRootWithOptions(t, cfg, configPath, store, baseURL, stdin, isTTY, args)
}

func executeAuthRootWithOptions(t *testing.T, cfg *config.Config, configPath string, store config.CredentialStore, baseURL string, stdin io.Reader, isTTY bool, args []string, options ...RootOption) (string, string, error) {
	t.Helper()
	stdout := &strings.Builder{}
	stderr := &strings.Builder{}
	rootOptions := []RootOption{
		WithConfig(cfg),
		WithConfigPath(configPath),
		WithCredentialStore(store),
		WithSlackBaseURL(baseURL),
		WithIO(stdin, stdout, stderr),
		WithTTY(isTTY),
	}
	rootOptions = append(rootOptions, options...)
	cmd := NewRootCommand(rootOptions...)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

type lineByLineReader struct {
	lines []string
}

func lineReader(value string) *lineByLineReader {
	return &lineByLineReader{lines: strings.SplitAfter(value, "\n")}
}

func (r *lineByLineReader) Read(p []byte) (int, error) {
	if len(r.lines) == 0 {
		return 0, io.EOF
	}
	line := r.lines[0]
	r.lines = r.lines[1:]
	return copy(p, line), nil
}

func authTestConfig() *config.Config {
	return &config.Config{
		SchemaVersion:    config.SchemaVersion,
		DefaultWorkspace: "default",
		Workspaces: map[string]config.WorkspaceProfile{
			"default": {Name: "default", TeamID: "T123", TokenType: config.TokenTypeBot, TokenRef: "keychain:slack-cli/default"},
			"other":   {Name: "other", TeamID: "T999", TokenType: config.TokenTypeUser, TokenRef: "keychain:slack-cli/other"},
		},
	}
}

func TestAuthLogoutCallsAuthRevoke(t *testing.T) {
	store := testutil.NewFakeKeychain()
	secret, err := config.EncodeCredential(config.CredentialPayload{AccessToken: "xoxb-revoke-me"})
	if err != nil {
		t.Fatalf("encode credential: %v", err)
	}
	if err := store.Set("slack-cli", "default", secret); err != nil {
		t.Fatalf("store credential: %v", err)
	}

	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"auth.revoke": func(req testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"revoked":true}`)
		},
	})
	defer server.Close()

	cfg := authTestConfig()
	configPath := filepath.Join(t.TempDir(), "config.toml")
	_, stderr, err := executeAuthRoot(t, cfg, configPath, store, server.BaseURL(),
		[]string{"auth", "logout", "default"},
	)
	if err != nil {
		t.Fatalf("auth logout returned error: %v\nstderr=%s", err, stderr)
	}
	if got := len(server.Requests("auth.revoke")); got != 1 {
		t.Fatalf("auth.revoke called %d times, want 1", got)
	}
	if _, err := store.Get("slack-cli", "default"); err == nil {
		t.Fatal("credential still present after logout")
	}
}

func TestAuthLogoutKeepTokenSkipsRevoke(t *testing.T) {
	store := testutil.NewFakeKeychain()
	secret, err := config.EncodeCredential(config.CredentialPayload{AccessToken: "xoxb-keep-me"})
	if err != nil {
		t.Fatalf("encode credential: %v", err)
	}
	if err := store.Set("slack-cli", "default", secret); err != nil {
		t.Fatalf("store credential: %v", err)
	}

	server := testutil.NewSlackServer(t, nil)
	defer server.Close()

	cfg := authTestConfig()
	configPath := filepath.Join(t.TempDir(), "config.toml")
	_, stderr, err := executeAuthRoot(t, cfg, configPath, store, server.BaseURL(),
		[]string{"auth", "logout", "--keep-token", "default"},
	)
	if err != nil {
		t.Fatalf("auth logout --keep-token returned error: %v\nstderr=%s", err, stderr)
	}
	if got := len(server.Requests("auth.revoke")); got != 0 {
		t.Fatalf("auth.revoke called %d times, want 0 for --keep-token", got)
	}
	// Credential must still be present.
	if _, err := store.Get("slack-cli", "default"); err != nil {
		t.Fatalf("credential deleted after --keep-token logout: %v", err)
	}
	// Auth fields must be cleared from the profile.
	profile := cfg.Workspaces["default"]
	if profile.TokenRef != "" || profile.TokenType != "" {
		t.Fatalf("profile auth fields not cleared after --keep-token: %#v", profile)
	}
	// Warning about token remaining live must appear on stderr.
	if !strings.Contains(stderr, "keep-token") || !strings.Contains(stderr, "still valid") {
		t.Fatalf("stderr = %q, want --keep-token warning about token remaining live", stderr)
	}
}

func TestAuthLogoutRevokeFailureProceedsWithCleanup(t *testing.T) {
	store := testutil.NewFakeKeychain()
	secret, err := config.EncodeCredential(config.CredentialPayload{AccessToken: "xoxb-revoke-fails"})
	if err != nil {
		t.Fatalf("encode credential: %v", err)
	}
	if err := store.Set("slack-cli", "default", secret); err != nil {
		t.Fatalf("store credential: %v", err)
	}

	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"auth.revoke": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.SlackResponse{Status: http.StatusInternalServerError, Body: `{"ok":false,"error":"internal_error"}`}
		},
	})
	defer server.Close()

	cfg := authTestConfig()
	configPath := filepath.Join(t.TempDir(), "config.toml")
	_, stderr, err := executeAuthRoot(t, cfg, configPath, store, server.BaseURL(),
		[]string{"auth", "logout", "default"},
	)
	if err != nil {
		t.Fatalf("auth logout returned error on revoke failure: %v\nstderr=%s", err, stderr)
	}
	// Local credential must still be deleted despite the revoke failure.
	if _, err := store.Get("slack-cli", "default"); err == nil {
		t.Fatal("credential still present after logout despite revoke failure")
	}
	// Revoke failure must be logged to stderr.
	if !strings.Contains(stderr, "token revocation failed") {
		t.Fatalf("stderr = %q, want token revocation failure message", stderr)
	}
}
