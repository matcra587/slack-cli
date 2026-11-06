package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type sourceTransport struct{ target *url.URL }

func (transport sourceTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.URL.Scheme = transport.target.Scheme
	clone.URL.Host = transport.target.Host
	return http.DefaultTransport.RoundTrip(clone)
}

func TestFetchReadsBoundedOfficialSourceThroughLocalHTTPFixture(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/reference/methods/auth.test.md" || r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected source request: %s %s, authorization=%t", r.Method, r.URL.Path, r.Header.Get("Authorization") != "")
		}
		_, _ = w.Write([]byte(historySource))
	}))
	defer server.Close()
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: sourceTransport{target}}
	raw, err := fetch(t.Context(), client, "https://docs.slack.dev/reference/methods/auth.test.md")
	if err != nil || string(raw) != historySource {
		t.Fatalf("source = %q, error = %v", raw, err)
	}
	if _, err := fetch(t.Context(), client, "https://example.com/reference/methods/auth.test.md"); err == nil {
		t.Fatal("untrusted source accepted")
	}
}

func TestFetchRejectsErrorAndOversizedSources(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{"missing source", http.StatusNotFound, "not found"},
		{"oversized source", http.StatusOK, strings.Repeat("x", (2<<20)+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			target, err := url.Parse(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			client := &http.Client{Transport: sourceTransport{target}}
			if _, err := fetch(t.Context(), client, "https://docs.slack.dev/reference/methods/auth.test.md"); err == nil {
				t.Fatal("invalid source accepted")
			}
		})
	}
}

func TestLoadMethodsRejectsPartialSnapshotsAndCancellation(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "conversations.history.md"), []byte(historySource), 0o600); err != nil {
		t.Fatal(err)
	}
	urls := []string{"https://docs.slack.dev/reference/methods/conversations.history.md", "https://docs.slack.dev/reference/methods/missing.md"}
	if result, err := loadMethods(t.Context(), nil, urls, directory); err == nil || result != nil {
		t.Fatalf("partial results must not replace a complete snapshot: %#v, %v", result, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := loadMethods(ctx, nil, urls[:1], directory); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled generation error = %v", err)
	}
}

func TestWriteJSONPreservesPriorStateOnEncodingFailure(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "catalog.json")
	prior := `{"prior":true}`
	if err := os.WriteFile(path, []byte(prior), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(path, make(chan int)); err == nil {
		t.Fatal("invalid JSON value accepted")
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != prior {
		t.Fatalf("prior snapshot changed: %s, %v", raw, err)
	}
	if err := writeJSON(path, map[string]bool{"updated": true}); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(path)
	if err != nil || !strings.Contains(string(raw), `"updated": true`) {
		t.Fatalf("updated snapshot missing: %s, %v", raw, err)
	}
}
