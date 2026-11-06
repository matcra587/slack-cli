package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gechr/clog"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	"github.com/matcra587/slack-cli/internal/config"
)

func TestAuthOAuthLifecycleHonorsCancellationAndProtectsState(t *testing.T) {
	for _, tc := range []struct {
		name, mode, timeout string
		exit, calls         int
	}{
		{"caller cancellation", "json", "1s", 6, 0},
		{"caller deadline", "compact", "1s", 7, 0},
		{"browser outlasts request budget", "json", "30ms", 0, 1},
		{"exchange outlasts browser budget", "compact", "1s", 0, 1},
		{"exchange timeout", "json", "30ms", 7, 1},
		{"late exchange response", "compact", "30ms", 7, 1},
		{"private API error", "json", "1s", 5, 1},
		{"malformed response", "compact", "1s", 5, 1},
		{"permission failure", "json", "1s", 1, 1},
		{"invalid OAuth client", "json", "1s", 1, 1},
		{"invalid OAuth code", "compact", "1s", 1, 1},
		{"invalid PKCE verifier", "json", "1s", 1, 1},
		{"redirect mismatch", "compact", "1s", 1, 1},
		{"PKCE unavailable", "json", "1s", 1, 1},
		{"rate limit", "compact", "1s", 3, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			verbose := clog.IsVerbose()
			t.Cleanup(func() { clog.SetVerbose(verbose) })
			cfg := authTestConfig()
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := config.SaveFile(path, cfg); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			store := config.NewMemoryCredentialStore()
			if err := store.Set("slack-cli", "other", "xoxb-synthetic-prior"); err != nil {
				t.Fatal(err)
			}
			parent, stop := context.WithTimeout(t.Context(), 2*time.Second)
			defer stop()
			caller, cancel := context.WithCancel(parent)
			defer cancel()
			if tc.name == "caller deadline" {
				var deadlineCancel context.CancelFunc
				caller, deadlineCancel = context.WithTimeout(parent, 30*time.Millisecond)
				defer deadlineCancel()
			}
			var calls atomic.Int32
			var wireAuthError string
			transportDone := make(chan struct{})
			client := &http.Client{Transport: authStatusTransport(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				defer close(transportDone)
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Method != "POST" || r.URL.Path != "/api/oauth.v2.access" || r.PostForm.Get("code") != "synthetic-lifecycle-code" || len(r.PostForm) != 4 || r.Header.Get("Authorization") != "" {
					t.Error("OAuth lifecycle changed exchange contract")
				}
				body := `{"ok":true,"access_token":"xoxb-synthetic-lifecycle","token_type":"bot","team":{"id":"T_LIFECYCLE","name":"Lifecycle"}}`
				status := 200
				switch tc.name {
				case "exchange timeout":
					<-r.Context().Done()
					return nil, r.Context().Err()
				case "late exchange response":
					<-r.Context().Done()
				case "exchange outlasts browser budget":
					timer := time.NewTimer(300 * time.Millisecond)
					defer timer.Stop()
					select {
					case <-timer.C:
					case <-r.Context().Done():
						return nil, r.Context().Err()
					}
				case "private API error":
					body = `{"ok":false,"error":"synthetic-private-lifecycle"}`
				case "malformed response":
					body = `{"ok":`
				case "permission failure":
					body = `{"ok":false,"error":"no_permission"}`
				case "invalid OAuth client":
					body = `{"ok":false,"error":"invalid_client_id"}`
				case "invalid OAuth code":
					body = `{"ok":false,"error":"invalid_code"}`
				case "invalid PKCE verifier":
					body = `{"ok":false,"error":"invalid_code_verifier"}`
				case "redirect mismatch":
					body = `{"ok":false,"error":"bad_redirect_uri"}`
				case "PKCE unavailable":
					body = `{"ok":false,"error":"pkce_not_allowed"}`
				case "rate limit":
					status, body = 429, "synthetic-private-lifecycle"
				}
				if tc.exit == 1 {
					var response struct{ Error string }
					if err := json.Unmarshal([]byte(body), &response); err != nil {
						t.Error("invalid synthetic OAuth error fixture")
					}
					wireAuthError = response.Error
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}
			opener := func(raw string) error {
				switch tc.name {
				case "caller cancellation":
					cancel()
					return nil
				case "caller deadline":
					return nil
				case "browser outlasts request budget":
					time.Sleep(100 * time.Millisecond)
				}
				return oauthTestCallbackOpener(t, "synthetic-lifecycle-code")(raw)
			}
			var out, diagnostic bytes.Buffer
			root := NewRootCommand(WithConfig(cfg), WithConfigPath(path), WithCredentialStore(store), WithSlackBaseURL("http://127.0.0.1:1"), WithIO(strings.NewReader(""), &out, &diagnostic), WithTTY(false), WithOAuthTimeout(200*time.Millisecond), WithURLOpener(opener), func(runtime *RootRuntime) { runtime.HTTPClient = client })
			args := []string{"auth", "login", "--workspace-name=Lifecycle", "--method=oauth", "--oauth-client-id=synthetic-client", "--oauth-callback-port=0", "--output=" + tc.mode, "--color=never", "--timeout=" + tc.timeout}
			if tc.name == "private API error" {
				args = append(args, "-D")
			}
			root.SetArgs(args)
			runErr := root.ExecuteContext(caller)
			if tc.name == "private API error" && !clog.IsVerbose() {
				t.Error("debug shorthand did not enable diagnostics")
			}
			if calls.Load() != 0 {
				select {
				case <-transportDone:
				case <-parent.Done():
					t.Fatal("OAuth exchange worker did not finish")
				}
			}
			gotExit := 0
			if failure, ok := errors.AsType[clioutput.CommandError](runErr); ok {
				gotExit = failure.CLIError.ExitCode
			} else if runErr != nil {
				t.Fatalf("unstructured OAuth error: %v", runErr)
			}
			if gotExit != tc.exit || calls.Load() != int32(tc.calls) {
				t.Errorf("OAuth exit/calls = %d/%d, want %d/%d", gotExit, calls.Load(), tc.exit, tc.calls)
			}
			if tc.exit == 1 && (wireAuthError == "" || runErr == nil || runErr.Error() != wireAuthError) {
				t.Error("known OAuth auth failure lost its defined error code")
			}
			var frame struct {
				OK        bool
				Workspace string
			}
			decodeErr := json.Unmarshal(out.Bytes(), &frame)
			validResult := frame.OK == (tc.exit == 0)
			if tc.mode == "compact" && tc.exit == 0 {
				validResult = frame.Workspace == "Lifecycle"
			}
			if decodeErr != nil || !validResult || diagnostic.Len() != 0 {
				t.Error("OAuth lifecycle lost machine result shape or stream separation")
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if tc.exit != 0 {
				if !bytes.Equal(before, after) {
					t.Error("failed OAuth changed prior config")
				}
				if _, err := store.Get("slack-cli", "Lifecycle"); err == nil {
					t.Error("failed OAuth stored a credential")
				}
			} else if _, err := store.Get("slack-cli", "Lifecycle"); err != nil {
				t.Error("successful OAuth did not save credential after browser wait")
			}
			for _, secret := range []string{"synthetic-private-lifecycle", "xoxb-synthetic-lifecycle", "synthetic-lifecycle-code"} {
				if strings.Contains(out.String()+diagnostic.String()+string(after), secret) {
					t.Error("OAuth lifecycle exposed sensitive fields")
				}
			}
			if value, err := store.Get("slack-cli", "other"); err != nil || value != "xoxb-synthetic-prior" {
				t.Error("OAuth lifecycle changed unrelated credential")
			}
		})
	}
}
