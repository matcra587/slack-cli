package api_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"time"

	"github.com/matcra587/slack-cli/api"
	slackgo "github.com/slack-go/slack"
)

func TestSDKReactionResponsesFailClosedWithoutReplay(t *testing.T) {
	for _, tc := range []struct {
		name, method, body string
		valid              bool
		slackError         bool
	}{
		{name: "add missing ok", method: "reactions.add", body: `{}`},
		{name: "remove false without error", method: "reactions.remove", body: `{"ok":false}`},
		{name: "add null ok", method: "reactions.add", body: `{"ok":null}`},
		{name: "add conflicting acknowledgement", method: "reactions.add", body: `{"ok":true,"error":"invalid_auth"}`},
		{name: "remove conflicting acknowledgement", method: "reactions.remove", body: `{"ok":true,"error":"invalid_auth"}`},
		{name: "get conflicting acknowledgement", method: "reactions.get", body: `{"ok":true,"error":"invalid_auth","type":"message","message":{"reactions":[{"name":"rocket","count":1}]}}`},
		{name: "get missing type", method: "reactions.get", body: `{"ok":true}`},
		{name: "get missing message", method: "reactions.get", body: `{"ok":true,"type":"message"}`},
		{name: "get empty message", method: "reactions.get", body: `{"ok":true,"type":"message","message":{}}`},
		{name: "get null message", method: "reactions.get", body: `{"ok":true,"type":"message","message":null}`},
		{name: "get wrong message type", method: "reactions.get", body: `{"ok":true,"type":"message","message":[]}`},
		{name: "unknown item type", method: "reactions.get", body: `{"ok":true,"type":"unknown"}`},
		{name: "malformed response", method: "reactions.add", body: `{"ok":true,"token":"xoxb-synthetic-sdk-reaction"`},
		{name: "successful add", method: "reactions.add", body: `{"ok":true}`, valid: true},
		{name: "successful remove", method: "reactions.remove", body: `{"ok":true}`, valid: true},
		{name: "empty message reactions", method: "reactions.get", body: `{"ok":true,"type":"message","message":{"ts":"1746284582.123456"}}`, valid: true},
		{name: "SDK file support", method: "reactions.get", body: `{"ok":true,"type":"file","file":{"id":"F1","reactions":[]}}`, valid: true},
		{name: "SDK file comment support", method: "reactions.get", body: `{"ok":true,"type":"file_comment","file":{"id":"F1"},"comment":{"id":"Fc1","reactions":[]}}`, valid: true},
		{name: "structured permission error", method: "reactions.add", body: `{"ok":false,"error":"missing_scope","needed":"reactions:write"}`, slackError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Method != http.MethodPost || r.URL.Path != "/api/"+tc.method || r.PostForm.Get("token") != "xoxb-synthetic-sdk-reaction" || r.URL.RawQuery != "" {
					t.Error("wrong reaction request or synthetic authentication")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			client := api.NewSDKClient("xoxb-synthetic-sdk-reaction", api.SDKOptions{APIURL: server.URL, NoThrottle: true})
			var err error
			panicked := false
			func() {
				defer func() {
					if recover() != nil {
						panicked = true
					}
				}()
				target := slackgo.NewRefToMessage("C123", "1746284582.123456")
				switch tc.method {
				case "reactions.add":
					err = client.AddReactionContext(t.Context(), "rocket", target)
				case "reactions.remove":
					err = client.RemoveReactionContext(t.Context(), "rocket", target)
				default:
					_, err = client.GetReactionsContext(t.Context(), target, slackgo.GetReactionsParameters{Full: true})
				}
			}()
			if panicked || (err == nil) != tc.valid {
				t.Fatalf("invalid response caused panic or false success: panicked=%t err=%v", panicked, err)
			}
			if err != nil && strings.Contains(err.Error(), "xoxb-synthetic-sdk-reaction") {
				t.Fatal("invalid response disclosed synthetic credentials")
			}
			if tc.slackError {
				var response slackgo.SlackErrorResponse
				if !errors.As(err, &response) || response.Err != "missing_scope" || response.Needed != "reactions:write" {
					t.Fatal("guard lost structured Slack permission details")
				}
			}
			if calls.Load() != 1 {
				t.Fatal("reaction request was replayed")
			}
		})
	}
}

func TestSDKReactionGuardClosesBodiesAndHidesReadErrors(t *testing.T) {
	for _, tc := range []struct {
		name  string
		body  io.Reader
		valid bool
	}{
		{"success", strings.NewReader(`{"ok":true}`), true},
		{"invalid response", strings.NewReader(`{"ok":null}`), false},
		{"read failure", iotest.ErrReader(errors.New("provider xoxb-synthetic-private-response")), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls, closes atomic.Int32
			transport := sdkTestRoundTripper(func(*http.Request) (*http.Response, error) {
				calls.Add(1)
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: &sdkReactionBody{Reader: tc.body, closes: &closes}}, nil
			})
			client := api.NewSDKClient("xoxb-synthetic-reaction", api.SDKOptions{HTTPClient: &http.Client{Transport: transport}, NoThrottle: true})
			err := client.AddReactionContext(t.Context(), "rocket", slackgo.NewRefToMessage("C123", "1746284582.123456"))
			if (err == nil) != tc.valid || closes.Load() != 1 || calls.Load() != 1 {
				t.Fatal("response body leaked, mutation replayed, or validity changed")
			}
			if err != nil && strings.Contains(err.Error(), "synthetic-private-response") {
				t.Fatal("body read error exposed unreviewed provider text")
			}
		})
	}
}

type sdkReactionBody struct {
	io.Reader
	closes *atomic.Int32
}

func (b *sdkReactionBody) Close() error {
	b.closes.Add(1)
	return nil
}

func TestSDKReactionGuardHonorsResponseBodyDeadline(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Error(err)
			return
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	client := api.NewSDKClient("xoxb-synthetic-reaction", api.SDKOptions{APIURL: server.URL, NoThrottle: true})
	err := client.AddReactionContext(ctx, "rocket", slackgo.NewRefToMessage("C123", "1746284582.123456"))
	if !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 1 {
		t.Fatalf("body deadline was lost or mutation replayed: err=%v calls=%d", err, calls.Load())
	}
}
