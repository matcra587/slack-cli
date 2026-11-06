package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	slackgo "github.com/slack-go/slack"
)

// Keep the SDK's bounded retries for a usable Slack delay. An absent,
// ambiguous or unrepresentable delay must not become a guessed retry or an
// overflowing time.Duration. Reject it before the SDK retry client sees it.
func guardSDKHTTPRateLimit(response *http.Response) (*http.Response, error) {
	if response == nil || response.StatusCode != http.StatusTooManyRequests {
		return response, nil
	}
	var timing *int
	values := response.Header.Values("Retry-After")
	if len(values) == 1 {
		value := strings.TrimSpace(values[0])
		if value != "" && strings.IndexFunc(value, func(r rune) bool { return r < '0' || r > '9' }) < 0 {
			if seconds, err := strconv.Atoi(value); err == nil {
				timing = &seconds
				if int64(seconds) <= int64((1<<63-1)/time.Second) {
					// Normalize only our copy; the supplied transport may reuse its headers.
					if value != values[0] {
						copy := *response
						copy.Header = response.Header.Clone()
						copy.Header.Set("Retry-After", value)
						response = &copy
					}
					return response, nil
				}
			}
		}
	}
	if response.Body != nil {
		_ = response.Body.Close()
	}
	return nil, sdkHTTPRateLimitError{seconds: timing}
}

type sdkHTTPRateLimitError struct{ seconds *int }

func (sdkHTTPRateLimitError) Error() string { return "ratelimited" }
func (sdkHTTPRateLimitError) Unwrap() error { return slackgo.SlackErrorResponse{Err: "ratelimited"} }

// RetryAfterSeconds exposes exact integer timing when time.Duration cannot
// represent it. Nil means the response supplied no trustworthy delay.
func (e sdkHTTPRateLimitError) RetryAfterSeconds() *int {
	if e.seconds == nil {
		return nil
	}
	seconds := *e.seconds
	return &seconds
}
