package credentials

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path"
	"strings"
	"time"
	"unicode"
)

// NewOAuthHTTPClient copies the caller's settings and guards package-level
// OAuth SDK helpers. It reads no credentials and adds no automatic retries.
func NewOAuthHTTPClient(original *http.Client) *http.Client {
	client := &http.Client{}
	if original != nil {
		*client = *original
	}
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	client.Transport = oauthTransport{base: base}
	if client.CheckRedirect == nil {
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	return client
}

type oauthTransport struct{ base http.RoundTripper }

const oauthResponseLimit int64 = 8 << 20

func (t oauthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(req)
	if err != nil || response == nil || req.Method != http.MethodPost || path.Base(req.URL.Path) != "oauth.v2.access" || response.StatusCode < 200 || response.StatusCode >= 300 {
		return response, err
	}
	if response.Body == nil {
		return nil, errors.New("slack returned an empty OAuth response")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, oauthResponseLimit+1))
	_ = response.Body.Close()
	if err != nil {
		if cause := req.Context().Err(); cause != nil {
			return nil, cause
		}
		return nil, errors.New("slack returned an incomplete OAuth response")
	}
	var ack struct {
		OK          *bool           `json:"ok"`
		Error       string          `json:"error"`
		AccessToken string          `json:"access_token"`
		ExpiresIn   json.RawMessage `json:"expires_in"`
		AuthedUser  struct {
			AccessToken string          `json:"access_token"`
			ExpiresIn   json.RawMessage `json:"expires_in"`
		} `json:"authed_user"`
	}
	if int64(len(body)) > oauthResponseLimit || json.Unmarshal(body, &ack) != nil || ack.OK == nil || (*ack.OK && ack.Error != "") || (!*ack.OK && strings.TrimSpace(ack.Error) == "") {
		return nil, errors.New("slack returned an invalid OAuth response")
	}
	if *ack.OK {
		token := ack.AccessToken
		if ack.AuthedUser.AccessToken != "" {
			token = ack.AuthedUser.AccessToken
		}
		if token == "" || strings.IndexFunc(token, unicode.IsSpace) >= 0 || !validOAuthExpiry(ack.ExpiresIn) || !validOAuthExpiry(ack.AuthedUser.ExpiresIn) {
			return nil, errors.New("slack returned invalid OAuth credentials or expiry")
		}
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	return response, nil
}

func validOAuthExpiry(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	var seconds *int64
	return json.Unmarshal(raw, &seconds) == nil && seconds != nil && *seconds >= 0 && *seconds <= (1<<63-1)/int64(time.Second)
}
