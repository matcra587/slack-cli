package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func runHealthFlagCommand(t *testing.T, baseURL, mode string, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root, runtime := NewRootCommandWithRuntime(WithConfig(nil), WithSlackBaseURL(baseURL), WithSlackStatusBaseURL(baseURL), WithTTY(false), WithIO(strings.NewReader(""), &stdout, &stderr), WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
		t.Error("unauthenticated health command accessed credentials")
		return "", fmt.Errorf("unexpected credential access")
	})))
	root.SetArgs(append([]string{"--output=" + mode, "--color=never"}, args...))
	err := root.ExecuteContext(t.Context())
	if runtime.CancelTimeout != nil {
		runtime.CancelTimeout()
	}
	return stdout.String(), stderr.String(), err
}

func healthFlagData(t *testing.T, mode, out, command string) map[string]json.RawMessage {
	t.Helper()
	data := []byte(out)
	if mode == "json" {
		var frame struct {
			OK   bool
			Data json.RawMessage
			Meta struct {
				Command string
				Exit    int `json:"exit_code"`
			}
		}
		if json.Unmarshal(data, &frame) != nil || !frame.OK || frame.Meta.Command != command || frame.Meta.Exit != 0 {
			t.Fatal("health success lost common frame")
		}
		data = frame.Data
	}
	var result map[string]json.RawMessage
	if json.Unmarshal(data, &result) != nil {
		t.Fatal("health result is not structured data")
	}
	return result
}

func TestHealthServiceAndLimitFlagsApplyLocallyAndCanBeRemoved(t *testing.T) {
	t.Setenv("SLICK_CONFIG", filepath.Join(t.TempDir(), "unused.toml"))
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	incidents := make([]map[string]any, 25)
	for i := range incidents {
		service := "Files"
		if i%2 == 1 {
			service = "Messaging"
		}
		incidents[i] = map[string]any{"id": fmt.Sprintf("fixture-%02d", i+1), "title": fmt.Sprintf("Incident %02d", i+1), "status": "active", "type": "incident", "services": []string{service}, "notes": []any{}}
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, err := io.ReadAll(r.Body)
		if err != nil || len(body) != 0 || r.URL.RawQuery != "" || r.Method != http.MethodGet || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("health filter or limit was sent to server, or authentication changed")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/history":
			if err := json.NewEncoder(w).Encode(incidents); err != nil {
				t.Error(err)
			}
		case "/current":
			if err := json.NewEncoder(w).Encode(map[string]any{"status": "active", "active_incidents": incidents}); err != nil {
				t.Error(err)
			}
		default:
			t.Error("unexpected endpoint")
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	for _, tc := range []struct {
		name, command, service, first string
		flags                         []string
		count, limit                  int
	}{
		{"history default", "history", "", "fixture-01", nil, 20, 20},
		{"history long zero", "history", "", "fixture-01", []string{"--limit=0"}, 25, 0},
		{"history short limit", "history", "", "fixture-01", []string{"-L", "1"}, 1, 1},
		{"history filter before limit", "history", "Messaging", "fixture-02", []string{"--service=Messaging", "--limit=1"}, 1, 1},
		{"history short filter", "history", "Files", "fixture-01", []string{"-s", "Files", "-L", "2"}, 2, 2},
		{"history padded case", "history", "mEsSaGiNg", "fixture-02", []string{"-s", " mEsSaGiNg ", "-L", "0"}, 12, 0},
		{"history empty filter", "history", "", "fixture-01", []string{"--service=", "--limit=0"}, 25, 0},
		{"history unknown filter", "history", "Future service", "", []string{"--service=Future service"}, 0, 20},
		{"history last filter wins", "history", "Files", "fixture-01", []string{"--service=Messaging", "--service=Files", "--limit=1"}, 1, 1},
		{"current all", "current", "", "fixture-01", nil, 25, 0},
		{"current long filter", "current", "Messaging", "fixture-02", []string{"--service=Messaging"}, 12, 0},
		{"current short padded", "current", "fIlEs", "fixture-01", []string{"-s", " fIlEs "}, 13, 0},
		{"current explicit empty", "current", "", "fixture-01", []string{"--service="}, 25, 0},
		{"current unknown", "current", "Future service", "", []string{"-s", "Future service"}, 0, 0},
	} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				before := calls.Load()
				args := append([]string{"health", tc.command}, tc.flags...)
				out, stderr, err := runHealthFlagCommand(t, server.URL, mode, args...)
				if err != nil || stderr != "" || calls.Load() != before+1 {
					t.Fatalf("health flags failed or replayed: %v", err)
				}
				if mode != "human" {
					data := healthFlagData(t, mode, out, "health."+tc.command)
					var service string
					if raw, ok := data["service"]; ok && json.Unmarshal(raw, &service) != nil {
						t.Fatal("invalid service")
					}
					if service != tc.service {
						t.Fatal("service result lost cleaned selector")
					}
					key, countKey := "incidents", "incident_count"
					if tc.command == "current" {
						key, countKey = "active_incidents", "active_incident_count"
						if string(data["total_active_incident_count"]) != "25" {
							t.Fatal("filter lost unfiltered total")
						}
					}
					var rows []struct{ ID string }
					if json.Unmarshal(data[key], &rows) != nil || rows == nil || len(rows) != tc.count || string(data[countKey]) != fmt.Sprint(tc.count) {
						t.Fatal("local filter/limit or empty result count is wrong")
					}
					if len(rows) > 0 && rows[0].ID != tc.first {
						t.Fatal("limit was applied before filtering or incident order changed")
					}
					if tc.command == "history" {
						if tc.limit == 0 {
							if _, ok := data["limit"]; ok {
								t.Fatal("zero limit should retain existing omitted-limit representation")
							}
						} else if string(data["limit"]) != fmt.Sprint(tc.limit) {
							t.Fatal("wrong limit result")
						}
					}
				} else if out == "" {
					t.Fatal("human health result disappeared")
				}
				// A fresh invocation without optional flags must recover defaults.
				out, stderr, err = runHealthFlagCommand(t, server.URL, mode, "health", tc.command)
				if err != nil || stderr != "" || calls.Load() != before+2 {
					t.Fatalf("removing optional flags did not recover defaults: %v", err)
				}
				if mode != "human" {
					data := healthFlagData(t, mode, out, "health."+tc.command)
					if _, ok := data["service"]; ok {
						t.Fatal("service filter persisted into next invocation")
					}
					key, want := "incident_count", "20"
					if tc.command == "current" {
						key, want = "active_incident_count", "25"
					}
					if string(data[key]) != want {
						t.Fatal("limit/filter persisted into next invocation")
					}
				}
			})
		}
	}
}

func TestHealthCheckStopsAtFailureAndRecovers(t *testing.T) {
	t.Setenv("SLICK_CONFIG", filepath.Join(t.TempDir(), "unused.toml"))
	for _, phase := range []string{"api", "status"} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(phase+"/"+mode, func(t *testing.T) {
				var mu sync.Mutex
				var paths []string
				rejected := true
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					defer mu.Unlock()
					paths = append(paths, r.URL.Path)
					if (r.URL.Path == "/api/api.test" && r.Method != http.MethodPost) || (r.URL.Path == "/current" && r.Method != http.MethodGet) {
						t.Error("combined check changed request methods")
					}
					if r.Header.Get("Authorization") != "" || r.URL.RawQuery != "" {
						t.Error("combined check sent credentials or parameters")
					}
					w.Header().Set("Content-Type", "application/json")
					switch r.URL.Path {
					case "/api/api.test":
						if rejected && phase == "api" {
							_, _ = io.WriteString(w, `{"ok":false,"error":"access_denied"}`)
						} else {
							_, _ = io.WriteString(w, `{"ok":true}`)
						}
					case "/current":
						if rejected && phase == "status" {
							_, _ = io.WriteString(w, `null`)
						} else {
							_, _ = io.WriteString(w, `{"status":"ok","active_incidents":[]}`)
						}
					default:
						t.Error("unexpected health request")
						w.WriteHeader(500)
					}
				}))
				defer server.Close()
				out, stderr, err := runHealthFlagCommand(t, server.URL, mode, "health", "check")
				if err == nil {
					t.Fatal("combined check accepted a failed phase")
				}
				mu.Lock()
				got := append([]string(nil), paths...)
				mu.Unlock()
				want := []string{"/api/api.test"}
				if phase == "status" {
					want = append(want, "/current")
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatal("combined check continued after failure or retried")
				}
				if mode == "human" {
					if out != "" || stderr == "" {
						t.Fatal("combined check published partial success")
					}
				} else {
					var frame struct {
						OK   bool
						Data json.RawMessage
						Meta struct{ Command string }
					}
					if json.Unmarshal([]byte(out), &frame) != nil || frame.OK || string(frame.Data) != "null" || frame.Meta.Command != "health.check" || stderr != "" {
						t.Fatal("combined failure lost frame or published partial success")
					}
				}
				mu.Lock()
				rejected = false
				paths = nil
				mu.Unlock()
				out, stderr, err = runHealthFlagCommand(t, server.URL, mode, "health", "check")
				if err != nil || stderr != "" {
					t.Fatalf("combined check recovery failed: %v", err)
				}
				mu.Lock()
				got = append([]string(nil), paths...)
				mu.Unlock()
				if !reflect.DeepEqual(got, []string{"/api/api.test", "/current"}) {
					t.Fatal("combined recovery omitted or repeated a phase")
				}
				if mode != "human" {
					data := healthFlagData(t, mode, out, "health.check")
					if string(data["healthy"]) != "true" || string(data["api_ok"]) != "true" || string(data["active_incidents"]) != "[]" {
						t.Fatal("combined recovery lost healthy result")
					}
				}
			})
		}
	}
}

func TestHealthInvalidArgumentsFailBeforeRequests(t *testing.T) {
	t.Setenv("SLICK_CONFIG", filepath.Join(t.TempDir(), "unused.toml"))
	for _, tc := range []struct {
		name    string
		args    []string
		handled bool
	}{
		{"negative long", []string{"health", "history", "--limit=-1"}, true},
		{"negative short", []string{"health", "history", "-L", "-1"}, true},
		{"invalid number", []string{"health", "history", "--limit=many"}, false},
		{"empty number", []string{"health", "history", "--limit="}, false},
		{"missing number", []string{"health", "history", "-L"}, false},
		{"overflow", []string{"health", "history", "--limit=999999999999999999999"}, false},
		{"current arguments", []string{"health", "current", "ignored"}, false},
		{"history arguments", []string{"health", "history", "ignored"}, false},
		{"check arguments", []string{"health", "check", "ignored"}, false},
		{"probe arguments", []string{"health", "api-test", "ignored"}, false},
		{"current missing service", []string{"health", "current", "--service"}, false},
		{"history missing service", []string{"health", "history", "-s"}, false},
		{"check missing service", []string{"health", "check", "--service"}, false},
		{"unsupported probe filter", []string{"health", "api-test", "--service=Files"}, false},
		{"unsupported current limit", []string{"health", "current", "--limit=1"}, false},
		{"unsupported check limit", []string{"health", "check", "-L", "1"}, false},
	} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				var requests atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); w.WriteHeader(500) }))
				defer server.Close()
				out, diagnostic, err := runHealthFlagCommand(t, server.URL, mode, tc.args...)
				if err == nil || requests.Load() != 0 {
					t.Fatal("invalid arguments were accepted or reached an endpoint")
				}
				if tc.handled {
					if mode == "human" {
						if out != "" || diagnostic == "" {
							t.Fatal("wrong validation streams")
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
						if json.Unmarshal([]byte(out), &frame) != nil || frame.OK || string(frame.Data) != "null" || frame.Meta.Command != "health.history" || frame.Meta.Exit != 4 || diagnostic != "" {
							t.Fatal("negative limit lost failure envelope")
						}
					}
				} else if out != "" {
					t.Fatal("argument parsing emitted a success result")
				}
			})
		}
	}
}

func TestHealthCheckServiceFlagsAndRemoval(t *testing.T) {
	t.Setenv("SLICK_CONFIG", filepath.Join(t.TempDir(), "unused.toml"))
	for _, tc := range []struct {
		name, status      string
		flags             []string
		incident, healthy bool
		count             int
	}{
		{"healthy", "ok", nil, false, true, 0},
		{"global active without incidents", "active", nil, false, false, 0},
		{"active incident", "active", nil, true, false, 1},
		{"unaffected long", "active", []string{"--service=Huddles"}, true, true, 0},
		{"affected short", "active", []string{"-s", "Messaging"}, true, false, 1},
		{"affected padded case", "active", []string{"--service= mEsSaGiNg "}, true, false, 1},
		{"empty filter", "active", []string{"--service="}, true, false, 1},
		{"unknown service", "active", []string{"-s", "Future service"}, true, true, 0},
	} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					w.Header().Set("Content-Type", "application/json")
					if r.Header.Get("Authorization") != "" || r.URL.RawQuery != "" {
						t.Error("combined filter leaked credentials or parameters")
					}
					if r.URL.Path == "/api/api.test" {
						if r.Method != http.MethodPost {
							t.Error("wrong probe method")
						}
						_, _ = io.WriteString(w, `{"ok":true}`)
						return
					}
					if r.URL.Path != "/current" || r.Method != http.MethodGet {
						t.Error("unexpected status request")
						w.WriteHeader(500)
						return
					}
					incidents := []map[string]any{}
					if tc.incident {
						incidents = append(incidents, map[string]any{"id": "fixture", "title": "Messaging delay", "services": []string{"Messaging"}, "notes": []any{}})
					}
					if err := json.NewEncoder(w).Encode(map[string]any{"status": tc.status, "active_incidents": incidents}); err != nil {
						t.Error(err)
					}
				}))
				defer server.Close()
				args := append([]string{"health", "check"}, tc.flags...)
				out, stderr, err := runHealthFlagCommand(t, server.URL, mode, args...)
				if err != nil || stderr != "" || calls.Load() != 2 {
					t.Fatalf("valid degraded result should succeed: %v", err)
				}
				if mode != "human" {
					data := healthFlagData(t, mode, out, "health.check")
					total := 0
					if tc.incident {
						total = 1
					}
					if string(data["healthy"]) != fmt.Sprint(tc.healthy) || string(data["api_ok"]) != "true" || string(data["active_incident_count"]) != fmt.Sprint(tc.count) || string(data["total_active_incident_count"]) != fmt.Sprint(total) {
						t.Fatal("health check selected the wrong service/status semantics")
					}
				} else if !strings.Contains(out, "healthy="+fmt.Sprint(tc.healthy)) {
					t.Fatal("human check lost true/false health state")
				}
				out, stderr, err = runHealthFlagCommand(t, server.URL, mode, "health", "check")
				if err != nil || stderr != "" || calls.Load() != 4 {
					t.Fatal("check flags did not reset for next invocation")
				}
				if mode != "human" {
					data := healthFlagData(t, mode, out, "health.check")
					want := tc.status == "ok" && !tc.incident
					if string(data["healthy"]) != fmt.Sprint(want) {
						t.Fatal("service flag persisted into next check")
					}
				}
			})
		}
	}
}
