package auth

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
)

func TestOAuthExchangeUsesGuardedHTTPClient(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"missing acknowledgement", `{"access_token":"xoxp-synthetic-exchange"}`},
		{"invalid expiry", `{"ok":true,"access_token":"xoxp-synthetic-exchange","expires_in":-1}`},
		{"redirect", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls, forwarded atomic.Int32
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				forwarded.Add(1)
				_, _ = io.WriteString(w, `{"ok":true,"access_token":"xoxp-synthetic-forwarded"}`)
			}))
			defer destination.Close()
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Method != http.MethodPost || r.URL.Path != "/api/oauth.v2.access" || r.URL.RawQuery != "" || len(r.PostForm) != 4 || r.PostForm.Get("client_id") != "synthetic-client" || r.PostForm.Get("code") != "synthetic-code" || r.PostForm.Get("redirect_uri") != "http://127.0.0.1:1234/callback" || r.PostForm.Get("code_verifier") != "synthetic-verifier" || r.Header.Get("Authorization") != "" {
					t.Error("exchange guard changed PKCE method, parameters or authentication")
				}
				if tc.name == "redirect" {
					w.Header().Set("Location", destination.URL+"/api/oauth.v2.access")
					w.WriteHeader(http.StatusTemporaryRedirect)
					return
				}
				_, _ = io.WriteString(w, tc.body)
			}))
			defer origin.Close()
			runtime := &cliruntime.RootRuntime{SlackBaseURL: origin.URL, HTTPClient: origin.Client(), Timeout: time.Second}
			response, err := oauthExchangeCode(t.Context(), runtime, "synthetic-client", "synthetic-code", "http://127.0.0.1:1234/callback", "synthetic-verifier")
			if err == nil || response != nil || calls.Load() != 1 || forwarded.Load() != 0 {
				t.Fatal("OAuth exchange accepted unsafe response, forwarded verifier/code or retried")
			}
		})
	}
}
