package credentials_test

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/matcra587/slack-cli/internal/credentials"
	slackgo "github.com/slack-go/slack"
)

func TestOAuthHTTPClientKeepsCallerCookiesAndExplicitRedirectPolicy(t *testing.T) {
	var calls, policies atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		cookie, err := r.Cookie("synthetic-session")
		if err != nil || cookie.Value != "synthetic-cookie-value" {
			t.Error("OAuth client discarded caller cookie jar")
		}
		w.Header().Set("Location", "/stop")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	jar.SetCookies(u, []*http.Cookie{{Name: "synthetic-session", Value: "synthetic-cookie-value"}})
	original := server.Client()
	original.Jar, original.Timeout = jar, time.Second
	original.CheckRedirect = func(*http.Request, []*http.Request) error {
		policies.Add(1)
		return http.ErrUseLastResponse
	}
	transport := original.Transport
	wrapped := credentials.NewOAuthHTTPClient(original)
	_, err = slackgo.GetOAuthV2ResponseContext(t.Context(), wrapped, "synthetic-client", "", "synthetic-code", "http://127.0.0.1:1234/callback", slackgo.OAuthOptionCodeVerifier("synthetic-verifier"), slackgo.OAuthOptionAPIURL(server.URL+"/api/"))
	if err == nil || calls.Load() != 1 || policies.Load() != 1 || original.Transport != transport || original.Timeout != time.Second || original.Jar != jar {
		t.Fatal("OAuth guard discarded caller policy/settings or mutated original client")
	}
}

func TestOAuthHTTPClientKeepsCallerTimeout(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer server.Close()
	defer close(release)
	original := server.Client()
	original.Timeout = 50 * time.Millisecond
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/api/oauth.v2.access", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := credentials.NewOAuthHTTPClient(original).Do(req)
	if response != nil {
		_ = response.Body.Close()
	}
	if err == nil || ctx.Err() != nil {
		t.Fatal("OAuth client discarded caller timeout")
	}
}
