package slackclient_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	"github.com/matcra587/slack-cli/internal/cli/slackclient"
	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

const syntheticToken = "xoxb-synthetic-client-test-token"

func TestNewRoutesAuthenticatedRequestsToConfiguredEndpoint(t *testing.T) {
	t.Parallel()
	for _, suffix := range []string{"", "/", "/api", "/api/"} {
		t.Run(suffix, func(t *testing.T) {
			t.Parallel()
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.URL.Path != "/api/conversations.info" || r.Method != http.MethodPost {
					t.Errorf("request = %s %s, want POST /api/conversations.info", r.Method, r.URL.Path)
				}
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Form.Get("token") != syntheticToken {
					t.Error("request did not carry the configured synthetic form token")
				}
				if r.Form.Get("channel") != "C_SYNTHETIC" {
					t.Errorf("channel = %q, want C_SYNTHETIC", r.Form.Get("channel"))
				}
				if strings.Contains(r.URL.RawQuery, syntheticToken) {
					t.Error("credential appeared in request URL")
				}
				writeResponse(t, w, `{"ok":true,"channel":{"id":"C_SYNTHETIC","name":"fixture"}}`)
			}))
			t.Cleanup(server.Close)
			client := testClient(t, &cliruntime.RootRuntime{SlackBaseURL: server.URL + suffix})
			channel, err := client.GetConversationInfoContext(t.Context(), &slackgo.GetConversationInfoInput{ChannelID: "C_SYNTHETIC"})
			if err != nil {
				t.Fatal(err)
			}
			if channel.ID != "C_SYNTHETIC" || requests.Load() != 1 {
				t.Fatalf("channel = %q, requests = %d", channel.ID, requests.Load())
			}
		})
	}
}

func TestNewRedactsTokenFromDebugDiagnostics(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeResponse(t, w, `{"ok":false,"error":"missing_scope","needed":"channels:read","provided":"chat:write"}`)
	}))
	t.Cleanup(server.Close)
	var diagnostics bytes.Buffer
	client := testClient(t, &cliruntime.RootRuntime{SlackBaseURL: server.URL, Stderr: &diagnostics}, slackgo.OptionDebug(true))
	_, _, err := client.PostMessageContext(t.Context(), "C_SYNTHETIC", slackgo.MsgOptionText("fixture message", false))
	var slackErr slackgo.SlackErrorResponse
	if !errors.As(err, &slackErr) || slackErr.Err != "missing_scope" || slackErr.Needed != "channels:read" {
		t.Fatalf("error = %v, want structured missing_scope", err)
	}
	if diagnostics.Len() == 0 {
		t.Fatal("debug logging was not exercised")
	}
	if strings.Contains(diagnostics.String(), syntheticToken) || strings.Contains(err.Error(), syntheticToken) {
		t.Fatal("synthetic credential leaked into diagnostics")
	}
}

func TestNewUsesSuppliedTransportAndTrailingOptions(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests.Add(1)
		if r.URL.String() != "https://fixture.invalid/custom/conversations.info" {
			t.Errorf("URL = %s, want explicit option endpoint", r.URL)
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("token") != syntheticToken {
			t.Error("missing synthetic form token")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":true,"channel":{"id":"C_SYNTHETIC"}}`)), Request: r}, nil
	})
	client := testClient(t, &cliruntime.RootRuntime{HTTPClient: &http.Client{Transport: transport}, SlackBaseURL: "https://unused.invalid"}, slackgo.OptionAPIURL("https://fixture.invalid/custom/"))
	if _, err := client.GetConversationInfoContext(t.Context(), &slackgo.GetConversationInfoInput{ChannelID: "C_SYNTHETIC"}); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1", requests.Load())
	}
}

func TestNewDoesNotReplayRejectedMutation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "permission failure", status: http.StatusOK, body: `{"ok":false,"error":"missing_scope","needed":"chat:write"}`},
		{name: "server failure", status: http.StatusServiceUnavailable, body: `{"ok":false,"error":"service_unavailable"}`},
		{name: "malformed response", status: http.StatusOK, body: `{"ok":true`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != http.MethodPost || r.URL.Path != "/api/chat.postMessage" {
					t.Errorf("request = %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(tc.status)
				writeResponse(t, w, tc.body)
			}))
			t.Cleanup(server.Close)
			client := testClient(t, &cliruntime.RootRuntime{SlackBaseURL: server.URL})
			_, _, err := client.PostMessageContext(t.Context(), "C_SYNTHETIC", slackgo.MsgOptionText("fixture mutation", false))
			if err == nil {
				t.Fatal("mutation unexpectedly succeeded")
			}
			if requests.Load() != 1 {
				t.Fatalf("mutation requests = %d, want exactly 1", requests.Load())
			}
		})
	}
}

func TestNewRetriesRateLimitsWithinConfiguredBudget(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		writeResponse(t, w, `{"ok":false,"error":"ratelimited"}`)
	}))
	t.Cleanup(server.Close)
	client := testClient(t, &cliruntime.RootRuntime{SlackBaseURL: server.URL})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, err := client.GetConversationInfoContext(ctx, &slackgo.GetConversationInfoInput{ChannelID: "C_SYNTHETIC"})
	if _, ok := errors.AsType[*slackgo.RateLimitedError](err); !ok {
		t.Fatalf("error = %v, want rate-limit error", err)
	}
	if requests.Load() != 3 {
		t.Fatalf("requests = %d, want original plus two retries", requests.Load())
	}
}

func TestNewCancelsRateLimitWaitWithoutReplay(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Retry-After", "10")
		w.WriteHeader(http.StatusTooManyRequests)
		writeResponse(t, w, `{"ok":false,"error":"ratelimited"}`)
	}))
	t.Cleanup(server.Close)
	client := testClient(t, &cliruntime.RootRuntime{SlackBaseURL: server.URL})
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_, err := client.GetConversationInfoContext(ctx, &slackgo.GetConversationInfoInput{ChannelID: "C_SYNTHETIC"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1", requests.Load())
	}
}

func TestNewNilTransportUsesDefaultTransport(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeResponse(t, w, `{"ok":true,"channel":{"id":"C_SYNTHETIC"}}`)
	}))
	t.Cleanup(server.Close)
	client := testClient(t, &cliruntime.RootRuntime{SlackBaseURL: server.URL, HTTPClient: &http.Client{}})
	if _, err := client.GetConversationInfoContext(t.Context(), &slackgo.GetConversationInfoInput{ChannelID: "C_SYNTHETIC"}); err != nil {
		t.Fatal(err)
	}
}

func testClient(t *testing.T, runtime *cliruntime.RootRuntime, options ...slackgo.Option) *slackgo.Client {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.PersistentFlags().Bool("no-throttle", true, "fixture bypass")
	return slackclient.New(t.Context(), cmd, runtime, syntheticToken, options...)
}

func writeResponse(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	if _, err := io.WriteString(w, body); err != nil {
		t.Error(err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
