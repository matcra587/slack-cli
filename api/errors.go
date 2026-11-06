package api

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"
)

// Error reports a generic-call failure without response bodies or credentials.
// Use errors.As for an underlying slack.SlackErrorResponse or RateLimitedError.
// Raw upstream scope diagnostics are available on those underlying errors;
// treat them as remote data rather than text to print without review.
type Error struct {
	Method     string        `json:"method,omitempty"`
	StatusCode int           `json:"status_code,omitempty"`
	Code       string        `json:"code"`
	RetryAfter time.Duration `json:"retry_after,omitempty"`
	cause      error
}

var safeErrorCode = regexp.MustCompile(`^[a-z][a-z0-9_]{0,127}$`)

func (e *Error) Error() string {
	if e.Method == "" {
		return "Slack API request failed: " + e.Code
	}
	if e.StatusCode != 0 {
		return fmt.Sprintf("Slack API %s failed: %s (HTTP %d)", e.Method, e.Code, e.StatusCode)
	}
	return fmt.Sprintf("Slack API %s failed: %s", e.Method, e.Code)
}

// String protects value formatting as well as the usual error pointer.
func (e Error) String() string { return (&e).Error() }

// GoString keeps provider errors and response contents out of %#v diagnostics.
func (e Error) GoString() string { return e.String() }

// Unwrap preserves error classification without printing provider diagnostics.
func (e *Error) Unwrap() error { return e.cause }

// MarshalJSON includes only the reviewed error summary fields.
func (e Error) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Method     string        `json:"method,omitempty"`
		StatusCode int           `json:"status_code,omitempty"`
		Code       string        `json:"code"`
		RetryAfter time.Duration `json:"retry_after,omitempty"`
	}{e.Method, e.StatusCode, e.Code, e.RetryAfter})
}

func callError(method, code string, status int, cause error) *Error {
	if !safeErrorCode.MatchString(code) {
		code = "invalid_response"
	}
	return &Error{Method: method, Code: code, StatusCode: status, cause: cause}
}
