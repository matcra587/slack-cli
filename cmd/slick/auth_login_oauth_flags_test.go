package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/matcra587/slack-cli/internal/config"
)

func TestAuthLoginOAuthShortFlagsUseExactPKCECallback(t *testing.T) {
	for _, port := range []bool{true, false} {
		name := "redirect with bot token"
		if port {
			name = "port with user token"
		}
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			var redirect, challenge string
			client := &http.Client{Transport: authStatusTransport(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				hash := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
				if r.Method != http.MethodPost || r.URL.Path != "/api/oauth.v2.access" || r.URL.RawQuery != "" || len(r.PostForm) != 4 || r.PostForm.Get("client_id") != "synthetic-client" || r.PostForm.Get("code") != "synthetic-code" || r.PostForm.Get("redirect_uri") != redirect || r.PostForm.Get("code_verifier") == "" || base64.RawURLEncoding.EncodeToString(hash[:]) != challenge || r.Header.Get("Authorization") != "" {
					t.Error("OAuth short flags changed PKCE exchange method, encoding, client, callback or secret-free authentication")
				}
				body := `{"ok":true,"access_token":"xoxb-synthetic-oauth","token_type":"bot","refresh_token":"xoxe-synthetic-refresh","expires_in":3600,"team":{"id":"T_OAUTH","name":"OAuth Team"},"unknown_secret":"synthetic-private-oauth-data"}`
				if port {
					body = `{"ok":true,"access_token":"xoxb-unselected-synthetic-token","authed_user":{"id":"U_ACTOR","access_token":"xoxe.xoxp-synthetic-oauth","refresh_token":"xoxe-synthetic-refresh","expires_in":3600},"team":{"id":"T_OAUTH","name":"OAuth Team"},"unknown_secret":"synthetic-private-oauth-data"}`
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}
			opener := func(raw string) error {
				authorize, err := url.Parse(raw)
				if err != nil {
					return err
				}
				query := authorize.Query()
				redirect, challenge = query.Get("redirect_uri"), query.Get("code_challenge")
				callback, err := url.Parse(redirect)
				if err != nil || callback.Hostname() == "" || (callback.Hostname() != "127.0.0.1" && callback.Hostname() != "localhost") || callback.Port() == "0" || callback.Port() == "" || callback.Path != "/callback" || challenge == "" || query.Get("code_challenge_method") != "S256" || query.Get("client_id") != "synthetic-client" || query.Get("state") == "" || !strings.Contains(query.Get("user_scope"), "chat:write") {
					return errors.New("invalid synthetic OAuth authorization or assigned callback")
				}
				callbackQuery := callback.Query()
				callbackQuery.Set("code", "synthetic-code")
				callbackQuery.Set("state", query.Get("state"))
				callback.RawQuery = callbackQuery.Encode()
				req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, callback.String(), nil)
				if err != nil {
					return err
				}
				response, err := (&http.Client{Timeout: time.Second}).Do(req)
				if err != nil {
					return err
				}
				defer func() { _ = response.Body.Close() }()
				if response.StatusCode != 200 {
					return errors.New("synthetic OAuth callback rejected")
				}
				return nil
			}
			path := filepath.Join(t.TempDir(), "config.toml")
			store := config.NewMemoryCredentialStore()
			now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
			args := []string{"auth", "login", "--workspace-name=OAuth", "-m", "oauth", "-C", "synthetic-client", "--output=json", "--color=never"}
			if port {
				args = append(args, "-p", "0")
			} else {
				args = append(args, "-r", "http://127.0.0.1:0/callback")
			}
			out, diagnostic, err := executeAuthRootWithOptions(t, nil, path, store, "http://127.0.0.1:1", strings.NewReader(""), false, args, func(runtime *RootRuntime) { runtime.HTTPClient = client }, WithOAuthTimeout(2*time.Second), WithURLOpener(opener), WithNow(func() time.Time { return now }))
			if err != nil || diagnostic != "" || calls.Load() != 1 {
				t.Fatalf("short OAuth flow failed or replayed exchange: %v calls=%d", err, calls.Load())
			}
			stored, err := store.Get("slack-cli", "OAuth")
			if err != nil {
				t.Fatal(err)
			}
			payload, err := config.DecodeCredential(stored)
			wantToken, wantType := "xoxb-synthetic-oauth", config.TokenTypeBot
			if port {
				wantToken, wantType = "xoxe.xoxp-synthetic-oauth", config.TokenTypeUser
			}
			var fields map[string]any
			if err != nil || payload.AccessToken != wantToken || payload.RefreshToken != "xoxe-synthetic-refresh" || payload.ClientID != "synthetic-client" || payload.ExpiresAt == nil || !payload.ExpiresAt.Equal(now.Add(time.Hour)) || json.Unmarshal([]byte(stored), &fields) != nil || len(fields) != 4 {
				t.Error("OAuth flags lost reviewed credential fields or selected wrong token")
			}
			cfg, err := config.LoadFile(path)
			if err != nil || cfg.Workspaces["OAuth"].TokenType != wantType || cfg.Workspaces["OAuth"].TeamID != "T_OAUTH" || cfg.Workspaces["OAuth"].TeamName != "OAuth Team" {
				t.Error("OAuth flags lost canonical workspace or token type")
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{wantToken, "xoxb-unselected-synthetic-token", "xoxe-synthetic-refresh", "synthetic-private-oauth-data", "synthetic-code"} {
				if strings.Contains(out+diagnostic+string(raw), secret) {
					t.Error("OAuth flags exposed sensitive response or exchange fields")
				}
			}
			var frame struct {
				OK   bool
				Meta struct{ Command, Workspace string }
				Data struct{ Workspace string }
			}
			if json.Unmarshal([]byte(out), &frame) != nil || !frame.OK || frame.Meta.Command != "auth.login" || frame.Meta.Workspace != "OAuth" || frame.Data.Workspace != "OAuth" {
				t.Error("OAuth flags lost machine result metadata")
			}
		})
	}
}
