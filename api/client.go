package api

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/matcra587/slack-cli/auth"
	"github.com/matcra587/slack-cli/internal/ratelimit"
	"github.com/matcra587/slack-cli/internal/slackcatalog"
	slackgo "github.com/slack-go/slack"
)

const defaultResponseLimit int64 = 8 << 20

// ClientOptions configures a Slack client. AuthToken bypasses all saved config,
// environment, keychain, and secret-manager credential discovery when nonempty.
type ClientOptions struct {
	AuthToken        string `json:"-"`
	Workspace        string
	ConfigPath       string
	APIURL           string
	HTTPClient       *http.Client
	Timeout          time.Duration
	NoThrottle       bool
	CredentialStore  auth.CredentialStore
	SecretReader     auth.SecretReader
	MaxResponseBytes int64
}

// String returns safe configuration metadata, never credential/provider state.
func (o ClientOptions) String() string {
	raw, _ := o.MarshalJSON()
	return string(raw)
}

// GoString keeps %#v from inspecting credentials in options.
func (o ClientOptions) GoString() string { return o.String() }

// MarshalJSON omits credentials, transport internals, and local config paths.
func (o ClientOptions) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Workspace        string        `json:"workspace,omitempty"`
		Timeout          time.Duration `json:"timeout,omitempty"`
		NoThrottle       bool          `json:"no_throttle,omitempty"`
		MaxResponseBytes int64         `json:"max_response_bytes,omitempty"`
	}{o.Workspace, o.Timeout, o.NoThrottle, o.MaxResponseBytes})
}

// Client combines native SDK operations with catalog-checked generic calls.
// Generic calls never retry automatically; native SDK methods retain the SDK's
// configured retry behavior. Create a client when starting an operation to pick
// up a new workspace selection or a replaced saved credential.
type Client struct {
	*slackgo.Client
	endpoint         string
	token            string
	httpClient       *http.Client
	maxResponseBytes int64
	session          auth.Session
	throttler        *ratelimit.Throttler
	throttleGate     chan struct{}
	noThrottle       bool
}

// DefaultClient resolves the saved default workspace and creates a client.
func DefaultClient() (*Client, error) { return NewClient(ClientOptions{}) }

// NewClient creates a client using background context for credential resolution.
// Use NewClientWithContext when resolution needs a deadline or cancellation.
func NewClient(opts ClientOptions) (*Client, error) {
	return NewClientWithContext(context.Background(), opts)
}

// NewClientWithContext creates a client, resolving saved credentials only when
// AuthToken is empty. Construction makes no Slack API request unless a saved
// OAuth credential needs refreshing.
func NewClientWithContext(ctx context.Context, opts ClientOptions) (*Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if opts.Timeout < 0 || opts.MaxResponseBytes < 0 || opts.MaxResponseBytes == 1<<63-1 {
		return nil, errors.New("timeout and response size limits must be valid nonnegative values")
	}
	endpoint, err := normalizedAPIURL(opts.APIURL)
	if err != nil {
		return nil, err
	}
	httpClient := configuredHTTPClient(opts.HTTPClient, opts.Timeout)
	value := opts.AuthToken
	var session auth.Session
	if value == "" {
		session, err = auth.Resolve(ctx, auth.Options{
			Workspace: opts.Workspace, ConfigPath: opts.ConfigPath,
			HTTPClient: httpClient, CredentialStore: opts.CredentialStore,
			SecretReader: opts.SecretReader, SlackBaseURL: endpoint,
		})
		if err != nil {
			return nil, err
		}
		value = session.Token()
	}
	if strings.TrimSpace(value) == "" || strings.ContainsAny(value, "\r\n") {
		return nil, errors.New("slack access token is empty or malformed")
	}
	limit := opts.MaxResponseBytes
	if limit == 0 {
		limit = defaultResponseLimit
	}
	return &Client{
		Client:   NewSDKClient(value, SDKOptions{HTTPClient: httpClient, APIURL: endpoint, NoThrottle: opts.NoThrottle}),
		endpoint: endpoint, token: value, httpClient: httpClient,
		maxResponseBytes: limit, session: session,
		throttler: ratelimit.NewThrottler(), throttleGate: make(chan struct{}, 1), noThrottle: opts.NoThrottle,
	}, nil
}

// SDK returns the native SDK client for operations outside the generic catalog.
func (c *Client) SDK() *slackgo.Client { return c.Client }

// Session returns the resolved workspace identity. Explicit-token clients have
// no saved session. Session's formatted/JSON representations omit credentials.
func (c *Client) Session() auth.Session { return c.session }

// String returns safe client identity metadata without credentials or transport.
func (c Client) String() string {
	raw, _ := c.MarshalJSON()
	return string(raw)
}

// GoString keeps %#v from inspecting the embedded SDK's private token.
func (c Client) GoString() string { return c.String() }

// MarshalJSON exposes safe identity metadata only.
func (c Client) MarshalJSON() ([]byte, error) { return json.Marshal(c.session) }

// Method describes a published Slack API method and slick's execution policy.
type Method = slackcatalog.Method

// Methods lists the catalog without reading saved credentials or calling Slack.
func Methods() []Method { return slackcatalog.All() }

// LookupMethod looks up a canonical method or its documentation spelling.
func LookupMethod(name string) (Method, bool) { return slackcatalog.Lookup(name) }

func configuredHTTPClient(original *http.Client, timeout time.Duration) *http.Client {
	client := &http.Client{Timeout: 30 * time.Second}
	if original != nil {
		copy := *original
		client = &copy
	}
	if timeout > 0 {
		client.Timeout = timeout
	}
	redirect := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if redirect != nil {
			if err := redirect(req, via); err != nil {
				return err
			}
		}
		return errors.New("slack API redirects are not followed")
	}
	return client
}

func normalizedAPIURL(value string) (string, error) {
	if value == "" {
		return "https://slack.com/api/", nil
	}
	u, err := url.Parse(value)
	if err != nil || strings.TrimSpace(value) != value || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || u.RawPath != "" {
		return "", errors.New("invalid Slack API URL")
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || (u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback())) {
			return "", errors.New("slack API URL must use HTTPS; HTTP is allowed only for explicit loopback fixtures")
		}
	}
	if u.Path != "" && u.Path != "/" && path.Clean(u.Path) != strings.TrimSuffix(u.Path, "/") {
		return "", errors.New("slack API URL contains an invalid path")
	}
	return sdkAPIURL(u.String()), nil
}
