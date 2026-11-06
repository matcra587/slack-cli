package api

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// Slack RPC and multipart POSTs carry credentials and can mutate state. Never
// replay them or turn them into GETs after a redirect. Downloads may redirect,
// but HTTP's hostname-based header policy is too broad for a different origin.
func sdkCheckRedirect(req *http.Request, via []*http.Request) error {
	for _, previous := range via {
		if previous.Method != http.MethodGet && previous.Method != http.MethodHead {
			return http.ErrUseLastResponse
		}
	}
	if req.URL.User != nil {
		return http.ErrUseLastResponse
	}
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	if len(via) > 0 && !sdkSameOrigin(req.URL, via[0].URL) {
		for _, key := range []string{"Authorization", "Proxy-Authorization", "Cookie", "Cookie2"} {
			req.Header.Del(key)
		}
	}
	return nil
}

func sdkSameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Hostname(), b.Hostname()) && sdkOriginPort(a) == sdkOriginPort(b)
}

func sdkOriginPort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	if strings.EqualFold(u.Scheme, "https") {
		return "443"
	}
	return "80"
}
