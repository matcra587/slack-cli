package auth_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/matcra587/slack-cli/auth"
)

func TestResolveRejectsInvalidOAuthRefreshBeforeStoring(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"missing acknowledgement", `{"access_token":"xoxp-synthetic-new"}`},
		{"contradictory acknowledgement", `{"ok":true,"error":"invalid_refresh_token","access_token":"xoxp-synthetic-new"}`},
		{"negative expiry", `{"ok":true,"access_token":"xoxp-synthetic-new","expires_in":-1}`},
		{"overflow expiry", `{"ok":true,"authed_user":{"access_token":"xoxp-synthetic-new","expires_in":9223372037}}`},
		{"null expiry", `{"ok":true,"access_token":"xoxp-synthetic-new","expires_in":null}`},
		{"malformed token", `{"ok":true,"access_token":"xoxp-synthetic\nnew"}`},
		{"oversized reply", `{"ok":true,"access_token":"xoxp-synthetic-new"}` + strings.Repeat(" ", 8<<20)},
		{"truncated success", `{"ok":true,"access_token":"xoxp-synthetic-new"}`},
		{"private error", `{"ok":false,"error":"synthetic-private-oauth-boundary"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(isolate(t), "config.toml")
			writeConfig(t, path, "keychain:slack-cli/one")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			const old = `{"access_token":"xoxp-synthetic-old","refresh_token":"synthetic-refresh-old","client_id":"synthetic-client","expires_at":"2000-01-01T00:00:00Z"}`
			store := &credentialStore{values: map[string]string{"one": old, "two": "unrelated-synthetic-value"}}
			var calls atomic.Int32
			var recoverReply atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Method != http.MethodPost || r.URL.Path != "/api/oauth.v2.access" || r.URL.RawQuery != "" || len(r.PostForm) != 3 || r.PostForm.Get("grant_type") != "refresh_token" || r.PostForm.Get("client_id") != "synthetic-client" || r.PostForm.Get("refresh_token") != "synthetic-refresh-old" || r.Header.Get("Authorization") != "" {
					t.Error("OAuth boundary changed refresh method, params or secret-free client authentication")
				}
				body := tc.body
				if recoverReply.Load() {
					body = `{"ok":true,"access_token":"xoxp-synthetic-recovered","refresh_token":"synthetic-refresh-recovered","expires_in":3600}`
				} else if tc.name == "truncated success" {
					w.Header().Set("Content-Length", strconv.Itoa(len(body)+16))
				}
				_, _ = io.WriteString(w, body)
			}))
			defer server.Close()
			opts := auth.Options{ConfigPath: path, CredentialStore: store, HTTPClient: server.Client(), SlackBaseURL: server.URL}
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			session, err := auth.Resolve(ctx, opts)
			if err == nil || !errors.Is(err, auth.ErrNotAuthenticated) || session.Token() != "" || calls.Load() != 1 || store.writes != 0 || store.values["one"] != old || store.values["two"] != "unrelated-synthetic-value" {
				t.Fatal("invalid OAuth refresh was stored, retried or changed prior state")
			}
			if strings.Contains(err.Error(), "synthetic-private-oauth-boundary") {
				t.Error("OAuth refresh exposed untrusted diagnostic data")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Error("failed OAuth refresh changed config")
			}
			recoverReply.Store(true)
			session, err = auth.Resolve(ctx, opts)
			if err != nil || session.Token() != "xoxp-synthetic-recovered" || calls.Load() != 2 || store.writes != 1 || store.values["two"] != "unrelated-synthetic-value" {
				t.Error("explicit OAuth refresh recovery failed or replayed")
			}
		})
	}
}

func TestResolveDoesNotForwardRefreshCredentialOnRedirect(t *testing.T) {
	path := filepath.Join(isolate(t), "config.toml")
	writeConfig(t, path, "keychain:slack-cli/one")
	const old = `{"access_token":"xoxp-synthetic-old","refresh_token":"synthetic-refresh-old","client_id":"synthetic-client","expires_at":"2000-01-01T00:00:00Z"}`
	store := &credentialStore{values: map[string]string{"one": old}}
	var originCalls, forwarded atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		forwarded.Add(1)
		_, _ = io.WriteString(w, `{"ok":true,"access_token":"xoxp-synthetic-forwarded","expires_in":3600}`)
	}))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		originCalls.Add(1)
		w.Header().Set("Location", destination.URL+"/api/oauth.v2.access")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	session, err := auth.Resolve(ctx, auth.Options{ConfigPath: path, CredentialStore: store, HTTPClient: origin.Client(), SlackBaseURL: origin.URL})
	if err == nil || session.Token() != "" || originCalls.Load() != 1 || forwarded.Load() != 0 || store.writes != 0 || store.values["one"] != old {
		t.Fatal("OAuth redirect forwarded refresh credentials or changed saved state")
	}
}
