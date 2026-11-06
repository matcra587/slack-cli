package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestHealthHTTPFailuresRetainSafeStatusAndRetryDetails(t *testing.T) {
	t.Setenv("SLICK_CONFIG", filepath.Join(t.TempDir(), "unused.toml"))
	cases := []struct {
		name    string
		status  int
		headers []string
		delay   string
	}{
		{"rate delay", 429, []string{"7"}, "7"},
		{"rate zero", 429, []string{"0"}, "0"},
		{"rate padded", 429, []string{" 007 "}, "7"},
		{"rate missing", 429, nil, ""},
		{"rate blank", 429, []string{""}, ""},
		{"rate negative", 429, []string{"-1"}, ""},
		{"rate fractional", 429, []string{"1.5"}, ""},
		{"rate plus", 429, []string{"+7"}, ""},
		{"rate overflow", 429, []string{"9999999999999999999999999999"}, ""},
		{"rate duplicate", 429, []string{"7", "9"}, ""},
		{"rate combined", 429, []string{"7,9"}, ""},
		{"rate date", 429, []string{"Wed, 21 Oct 2015 07:28:00 GMT"}, ""},
		{"rate untrusted", 429, []string{"synthetic-http-header-canary"}, ""},
		{"unauthorized", 401, nil, ""},
		{"forbidden", 403, nil, ""},
		{"missing", 404, nil, ""},
		{"internal", 500, nil, ""},
		{"unavailable", 503, nil, ""},
	}
	cases = append(cases, struct {
		name    string
		status  int
		headers []string
		delay   string
	}{"rate max integer", 429, []string{strconv.Itoa(int(^uint(0) >> 1))}, strconv.Itoa(int(^uint(0) >> 1))})
	if strconv.IntSize == 64 {
		cases = append(cases, struct {
			name    string
			status  int
			headers []string
			delay   string
		}{"rate beyond duration", 429, []string{"10000000000"}, "10000000000"})
	}
	for _, phase := range []struct{ name, command, target string }{
		{"probe", "api-test", "/api/api.test"},
		{"current", "current", "/current"},
		{"history", "history", "/history"},
		{"check probe", "check", "/api/api.test"},
		{"check status", "check", "/current"},
	} {
		for _, tc := range cases {
			for _, mode := range []string{"json", "compact", "human"} {
				t.Run(phase.name+"/"+tc.name+"/"+mode, func(t *testing.T) {
					var mu sync.Mutex
					var paths []string
					rejected := true
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						mu.Lock()
						defer mu.Unlock()
						paths = append(paths, r.URL.Path)
						body, err := io.ReadAll(r.Body)
						if err != nil || len(body) != 0 || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
							t.Error("health failure changed request inputs or authentication")
						}
						method := http.MethodGet
						if r.URL.Path == "/api/api.test" {
							method = http.MethodPost
						}
						if r.Method != method {
							t.Error("health failure changed request method")
						}
						if rejected && r.URL.Path == phase.target {
							for _, value := range tc.headers {
								w.Header().Add("Retry-After", value)
							}
							w.WriteHeader(tc.status)
							_, _ = io.WriteString(w, "synthetic-http-body-canary")
							return
						}
						w.Header().Set("Content-Type", "application/json")
						switch r.URL.Path {
						case "/api/api.test":
							_, _ = io.WriteString(w, `{"ok":true}`)
						case "/current":
							_, _ = io.WriteString(w, `{"status":"ok","active_incidents":[]}`)
						case "/history":
							_, _ = io.WriteString(w, `[]`)
						default:
							t.Error("unexpected health endpoint")
							w.WriteHeader(500)
						}
					}))
					defer server.Close()
					out, diagnostic, err := runHealthFlagCommand(t, server.URL, mode, "health", phase.command, "--timeout=1s")
					if err == nil {
						t.Fatal("HTTP failure was accepted")
					}
					wantPaths := []string{phase.target}
					if phase.name == "check status" {
						wantPaths = []string{"/api/api.test", "/current"}
					}
					mu.Lock()
					got := append([]string(nil), paths...)
					mu.Unlock()
					if !reflect.DeepEqual(got, wantPaths) {
						t.Fatal("HTTP failure was retried or check continued after failure")
					}
					if strings.Contains(out+diagnostic, "synthetic-http-") {
						t.Fatal("untrusted HTTP response leaked into diagnostics")
					}
					wantExit, wantType, wantMessage := 5, "server_error", fmt.Sprintf("slack server error: %d %s", tc.status, http.StatusText(tc.status))
					if tc.status == 429 {
						wantExit, wantType, wantMessage = 3, "rate_limit", "ratelimited"
					}
					if mode == "human" {
						if out != "" || diagnostic == "" || !strings.Contains(diagnostic, wantMessage) {
							t.Fatal("human HTTP failure lost safe diagnostic or used stdout")
						}
						if tc.delay != "" && !strings.Contains(diagnostic, "retry_after_seconds="+tc.delay) {
							t.Fatal("human rate-limit delay was lost")
						}
						if tc.delay == "" && strings.Contains(diagnostic, "retry_after_seconds") {
							t.Fatal("unknown rate delay was invented")
						}
					} else {
						var frame struct {
							OK     bool
							Data   json.RawMessage
							Errors []struct {
								Type, Message string
								Exit          int             `json:"exit_code"`
								Delay         json.RawMessage `json:"retry_after_seconds"`
							}
							Meta struct {
								Command string
								Exit    int `json:"exit_code"`
							}
						}
						if json.Unmarshal([]byte(out), &frame) != nil || frame.OK || string(frame.Data) != "null" || len(frame.Errors) != 1 || frame.Meta.Command != "health."+phase.command || frame.Meta.Exit != wantExit || frame.Errors[0].Exit != wantExit || frame.Errors[0].Type != wantType || frame.Errors[0].Message != wantMessage || string(frame.Errors[0].Delay) != tc.delay || diagnostic != "" {
							t.Fatal("HTTP failure lost common frame, safe status, classification or exact delay")
						}
					}
					mu.Lock()
					rejected = false
					paths = nil
					mu.Unlock()
					out, diagnostic, err = runHealthFlagCommand(t, server.URL, mode, "health", phase.command, "--timeout=1s")
					if err != nil || diagnostic != "" || out == "" {
						t.Fatalf("health command did not recover: %v", err)
					}
					wantPaths = []string{phase.target}
					if phase.command == "check" {
						wantPaths = []string{"/api/api.test", "/current"}
					}
					mu.Lock()
					got = append([]string(nil), paths...)
					mu.Unlock()
					if !reflect.DeepEqual(got, wantPaths) {
						t.Fatal("recovery repeated or omitted a health phase")
					}
				})
			}
		}
	}
}
