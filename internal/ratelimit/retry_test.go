package ratelimit_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/matcra587/slack-cli/internal/ratelimit"
)

func TestRetrySleepsRetryAfterOn429(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	attempts := 0

	resp, err := ratelimit.Retry(context.Background(), ratelimit.RetryPolicy{
		MaxAttempts: 2,
		BaseBackoff: time.Second,
		Clock:       clock,
	}, func() (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return retryResponse(http.StatusTooManyRequests, "3"), nil
		}
		return retryResponse(http.StatusOK, ""), nil
	})
	if err != nil {
		t.Fatalf("Retry returned error: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if len(clock.sleeps) != 1 || clock.sleeps[0] != 3*time.Second {
		t.Fatalf("sleeps = %#v, want retry-after 3s", clock.sleeps)
	}
}

func TestRetryReturnsRateLimitErrorAfterExhaustion(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}

	resp, err := ratelimit.Retry(context.Background(), ratelimit.RetryPolicy{
		MaxAttempts: 2,
		BaseBackoff: time.Second,
		Clock:       clock,
	}, func() (*http.Response, error) {
		return retryResponse(http.StatusTooManyRequests, ""), nil
	})
	if resp != nil {
		defer func() {
			_ = resp.Body.Close()
		}()
	}
	if err == nil {
		t.Fatal("Retry returned nil error")
	}
	if !ratelimit.IsRateLimitError(err) {
		t.Fatalf("Retry error = %T, want rate limit error", err)
	}
	if len(clock.sleeps) != 1 || clock.sleeps[0] != time.Second {
		t.Fatalf("sleeps = %#v, want exponential fallback 1s", clock.sleeps)
	}
}

func retryResponse(statusCode int, retryAfter string) *http.Response {
	resp := &http.Response{
		StatusCode: statusCode,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader("")),
	}
	if retryAfter != "" {
		resp.Header.Set("Retry-After", retryAfter)
	}
	return resp
}
