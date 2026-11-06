package api_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/matcra587/slack-cli/api"
	slackgo "github.com/slack-go/slack"
)

func TestWatchReactionWaitsForRequestedEmojiAndUser(t *testing.T) {
	t.Parallel()
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api/reactions.get" || r.Form.Get("channel") != "C123" || r.Form.Get("timestamp") != "1746284582.123456" || r.Form.Get("full") != "true" {
			t.Errorf("unexpected reactions request: %s %s", r.Method, r.URL.Path)
		}
		if r.Form.Get("token") != "xoxb-synthetic-watch" || r.URL.RawQuery != "" {
			t.Error("authentication missing or exposed in URL")
		}
		w.Header().Set("Content-Type", "application/json")
		switch calls.Add(1) {
		case 1:
			_, _ = fmt.Fprint(w, `{"ok":true,"type":"message","message":{"reactions":[]}}`)
		case 2:
			_, _ = fmt.Fprint(w, `{"ok":true,"type":"message","message":{"reactions":[{"name":"heart","count":1,"users":["U123"]}]}}`)
		case 3:
			_, _ = fmt.Fprint(w, `{"ok":true,"type":"message","message":{"reactions":[{"name":"+1","count":3,"users":["U999"]}]}}`)
		default:
			_, _ = fmt.Fprint(w, `{"ok":true,"type":"message","message":{"reactions":[{"name":"thumbsup","count":4,"users":["U999","U123"]}]}}`)
		}
	}))
	defer server.Close()
	client := slackgo.New("xoxb-synthetic-watch", slackgo.OptionAPIURL(server.URL+"/api/"))
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	match, err := api.WatchReaction(ctx, client, api.ReactionWatchOptions{
		Channel: "C123", Timestamp: "1746284582.123456", Emoji: ":+1:", User: "U123", Interval: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 4 || match.Emoji != "+1" || match.Count != 4 || match.Timestamp != "1746284582.123456" {
		t.Fatalf("incorrect match: %#v after %d requests", match, calls.Load())
	}
}

func TestWatchReactionStopsOnCancellationAndPermissionFailure(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		cancel     bool
	}{
		{name: "permission failure", body: `{"ok":false,"error":"missing_scope","needed":"reactions:read"}`},
		{name: "missing message", body: `{"ok":false,"error":"message_not_found"}`},
		{name: "malformed response", body: `{"ok":`},
		{name: "cancel incomplete user list", body: `{"ok":true,"type":"message","message":{"reactions":[{"name":"+1","count":999,"users":["U999"]}]}}`, cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var calls atomic.Int64
			seen := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if calls.Add(1) == 1 {
					close(seen)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			client := slackgo.New("xoxb-synthetic-watch", slackgo.OptionAPIURL(server.URL+"/api/"))
			if tc.cancel {
				go func() {
					select {
					case <-seen:
						cancel()
					case <-ctx.Done():
					}
				}()
			}
			_, err := api.WatchReaction(ctx, client, api.ReactionWatchOptions{
				Channel: "C123", Timestamp: "1746284582.123456", Emoji: "+1", User: "U123", Interval: time.Hour,
			})
			if err == nil {
				t.Fatal("watch succeeded without an observed match")
			}
			if tc.cancel && !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want cancellation", err)
			}
			if calls.Load() != 1 {
				t.Fatalf("requests = %d, want 1", calls.Load())
			}
		})
	}
}

func TestWatchReactionRejectsInvalidOptionsWithoutRequest(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = fmt.Fprint(w, `{"ok":true,"type":"message","message":{"reactions":[]}}`)
	}))
	defer server.Close()
	client := slackgo.New("xoxb-synthetic-watch", slackgo.OptionAPIURL(server.URL+"/api/"))
	for _, opts := range []api.ReactionWatchOptions{
		{Timestamp: "1746284582.123456", Emoji: "+1"},
		{Channel: "C123", Emoji: "+1"},
		{Channel: "C123", Timestamp: "1746284582.123456"},
		{Channel: "C123", Timestamp: "1746284582.123456", Emoji: "+1,heart"},
		{Channel: "C123", Timestamp: "1746284582.123456", Emoji: "white check mark"},
		{Channel: "C123", Timestamp: "1746284582.123456", Emoji: "+1", Interval: -time.Second},
	} {
		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		_, err := api.WatchReaction(ctx, client, opts)
		cancel()
		if err == nil || errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("invalid options did not fail before polling: %+v, error=%v", opts, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid options made %d requests", calls.Load())
	}
}
