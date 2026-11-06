package health

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
)

func TestHealthMalformedResponsesFailAndRecover(t *testing.T) {
	for _, endpoint := range []struct {
		name, path, valid string
		method            string
		bad               []struct{ name, body string }
	}{
		{"api-test", "/api/api.test", `{"ok":true}`, http.MethodPost, []struct{ name, body string }{
			{"missing ack", `{}`}, {"false missing error", `{"ok":false}`}, {"false blank error", `{"ok":false,"error":" "}`}, {"contradictory", `{"ok":true,"error":"internal_error"}`}, {"wrong ack", `{"ok":"yes"}`},
		}},
		{"current", "/current", `{"status":"ok","active_incidents":[]}`, http.MethodGet, []struct{ name, body string }{
			{"missing status", `{"active_incidents":[]}`},
			{"blank status", `{"status":" ","active_incidents":[]}`},
			{"missing incidents", `{"status":"ok"}`},
			{"null incidents", `{"status":"ok","active_incidents":null}`},
			{"wrong incidents", `{"status":"ok","active_incidents":{}}`},
			{"null item", `{"status":"active","active_incidents":[null]}`},
			{"missing incident id", `{"status":"active","active_incidents":[{"title":"incident"}]}`},
		}},
		{"history", "/history", `[]`, http.MethodGet, []struct{ name, body string }{
			{"object", `{}`}, {"null item", `[null]`},
		}},
	} {
		bad := append(endpoint.bad, []struct{ name, body string }{
			{"null", `null`}, {"empty", ``}, {"truncated", `{"untrusted":"synthetic-response-canary`}, {"trailing document", endpoint.valid + ` {"secret":"synthetic-response-canary"}`}, {"trailing garbage", endpoint.valid + ` synthetic-response-canary`}, {"oversized", endpoint.valid + strings.Repeat(" ", 5<<20)},
		}...)
		for _, tc := range bad {
			for _, mode := range []string{"json", "compact", "human"} {
				t.Run(endpoint.name+"/"+tc.name+"/"+mode, func(t *testing.T) {
					var recovered atomic.Bool
					var calls atomic.Int32
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						calls.Add(1)
						body, err := io.ReadAll(r.Body)
						if err != nil || r.Method != endpoint.method || r.URL.Path != endpoint.path || r.URL.RawQuery != "" || len(body) != 0 || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
							t.Error("health probe sent unexpected method, parameters or credentials")
						}
						if endpoint.method == http.MethodPost && r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
							t.Error("api.test has wrong encoding")
						}
						if recovered.Load() {
							writeJSON(t, w, endpoint.valid)
						} else {
							writeJSON(t, w, tc.body)
						}
					}))
					defer server.Close()
					stdout, stderr, err := executeHealth(t, server.URL, []string{"health", endpoint.name, "--output=" + mode})
					var commandErr clioutput.CommandError
					if !errors.As(err, &commandErr) || commandErr.CLIError.ExitCode != 5 {
						t.Fatalf("invalid response did not fail with server error: %v", err)
					}
					if strings.Contains(stdout+stderr, "synthetic-response-canary") {
						t.Fatal("remote malformed response entered diagnostics")
					}
					if mode == "human" {
						if stdout != "" || stderr == "" {
							t.Fatal("wrong human error streams")
						}
					} else {
						var frame struct {
							OK   bool
							Data json.RawMessage
							Meta struct {
								Command string
								Exit    int `json:"exit_code"`
							}
						}
						if json.Unmarshal([]byte(stdout), &frame) != nil || frame.OK || string(frame.Data) != "null" || frame.Meta.Command != "health."+endpoint.name || frame.Meta.Exit != 5 || stderr != "" {
							t.Fatal("malformed response lost common error envelope")
						}
					}
					if calls.Load() != 1 {
						t.Fatal("health failure was retried")
					}
					recovered.Store(true)
					stdout, stderr, err = executeHealth(t, server.URL, []string{"health", endpoint.name, "--output=" + mode})
					if err != nil || stderr != "" || stdout == "" || calls.Load() != 2 {
						t.Fatalf("valid response did not recover without replay: %v", err)
					}
				})
			}
		}
	}
}

type healthResponseTransport func(*http.Request) (*http.Response, error)

func (f healthResponseTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type failingHealthBody struct{ closed bool }

func (*failingHealthBody) Read([]byte) (int, error) {
	return 0, errors.New("synthetic-private-read-canary")
}
func (b *failingHealthBody) Close() error { b.closed = true; return nil }

func TestHealthReadFailureClosesBodyAndProtectsDiagnostics(t *testing.T) {
	body := &failingHealthBody{}
	var calls int
	client := Client{HTTPClient: &http.Client{Transport: healthResponseTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: body}, nil
	})}, SlackAPIBaseURL: "https://fixture.invalid/api/"}
	_, err := client.APITest(t.Context())
	if err == nil || strings.Contains(err.Error(), "synthetic-private-read-canary") || !body.closed || calls != 1 {
		t.Fatal("read failure exposed data, leaked its body, or retried")
	}
}

func TestHealthResponseAcceptsExactSizeLimit(t *testing.T) {
	prefix := `{"ok":true}`
	body := prefix + strings.Repeat(" ", (5<<20)-len(prefix))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { writeJSON(t, w, body) }))
	defer server.Close()
	stdout, stderr, err := executeHealth(t, server.URL, []string{"health", "api-test", "--output=json"})
	var frame struct {
		OK   bool
		Data struct{ OK bool }
	}
	if err != nil || stderr != "" || json.Unmarshal([]byte(stdout), &frame) != nil || !frame.OK || !frame.Data.OK {
		t.Fatalf("exactly 5 MiB valid response was rejected: %v", err)
	}
}

type untrustedHealthErrorBody struct {
	reads  int
	closed bool
}

func (b *untrustedHealthErrorBody) Read([]byte) (int, error) {
	b.reads++
	return 0, errors.New("synthetic-error-body-canary")
}
func (b *untrustedHealthErrorBody) Close() error { b.closed = true; return nil }

func TestHealthHTTPErrorBodiesAreClosedWithoutReading(t *testing.T) {
	for _, status := range []int{429, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			body := &untrustedHealthErrorBody{}
			calls := 0
			client := Client{HTTPClient: &http.Client{Transport: healthResponseTransport(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: status, Status: "synthetic-status-reason-canary", Header: http.Header{"Retry-After": []string{"7"}}, Body: body}, nil
			})}, SlackAPIBaseURL: "https://fixture.invalid/api/"}
			_, err := client.APITest(t.Context())
			if err == nil || strings.Contains(err.Error(), "canary") || !body.closed || body.reads != 0 || calls != 1 {
				t.Fatal("HTTP failure read untrusted body, exposed its status, leaked the body or retried")
			}
		})
	}
}
