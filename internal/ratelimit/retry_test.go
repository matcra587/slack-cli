package ratelimit_test

import (
	"context"
	"net/http"
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
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     http.Header{"Retry-After": []string{"3"}},
			}, nil
		}
		return &http.Response{StatusCode: http.StatusOK}, nil
	})

	if err != nil {
		t.Fatalf("Retry returned error: %v", err)
	}
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

	_, err := ratelimit.Retry(context.Background(), ratelimit.RetryPolicy{
		MaxAttempts: 2,
		BaseBackoff: time.Second,
		Clock:       clock,
	}, func() (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusTooManyRequests}, nil
	})
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
