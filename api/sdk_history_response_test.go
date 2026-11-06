package api_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/matcra587/slack-cli/api"
	slackgo "github.com/slack-go/slack"
)

func TestSDKHistoryPreservesDataAndClosesResponse(t *testing.T) {
	for _, method := range []string{"conversations.history", "conversations.replies"} {
		for _, tc := range []struct {
			name, body string
			readError  bool
			valid      bool
		}{
			{"populated", `{"ok":true,"messages":[{"type":"message","text":"Review approved","ts":"1746284582.123456","blocks":[{"type":"section","text":{"type":"mrkdwn","text":"*Approved*"}}]}],"has_more":true,"response_metadata":{"next_cursor":"opaque-next"}}`, false, true},
			{"empty", `{"ok":true,"messages":[]}`, false, true},
			{"missing ack", `{"messages":[]}`, false, false},
			{"false ack", `{"ok":false,"messages":[]}`, false, false},
			{"contradictory", `{"ok":true,"error":"private-field-canary","messages":[]}`, false, false},
			{"missing messages", `{"ok":true}`, false, false},
			{"private read failure", "", true, false},
		} {
			t.Run(method+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				reader := io.Reader(strings.NewReader(tc.body))
				if tc.readError {
					reader = iotest.ErrReader(errors.New("private-field-canary"))
				}
				body := &historyTrackedBody{Reader: reader}
				calls := 0
				client := api.NewSDKClient("xoxp-synthetic-history", api.SDKOptions{NoThrottle: true, HTTPClient: &http.Client{Transport: historyTransport(func(r *http.Request) (*http.Response, error) {
					calls++
					if r.Method != http.MethodPost || r.URL.Path != "/api/"+method {
						t.Error("SDK history validation changed routing")
					}
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: body}, nil
				})}})
				messages, hasMore, cursor, err := invokeHistorySDK(t.Context(), client, method)
				if (err == nil) != tc.valid || calls != 1 || !body.closed {
					t.Fatalf("history validation lost acknowledgement, replay or close guard: valid=%v err=%v calls=%d closed=%v", tc.valid, err, calls, body.closed)
				}
				if err != nil && strings.Contains(err.Error(), "private-field-canary") {
					t.Fatal("history response leaked private error data")
				}
				if tc.name == "populated" && (len(messages) != 1 || messages[0].Timestamp != "1746284582.123456" || messages[0].Text != "Review approved" || len(messages[0].Blocks.BlockSet) != 1 || !hasMore || cursor != "opaque-next") {
					t.Fatal("history validation lost Slack message data or pagination")
				}
				if tc.name == "empty" && (messages == nil || len(messages) != 0 || hasMore || cursor != "") {
					t.Fatal("history validation lost explicit empty page")
				}
			})
		}
	}
}

type historyTransport func(*http.Request) (*http.Response, error)

func (f historyTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type historyTrackedBody struct {
	io.Reader
	closed bool
}

func (b *historyTrackedBody) Close() error { b.closed = true; return nil }

func invokeHistorySDK(ctx context.Context, client *slackgo.Client, method string) ([]slackgo.Message, bool, string, error) {
	if method == "conversations.replies" {
		return client.GetConversationRepliesContext(ctx, &slackgo.GetConversationRepliesParameters{ChannelID: "C123", Timestamp: "1746284582.123456"})
	}
	response, err := client.GetConversationHistoryContext(ctx, &slackgo.GetConversationHistoryParameters{ChannelID: "C123"})
	if err != nil {
		return nil, false, "", err
	}
	return response.Messages, response.HasMore, response.ResponseMetaData.NextCursor, nil
}
