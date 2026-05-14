package health

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/matcra587/slack-cli/internal/cli/runtime/runtimetest"
)

func TestHealthCheckJSON(t *testing.T) {
	t.Parallel()
	var apiTestCalled, currentCalled atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/api.test":
			apiTestCalled.Store(true)
			if r.Method != http.MethodPost {
				t.Fatalf("api.test method = %s, want POST", r.Method)
			}
			writeJSON(t, w, `{"ok":true,"args":{}}`)
		case "/current":
			currentCalled.Store(true)
			writeJSON(t, w, `{"status":"ok","date_created":"2026-05-08T11:31:22-07:00","date_updated":"2026-05-11T14:48:12-07:00","active_incidents":[]}`)
		default:
			t.Fatalf("unexpected request %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	stdout, stderr, err := executeHealth(t, server.URL, []string{"health", "check", "--output=json"})
	if err != nil {
		t.Fatalf("health check returned error: %v\nstderr=%s", err, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	if !apiTestCalled.Load() || !currentCalled.Load() {
		t.Fatalf("apiTestCalled=%v currentCalled=%v, want both true", apiTestCalled.Load(), currentCalled.Load())
	}
	var envelope struct {
		Meta struct {
			Command string `json:"command"`
		} `json:"meta"`
		Data CheckData `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if envelope.Meta.Command != "health.check" {
		t.Fatalf("meta.command = %q, want health.check", envelope.Meta.Command)
	}
	if !envelope.Data.Healthy || !envelope.Data.APIOK || envelope.Data.Status != "ok" || envelope.Data.ActiveIncidentCount != 0 {
		t.Fatalf("data = %+v, want healthy ok check", envelope.Data)
	}
}

func TestHealthCurrentHumanIncidentTable(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/current" {
			t.Fatalf("unexpected request %s", r.URL.Path)
		}
		writeJSON(t, w, `{"status":"active","date_updated":"2026-05-11T14:48:12-07:00","active_incidents":[{"id":"546","title":"Messaging is delayed","type":"incident","status":"active","url":"https://slack-status.com/incident","date_created":"2026-05-11T13:00:00-07:00","date_updated":"2026-05-11T14:48:12-07:00","services":["Messaging","Apps/Integrations/APIs"],"notes":[{"body":"Investigating","date_created":"2026-05-11T13:00:00-07:00"}]}]}`)
	}))
	t.Cleanup(server.Close)

	stdout, stderr, err := executeHealth(t, server.URL, []string{"health", "current", "--output=human"})
	if err != nil {
		t.Fatalf("health current returned error: %v\nstderr=%s", err, stderr)
	}
	for _, fragment := range []string{"ID", "STATUS", "SERVICES", "Messaging is delayed", "Messaging,Apps/Integrations/APIs"} {
		if !strings.Contains(stdout, fragment) {
			t.Fatalf("stdout = %q, want fragment %q", stdout, fragment)
		}
	}
}

func TestHealthCheckServiceFilterIgnoresUnrelatedActiveIncidents(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/api.test":
			writeJSON(t, w, `{"ok":true,"args":{}}`)
		case "/current":
			writeJSON(t, w, `{"status":"active","date_updated":"2026-05-11T14:48:12-07:00","active_incidents":[{"id":"546","title":"Huddles is delayed","type":"incident","status":"active","url":"https://slack-status.com/incident","date_created":"2026-05-11T13:00:00-07:00","date_updated":"2026-05-11T14:48:12-07:00","services":["Huddles"],"notes":[]}]}`)
		default:
			t.Fatalf("unexpected request %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	stdout, stderr, err := executeHealth(t, server.URL, []string{"health", "check", "--service", "Messaging", "--output=json"})
	if err != nil {
		t.Fatalf("health check returned error: %v\nstderr=%s", err, stderr)
	}
	var envelope struct {
		Data CheckData `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if !envelope.Data.Healthy || envelope.Data.ActiveIncidentCount != 0 || envelope.Data.TotalActiveIncidentCount != 1 {
		t.Fatalf("data = %+v, want healthy filtered check with one unrelated active incident", envelope.Data)
	}
}

func TestHealthHistoryFiltersAndLimits(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/history" {
			t.Fatalf("unexpected request %s", r.URL.Path)
		}
		writeJSON(t, w, `[
			{"id":1551,"title":"Messaging issue","type":"incident","status":"resolved","url":"https://slack-status.com/1","date_created":"2026-05-08T11:31:22-07:00","date_updated":"2026-05-11T14:48:12-07:00","services":["Messaging"],"notes":[{"body":"Resolved","date_created":"2026-05-11T14:48:12-07:00"}]},
			{"id":"1552","title":"Huddles issue","type":"incident","status":"resolved","url":"https://slack-status.com/2","date_created":"2026-05-07T11:31:22-07:00","date_updated":"2026-05-07T14:48:12-07:00","services":["Huddles"],"notes":[]},
			{"id":"1553","title":"Another messaging issue","type":"notice","status":"resolved","url":"https://slack-status.com/3","date_created":"2026-05-06T11:31:22-07:00","date_updated":"2026-05-06T14:48:12-07:00","services":["Messaging"],"notes":[]}
		]`)
	}))
	t.Cleanup(server.Close)

	stdout, stderr, err := executeHealth(t, server.URL, []string{"health", "history", "--service", "Messaging", "--limit", "1", "--output=json"})
	if err != nil {
		t.Fatalf("health history returned error: %v\nstderr=%s", err, stderr)
	}
	var envelope struct {
		Data HistoryData `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if envelope.Data.IncidentCount != 1 || len(envelope.Data.Incidents) != 1 {
		t.Fatalf("incident count = %d len=%d, want 1", envelope.Data.IncidentCount, len(envelope.Data.Incidents))
	}
	if got := envelope.Data.Incidents[0].ID; got != "1551" {
		t.Fatalf("incident id = %q, want numeric id coerced to string 1551", got)
	}
}

func TestHealthAPITestMapsSlackError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/api.test" {
			t.Fatalf("unexpected request %s", r.URL.Path)
		}
		writeJSON(t, w, `{"ok":false,"error":"invalid_auth"}`)
	}))
	t.Cleanup(server.Close)

	stdout, stderr, err := executeHealth(t, server.URL, []string{"health", "api-test", "--output=json"})
	if err == nil {
		t.Fatalf("health api-test unexpectedly succeeded\nstdout=%s", stdout)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty on error", stdout)
	}
	for _, fragment := range []string{`"type":"auth_failure"`, `"message":"invalid_auth"`, `"exit_code":1`} {
		if !strings.Contains(stderr, fragment) {
			t.Fatalf("stderr = %q, want fragment %q", stderr, fragment)
		}
	}
}

func TestHealthHistoryRejectsNegativeLimit(t *testing.T) {
	t.Parallel()
	stdout, stderr, err := executeHealth(t, "http://example.invalid", []string{"health", "history", "--limit", "-1", "--output=json"})
	if err == nil {
		t.Fatalf("health history unexpectedly succeeded\nstdout=%s", stdout)
	}
	for _, fragment := range []string{`"type":"validation_error"`, `"message":"limit must be zero or positive"`, `"exit_code":4`} {
		if !strings.Contains(stderr, fragment) {
			t.Fatalf("stderr = %q, want fragment %q", stderr, fragment)
		}
	}
}

func executeHealth(t *testing.T, baseURL string, args []string) (string, string, error) {
	t.Helper()
	runtime, stdout, stderr := runtimetest.NewRuntime(t, runtimetest.Options{})
	runtime.SlackBaseURL = baseURL
	runtime.SlackStatusBaseURL = baseURL
	root := runtimetest.NewRoot(runtime, stdout, stderr)
	root.AddCommand(NewCommand(runtime))
	return runtimetest.Run(t, root, args, stdout, stderr)
}

func writeJSON(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}
