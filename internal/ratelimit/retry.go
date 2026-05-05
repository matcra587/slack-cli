package ratelimit

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"
)

type RetryPolicy struct {
	MaxAttempts int
	BaseBackoff time.Duration
	Clock       Clock
}

type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return "rate limit exhausted"
}

func IsRateLimitError(err error) bool {
	var rateErr *RateLimitError
	return errors.As(err, &rateErr)
}

func Retry(ctx context.Context, policy RetryPolicy, do func() (*http.Response, error)) (*http.Response, error) {
	if policy.MaxAttempts <= 0 {
		policy.MaxAttempts = 1
	}
	if policy.BaseBackoff <= 0 {
		policy.BaseBackoff = time.Second
	}
	if policy.Clock == nil {
		policy.Clock = realClock{}
	}

	var lastRetryAfter time.Duration
	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		resp, err := do()
		if err != nil {
			return resp, err
		}
		if resp == nil || resp.StatusCode != http.StatusTooManyRequests {
			return resp, nil
		}

		lastRetryAfter = retryAfter(resp, fallbackBackoff(policy.BaseBackoff, attempt))
		if attempt == policy.MaxAttempts {
			return resp, &RateLimitError{RetryAfter: lastRetryAfter}
		}
		closeResponseBody(resp)
		if err := policy.Clock.Sleep(ctx, lastRetryAfter); err != nil {
			return resp, err
		}
	}

	return nil, &RateLimitError{RetryAfter: lastRetryAfter}
}

func retryAfter(resp *http.Response, fallback time.Duration) time.Duration {
	header := resp.Header.Get("Retry-After")
	if header == "" {
		return fallback
	}
	seconds, err := strconv.Atoi(header)
	if err != nil || seconds < 0 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}

func closeResponseBody(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_ = resp.Body.Close()
}

func fallbackBackoff(base time.Duration, attempt int) time.Duration {
	if attempt <= 1 {
		return base
	}
	return base * time.Duration(1<<(attempt-1))
}
