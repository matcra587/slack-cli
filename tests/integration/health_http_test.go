//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestBinaryHealthHTTPRateLimitsAndStatusesRecoverWithoutReplay(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tc := range []struct {
		name         string
		status, exit int
		headers      []string
		delay        string
	}{
		{"delay", 429, 3, []string{"7"}, "7"},
		{"zero", 429, 3, []string{"0"}, "0"},
		{"missing", 429, 3, nil, ""},
		{"invalid", 429, 3, []string{"synthetic-http-native-canary"}, ""},
		{"negative", 429, 3, []string{"-1"}, ""},
		{"duplicate", 429, 3, []string{"7", "9"}, ""},
		{"large delay", 429, 3, []string{"10000000000"}, "10000000000"},
		{"unauthorized", 401, 5, nil, ""},
		{"forbidden", 403, 5, nil, ""},
		{"unavailable", 503, 5, nil, ""},
	} {
		if strconv.IntSize == 32 && tc.name == "large delay" {
			tc.delay = ""
		}
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				path := writePipeConfig(t)
				before, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var recovered atomic.Bool
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					body, err := io.ReadAll(r.Body)
					if err != nil || len(body) != 0 || r.Method != http.MethodPost || r.URL.Path != "/api/api.test" || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "" {
						t.Error("probe failure changed method/path/auth/parameters")
					}
					if recovered.Load() {
						writeJSON(w, `{"ok":true}`)
						return
					}
					for _, value := range tc.headers {
						w.Header().Add("Retry-After", value)
					}
					w.WriteHeader(tc.status)
					_, _ = io.WriteString(w, "synthetic-http-native-canary")
				}))
				defer server.Close()
				args := []string{"health", "api-test", "--output=" + mode, "--color=never", "--timeout=1s"}
				out, diagnostic, err := runSlackBinary(t, binary, path, server.URL, "", args...)
				if exitCode(err) != tc.exit || calls.Load() != 1 {
					t.Fatalf("HTTP failure changed exit or retried: exit=%d requests=%d", exitCode(err), calls.Load())
				}
				if strings.Contains(out+diagnostic, "synthetic-http-native-canary") {
					t.Fatal("HTTP failure exposed untrusted response")
				}
				if mode == "human" {
					if out != "" || diagnostic == "" {
						t.Fatal("wrong HTTP failure streams")
					}
					if tc.delay != "" && !strings.Contains(diagnostic, "retry_after_seconds="+tc.delay) {
						t.Fatal("human HTTP failure lost retry delay")
					}
				} else {
					var frame struct {
						OK     bool
						Data   json.RawMessage
						Errors []struct {
							Type, Message string
							Delay         json.RawMessage `json:"retry_after_seconds"`
						}
						Meta struct {
							Command string
							Exit    int `json:"exit_code"`
						}
					}
					if json.Unmarshal([]byte(out), &frame) != nil || frame.OK || string(frame.Data) != "null" || frame.Meta.Command != "health.api-test" || frame.Meta.Exit != tc.exit || len(frame.Errors) != 1 || string(frame.Errors[0].Delay) != tc.delay || diagnostic != "" {
						t.Fatal("HTTP failure lost frame or delay")
					}
					if tc.status == 429 && (frame.Errors[0].Type != "rate_limit" || frame.Errors[0].Message != "ratelimited") {
						t.Fatal("429 was not a rate-limit error")
					}
				}
				recovered.Store(true)
				out, diagnostic, err = runSlackBinary(t, binary, path, server.URL, "", args...)
				if err != nil || diagnostic != "" || out == "" || calls.Load() != 2 {
					t.Fatalf("HTTP probe did not recover explicitly: %v", err)
				}
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatal("HTTP probe changed saved preferences")
				}
			})
		}
	}
}
