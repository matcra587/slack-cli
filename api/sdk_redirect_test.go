package api_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/matcra587/slack-cli/api"
	slackgo "github.com/slack-go/slack"
)

func TestSDKDefaultRedirectPolicyNeverReplaysPostCredentials(t *testing.T) {
	t.Parallel()
	for _, code := range []int{301, 302, 303, 307, 308} {
		for _, sameOrigin := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/same=%t", code, sameOrigin), func(t *testing.T) {
				t.Parallel()
				var targets atomic.Int32
				targetHandler := func(w http.ResponseWriter, _ *http.Request) {
					targets.Add(1)
					w.Header().Set("Content-Type", "application/json")
					if _, err := io.WriteString(w, `{"ok":true,"channel":{"id":"C1"}}`); err != nil {
						t.Error(err)
					}
				}
				target := httptest.NewServer(http.HandlerFunc(targetHandler))
				defer target.Close()
				origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/target" {
						targetHandler(w, r)
						return
					}
					location := target.URL + "/target"
					if sameOrigin {
						location = "/target"
					}
					http.Redirect(w, r, location, code)
				}))
				defer origin.Close()
				supplied := &http.Client{}
				client := api.NewSDKClient("xoxp-synthetic-redirect", api.SDKOptions{APIURL: origin.URL, HTTPClient: supplied, NoThrottle: true})
				_, err := client.GetConversationInfoContext(t.Context(), &slackgo.GetConversationInfoInput{ChannelID: "C1"})
				status, ok := errors.AsType[slackgo.StatusCodeError](err)
				if !ok || status.Code != code || targets.Load() != 0 {
					t.Fatal("default redirect policy replayed POST credentials or changed the original status")
				}
				if supplied.CheckRedirect != nil {
					t.Fatal("factory changed the caller's HTTP client")
				}
			})
		}
	}
}

func TestSDKUploadRedirectCannotForwardBearerToken(t *testing.T) {
	t.Parallel()
	for _, code := range []int{302, 307, 308} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			t.Parallel()
			var targets atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { targets.Add(1); w.WriteHeader(200) }))
			defer target.Close()
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL+"/target", code) }))
			defer origin.Close()
			client := api.NewSDKClient("xoxp-synthetic-upload-redirect", api.SDKOptions{APIURL: origin.URL, NoThrottle: true})
			err := client.UploadToURL(t.Context(), slackgo.UploadToURLParameters{UploadURL: origin.URL + "/upload", Filename: "artifact.bin", Reader: strings.NewReader("artifact")})
			if err == nil || targets.Load() != 0 {
				t.Fatal("multipart redirect forwarded a bearer credential or reported success")
			}
		})
	}
}

func TestSDKDownloadRedirectClearsCredentialsOnlyOnOriginChange(t *testing.T) {
	t.Parallel()
	for _, sameOrigin := range []bool{false, true} {
		t.Run(fmt.Sprintf("same=%t", sameOrigin), func(t *testing.T) {
			t.Parallel()
			var targets atomic.Int32
			handler := func(w http.ResponseWriter, r *http.Request) {
				targets.Add(1)
				want := ""
				if sameOrigin {
					want = "Bearer xoxp-synthetic-download-redirect"
				}
				if r.Method != http.MethodGet || r.Header.Get("Authorization") != want {
					t.Error("download redirect retained credentials for another origin or dropped same-origin authentication")
				}
				if _, err := io.WriteString(w, "artifact"); err != nil {
					t.Error(err)
				}
			}
			target := httptest.NewServer(http.HandlerFunc(handler))
			defer target.Close()
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/target" {
					handler(w, r)
					return
				}
				location := target.URL + "/target"
				if sameOrigin {
					location = "/target"
				}
				http.Redirect(w, r, location, http.StatusFound)
			}))
			defer origin.Close()
			client := api.NewSDKClient("xoxp-synthetic-download-redirect", api.SDKOptions{APIURL: origin.URL, NoThrottle: true})
			var out bytes.Buffer
			if err := client.GetFileContext(t.Context(), origin.URL+"/download", &out); err != nil || out.String() != "artifact" || targets.Load() != 1 {
				t.Fatalf("safe download redirect stopped working: %v", err)
			}
		})
	}
}

func TestSDKDefaultDownloadRedirectsRemainBounded(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) > 10 {
			w.WriteHeader(200)
			return
		}
		http.Redirect(w, r, "/loop", http.StatusFound)
	}))
	defer server.Close()
	client := api.NewSDKClient("xoxp-synthetic-loop", api.SDKOptions{APIURL: server.URL, NoThrottle: true})
	var out bytes.Buffer
	if err := client.GetFileContext(t.Context(), server.URL+"/loop", &out); err == nil || calls.Load() != 10 {
		t.Fatal("redirect loop lost the ten-request bound")
	}
}

func TestSDKExplicitRedirectPolicyCanAllowPostRedirect(t *testing.T) {
	t.Parallel()
	var targets, checks atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/target" {
			targets.Add(1)
			if r.Method != http.MethodPost {
				t.Error("explicit policy did not retain POST")
			}
			write := `{"ok":true,"channel":{"id":"C1"}}`
			if _, err := io.WriteString(w, write); err != nil {
				t.Error(err)
			}
			return
		}
		http.Redirect(w, r, "/target", http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	supplied := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { checks.Add(1); return nil }}
	client := api.NewSDKClient("xoxp-synthetic-explicit-redirect", api.SDKOptions{APIURL: server.URL, HTTPClient: supplied, NoThrottle: true})
	result, err := client.GetConversationInfoContext(t.Context(), &slackgo.GetConversationInfoInput{ChannelID: "C1"})
	if err != nil || result == nil || result.ID != "C1" || targets.Load() != 1 || checks.Load() != 1 {
		t.Fatal("explicit caller redirect policy was overridden")
	}
}

func TestSDKDownloadRedirectRejectsURLCredentials(t *testing.T) {
	t.Parallel()
	var targets atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { targets.Add(1); w.WriteHeader(200) }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, strings.Replace(target.URL, "://", "://synthetic-user:synthetic-password@", 1)+"/target", http.StatusFound)
	}))
	defer server.Close()
	client := api.NewSDKClient("xoxp-synthetic-url-redirect", api.SDKOptions{APIURL: server.URL, NoThrottle: true})
	var out bytes.Buffer
	if err := client.GetFileContext(t.Context(), server.URL+"/download", &out); err == nil || targets.Load() != 0 {
		t.Fatal("redirect target injected URL credentials")
	}
}
