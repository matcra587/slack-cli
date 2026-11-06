package api_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/matcra587/slack-cli/api"
	slackgo "github.com/slack-go/slack"
)

func TestSDKHTTPRateLimitsKeepTimingWithoutUnsafeReplay(t *testing.T) {
	for _, tc := range []struct {
		name    string
		headers []string
		seconds string
		native  bool
	}{
		{name: "missing"},
		{name: "invalid", headers: []string{"synthetic-http-rate-canary"}},
		{name: "negative", headers: []string{"-1"}},
		{name: "plus", headers: []string{"+7"}},
		{name: "fractional", headers: []string{"1.5"}},
		{name: "HTTP date", headers: []string{"Wed, 21 Oct 2037 07:28:00 GMT"}},
		{name: "duplicate", headers: []string{"7", "9"}},
		{name: "comma joined", headers: []string{"7, 9"}},
		{name: "overflow", headers: []string{"9223372036854775808"}},
		{name: "duration overflow", headers: []string{"10000000000"}, seconds: "10000000000"},
		{name: "zero", headers: []string{"0"}, seconds: "0", native: true},
		{name: "seconds", headers: []string{"7"}, seconds: "7", native: true},
		{name: "leading zero", headers: []string{"007"}, seconds: "7", native: true},
		{name: "duration boundary", headers: []string{"9223372036"}, seconds: "9223372036", native: strconv.IntSize == 64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			var recovered atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Method != http.MethodPost || r.URL.Path != "/api/users.profile.set" || r.URL.RawQuery != "" || r.PostForm.Get("token") != "xoxp-synthetic-http-rate" {
					t.Error("rate-limit handling changed request method/path/auth")
				}
				if recovered.Load() {
					_, _ = io.WriteString(w, `{"ok":true}`)
					return
				}
				for _, value := range tc.headers {
					w.Header().Add("Retry-After", value)
				}
				w.WriteHeader(429)
				_, _ = io.WriteString(w, "synthetic-http-rate-canary")
			}))
			defer server.Close()
			var diagnostics bytes.Buffer
			client := api.NewSDKClient("xoxp-synthetic-http-rate", api.SDKOptions{APIURL: server.URL, NoThrottle: true, Debug: true, Log: &diagnostics})
			ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
			err := client.SetUserCustomStatusContext(ctx, "PR review", ":eyes:", 0)
			cancel()
			if err == nil {
				t.Fatal("HTTP429 reported success")
			}
			if calls.Load() != 1 {
				t.Fatal("HTTP429 with unusable timing replayed a mutation")
			}
			if tc.native {
				// The default SDK waits at least one second, including Retry-After: 0.
				// This shorter request deadline must stop that wait before a replay.
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("usable retry timing bypassed the SDK wait: %v", err)
				}
			} else {
				var code slackgo.SlackErrorResponse
				if !errors.As(err, &code) || code.Err != "ratelimited" {
					t.Fatalf("HTTP429 lost rate-limit classification: %v", err)
				}
				var timing interface{ RetryAfterSeconds() *int }
				if !errors.As(err, &timing) {
					t.Fatal("rate-limit error lost optional seconds")
				}
				want := tc.seconds
				if strconv.IntSize == 32 && (tc.name == "duration overflow" || tc.name == "duration boundary") {
					want = ""
				}
				got := timing.RetryAfterSeconds()
				if (got == nil) != (want == "") || (got != nil && strconv.Itoa(*got) != want) {
					t.Fatal("rate limit invented, overflowed or lost its retry delay")
				}
				if got != nil {
					*got = 17
					again := timing.RetryAfterSeconds()
					if again == nil || strconv.Itoa(*again) != want {
						t.Fatal("caller changed stored retry timing")
					}
				}
			}
			if strings.Contains(err.Error()+diagnostics.String(), "synthetic-http-rate-canary") || strings.Contains(diagnostics.String(), "xoxp-synthetic-http-rate") {
				t.Fatal("HTTP429 diagnostics leaked private response, header or credentials")
			}
			recovered.Store(true)
			if err := client.SetUserCustomStatusContext(t.Context(), "PR review", ":eyes:", 0); err != nil || calls.Load() != 2 {
				t.Fatalf("explicit invocation did not recover exactly once: %v", err)
			}
		})
	}
}

func TestSDKUnknownRateTimingClosesWithoutReadingBody(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(strconv.FormatBool(empty), func(t *testing.T) {
			var calls, closes, reads atomic.Int32
			transport := sdkTestRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				var body io.ReadCloser
				if !empty {
					body = &sdkReactionBody{Reader: sdkRateReadCounter{reads: &reads}, closes: &closes}
				}
				return &http.Response{StatusCode: 429, Header: make(http.Header), Body: body, Request: r}, nil
			})
			client := api.NewSDKClient("xoxp-synthetic-http-rate", api.SDKOptions{HTTPClient: &http.Client{Transport: transport}, NoThrottle: true})
			ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
			defer cancel()
			err := client.SetUserCustomStatusContext(ctx, "PR review", ":eyes:", 0)
			var code slackgo.SlackErrorResponse
			wantCloses := int32(1)
			if empty {
				wantCloses = 0
			}
			if reads.Load() != 0 {
				t.Fatal("rate limit consumed an unreviewed response body")
			}
			if !errors.As(err, &code) || code.Err != "ratelimited" || calls.Load() != 1 || closes.Load() != wantCloses || reads.Load() != 0 {
				t.Fatalf("unknown timing leaked body, lost classification or replayed: %v", err)
			}
		})
	}
}

type sdkRateReadCounter struct{ reads *atomic.Int32 }

func (r sdkRateReadCounter) Read([]byte) (int, error) {
	r.reads.Add(1)
	return 0, errors.New("synthetic-http-rate-canary")
}

func TestSDKUsableRateLimitRetainsBoundedRetriesAndNativeError(t *testing.T) {
	var calls atomic.Int32
	var recovered atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/api/users.profile.set" || r.FormValue("token") != "xoxp-synthetic-http-rate" {
			t.Error("wrong bounded-retry request")
		}
		if recovered.Load() {
			_, _ = io.WriteString(w, `{"ok":true}`)
			return
		}
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(429)
		_, _ = io.WriteString(w, "synthetic-http-rate-canary")
	}))
	defer server.Close()
	client := api.NewSDKClient("xoxp-synthetic-http-rate", api.SDKOptions{APIURL: server.URL, NoThrottle: true})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	err := client.SetUserCustomStatusContext(ctx, "PR review", ":eyes:", 0)
	var native *slackgo.RateLimitedError
	if !errors.As(err, &native) || native.RetryAfter != 0 || calls.Load() != 3 {
		t.Fatalf("default bounded retries or native rate error changed: %v requests=%d", err, calls.Load())
	}
	recovered.Store(true)
	if err := client.SetUserCustomStatusContext(t.Context(), "PR review", ":eyes:", 0); err != nil || calls.Load() != 4 {
		t.Fatalf("exhausted rate limit did not recover on new invocation: %v", err)
	}
}

func TestSDKRateLimitGuardLeavesTransportHeadersUnchanged(t *testing.T) {
	original := http.Header{"Retry-After": []string{" 0 "}, "X-Fixture": []string{"preserve"}}
	var calls atomic.Int32
	transport := sdkTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: 429, Header: original, Body: io.NopCloser(strings.NewReader("synthetic-http-rate-canary")), Request: r}, nil
	})
	client := api.NewSDKClient("xoxp-synthetic-http-rate", api.SDKOptions{HTTPClient: &http.Client{Transport: transport}, NoThrottle: true})
	ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancel()
	if err := client.SetUserCustomStatusContext(ctx, "PR review", ":eyes:", 0); !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 1 {
		t.Fatalf("usable normalized header lost SDK wait: %v", err)
	}
	if original.Get("Retry-After") != " 0 " || original.Get("X-Fixture") != "preserve" || len(original) != 2 {
		t.Fatal("rate-limit guard mutated caller-owned transport headers")
	}
}

func TestSDKDownloadRateLimitPreservesWriterAndRecovers(t *testing.T) {
	var calls atomic.Int32
	var repaired atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/download" || r.Header.Get("Authorization") != "Bearer xoxp-synthetic-http-rate" {
			t.Error("download rate limit changed request/auth")
		}
		if repaired.Load() {
			_, _ = io.WriteString(w, "download bytes")
			return
		}
		w.WriteHeader(429)
		_, _ = io.WriteString(w, "synthetic-http-rate-canary")
	}))
	defer server.Close()
	client := api.NewSDKClient("xoxp-synthetic-http-rate", api.SDKOptions{APIURL: server.URL, NoThrottle: true})
	var dst bytes.Buffer
	_, _ = dst.WriteString("prior bytes")
	ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancel()
	err := client.GetFileContext(ctx, server.URL+"/download", &dst)
	var code slackgo.SlackErrorResponse
	if !errors.As(err, &code) || code.Err != "ratelimited" || calls.Load() != 1 || dst.String() != "prior bytes" {
		t.Fatalf("download rate limit replayed, lost class or changed destination: %v", err)
	}
	repaired.Store(true)
	if err := client.GetFileContext(t.Context(), server.URL+"/download", &dst); err != nil || calls.Load() != 2 || dst.String() != "prior bytesdownload bytes" {
		t.Fatalf("download did not recover while preserving bytes: %v", err)
	}
}
