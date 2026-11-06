package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"time"

	"github.com/matcra587/slack-cli/api"
	slackgo "github.com/slack-go/slack"
)

func TestSDKStatusMutationRequiresAcknowledgementWithoutReplay(t *testing.T) {
	for _, tc := range []struct {
		name, body        string
		valid, permission bool
	}{
		{name: "missing", body: `{}`},
		{name: "null document", body: `null`},
		{name: "null acknowledgement", body: `{"ok":null}`},
		{name: "missing error", body: `{"ok":false}`},
		{name: "blank error", body: `{"ok":false,"error":" "}`},
		{name: "contradictory", body: `{"ok":true,"error":"internal_error"}`},
		{name: "wrong acknowledgement", body: `{"ok":"true"}`},
		{name: "trailing document", body: `{"ok":true} {"secret":"synthetic-status-response"}`},
		{name: "trailing garbage", body: `{"ok":true} synthetic-status-response`},
		{name: "truncated", body: `{"secret":"synthetic-status-response`},
		{name: "empty", body: ``},
		{name: "permission", body: `{"ok":false,"error":"missing_scope","needed":"users.profile:write"}`, permission: true},
		{name: "success", body: `{"ok":true,"profile":{"status_text":"PR review","status_emoji":":eyes:","status_expiration":0,"private_field":"synthetic-status-response"}}`, valid: true},
		{name: "acknowledgement only", body: `{"ok":true}`, valid: true},
	} {
		for _, clear := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/clear=%t", tc.name, clear), func(t *testing.T) {
				t.Parallel()
				var calls atomic.Int32
				var recoverResponse atomic.Bool
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if r.Method != http.MethodPost || r.URL.Path != "/api/users.profile.set" || r.URL.RawQuery != "" || r.PostForm.Get("token") != "xoxp-synthetic-status" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || r.PostForm.Has("user") || len(r.PostForm) != 2 {
						t.Error("wrong status method/path/auth/parameters")
					}
					wantText, wantEmoji := "PR review", ":eyes:"
					if clear {
						wantText, wantEmoji = "", ""
					}
					var profile map[string]any
					if json.Unmarshal([]byte(r.PostForm.Get("profile")), &profile) != nil || !reflect.DeepEqual(profile, map[string]any{"status_text": wantText, "status_emoji": wantEmoji, "status_expiration": float64(0)}) {
						t.Error("status mutation touched unrelated profile fields or omitted explicit clears")
					}
					w.Header().Set("Content-Type", "application/json")
					body := tc.body
					if recoverResponse.Load() {
						body = `{"ok":true}`
					}
					_, _ = io.WriteString(w, body)
				}))
				defer server.Close()
				client := api.NewSDKClient("xoxp-synthetic-status", api.SDKOptions{APIURL: server.URL, NoThrottle: true})
				text, emoji := "PR review", ":eyes:"
				if clear {
					text, emoji = "", ""
				}
				err := client.SetUserCustomStatusContext(t.Context(), text, emoji, 0)
				if (err == nil) != tc.valid {
					t.Fatalf("status acknowledgement validity: err=%v want success=%t", err, tc.valid)
				}
				if err != nil && strings.Contains(err.Error(), "synthetic-status-response") {
					t.Fatal("unreviewed response leaked")
				}
				if tc.permission {
					var permission slackgo.SlackErrorResponse
					if !errors.As(err, &permission) || permission.Err != "missing_scope" || permission.Needed != "users.profile:write" {
						t.Fatal("structured permission details lost")
					}
				}
				if calls.Load() != 1 {
					t.Fatal("status mutation replayed")
				}
				recoverResponse.Store(true)
				if err := client.SetUserCustomStatusContext(t.Context(), text, emoji, 0); err != nil || calls.Load() != 2 {
					t.Fatalf("explicit next invocation did not recover once: %v", err)
				}
			})
		}
	}
}

func TestSDKStatusMutationBodyFailuresCloseAndPreserveContext(t *testing.T) {
	for _, tc := range []struct {
		name  string
		body  io.Reader
		valid bool
	}{
		{"success", strings.NewReader(`{"ok":true}`), true},
		{"invalid", strings.NewReader(`{}`), false},
		{"read error", iotest.ErrReader(errors.New("synthetic-private-status-read-error")), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls, closes atomic.Int32
			transport := sdkTestRoundTripper(func(*http.Request) (*http.Response, error) {
				calls.Add(1)
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: &sdkReactionBody{Reader: tc.body, closes: &closes}}, nil
			})
			client := api.NewSDKClient("xoxp-synthetic-status", api.SDKOptions{HTTPClient: &http.Client{Transport: transport}, NoThrottle: true})
			err := client.SetUserCustomStatusContext(t.Context(), "PR review", ":eyes:", 0)
			if (err == nil) != tc.valid || calls.Load() != 1 || closes.Load() != 1 {
				t.Fatalf("body handling changed: err=%v calls=%d closes=%d", err, calls.Load(), closes.Load())
			}
			if err != nil && strings.Contains(err.Error(), "synthetic-private-status-read-error") {
				t.Fatal("body provider error leaked")
			}
		})
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Error(err)
			return
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	client := api.NewSDKClient("xoxp-synthetic-status", api.SDKOptions{APIURL: server.URL, NoThrottle: true})
	if err := client.SetUserCustomStatusContext(ctx, "PR review", ":eyes:", 0); !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 1 {
		t.Fatalf("status deadline lost or replayed: %v", err)
	}
}
