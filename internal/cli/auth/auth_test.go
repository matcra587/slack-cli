package auth

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestAuthLoginOAuthDefaultRedirectUsesOSAssignedPort(t *testing.T) {
	t.Setenv("SLACK_CLI_CALLBACK_PORT", "")
	if got := defaultOAuthRedirectURL(); got != "http://localhost:0/callback" {
		t.Fatalf("defaultOAuthRedirectURL = %q, want OS-assigned port redirect", got)
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
		"clitheme.LoginHuhTheme",
		"WithTheme(clitheme.LoginHuhTheme",
	} {
		if !strings.Contains(content, fragment) {
			t.Fatalf("auth.go missing theme-aware fragment %q", fragment)
		}
	}
}

func TestOAuthCallbackHandlerWritesStyledHTML(t *testing.T) {
	resultCh := make(chan oauthCallbackResult, 1)
	handler := oauthCallbackHandler("state-1", "/callback", resultCh)
	req := httptest.NewRequest(http.MethodGet, "/callback?code=abc&state=state-1", http.NoBody)
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
