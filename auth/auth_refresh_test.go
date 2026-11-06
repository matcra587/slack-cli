package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/matcra587/slack-cli/auth"
)

func TestResolveRefreshesOAuthAndStoresOnlyReviewedCredentialFields(t *testing.T) {
	dir := isolate(t)
	path := filepath.Join(dir, "config.toml")
	writeConfig(t, path, "keychain:slack-cli/one")
	now := time.Date(2026, time.November, 6, 0, 0, 0, 0, time.UTC)
	original := `{"access_token":"xoxp-synthetic-old","refresh_token":"synthetic-refresh-old","client_id":"123.synthetic","expires_at":"2026-11-06T00:00:00Z"}`
	unrelated := `{"access_token":"xoxb-synthetic-unrelated"}`
	store := &credentialStore{values: map[string]string{"one": original, "two": unrelated}}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.URL.Path != "/api/oauth.v2.access" {
			t.Errorf("unexpected refresh request %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("client_id") != "123.synthetic" || r.Form.Get("refresh_token") != "synthetic-refresh-old" || r.Form.Get("client_secret") != "" {
			t.Error("unexpected OAuth refresh parameters")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"authed_user":{"access_token":"xoxp-synthetic-new","refresh_token":"synthetic-refresh-new","expires_in":3600},"unreviewed_secret":"never-persist-this"}`))
	}))
	defer server.Close()
	opts := auth.Options{ConfigPath: path, CredentialStore: store, SlackBaseURL: server.URL, HTTPClient: server.Client(), Now: func() time.Time { return now }}
	session, err := auth.Resolve(t.Context(), opts)
	if err != nil || session.Token() != "xoxp-synthetic-new" {
		t.Fatalf("refresh failed: %v", err)
	}
	if calls != 1 || store.writes != 1 {
		t.Fatalf("refresh calls=%d writes=%d, want one each", calls, store.writes)
	}
	var saved map[string]any
	if err := json.Unmarshal([]byte(store.values["one"]), &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved) != 4 || saved["access_token"] != "xoxp-synthetic-new" || saved["refresh_token"] != "synthetic-refresh-new" || saved["expires_at"] != "2026-11-06T01:00:00Z" || saved["client_id"] != "123.synthetic" {
		t.Fatal("unexpected persisted credential fields")
	}
	if strings.Contains(store.values["one"], "never-persist-this") || store.values["two"] != unrelated {
		t.Fatal("refresh changed unreviewed or unrelated state")
	}
	next, err := auth.Resolve(t.Context(), opts)
	if err != nil || next.Token() != session.Token() || calls != 1 {
		t.Fatalf("fresh token was refreshed again: %v", err)
	}
}

func TestResolveFailedRefreshPreservesPriorCredential(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		failStore  bool
	}{
		{name: "Slack rejects refresh", body: `{"ok":false,"error":"invalid_refresh_token"}`},
		{name: "malformed Slack response", body: `{"ok":`},
		{name: "empty refreshed token", body: `{"ok":true}`},
		{name: "credential store rejects update", body: `{"ok":true,"access_token":"xoxp-synthetic-new","refresh_token":"synthetic-refresh-new","expires_in":3600}`, failStore: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := isolate(t)
			path := filepath.Join(dir, "config.toml")
			writeConfig(t, path, "keychain:slack-cli/one")
			original := `{"access_token":"xoxp-synthetic-old","refresh_token":"synthetic-refresh-old","client_id":"123.synthetic","expires_at":"2000-01-01T00:00:00Z"}`
			store := &credentialStore{values: map[string]string{"one": original}}
			if tc.failStore {
				store.setErr = errors.New("provider rejected xoxp-synthetic-new")
			}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				body := tc.body
				if calls > 1 {
					body = `{"ok":true,"access_token":"xoxp-synthetic-recovered","refresh_token":"synthetic-refresh-recovered","expires_in":3600}`
				}
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			session, err := auth.Resolve(t.Context(), auth.Options{ConfigPath: path, CredentialStore: store, HTTPClient: server.Client(), SlackBaseURL: server.URL})
			if !errors.Is(err, auth.ErrNotAuthenticated) || session.Token() != "" {
				t.Fatalf("failed refresh accepted: session=%v err=%v", session, err)
			}
			if store.values["one"] != original || store.writes != 0 || calls != 1 {
				t.Fatalf("failure mutated state or retried: writes=%d calls=%d", store.writes, calls)
			}
			if strings.Contains(err.Error(), "xoxp-") || strings.Contains(err.Error(), "synthetic-refresh") {
				t.Fatal("refresh diagnostics leaked credential")
			}
			store.setErr = nil
			recovered, err := auth.Resolve(t.Context(), auth.Options{ConfigPath: path, CredentialStore: store, HTTPClient: server.Client(), SlackBaseURL: server.URL})
			if err != nil || recovered.Token() != "xoxp-synthetic-recovered" || store.writes != 1 || calls != 2 {
				t.Fatalf("explicit retry did not recover: err=%v writes=%d calls=%d", err, store.writes, calls)
			}
		})
	}
}

func TestResolveOAuthRefreshHonorsContextDeadline(t *testing.T) {
	dir := isolate(t)
	path := filepath.Join(dir, "config.toml")
	writeConfig(t, path, "keychain:slack-cli/one")
	original := `{"access_token":"xoxp-synthetic-old","refresh_token":"synthetic-refresh-old","client_id":"123.synthetic","expires_at":"2000-01-01T00:00:00Z"}`
	store := &credentialStore{values: map[string]string{"one": original}}
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer func() { close(release); server.Close() }()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	_, err := auth.Resolve(ctx, auth.Options{ConfigPath: path, CredentialStore: store, HTTPClient: server.Client(), SlackBaseURL: server.URL})
	if !errors.Is(err, context.DeadlineExceeded) || store.values["one"] != original || store.writes != 0 {
		t.Fatalf("deadline did not preserve credential state: %v", err)
	}
}
