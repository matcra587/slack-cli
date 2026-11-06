package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func TestUploadCancellationKeepsKnownProgressAndStopsLaterSteps(t *testing.T) {
	t.Setenv("SLICK_CONFIG", filepath.Join(t.TempDir(), "unused.toml"))
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	file := filepath.Join(t.TempDir(), "artifact.bin")
	if err := os.WriteFile(file, []byte("artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	for stage, stop := range []string{"prepare", "transfer", "complete"} {
		t.Run(stop, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var counts [3]atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/auth.test":
					w.Header().Set("X-OAuth-Scopes", "files:write")
					if _, err := io.WriteString(w, `{"ok":true,"user_id":"U1"}`); err != nil {
						t.Error(err)
					}
				case "/api/files.getUploadURLExternal":
					counts[0].Add(1)
					if stage == 0 {
						cancel()
						if _, err := io.WriteString(w, "{"); err != nil {
							t.Error(err)
						}
						return
					}
					if _, err := io.WriteString(w, `{"ok":true,"file_id":"F1","upload_url":"http://`+r.Host+`/upload"}`); err != nil {
						t.Error(err)
					}
				case "/upload":
					counts[1].Add(1)
					if stage == 1 {
						cancel()
						w.WriteHeader(500)
						return
					}
					w.WriteHeader(200)
				case "/api/files.completeUploadExternal":
					counts[2].Add(1)
					if stage == 2 {
						cancel()
						if _, err := io.WriteString(w, "{"); err != nil {
							t.Error(err)
						}
						return
					}
					if _, err := io.WriteString(w, `{"ok":true,"files":[{"id":"F1"}]}`); err != nil {
						t.Error(err)
					}
				default:
					t.Errorf("cancellation continued or cleaned up: %s", r.URL.Path)
				}
			}))
			defer server.Close()
			var out, diagnostic bytes.Buffer
			root := NewRootCommand(WithConfig(workspaceConfig(config.TokenTypeUser)), WithIO(strings.NewReader(""), &out, &diagnostic), WithTTY(false), WithSlackBaseURL(server.URL), WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) { return "xoxp-synthetic-cancel", nil })))
			root.SetArgs([]string{"file", "upload", "--channel=C123", "--file=" + file, "--no-attribution", "--no-throttle", "--output=json"})
			err := root.ExecuteContext(ctx)
			var frame struct {
				Meta struct {
					Exit int `json:"exit_code"`
				} `json:"meta"`
				Errors []struct {
					Type    string `json:"type"`
					Details struct {
						Stage  string `json:"stage"`
						FileID string `json:"file_id"`
					} `json:"details"`
				} `json:"errors"`
			}
			if decodeErr := json.Unmarshal(out.Bytes(), &frame); decodeErr != nil {
				t.Fatal(decodeErr)
			}
			wantID := "F1"
			if stage == 0 {
				wantID = ""
			}
			if err == nil || diagnostic.Len() != 0 || frame.Meta.Exit != 6 || len(frame.Errors) != 1 || frame.Errors[0].Type != "canceled" || frame.Errors[0].Details.Stage != stop || frame.Errors[0].Details.FileID != wantID {
				t.Fatalf("cancellation lost class/progress: err=%v stdout=%s stderr=%s", err, &out, &diagnostic)
			}
			for i := range counts {
				want := int32(0)
				if i <= stage {
					want = 1
				}
				if counts[i].Load() != want {
					t.Fatal("cancellation replayed or continued upload steps")
				}
			}
		})
	}
}

func TestUploadWrappedTransportContextErrorsKeepExitClass(t *testing.T) {
	t.Setenv("SLICK_CONFIG", filepath.Join(t.TempDir(), "unused.toml"))
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	file := filepath.Join(t.TempDir(), "artifact.bin")
	if err := os.WriteFile(file, []byte("artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		cause error
		exit  int
	}{{"canceled", context.Canceled, 6}, {"timeout", context.DeadlineExceeded, 7}} {
		t.Run(tc.name, func(t *testing.T) {
			var out, diagnostic bytes.Buffer
			var attempts atomic.Int32
			transport := uploadContextTransport(func(r *http.Request) (*http.Response, error) {
				if r.Body != nil {
					if _, err := io.Copy(io.Discard, r.Body); err != nil {
						t.Error(err)
					}
					if err := r.Body.Close(); err != nil {
						t.Error(err)
					}
				}
				header := make(http.Header)
				body := ""
				switch r.URL.Path {
				case "/api/auth.test":
					header.Set("X-OAuth-Scopes", "files:write")
					body = `{"ok":true,"user_id":"U1"}`
				case "/api/files.getUploadURLExternal":
					body = `{"ok":true,"file_id":"F1","upload_url":"http://127.0.0.1:1/upload"}`
				case "/upload":
				case "/api/files.completeUploadExternal":
					attempts.Add(1)
					return nil, fmt.Errorf("synthetic-private-transport: %w", tc.cause)
				default:
					t.Errorf("unexpected upload request: %s", r.URL.Path)
				}
				return &http.Response{StatusCode: 200, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			root := NewRootCommand(WithConfig(workspaceConfig(config.TokenTypeUser)), WithIO(strings.NewReader(""), &out, &diagnostic), WithTTY(false), WithSlackBaseURL("http://127.0.0.1:1"), WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
				return "xoxp-synthetic-upload-context", nil
			})), func(runtime *RootRuntime) { runtime.HTTPClient = &http.Client{Transport: transport} })
			root.SetArgs([]string{"file", "upload", "--channel=C123", "--file=" + file, "--no-attribution", "--no-throttle", "--output=json"})
			err := root.ExecuteContext(t.Context())
			var frame struct {
				Meta struct {
					Exit int `json:"exit_code"`
				} `json:"meta"`
				Errors []struct {
					Details struct {
						Stage  string `json:"stage"`
						FileID string `json:"file_id"`
					} `json:"details"`
				} `json:"errors"`
			}
			if decodeErr := json.Unmarshal(out.Bytes(), &frame); decodeErr != nil {
				t.Fatal(decodeErr)
			}
			if err == nil || t.Context().Err() != nil || frame.Meta.Exit != tc.exit || attempts.Load() != 1 || len(frame.Errors) != 1 || frame.Errors[0].Details.Stage != "complete" || frame.Errors[0].Details.FileID != "F1" {
				t.Fatalf("wrapped transport context lost class/progress or replayed: err=%v stdout=%s", err, &out)
			}
			if diagnostic.Len() != 0 || strings.Contains(out.String(), "synthetic-private") {
				t.Fatal("wrapped transport diagnostics leaked or duplicated human error")
			}
		})
	}
}

type uploadContextTransport func(*http.Request) (*http.Response, error)

func (f uploadContextTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
