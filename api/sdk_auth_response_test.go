package api_test

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/api"
	slackgo "github.com/slack-go/slack"
)

func TestSDKAuthResponsesRequireExplicitAcknowledgement(t *testing.T) {
	for _, tc := range []struct {
		name, method, body string
		valid, revoked     bool
		code               string
	}{
		{"identity and scopes", "auth.test", `{"ok":true,"team_id":"T_FIXTURE","user_id":"U_FIXTURE"}`, true, false, ""},
		{"missing auth acknowledgement", "auth.test", `{"team_id":"T_FIXTURE","user_id":"U_FIXTURE"}`, false, false, ""},
		{"contradictory auth response", "auth.test", `{"ok":true,"error":"invalid_auth"}`, false, false, ""},
		{"trailing auth data", "auth.test", `{"ok":true}{"private":"synthetic-private-auth-response"}`, false, false, ""},
		{"oversized auth data", "auth.test", `{"ok":true,"team_id":"T_FIXTURE","user_id":"U_FIXTURE"}` + strings.Repeat(" ", 8<<20), false, false, ""},
		{"auth read failure", "auth.test", `{"ok":true,"team_id":"T_FIXTURE","user_id":"U_FIXTURE"}`, false, false, ""},
		{"auth rejection", "auth.test", `{"ok":false,"error":"invalid_auth"}`, false, false, "invalid_auth"},
		{"revoked", "auth.revoke", `{"ok":true,"revoked":true}`, true, true, ""},
		{"explicitly not revoked", "auth.revoke", `{"ok":true,"revoked":false}`, true, false, ""},
		{"missing revoke acknowledgement", "auth.revoke", `{"revoked":true}`, false, false, ""},
		{"missing revoked state", "auth.revoke", `{"ok":true}`, false, false, ""},
		{"null revoked state", "auth.revoke", `{"ok":true,"revoked":null}`, false, false, ""},
		{"revoke rejection", "auth.revoke", `{"ok":false,"error":"invalid_auth"}`, false, false, "invalid_auth"},
		{"false without error", "auth.revoke", `{"ok":false}`, false, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := api.NewSDKClient("xoxp-synthetic-sdk-auth", api.SDKOptions{NoThrottle: true, APIURL: "http://127.0.0.1:1", HTTPClient: &http.Client{Transport: sdkTestRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls++
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Method != http.MethodPost || r.URL.Path != "/api/"+tc.method || r.URL.RawQuery != "" || len(r.PostForm) != 1 || r.PostForm.Get("token") != "xoxp-synthetic-sdk-auth" {
					t.Error("auth response guard changed request method, path or authentication")
				}
				var body io.Reader = strings.NewReader(tc.body)
				if tc.name == "auth read failure" {
					body = io.MultiReader(body, authSDKReadError{})
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"X-Oauth-Scopes": []string{"chat:write"}}, Body: io.NopCloser(body), Request: r}, nil
			})}})
			var err error
			if tc.method == "auth.test" {
				var response *slackgo.AuthTestResponse
				response, err = client.AuthTestContext(t.Context())
				if tc.valid && (response == nil || response.TeamID != "T_FIXTURE" || response.UserID != "U_FIXTURE" || response.Header.Get("X-OAuth-Scopes") != "chat:write") {
					t.Error("auth guard discarded SDK identity or scope headers")
				}
			} else {
				var response *slackgo.AuthRevokeResponse
				response, err = client.SendAuthRevokeContext(t.Context(), "")
				if tc.valid && (response == nil || response.Revoked != tc.revoked) {
					t.Error("auth guard discarded explicit revoked state")
				}
			}
			if (err == nil) != tc.valid || calls != 1 {
				t.Fatalf("SDK accepted malformed auth acknowledgement or replayed: error=%v calls=%d", err, calls)
			}
			if tc.code != "" {
				failure, ok := errors.AsType[slackgo.SlackErrorResponse](err)
				if !ok || failure.Err != tc.code {
					t.Error("auth guard changed native Slack error classification")
				}
			} else if err != nil && strings.Contains(err.Error(), "synthetic-private-auth-response") {
				t.Error("structural auth response error exposed raw response or read diagnostics")
			}
		})
	}
}

type authSDKReadError struct{}

func (authSDKReadError) Read([]byte) (int, error) {
	return 0, errors.New("synthetic-private-auth-response")
}
