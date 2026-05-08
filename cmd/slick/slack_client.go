package main

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/gechr/clog"
	"github.com/matcra587/slack-cli/internal/ratelimit"
	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

// clogAdapter routes slack-go debug output to runtime.Stderr.
type clogAdapter struct{ w io.Writer }

func (a clogAdapter) Output(_ int, s string) error {
	_, err := fmt.Fprintln(a.w, s)
	return err
}

// newSlackClient builds a slack-go Client with the standard option set.
// Always installs: rate-limit transport, retry config, log adapter, debug
// gate, and API URL override. Caller opts are applied last and may override
// any default (including the HTTP client, for auth paths that skip throttling).
func newSlackClient(_ context.Context, cmd *cobra.Command, runtime *RootRuntime, token string, opts ...slackgo.Option) *slackgo.Client {
	stderr := io.Discard
	if runtime.Stderr != nil {
		stderr = runtime.Stderr
	}

	base := http.DefaultTransport
	if runtime.HTTPClient != nil {
		base = runtime.HTTPClient.Transport
		if base == nil {
			base = http.DefaultTransport
		}
	}

	var noThrottle bool
	if cmd != nil {
		noThrottle, _ = cmd.Root().PersistentFlags().GetBool("no-throttle")
	}
	transport := rateLimitTransport{
		base:      base,
		throttler: ratelimit.NewThrottler(),
		disabled:  noThrottle,
	}
	httpClient := &http.Client{Transport: transport}

	defaults := []slackgo.Option{
		slackgo.OptionHTTPClient(httpClient),
		slackgo.OptionRetryConfig(slackRetryConfig()),
		slackgo.OptionLog(clogAdapter{stderr}),
		slackgo.OptionDebug(clog.IsVerbose()),
	}
	if runtime.SlackBaseURL != "" {
		defaults = append(defaults, slackgo.OptionAPIURL(slackAPIURL(runtime.SlackBaseURL)))
	}

	return slackgo.New(token, append(defaults, opts...)...)
}

// noThrottleHTTPClient returns a bare http.Client used by auth paths that
// do not need the proactive rate-limit transport.
func noThrottleHTTPClient(runtime *RootRuntime) *http.Client {
	if runtime.HTTPClient != nil {
		return runtime.HTTPClient
	}
	return &http.Client{}
}
