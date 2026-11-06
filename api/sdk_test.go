package api_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/matcra587/slack-cli/api"
	slackgo "github.com/slack-go/slack"
)

func TestSDKClientMakesAuthenticatedRequest(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/conversations.info" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("token") != "xoxb-synthetic-sdk-token" {
			t.Error("missing configured synthetic token")
		}
		if _, err := io.WriteString(w, `{"ok":true,"channel":{"id":"C_FIXTURE"}}`); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	client := api.NewSDKClient("xoxb-synthetic-sdk-token", api.SDKOptions{APIURL: server.URL, NoThrottle: true})
	channel, err := client.GetConversationInfoContext(t.Context(), &slackgo.GetConversationInfoInput{ChannelID: "C_FIXTURE"})
	if err != nil {
		t.Fatal(err)
	}
	if channel.ID != "C_FIXTURE" {
		t.Fatalf("channel ID = %q", channel.ID)
	}
}

func TestSDKClientPreservesCallerHTTPTimeout(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })
	supplied := &http.Client{Timeout: 20 * time.Millisecond}
	client := api.NewSDKClient("xoxb-synthetic-sdk-token", api.SDKOptions{APIURL: server.URL, HTTPClient: supplied, NoThrottle: true})
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_, err := client.GetConversationInfoContext(ctx, &slackgo.GetConversationInfoInput{ChannelID: "C_FIXTURE"})
	if err == nil {
		t.Fatal("request unexpectedly succeeded")
	}
	if ctx.Err() != nil {
		t.Fatalf("caller HTTP timeout was discarded; outer context ended: %v", err)
	}
	if supplied.Transport != nil || supplied.Timeout != 20*time.Millisecond {
		t.Fatal("factory mutated supplied HTTP client")
	}
}

func TestSDKClientPreservesCallerRedirectPolicy(t *testing.T) {
	t.Parallel()
	var followed atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirected" {
			followed.Add(1)
			if _, err := io.WriteString(w, `{"ok":true,"channel":{"id":"C_FIXTURE"}}`); err != nil {
				t.Error(err)
			}
			return
		}
		http.Redirect(w, r, "/redirected", http.StatusFound)
	}))
	t.Cleanup(server.Close)
	var checks atomic.Int32
	supplied := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { checks.Add(1); return http.ErrUseLastResponse }}
	client := api.NewSDKClient("xoxb-synthetic-sdk-token", api.SDKOptions{APIURL: server.URL, HTTPClient: supplied, NoThrottle: true})
	_, err := client.GetConversationInfoContext(t.Context(), &slackgo.GetConversationInfoInput{ChannelID: "C_FIXTURE"})
	if err == nil {
		t.Fatal("redirect policy was discarded")
	}
	if followed.Load() != 0 || checks.Load() != 1 {
		t.Fatalf("redirected requests = %d; policy checks = %d", followed.Load(), checks.Load())
	}
	if supplied.Transport != nil {
		t.Fatal("factory mutated supplied transport")
	}
}

func TestSDKClientPreservesCallerCookieJar(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("fixture_session")
		if err != nil || cookie.Value != "synthetic-cookie" {
			t.Error("caller cookie jar was discarded")
		}
		if _, err := io.WriteString(w, `{"ok":true,"channel":{"id":"C_FIXTURE"}}`); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	jar.SetCookies(endpoint, []*http.Cookie{{Name: "fixture_session", Value: "synthetic-cookie"}})
	supplied := &http.Client{Jar: jar}
	client := api.NewSDKClient("xoxb-synthetic-sdk-token", api.SDKOptions{APIURL: server.URL, HTTPClient: supplied, NoThrottle: true})
	if _, err := client.GetConversationInfoContext(t.Context(), &slackgo.GetConversationInfoInput{ChannelID: "C_FIXTURE"}); err != nil {
		t.Fatal(err)
	}
	if supplied.Jar != jar || supplied.Transport != nil {
		t.Fatal("factory mutated supplied client")
	}
}

func TestSDKClientConcurrentRequestsUseSharedThrottle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var mu sync.Mutex
		arrivals := []time.Time{}
		transport := sdkTestRoundTripper(func(r *http.Request) (*http.Response, error) {
			mu.Lock()
			arrivals = append(arrivals, time.Now())
			mu.Unlock()
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":true,"channel":{"id":"C_FIXTURE"}}`)), Request: r}, nil
		})
		client := api.NewSDKClient("xoxb-synthetic-sdk-token", api.SDKOptions{APIURL: "https://fixture.invalid", HTTPClient: &http.Client{Transport: transport}})
		var calls sync.WaitGroup
		for range 8 {
			calls.Go(func() {
				if _, err := client.GetConversationInfoContext(t.Context(), &slackgo.GetConversationInfoInput{ChannelID: "C_FIXTURE"}); err != nil {
					t.Error(err)
				}
			})
		}
		calls.Wait()
		if len(arrivals) != 8 {
			t.Fatalf("requests = %d, want 8", len(arrivals))
		}
		for i := 1; i < len(arrivals); i++ {
			if gap := arrivals[i].Sub(arrivals[i-1]); gap < 1200*time.Millisecond {
				t.Fatalf("request %d arrived after %s, want >=1.2s", i, gap)
			}
		}
	})
}

type sdkTestRoundTripper func(*http.Request) (*http.Response, error)

func (f sdkTestRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSDKClientRedactsCredentialsFromErrorResponseDebug(t *testing.T) {
	t.Parallel()
	const configured = "xoxb-synthetic-sdk-token"
	for _, tc := range []struct {
		name, body string
		secrets    []string
	}{
		{
			name:    "structured response credentials",
			body:    `{"error":"failed","echo":"` + configured + `","nested":{"access_token":"synthetic-access-secret","refresh_token":"synthetic-refresh-secret","client_secret":"synthetic-client-secret","Authorization":"Bearer synthetic-auth-secret"}}`,
			secrets: []string{configured, "synthetic-access-secret", "synthetic-refresh-secret", "synthetic-client-secret", "synthetic-auth-secret"},
		},
		{
			name:    "escaped JSON credential",
			body:    `{"refresh_token":"synthetic-escaped-\\part\"tail","error":"failed"}`,
			secrets: []string{"synthetic-escaped-", "part", "tail"},
		},
		{
			name:    "malformed response echo",
			body:    `<html>upstream error echo ` + configured + `</html> {"refresh_token":"synthetic-unclosed-secret`,
			secrets: []string{configured, "synthetic-unclosed-secret"},
		},
		{
			name:    "form and bearer diagnostics",
			body:    "upstream failed token=" + configured + "&refresh_token=synthetic-form-secret\nAuthorization: Bearer synthetic-header-secret\n",
			secrets: []string{configured, "synthetic-form-secret", "synthetic-header-secret"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Authorization", "Bearer synthetic-response-header-secret")
				w.WriteHeader(http.StatusBadGateway)
				if _, err := io.WriteString(w, tc.body); err != nil {
					t.Error(err)
				}
			}))
			t.Cleanup(server.Close)
			var log bytes.Buffer
			client := api.NewSDKClient(configured, api.SDKOptions{APIURL: server.URL, Log: &log, Debug: true, NoThrottle: true})
			if _, err := client.GetConversationInfoContext(t.Context(), &slackgo.GetConversationInfoInput{ChannelID: "C_FIXTURE"}); err == nil {
				t.Fatal("error response unexpectedly succeeded")
			}
			diagnostic := log.String()
			if !strings.Contains(diagnostic, "502 Bad Gateway") {
				t.Fatal("real SDK error-response logging was not exercised")
			}
			for _, secret := range append(tc.secrets, "synthetic-response-header-secret") {
				if strings.Contains(diagnostic, secret) {
					t.Error("credential leaked into SDK diagnostic")
				}
			}
		})
	}
}

func TestSDKClientConcurrentDiagnosticsStayRedacted(t *testing.T) {
	t.Parallel()
	const token = "xoxb-synthetic-concurrent-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		if _, err := io.WriteString(w, "error echoes "+token); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	var log bytes.Buffer
	client := api.NewSDKClient(token, api.SDKOptions{APIURL: server.URL, Log: &log, Debug: true, NoThrottle: true})
	var calls sync.WaitGroup
	for range 8 {
		calls.Go(func() {
			if _, err := client.GetConversationInfoContext(t.Context(), &slackgo.GetConversationInfoInput{ChannelID: "C_FIXTURE"}); err == nil {
				t.Error("error response unexpectedly succeeded")
			}
		})
	}
	calls.Wait()
	if strings.Contains(log.String(), token) {
		t.Fatal("configured credential leaked into concurrent SDK diagnostics")
	}
	if got := strings.Count(log.String(), "502 Bad Gateway"); got != 8 {
		t.Fatalf("error response records = %d, want 8", got)
	}
}
