package api

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"mime"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync"

	slackgo "github.com/slack-go/slack"
)

type sdkCallKey struct{}

type sdkCall struct {
	method    string
	guarded   bool
	request   *http.Request
	pacing    sync.Once
	pacingErr error
}

// Tag a logical SDK call before http.Client follows redirects. This preserves
// caller timeout, cookie and redirect policies without inspecting private SDK
// fields or treating file downloads as Web API responses.
type sdkHTTPClient struct {
	client   *http.Client
	endpoint *url.URL
}

func newSDKHTTPClient(client *http.Client, baseURL string) *sdkHTTPClient {
	endpoint := slackgo.APIURL
	if baseURL != "" {
		endpoint = sdkAPIURL(baseURL)
	}
	parsed, _ := url.Parse(endpoint) // The SDK reports an invalid URL when it builds the request.
	return &sdkHTTPClient{client: client, endpoint: parsed}
}

func (c *sdkHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if method, guarded := c.classify(req); method != "" {
		call := &sdkCall{method: method, guarded: guarded, request: req}
		req = req.WithContext(context.WithValue(req.Context(), sdkCallKey{}, call))
	}
	return c.client.Do(req)
}

var sdkMethodNameRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(?:\.[A-Za-z][A-Za-z0-9_]*)+$`)

func (c *sdkHTTPClient) classify(req *http.Request) (string, bool) {
	method := path.Base(req.URL.Path)
	if !sdkMethodNameRE.MatchString(method) {
		return "", false
	}
	mediaType, _, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
	if err != nil {
		return "", false
	}
	bearer := strings.HasPrefix(req.Header.Get("Authorization"), "Bearer ")
	known := c.endpoint != nil && strings.EqualFold(req.URL.Scheme, c.endpoint.Scheme) &&
		strings.EqualFold(req.URL.Host, c.endpoint.Host) && req.URL.Path == c.endpoint.Path+method
	switch req.Method {
	case http.MethodGet:
		// SDK GET Web API calls set a form content type; raw downloads do not.
		if mediaType == "application/x-www-form-urlencoded" && bearer {
			return method, false
		}
	case http.MethodPost:
		switch mediaType {
		case "application/x-www-form-urlencoded":
			if known || sdkFormHasToken(req) {
				return method, true
			}
		case "application/json":
			if known || bearer {
				return method, true
			}
		case "multipart/form-data":
			// Native multipart methods at the configured endpoint retain
			// pacing, but transfer replies have no Web API envelope contract.
			// Also retain canonical /api/ pacing for opaque native overrides.
			if known || (req.URL.Path == "/api/"+method && bearer) {
				return method, false
			}
		}
	}
	return "", false
}

// Native OptionAPIURL is opaque. Recognize its SDK form requests by the token
// key using a separate replay reader. Stream past arbitrarily large values
// without parsing, logging or consuming the real request body or credential.
func sdkFormHasToken(req *http.Request) bool {
	if req.GetBody == nil {
		return false
	}
	body, err := req.GetBody()
	if err != nil || body == nil {
		return false
	}
	defer func() { _ = body.Close() }()
	reader := bufio.NewReader(body)
	fieldStart := true
	for {
		part, err := reader.ReadSlice('&')
		if fieldStart && bytes.HasPrefix(part, []byte("token=")) {
			return true
		}
		fieldStart = err == nil
		if err != nil && !errors.Is(err, bufio.ErrBufferFull) {
			return false
		}
	}
}

func sdkResponseMethod(req *http.Request) string {
	if call, ok := req.Context().Value(sdkCallKey{}).(*sdkCall); ok && call.guarded {
		return call.method
	}
	return ""
}
