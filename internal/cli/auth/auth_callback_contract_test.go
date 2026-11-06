package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOAuthCallbackRejectsInvalidRequestsAndRecovers(t *testing.T) {
	for _, tt := range []struct {
		name, method, target string
		status               int
	}{
		{"wrong path", "GET", "/other?state=expected&code=private-code", 404},
		{"wrong method", "POST", "/callback?state=expected&code=private-code", 405},
		{"missing state", "GET", "/callback?code=private-code", 400},
		{"wrong state with code", "GET", "/callback?state=wrong&code=private-code", 400},
		{"wrong state with error", "GET", "/callback?state=wrong&error=private-error", 400},
		{"duplicate state", "GET", "/callback?state=expected&state=wrong&code=private-code", 400},
		{"missing code", "GET", "/callback?state=expected", 400},
		{"duplicate code", "GET", "/callback?state=expected&code=private-code&code=other", 400},
		{"duplicate error", "GET", "/callback?state=expected&error=private-error&error=other", 400},
		{"code and error", "GET", "/callback?state=expected&code=private-code&error=private-error", 400},
	} {
		t.Run(tt.name, func(t *testing.T) {
			results := make(chan oauthCallbackResult, 1)
			handler := oauthCallbackHandler("expected", "/callback", results)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.target, http.NoBody))
			if rec.Code != tt.status {
				t.Fatalf("invalid callback status=%d, want %d", rec.Code, tt.status)
			}
			if tt.status == 405 && rec.Header().Get("Allow") != "GET" {
				t.Error("method rejection must advertise GET")
			}
			if strings.Contains(rec.Body.String(), "private-") {
				t.Error("callback reflected sensitive query data")
			}
			select {
			case <-results:
				t.Fatal("invalid callback terminated or supplied a code to login")
			default:
			}
			recovery := httptest.NewRecorder()
			handler.ServeHTTP(recovery, httptest.NewRequest("GET", "/callback?state=expected&code=recovery-code", http.NoBody))
			select {
			case result := <-results:
				if recovery.Code != 200 || result.Err != nil || result.Code != "recovery-code" {
					t.Fatal("valid callback did not recover rejected request")
				}
			default:
				t.Fatal("valid callback did not reach login")
			}
		})
	}
}

func TestOAuthCallbackDenialDoesNotExposeRemoteText(t *testing.T) {
	results := make(chan oauthCallbackResult, 1)
	rec := httptest.NewRecorder()
	oauthCallbackHandler("expected", "/callback", results).ServeHTTP(rec,
		httptest.NewRequest("GET", "/callback?state=expected&error=private-error", http.NoBody))
	select {
	case result := <-results:
		if rec.Code != 400 || result.Err == nil || result.Code != "" {
			t.Fatal("valid denial did not fail authorization")
		}
		if strings.Contains(rec.Body.String()+result.Err.Error(), "private-error") {
			t.Fatal("OAuth denial exposed untrusted remote text")
		}
	default:
		t.Fatal("valid denial did not reach login")
	}
}
