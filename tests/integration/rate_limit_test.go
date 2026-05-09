//go:build integration

package integration_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBinaryRetriesRetryAfterRateLimit(t *testing.T) {
	binary := buildSlackBinary(t)
	configPath := writePipeConfig(t)
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth.test" {
			writeJSON(w, `{"ok":true,"user_id":"U123"}`)
			return
		}
		if r.URL.Path == "/api/chat.getPermalink" {
			writeJSON(w, `{"ok":true,"permalink":"https://example.slack.com/archives/C123/p1746284582123456"}`)
			return
		}
		if r.URL.Path != "/api/conversations.history" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		attempts++
		if attempts == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"ok":false,"error":"ratelimited"}`))
			return
		}
		writeJSON(w, `{"ok":true,"messages":[{"type":"message","text":"ok","ts":"1746284582.123456"}]}`)
	}))
	defer server.Close()

	stdout, stderr, err := runSlackBinary(t, binary, configPath, server.URL, "", "history", "list", "--channel", "C123")
	if err != nil {
		t.Fatalf("command returned error: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want retry", attempts)
	}
}

func TestBinaryRateLimitExhaustionUsesExitCode3(t *testing.T) {
	binary := buildSlackBinary(t)
	configPath := writePipeConfig(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth.test" {
			writeJSON(w, `{"ok":true,"user_id":"U123"}`)
			return
		}
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"ok":false,"error":"ratelimited"}`))
	}))
	defer server.Close()

	_, stderr, err := runSlackBinary(t, binary, configPath, server.URL, "", "history", "list", "--channel", "C123")
	if err == nil {
		t.Fatal("command succeeded, want rate-limit failure")
	}
	if exitCode := exitCode(err); exitCode != 3 {
		t.Fatalf("exit code = %d, want 3; stderr=%s", exitCode, stderr)
	}
	if !strings.Contains(stderr, `"type":"rate_limit"`) {
		t.Fatalf("stderr = %s, want rate_limit", stderr)
	}
}
