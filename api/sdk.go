// Package api provides authenticated Slack clients for Go applications.
package api

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
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
// A supplied HTTP client is copied so its timeout, redirects, and cookie jar
// remain effective without replacing its transport in the caller.
// Native SDK options run last and can override the defaults. The returned client
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
	defaults := []slackgo.Option{
		slackgo.OptionHTTPClient(httpClient),
		slackgo.OptionRetryConfig(sdkRetryConfig()),
		slackgo.OptionLog(&sdkLogAdapter{w: log, token: token}),
		slackgo.OptionDebug(opts.Debug),
	}
	if opts.APIURL != "" {
		defaults = append(defaults, slackgo.OptionAPIURL(sdkAPIURL(opts.APIURL)))
	}
	return slackgo.New(token, append(defaults, options...)...)
}

type sdkLogAdapter struct {
	mu    sync.Mutex
	w     io.Writer
	token string
}

// SDK debug output may contain an unparsed HTTP error body. Redact configured
// credentials and recognized credential fields before handing text to callers.
var sdkDiagnosticRedactors = sync.OnceValue(func() []*regexp.Regexp {
	return []*regexp.Regexp{
		regexp.MustCompile(`(?i)("(?:access_token|refresh_token|client_secret|token|authorization)"\s*:\s*)"(?:\\.|[^"\\])*(?:"|$)`),
		regexp.MustCompile(`(?i)((?:access_token|refresh_token|client_secret|token)=)[^&\s]*`),
		regexp.MustCompile(`(?im)^([ \t]*authorization[ \t]*:[ \t]*)[^\r\n]*`),
	}
})

func (a *sdkLogAdapter) Output(_ int, s string) error {
	if a.token != "" {
		s = strings.ReplaceAll(s, a.token, "[REDACTED]")
	}
	for _, pattern := range sdkDiagnosticRedactors() {
		s = pattern.ReplaceAllString(s, "${1}[REDACTED]")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	_, err := fmt.Fprintln(a.w, s)
	return err
}

type sdkRateLimitTransport struct {
	base      http.RoundTripper
	throttler *ratelimit.Throttler
	disabled  bool
}

func (t sdkRateLimitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !t.disabled && strings.HasPrefix(req.URL.Path, "/api/") {
		method := strings.TrimPrefix(req.URL.Path, "/api/")
		if err := t.throttler.Wait(req.Context(), ratelimit.TierForMethod(method)); err != nil {
			return nil, err
		}
	}
	return t.base.RoundTrip(req)
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
