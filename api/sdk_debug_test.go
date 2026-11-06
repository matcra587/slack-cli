package api_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/matcra587/slack-cli/api"
	slackgo "github.com/slack-go/slack"
)

func TestSDKDebugOmitsUnreviewedResponseFieldsAndHeaders(t *testing.T) {
	t.Parallel()
	const token = "xoxp-synthetic-debug-token"
	for _, body := range []string{
		`{"upload_url":"https://fixture.invalid/upload/synthetic-upload-capability","response_url":"https://fixture.invalid/hook/synthetic-webhook-capability","unreviewed_secret":"synthetic-unknown-secret"}`,
		"<html>synthetic-opaque-private-body</html>",
	} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Method != http.MethodPost || r.URL.Path != "/api/reactions.add" || r.URL.RawQuery != "" || r.PostForm.Get("token") != token || r.PostForm.Get("timestamp") != "1746284582.123456" {
					t.Error("wrong request method, path, timestamp or synthetic authentication")
				}
				w.Header().Set("Set-Cookie", "session=synthetic-private-cookie")
				w.Header().Set("Location", "https://fixture.invalid/synthetic-location-capability")
				w.Header().Set("X-Unreviewed-Secret", "synthetic-private-header")
				w.WriteHeader(http.StatusBadGateway)
				if _, err := io.WriteString(w, body); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			var log bytes.Buffer
			client := api.NewSDKClient(token, api.SDKOptions{APIURL: server.URL, Log: &log, Debug: true, NoThrottle: true})
			err := client.AddReactionContext(t.Context(), "rocket", slackgo.ItemRef{Channel: "C1", Timestamp: "1746284582.123456"})
			if err == nil || calls.Load() != 1 {
				t.Fatal("failed mutation succeeded or was replayed")
			}
			diagnostic := log.String()
			if !strings.Contains(diagnostic, "502 Bad Gateway") {
				t.Fatal("HTTP failure lost its useful status diagnostic")
			}
			for _, secret := range []string{token, "synthetic-upload-capability", "synthetic-webhook-capability", "synthetic-unknown-secret", "synthetic-opaque-private-body", "synthetic-private-cookie", "synthetic-location-capability", "synthetic-private-header"} {
				if strings.Contains(diagnostic, secret) {
					t.Fatal("unreviewed response data entered debug output")
				}
			}
		})
	}
}

func TestSDKDebugOmitsMessagePayloadAndUnknownDiagnostics(t *testing.T) {
	t.Parallel()
	const content = "synthetic-private-message-content"
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api/chat.postMessage" || r.PostForm.Get("text") != content {
			t.Error("SDK changed the requested message")
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.WriteString(w, `{"ok":true,"channel":"C1","ts":"1746284582.123456","message":{"text":"Posted"}}`); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	var log bytes.Buffer
	client := api.NewSDKClient("xoxp-synthetic-message-debug", api.SDKOptions{APIURL: server.URL, Log: &log, Debug: true, NoThrottle: true})
	channel, ts, err := client.PostMessageContext(t.Context(), "C1", slackgo.MsgOptionText(content, false))
	if err != nil || channel != "C1" || ts != "1746284582.123456" || calls.Load() != 1 {
		t.Fatalf("message failed or changed: %v", err)
	}
	if log.Len() == 0 || strings.Contains(log.String(), content) {
		t.Fatal("message payload entered debug output or diagnostics disappeared")
	}
	log.Reset()
	client.Debugln("unrecognized diagnostic", "synthetic-unreviewed-diagnostic")
	if log.Len() == 0 || strings.Contains(log.String(), "synthetic-unreviewed-diagnostic") {
		t.Fatal("unknown SDK diagnostic text was not safely summarized")
	}
}

func TestSDKDebugKeepsStatusAndRetryEventsWithoutRemoteReasonText(t *testing.T) {
	t.Parallel()
	for _, retry := range []bool{false, true} {
		name := "HTTP reason"
		if retry {
			name = "rate limit retry"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			transport := sdkTestRoundTripper(func(r *http.Request) (*http.Response, error) {
				attempt := calls.Add(1)
				code, status, body := http.StatusBadGateway, "502 synthetic-private-reason", `{}`
				header := make(http.Header)
				if retry {
					if attempt == 1 {
						code, status = http.StatusTooManyRequests, "429 synthetic-private-reason"
						header.Set("Retry-After", "0")
					} else {
						code, status, body = http.StatusOK, "200 OK", `{"ok":true}`
					}
				}
				return &http.Response{StatusCode: code, Status: status, Header: header, Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			var log bytes.Buffer
			client := api.NewSDKClient("xoxp-synthetic-retry-debug", api.SDKOptions{HTTPClient: &http.Client{Transport: transport}, Log: &log, Debug: true, NoThrottle: true})
			err := client.AddReactionContext(t.Context(), "rocket", slackgo.ItemRef{Channel: "C1", Timestamp: "1746284582.123456"})
			if retry {
				if err != nil || calls.Load() != 2 || !strings.Contains(strings.ToLower(log.String()), "retry") {
					t.Fatalf("documented rate retry changed: err=%v requests=%d", err, calls.Load())
				}
			} else if err == nil || calls.Load() != 1 || !strings.Contains(log.String(), "502 Bad Gateway") {
				t.Fatal("HTTP failure status changed or request replayed")
			}
			if strings.Contains(log.String(), "synthetic-private-reason") {
				t.Fatal("remote reason text entered debug diagnostics")
			}
		})
	}
}

func TestSDKDebugConnectionRetryOmitsTransportDiagnostics(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	transport := sdkTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			return nil, errors.New("connection reset: synthetic-private-transport")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Request: r}, nil
	})
	var log bytes.Buffer
	client := api.NewSDKClient("xoxp-synthetic-connection-debug", api.SDKOptions{HTTPClient: &http.Client{Transport: transport}, Log: &log, Debug: true, NoThrottle: true}, slackgo.OptionRetryConfig(slackgo.RetryConfig{MaxRetries: 1, Handlers: slackgo.ConnectionOnlyRetryHandlers(), BackoffInitial: time.Millisecond, BackoffMax: time.Millisecond}))
	err := client.AddReactionContext(t.Context(), "rocket", slackgo.ItemRef{Channel: "C1", Timestamp: "1746284582.123456"})
	if err != nil || calls.Load() != 2 || !strings.Contains(strings.ToLower(log.String()), "retry") {
		t.Fatalf("explicit SDK connection retry changed: err=%v requests=%d", err, calls.Load())
	}
	if strings.Contains(log.String(), "synthetic-private-transport") {
		t.Fatal("transport diagnostics entered debug output during retry")
	}
}
