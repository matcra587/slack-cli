package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	"github.com/matcra587/slack-cli/internal/config"
)

func TestAuthLoginOAuthCallbackRecoveryAndDenial(t *testing.T) {
	for _, denied := range []bool{false, true} {
		name := "reject unsolicited callback then authorize"
		if denied {
			name = "authenticated denial preserves prior state"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			cfg := authTestConfig()
			if err := config.SaveFile(path, cfg); err != nil {
				t.Fatal(err)
			}
			prior, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			store := config.NewMemoryCredentialStore()
			if err := store.Set("slack-cli", "other", "xoxb-synthetic-prior"); err != nil {
				t.Fatal(err)
			}
			var exchanges atomic.Int32
			client := &http.Client{Transport: authStatusTransport(func(r *http.Request) (*http.Response, error) {
				exchanges.Add(1)
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Method != "POST" || r.URL.Path != "/api/oauth.v2.access" || r.PostForm.Get("code") != "synthetic-recovery-code" {
					t.Error("callback supplied wrong code or OAuth endpoint")
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":true,"access_token":"xoxb-synthetic-new","token_type":"bot","team":{"id":"T_CALLBACK","name":"Callback"}}`)), Request: r}, nil
			})}
			opener := func(raw string) error {
				authorize, err := url.Parse(raw)
				if err != nil {
					return err
				}
				callback, err := url.Parse(authorize.Query().Get("redirect_uri"))
				if err != nil {
					return err
				}
				for _, valid := range []bool{false, true} {
					q := url.Values{"state": {"wrong"}, "error": {"synthetic-private-callback"}}
					want := 400
					if valid {
						q.Set("state", authorize.Query().Get("state"))
						if !denied {
							q.Del("error")
							q.Set("code", "synthetic-recovery-code")
							want = 200
						}
					}
					callback.RawQuery = q.Encode()
					req, err := http.NewRequestWithContext(t.Context(), "GET", callback.String(), nil)
					if err != nil {
						return err
					}
					resp, err := (&http.Client{Timeout: time.Second}).Do(req)
					if err != nil {
						return err
					}
					body, readErr := io.ReadAll(resp.Body)
					_ = resp.Body.Close()
					if readErr != nil || resp.StatusCode != want || strings.Contains(string(body), "synthetic-private-callback") {
						return errors.New("invalid callback status or exposed private callback text")
					}
				}
				return nil
			}
			out, diagnostic, runErr := executeAuthRootWithOptions(t, cfg, path, store, "http://127.0.0.1:1", strings.NewReader(""), false,
				[]string{"auth", "login", "--workspace-name=Callback", "--method=oauth", "--oauth-client-id=synthetic-client", "--oauth-callback-port=0", "--output=json", "--color=never"},
				func(runtime *RootRuntime) { runtime.HTTPClient = client }, WithOAuthTimeout(2*time.Second), WithURLOpener(opener))
			var frame struct{ OK bool }
			if json.Unmarshal([]byte(out), &frame) != nil || frame.OK == denied || diagnostic != "" || strings.Contains(out, "synthetic-private-callback") || strings.Contains(out, "synthetic-recovery-code") {
				t.Error("callback lost machine envelope, stream separation or privacy")
			}
			if denied {
				failure, ok := errors.AsType[clioutput.CommandError](runErr)
				if !ok || failure.CLIError.ExitCode != 1 || exchanges.Load() != 0 {
					t.Error("denied callback must fail auth without exchanging code")
				}
				after, err := os.ReadFile(path)
				if err != nil || string(after) != string(prior) {
					t.Error("denied authorization changed prior config")
				}
				if _, err := store.Get("slack-cli", "Callback"); err == nil {
					t.Error("denied authorization stored a credential")
				}
			} else if runErr != nil || exchanges.Load() != 1 {
				t.Errorf("valid callback failed recovery or replayed exchange: %v, calls=%d", runErr, exchanges.Load())
			} else if _, err := store.Get("slack-cli", "Callback"); err != nil {
				t.Error("recovered authorization did not store credential")
			}
			if value, err := store.Get("slack-cli", "other"); err != nil || value != "xoxb-synthetic-prior" {
				t.Error("callback changed unrelated credential")
			}
		})
	}
}
