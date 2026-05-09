package testutil_test

import (
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestSlackServerRoutesAPIRequestsAndRecordsFormValues(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"chat.postMessage": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("channel"); got != "C123" {
				t.Fatalf("channel = %q, want C123", got)
			}
			return testutil.JSONResponse(`{"ok":true,"ts":"1746284582.123456"}`)
		},
	})
	resp, err := http.PostForm(server.BaseURL()+"/api/chat.postMessage", url.Values{
		"channel": []string{"C123"},
	})
	if err != nil {
		t.Fatalf("PostForm returned error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	requests := server.Requests("chat.postMessage")
	if len(requests) != 1 {
		t.Fatalf("recorded requests = %d, want 1", len(requests))
	}
	if got := requests[0].Path; got != "/api/chat.postMessage" {
		t.Fatalf("recorded path = %q, want /api/chat.postMessage", got)
	}
}

func TestFakeKeychainStoresRetrievesAndDeletesSecrets(t *testing.T) {
	keychain := testutil.NewFakeKeychain()

	if err := keychain.Set("slack-cli", "default", "xoxb-test"); err != nil {
		t.Fatalf("Set returned error: %v", err)
	}
	got, err := keychain.Get("slack-cli", "default")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got != "xoxb-test" {
		t.Fatalf("secret = %q, want xoxb-test", got)
	}

	if err := keychain.Delete("slack-cli", "default"); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
	if _, err := keychain.Get("slack-cli", "default"); !errors.Is(err, testutil.ErrSecretNotFound) {
		t.Fatalf("Get after Delete error = %v, want ErrSecretNotFound", err)
	}
}
