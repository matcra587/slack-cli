package api_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/matcra587/slack-cli/api"
	slackgo "github.com/slack-go/slack"
)

func TestWatchReactionPollingIntervalAndMatchGuards(t *testing.T) {
	for _, tc := range []struct {
		name             string
		interval, period time.Duration
	}{
		{"default five seconds", 0, 5 * time.Second},
		{"custom interval", 3 * time.Second, 3 * time.Second},
		{"fractional API interval", 7 * time.Millisecond, 7 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				start := time.Now()
				var observed []time.Duration
				client := reactionTimingClient(func(r *http.Request) (*http.Response, error) {
					observed = append(observed, time.Since(start))
					if r.Method != http.MethodPost || r.URL.Path != "/api/reactions.get" {
						t.Fatal("watch invoked an unexpected API method")
					}
					if err := r.ParseForm(); err != nil {
						t.Fatal(err)
					}
					if r.PostForm.Get("token") != "xoxb-synthetic-reaction-clock" || r.PostForm.Get("timestamp") != "1746284582.000001" || r.PostForm.Get("full") != "true" || r.PostForm.Has("user") {
						t.Fatal("watch changed authentication, timestamp, full list, or local user filtering")
					}
					var body string
					switch len(observed) {
					case 1:
						body = `{"ok":true,"type":"message","message":{"reactions":[{"name":"+1","count":0,"users":["U_APPROVER"]}]}}`
					case 2:
						body = `{"ok":true,"type":"message","message":{"reactions":[{"name":"+1","count":3,"users":["U_OTHER"]}]}}`
					case 3:
						body = `{"ok":true,"type":"message","message":{"reactions":[{"name":"thumbsup","count":7,"users":["U_APPROVER"]}]}}`
					default:
						t.Fatal("watch polled after its match")
					}
					return reactionClockResponse(r, body), nil
				})
				ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
				defer cancel()
				match, err := api.WatchReaction(ctx, client, api.ReactionWatchOptions{Channel: " C_TARGET ", Timestamp: " 1746284582.000001 ", Emoji: ":+1:", User: " U_APPROVER ", Interval: tc.interval})
				if err != nil {
					t.Fatalf("watch failed: %v", err)
				}
				if match.Channel != "C_TARGET" || match.Timestamp != "1746284582.000001" || match.Emoji != "+1" || match.Count != 7 || !slices.Equal(match.Users, []string{"U_APPROVER"}) {
					t.Fatal("watch accepted zero count/wrong user or inferred unreported users")
				}
				if !slices.Equal(observed, []time.Duration{0, tc.period, 2 * tc.period}) {
					t.Fatalf("watch ignored immediate polling or its interval: %v", observed)
				}
				time.Sleep(2 * tc.period)
				if len(observed) != 3 {
					t.Fatal("watch kept polling after returning")
				}
			})
		})
	}
}

func TestWatchReactionStopsBeforeNextPollOnContextEnd(t *testing.T) {
	for _, deadline := range []bool{true, false} {
		name := "cancel during wait"
		if deadline {
			name = "deadline during wait"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				start := time.Now()
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				want := context.Canceled
				if deadline {
					ctx, cancel = context.WithTimeout(ctx, time.Second)
					defer cancel()
					want = context.DeadlineExceeded
				} else {
					go func() { time.Sleep(time.Second); cancel() }()
				}
				calls := 0
				client := reactionTimingClient(func(r *http.Request) (*http.Response, error) {
					calls++
					return reactionClockResponse(r, `{"ok":true,"type":"message","message":{"reactions":[{"name":"+1","count":7,"users":["U_OTHER"]}]}}`), nil
				})
				_, err := api.WatchReaction(ctx, client, api.ReactionWatchOptions{Channel: "C_TARGET", Timestamp: "1746284582.000001", Emoji: "+1", User: "U_APPROVER", Interval: time.Hour})
				if !errors.Is(err, want) || calls != 1 || time.Since(start) != time.Second {
					t.Fatalf("watch did not stop during its wait: calls=%d elapsed=%s err=%v", calls, time.Since(start), err)
				}
				time.Sleep(2 * time.Hour)
				if calls != 1 {
					t.Fatal("watch polled after context end")
				}
			})
		})
	}
}

type reactionClockTransport func(*http.Request) (*http.Response, error)

func (transport reactionClockTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return transport(r)
}

func reactionTimingClient(transport reactionClockTransport) *slackgo.Client {
	return slackgo.New("xoxb-synthetic-reaction-clock", slackgo.OptionAPIURL("https://fixture.invalid/api/"), slackgo.OptionHTTPClient(&http.Client{Transport: transport}))
}

func reactionClockResponse(r *http.Request, body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}
