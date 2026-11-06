// Package api provides authenticated Slack clients for Go applications.
package api

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/matcra587/slack-cli/internal/ratelimit"
	slackgo "github.com/slack-go/slack"
)

// SDKOptions configures a native Slack SDK client without reading credentials.
// Callers supply the token explicitly; request contexts control cancellation.
type SDKOptions struct {
	HTTPClient *http.Client
	APIURL     string
	Log        io.Writer
	Debug      bool
	NoThrottle bool
}

// NewSDKClient creates a native slack-go client with slick's transport policy.
// A supplied HTTP client is copied so its timeout, explicit redirect policy, and cookie jar
// remain effective without replacing its transport in the caller.
// Native SDK options run last and follow their SDK semantics. The returned client
// exposes every operation in the SDK version selected by slick's Go module.
func NewSDKClient(token string, opts SDKOptions, options ...slackgo.Option) *slackgo.Client {
	log := opts.Log
	if log == nil {
		log = io.Discard
	}
	base := http.DefaultTransport
	if opts.HTTPClient != nil && opts.HTTPClient.Transport != nil {
		base = opts.HTTPClient.Transport
	}
	transport := sdkRateLimitTransport{
		base:      base,
		throttler: ratelimit.NewThrottler(),
		disabled:  opts.NoThrottle,
	}
	httpClient := &http.Client{}
	if opts.HTTPClient != nil {
		copy := *opts.HTTPClient
		httpClient = &copy
	}
	httpClient.Transport = transport
	if httpClient.CheckRedirect == nil {
		httpClient.CheckRedirect = sdkCheckRedirect
	}
	defaults := []slackgo.Option{
		slackgo.OptionHTTPClient(newSDKHTTPClient(httpClient, opts.APIURL)),
		slackgo.OptionRetryConfig(sdkRetryConfig()),
		slackgo.OptionLog(&sdkLogAdapter{w: log}),
		slackgo.OptionDebug(opts.Debug),
	}
	if opts.APIURL != "" {
		defaults = append(defaults, slackgo.OptionAPIURL(sdkAPIURL(opts.APIURL)))
	}
	return slackgo.New(token, append(defaults, options...)...)
}

type sdkLogAdapter struct {
	mu sync.Mutex
	w  io.Writer
}

var sdkDiagnosticStatusRE = regexp.MustCompile(`^HTTP/[0-9]+(?:\.[0-9]+)? ([1-5][0-9]{2})(?:[ \r\n]|$)`)

// SDK diagnostics can contain complete request payloads, response headers and
// opaque error bodies. Summarize recognized events without forwarding their
// contents; a credential-field denylist cannot protect unknown secret fields.
func (a *sdkLogAdapter) Output(_ int, s string) error {
	summary := "Slack SDK diagnostic: details omitted"
	switch {
	case strings.HasPrefix(s, "Sending request:"):
		summary = "Slack SDK request: payload omitted"
	case strings.HasPrefix(s, "slack retry:"):
		summary = "Slack SDK retry: details omitted"
	case strings.TrimSpace(s) == "Challenging auth...":
		summary = "Slack SDK auth check"
	default:
		if match := sdkDiagnosticStatusRE.FindStringSubmatch(s); len(match) == 2 {
			if code, err := strconv.Atoi(match[1]); err == nil {
				summary = fmt.Sprintf("Slack HTTP response: %d %s (headers and body omitted)", code, http.StatusText(code))
			}
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	_, err := fmt.Fprintln(a.w, summary)
	return err
}

type sdkRateLimitTransport struct {
	base      http.RoundTripper
	throttler *ratelimit.Throttler
	disabled  bool
}

func (t sdkRateLimitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	call, _ := ctx.Value(sdkCallKey{}).(*sdkCall)
	if call != nil && !t.disabled {
		// HTTP-client timeouts include pacing. Redirect hops share this
		// reservation; a separate SDK retry receives a new logical call.
		call.pacing.Do(func() {
			call.pacingErr = t.throttler.Wait(ctx, ratelimit.TierForMethod(call.method))
		})
		if call.pacingErr != nil {
			return nil, call.pacingErr
		}
	}
	response, err := t.base.RoundTrip(req)
	if err != nil {
		return response, err
	}
	if call != nil {
		// An explicit caller redirect policy may change the final URL or
		// HTTP verb. Validate the original SDK operation's reply contract.
		req = call.request.WithContext(req.Context())
	}
	response, err = guardSDKHTTPRateLimit(response)
	if err != nil {
		return response, err
	}
	response, err = guardSDKAuthResponse(req, response)
	if err != nil {
		return response, err
	}
	response, err = guardSDKReactionResponse(req, response)
	if err != nil {
		return response, err
	}
	response, err = guardSDKUploadResponse(req, response)
	if err != nil {
		return response, err
	}
	response, err = guardSDKStatusResponse(req, response)
	if err != nil {
		return response, err
	}
	response, err = guardSDKMessageResponse(req, response)
	if err != nil {
		return response, err
	}
	response, err = guardSDKHistoryResponse(req, response)
	if err != nil {
		return response, err
	}
	return guardSDKLookupResponse(req, response)
}

func sdkRetryConfig() slackgo.RetryConfig {
	cfg := slackgo.DefaultRetryConfig()
	cfg.MaxRetries = 2
	cfg.RetryAfterJitter = 0
	cfg.BackoffJitter = 0
	cfg.Handlers = slackgo.DefaultRetryHandlers(cfg)
	return cfg
}

func sdkAPIURL(baseURL string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(baseURL, "/api") {
		return baseURL + "/"
	}
	return baseURL + "/api/"
}
