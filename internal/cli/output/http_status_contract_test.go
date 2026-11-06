package output_test

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"testing"

	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	slackgo "github.com/slack-go/slack"
)

func TestSlackNumericHTTPFailuresPreserveClassAndPrivacy(t *testing.T) {
	for _, code := range []int{401, 403, 404, 429, 500, 503, 599} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			t.Parallel()
			err := &url.Error{Op: "Post", URL: "https://fixture.invalid/synthetic-http-rate-canary", Err: fmt.Errorf("wrapped: %w", slackgo.StatusCodeError{Code: code, Status: "synthetic-http-rate-canary"})}
			got := clioutput.CliErrorFromSlack(context.Background(), err, "")
			kind, exit := "server_error", 5
			if code == 429 {
				kind, exit = "rate_limit", 3
			}
			if got.Type != kind || got.ExitCode != exit || strings.Contains(got.Message, "synthetic-http-rate-canary") {
				t.Fatalf("HTTP%d lost classification or privacy: %#v", code, got)
			}
			if got.RetryAfterSeconds != nil {
				t.Fatal("numeric HTTP status invented retry timing")
			}
		})
	}
}

func TestSlackRateLimitOptionalSecondsSurviveErrorWrapping(t *testing.T) {
	cases := []struct {
		name    string
		seconds *int
	}{{"unknown", nil}, {"zero", new(0)}, {"known", new(7)}}
	if strconv.IntSize == 64 {
		seconds, _ := strconv.Atoi("10000000000")
		cases = append(cases, struct {
			name    string
			seconds *int
		}{"beyond duration", &seconds})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := &url.Error{Op: "Post", URL: "https://fixture.invalid/synthetic-http-rate-canary", Err: httpRateTiming{seconds: tc.seconds}}
			got := clioutput.CliErrorFromSlack(context.Background(), err, "")
			if got.Type != "rate_limit" || got.ExitCode != 3 || got.Message != "ratelimited" || (got.RetryAfterSeconds == nil) != (tc.seconds == nil) || (tc.seconds != nil && *got.RetryAfterSeconds != *tc.seconds) {
				t.Fatal("rate-limit error lost class or optional exact seconds")
			}
		})
	}
}

type httpRateTiming struct{ seconds *int }

func (httpRateTiming) Error() string { return "synthetic-http-rate-canary" }
func (httpRateTiming) Unwrap() error { return slackgo.SlackErrorResponse{Err: "ratelimited"} }
func (e httpRateTiming) RetryAfterSeconds() *int {
	if e.seconds == nil {
		return nil
	}
	seconds := *e.seconds
	return &seconds
}
