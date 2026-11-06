package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/matcra587/slack-cli/api"
	slackgo "github.com/slack-go/slack"
)

func localClient(t *testing.T, handler http.HandlerFunc, options ...func(*api.ClientOptions)) (*api.Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	opts := api.ClientOptions{AuthToken: fixtureToken, APIURL: server.URL, HTTPClient: server.Client(), NoThrottle: true}
	for _, configure := range options {
		configure(&opts)
	}
	client, err := api.NewClient(opts)
	if err != nil {
		t.Fatal(err)
	}
	return client, server
}

func TestCallUsesBearerHeaderAndPublishedGETContract(t *testing.T) {
	client, _ := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/conversations.history" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer "+fixtureToken || r.URL.Query().Has("token") {
			t.Error("credential was not confined to Authorization header")
		}
		for key, want := range map[string]string{"channel": "C123", "inclusive": "false", "limit": "0", "latest": "1746284582.123456"} {
			if r.URL.Query().Get(key) != want {
				t.Errorf("parameter %s = %q, want %q", key, r.URL.Query().Get(key), want)
			}
		}
		if r.URL.Query().Has("cursor") {
			t.Error("omitted optional parameter was sent")
		}
		_, _ = io.WriteString(w, `{"ok":true,"messages":[],"has_more":false}`)
	})
	params := map[string]any{"channel": "C123", "inclusive": false, "limit": 0, "latest": "1746284582.123456"}
	var result map[string]any
	if err := client.CallWithContext(t.Context(), "conversations.history", params, &result); err != nil {
		t.Fatal(err)
	}
	if result["has_more"] != false || len(result["messages"].([]any)) != 0 || len(params) != 4 {
		t.Fatal("empty result or caller parameters were changed")
	}
}

func TestCallPreservesJSONArgumentsAndResponseValues(t *testing.T) {
	client, _ := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/chat.postMessage" || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			t.Error("published JSON POST contract was not used")
		}
		if r.Header.Get("Authorization") != "Bearer "+fixtureToken {
			t.Error("bearer authentication missing")
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload["channel"] != "C123" || payload["unfurl_links"] != false || payload["zero"] != float64(0) {
			t.Error("false or zero request parameters changed")
		}
		if value, exists := payload["metadata"]; !exists || value != nil {
			t.Error("explicit null parameter was dropped")
		}
		if _, exists := payload["thread_ts"]; exists {
			t.Error("omitted thread parameter was sent")
		}
		if _, exists := payload["token"]; exists || len(payload["blocks"].([]any)) != 1 {
			t.Error("nested arguments or token isolation changed")
		}
		_, _ = io.WriteString(w, `{"ok":true,"number":9007199254740993,"ts":"1746284582.123456","empty":"","zero":0,"false":false,"null":null}`)
	})
	params := map[string]any{
		"channel": "C123", "unfurl_links": false, "zero": 0, "metadata": nil,
		"blocks": []any{map[string]any{"type": "section", "text": map[string]any{"type": "plain_text", "text": "Synthetic"}}},
	}
	var result map[string]any
	if err := client.CallWithContext(t.Context(), "chat.postMessage", params, &result); err != nil {
		t.Fatal(err)
	}
	if result["number"] != json.Number("9007199254740993") || result["zero"] != json.Number("0") || result["false"] != false || result["empty"] != "" || result["null"] != nil || result["ts"] != "1746284582.123456" {
		t.Fatalf("response values changed: %#v", result)
	}
}

func TestCallUsesPublishedFormEncoding(t *testing.T) {
	client, _ := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/files.getUploadURLExternal" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Error("published form POST contract was not used")
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("filename") != "report.txt" || r.Form.Get("length") != "15" || r.Form.Get("nested") != `{"false":false,"null":null}` || r.Form.Has("token") {
			t.Error("form values changed or included a token")
		}
		_, _ = io.WriteString(w, `{"ok":true,"file_id":"FSYNTHETIC"}`)
	})
	var result json.RawMessage
	if err := client.CallWithContext(t.Context(), "files.getUploadURLExternal", map[string]any{"filename": "report.txt", "length": 15, "nested": map[string]any{"false": false, "null": nil}}, &result); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(result) || !strings.Contains(string(result), "FSYNTHETIC") {
		t.Fatal("raw JSON response was not preserved")
	}
}

func TestCallFailuresPreservePriorState(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"empty response", ""},
		{"truncated response", `{"ok":true,"count":`},
		{"missing ok", `{"count":3}`},
		{"null ok", `{"ok":null}`},
		{"invalid ok", `{"ok":"true"}`},
		{"nonobject response", `[]`},
		{"trailing JSON", `{"ok":true} {"other":true}`},
		{"type mismatch", `{"ok":true,"count":"invalid"}`},
		{"API failure", `{"ok":false,"error":"missing_scope","needed":"channels:history"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := localClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, tc.body) })
			result := struct {
				Count int `json:"count"`
			}{Count: 42}
			if err := client.CallWithContext(t.Context(), "auth.test", nil, &result); err == nil {
				t.Fatal("malformed or failed response was accepted")
			}
			if result.Count != 42 {
				t.Fatal("failed response changed caller state")
			}
		})
	}
}

func TestCallSuccessClearsAbsentFieldsAndFailureRecovery(t *testing.T) {
	var calls atomic.Int32
	client, _ := localClient(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			_, _ = io.WriteString(w, `{"ok":false,"error":"not_authed"}`)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true,"count":0}`)
	})
	result := struct {
		Count int    `json:"count"`
		Old   string `json:"old"`
	}{Count: 42, Old: "prior"}
	if err := client.CallWithContext(t.Context(), "auth.test", nil, &result); err == nil || result.Old != "prior" || calls.Load() != 1 {
		t.Fatal("failed response altered prior state or retried")
	}
	if err := client.CallWithContext(t.Context(), "auth.test", nil, &result); err != nil {
		t.Fatal(err)
	}
	if result.Count != 0 || result.Old != "" || calls.Load() != 2 {
		t.Fatal("explicit recovery did not replace successful response state")
	}
}

func TestCallRetainsScopeAndRateLimitClassification(t *testing.T) {
	client, _ := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth.test" {
			_, _ = io.WriteString(w, `{"ok":false,"error":"missing_scope","needed":"channels:history","provided":"chat:write"}`)
			return
		}
		w.Header().Set("Retry-After", "3")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	var scope slackgo.SlackErrorResponse
	err := client.CallWithContext(t.Context(), "auth.test", nil, nil)
	if !errors.As(err, &scope) || scope.Needed != "channels:history" || scope.Provided != "chat:write" {
		t.Fatalf("scope diagnostics lost: %v", err)
	}
	var rate *slackgo.RateLimitedError
	err = client.CallWithContext(t.Context(), "conversations.history", map[string]any{"channel": "C123"}, nil)
	if !errors.As(err, &rate) || rate.RetryAfter != 3*time.Second {
		t.Fatalf("rate-limit classification lost: %v", err)
	}
}

func TestCallRejectsInvalidRequestsBeforeSending(t *testing.T) {
	var calls atomic.Int32
	client, _ := localClient(t, func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); _, _ = io.WriteString(w, `{"ok":true}`) })
	for _, tc := range []struct {
		name   string
		params map[string]any
		target any
	}{
		{"https://example.com/steal", nil, nil},
		{"../auth.test", nil, nil},
		{"auth.test?token=private", nil, nil},
		{"madeup.method", nil, nil},
		{"auth.test", map[string]any{"token": "synthetic"}, nil},
		{"auth.test", map[string]any{"TOKEN": "synthetic"}, nil},
		{"conversations.history", nil, nil},
		{"oauth.v2.access", nil, nil},
		{"admin.analytics.getFile", map[string]any{"type": "public_channel"}, nil},
		{"auth.test", nil, map[string]any{}},
		{"auth.test", nil, (*json.RawMessage)(nil)},
		{"auth.test", map[string]any{"invalid": make(chan string)}, nil},
	} {
		if err := client.CallWithContext(t.Context(), tc.name, tc.params, tc.target); err == nil {
			t.Fatalf("invalid request %q was accepted", tc.name)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid requests reached transport: %d", calls.Load())
	}
}

func TestCallNeverReplaysFailedMutation(t *testing.T) {
	for _, drop := range []bool{false, true} {
		t.Run(fmt.Sprintf("connection_drop=%v", drop), func(t *testing.T) {
			var calls atomic.Int32
			client, _ := localClient(t, func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				if drop {
					connection, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					_ = connection.Close()
					return
				}
				w.WriteHeader(http.StatusInternalServerError)
			}, func(o *api.ClientOptions) { o.Timeout = 300 * time.Millisecond })
			prior := map[string]any{"unchanged": true}
			err := client.CallWithContext(t.Context(), "chat.postMessage", map[string]any{"channel": "C123", "text": "Synthetic mutation"}, &prior)
			if err == nil || calls.Load() != 1 || !reflect.DeepEqual(prior, map[string]any{"unchanged": true}) {
				t.Fatalf("mutation replayed or changed state: err=%v calls=%d", err, calls.Load())
			}
		})
	}
}

func TestCallBoundsResponsesAndHonorsDeadline(t *testing.T) {
	t.Run("size", func(t *testing.T) {
		client, _ := localClient(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"ok":true,"data":"`+strings.Repeat("x", 100)+`"}`)
		}, func(o *api.ClientOptions) { o.MaxResponseBytes = 32 })
		prior := json.RawMessage(`{"prior":true}`)
		if err := client.CallWithContext(t.Context(), "auth.test", nil, &prior); err == nil || string(prior) != `{"prior":true}` {
			t.Fatal("response limit did not preserve caller state")
		}
	})
	t.Run("deadline", func(t *testing.T) {
		release := make(chan struct{})
		defer close(release)
		client, _ := localClient(t, func(_ http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-release:
			}
		})
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
		defer cancel()
		err := client.CallWithContext(ctx, "auth.test", nil, nil)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("request ignored deadline: %v", err)
		}
	})
}

func TestCallRefusesCredentialForwardingRedirects(t *testing.T) {
	var forwarded atomic.Int32
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		forwarded.Add(1)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer sink.Close()
	client, _ := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, sink.URL+"/steal", http.StatusTemporaryRedirect)
	})
	if err := client.CallWithContext(t.Context(), "auth.test", nil, nil); err == nil || forwarded.Load() != 0 {
		t.Fatal("credential-bearing redirect was followed")
	}
}

func TestCallErrorDiagnosticsDoNotEchoRemoteSecrets(t *testing.T) {
	client, _ := localClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"ok":false,"error":"xoxp-synthetic-public-api","needed":"xoxp-synthetic-public-api","provided":"xoxp-synthetic-public-api"}`)
	})
	err := client.CallWithContext(t.Context(), "auth.test", nil, nil)
	if err == nil {
		t.Fatal("expected Slack error")
	}
	apiErr, ok := errors.AsType[*api.Error](err)
	if !ok {
		t.Fatal("missing public error wrapper")
	}
	for _, value := range []any{err, *apiErr} {
		raw, marshalErr := json.Marshal(value)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		for _, diagnostic := range []string{fmt.Sprint(value), fmt.Sprintf("%+v", value), fmt.Sprintf("%#v", value), string(raw)} {
			if strings.Contains(diagnostic, fixtureToken) {
				t.Fatal("remote response leaked a secret in diagnostics")
			}
		}
	}
}

func TestCallDoesNotRetainRemovedOptionalArguments(t *testing.T) {
	var calls atomic.Int32
	client, _ := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		cursor, present := r.URL.Query()["cursor"]
		if call == 1 && (!present || len(cursor) != 1 || cursor[0] != "page-two") {
			t.Error("first request lost its cursor")
		}
		if call == 2 && present {
			t.Error("removed optional cursor was retained")
		}
		_, _ = io.WriteString(w, `{"ok":true,"messages":[]}`)
	})
	params := map[string]any{"channel": "C123", "cursor": "page-two"}
	if err := client.CallWithContext(t.Context(), "conversations.history", params, nil); err != nil {
		t.Fatal(err)
	}
	delete(params, "cursor")
	if err := client.CallWithContext(t.Context(), "conversations.history", params, nil); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("unexpected request count")
	}
}

type failingTransport struct{ cause error }

func (transport failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, transport.cause
}

func TestCallRedactsTransportDiagnostics(t *testing.T) {
	cause := errors.New("provider https://example.invalid/?token=" + fixtureToken)
	client, err := api.NewClient(api.ClientOptions{
		AuthToken: fixtureToken, NoThrottle: true,
		HTTPClient: &http.Client{Transport: failingTransport{cause: cause}},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = client.CallWithContext(t.Context(), "auth.test", nil, nil)
	if err == nil || !errors.Is(err, cause) {
		t.Fatal("transport error cause was lost")
	}
	encoded, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	for _, formatted := range []string{fmt.Sprint(err), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err), string(encoded)} {
		if strings.Contains(formatted, fixtureToken) || strings.Contains(formatted, "example.invalid") {
			t.Fatal("transport diagnostics exposed sensitive provider text")
		}
	}
}
