//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestBinaryGenericAPIDiscoveryAndMutationGuard(t *testing.T) {
	binary := buildSlackBinary(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "missing.toml")
	for _, row := range []struct {
		args []string
		ok   bool
	}{
		{[]string{"api", "methods", "users."}, true},
		{[]string{"api", "describe", "users.info"}, true},
		{[]string{"api", "call", "chat.postMessage", "--field", "channel=C123", "--field", "text=Release checks passed.", "--dry-run"}, true},
		{[]string{"api", "call", "chat.postMessage", "--field", "channel=C123", "--field", "text=Release checks passed."}, false},
	} {
		out, diagnostic, err := runSlackBinary(t, binary, path, server.URL, "", append(row.args, "--output=json")...)
		var frame struct{ OK bool }
		if json.Unmarshal([]byte(out), &frame) != nil || frame.OK != row.ok || (err == nil) != row.ok || diagnostic != "" {
			t.Fatalf("unexpected API result for %v: stdout=%s stderr=%s error=%v", row.args, out, diagnostic, err)
		}
	}
	if requests.Load() != 0 {
		t.Fatal("API discovery, dry run, or mutation guard contacted Slack")
	}
}

func TestBinaryGenericAPIMutationDoesNotRetry(t *testing.T) {
	binary := buildSlackBinary(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/api/chat.postMessage" || r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer xoxb-test" {
			t.Error("generic API request lost its method or workspace authentication")
		}
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		writeJSON(w, `{"ok":false,"error":"ratelimited"}`)
	}))
	defer server.Close()
	out, diagnostic, err := runSlackBinary(t, binary, writePipeConfig(t), server.URL, "",
		"api", "call", "chat.postMessage", "--force", "--field", "channel=C123",
		"--field", "text=Release checks passed.", "--no-throttle", "--output=json")
	var frame struct {
		OK   bool
		Data json.RawMessage
	}
	if json.Unmarshal([]byte(out), &frame) != nil || frame.OK || string(frame.Data) != "null" || exitCode(err) != 3 || diagnostic != "" {
		t.Fatalf("unexpected mutation failure: stdout=%s stderr=%s error=%v", out, diagnostic, err)
	}
	if requests.Load() != 1 || strings.Contains(out+diagnostic, "xoxb-test") {
		t.Fatal("generic mutation retried or exposed credentials")
	}
}
