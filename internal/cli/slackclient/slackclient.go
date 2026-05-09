package slackclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gechr/clog"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	"github.com/matcra587/slack-cli/internal/config"
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

type rateLimitTransport struct {
	base      http.RoundTripper
	throttler *ratelimit.Throttler
	disabled  bool
}

func (t rateLimitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !t.disabled && strings.HasPrefix(req.URL.Path, "/api/") {
		method := strings.TrimPrefix(req.URL.Path, "/api/")
		if err := t.throttler.Wait(req.Context(), ratelimit.TierForMethod(method)); err != nil {
			return nil, err
		}
	}
	return t.base.RoundTrip(req)
}

func retryConfig() slackgo.RetryConfig {
	cfg := slackgo.DefaultRetryConfig()
	cfg.MaxRetries = 2
	cfg.RetryAfterJitter = 0
	cfg.BackoffJitter = 0
	cfg.Handlers = slackgo.DefaultRetryHandlers(cfg)
	return cfg
}

func apiURL(baseURL string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(baseURL, "/api") {
		return baseURL + "/"
	}
	return baseURL + "/api/"
}

// New builds a slack-go Client with the standard option set.
func New(_ context.Context, cmd *cobra.Command, runtime *cliruntime.RootRuntime, token string, opts ...slackgo.Option) *slackgo.Client {
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
		slackgo.OptionRetryConfig(retryConfig()),
		slackgo.OptionLog(clogAdapter{stderr}),
		slackgo.OptionDebug(clog.IsVerbose()),
	}
	if runtime.SlackBaseURL != "" {
		defaults = append(defaults, slackgo.OptionAPIURL(apiURL(runtime.SlackBaseURL)))
	}

	return slackgo.New(token, append(defaults, opts...)...)
}

// NoThrottle returns a bare http.Client used by auth paths that do not need
// the proactive rate-limit transport.
func NoThrottle(runtime *cliruntime.RootRuntime) *http.Client {
	if runtime.HTTPClient != nil {
		return runtime.HTTPClient
	}
	return &http.Client{}
}

// Client resolves a token for the given profile and returns an authenticated
// slack-go client. NewRootCommand populates runtime.TokenResolver eagerly so
// callers can assume it is non-nil.
func Client(cmd *cobra.Command, profile config.WorkspaceProfile, runtime *cliruntime.RootRuntime) (*slackgo.Client, error) {
	token, err := runtime.TokenResolver.ResolveToken(cmd.Context(), profile)
	if err != nil {
		if errors.Is(err, config.ErrCredentialNotFound) {
			workspace := profile.Name
			if workspace == "" {
				workspace = "selected workspace"
			}
			return nil, fmt.Errorf("workspace %s is not authenticated; run slick auth login or switch to an authenticated workspace: %w", workspace, err)
		}
		return nil, err
	}
	return New(cmd.Context(), cmd, runtime, token), nil
}
