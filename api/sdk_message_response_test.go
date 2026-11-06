package api_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"time"

	"github.com/matcra587/slack-cli/api"
	slackgo "github.com/slack-go/slack"
)

func TestSDKMessageMethodsRequireExplicitAcknowledgementAndRecover(t *testing.T) {
	for _, method := range []string{"chat.postMessage", "chat.update", "chat.delete", "chat.scheduleMessage", "chat.scheduledMessages.list", "chat.deleteScheduledMessage"} {
		for _, tc := range []struct {
			name, body string
			permission bool
		}{
			{"missing", `{}`, false},
			{"null", `null`, false},
			{"null ack", `{"ok":null}`, false},
			{"false without error", `{"ok":false}`, false},
			{"blank error", `{"ok":false,"error":" "}`, false},
			{"contradictory", `{"ok":true,"error":"internal_error"}`, false},
			{"wrong ack", `{"ok":"true"}`, false},
			{"empty", ``, false},
			{"truncated", `{"private_field":"synthetic-message-response-canary`, false},
			{"trailing", `{"ok":true} {"private_field":"synthetic-message-response-canary"}`, false},
			{"permission", `{"ok":false,"error":"missing_scope","needed":"chat:write"}`, true},
		} {
			t.Run(method+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				var calls atomic.Int32
				var repaired atomic.Bool
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if r.Method != http.MethodPost || r.URL.Path != "/api/"+method || r.URL.RawQuery != "" || r.PostForm.Get("token") != "xoxp-synthetic-message-response" {
						t.Error("message response validation changed method/path/auth")
					}
					body := tc.body
					if repaired.Load() {
						body = sdkMessageSuccess(method)
					}
					_, _ = io.WriteString(w, body)
				}))
				defer server.Close()
				client := api.NewSDKClient("xoxp-synthetic-message-response", api.SDKOptions{APIURL: server.URL, NoThrottle: true})
				_, err := invokeSDKMessage(t.Context(), client, method)
				if calls.Load() != 1 {
					t.Fatal("invalid chat response replayed request")
				}
				if err == nil {
					t.Fatal("invalid chat acknowledgement reported success")
				}
				if strings.Contains(err.Error(), "synthetic-message-response-canary") {
					t.Fatal("invalid chat response leaked private data")
				}
				if tc.permission {
					var scope slackgo.SlackErrorResponse
					if !errors.As(err, &scope) || scope.Err != "missing_scope" || scope.Needed != "chat:write" {
						t.Fatal("chat guard lost native structured permission details")
					}
				}
				repaired.Store(true)
				got, err := invokeSDKMessage(t.Context(), client, method)
				if err != nil || calls.Load() != 2 {
					t.Fatalf("explicit chat invocation did not recover once: %v", err)
				}
				switch method {
				case "chat.postMessage", "chat.update", "chat.delete":
					if got.channel != "C123" || got.id != "1746284582.000001" {
						t.Fatal("chat guard lost identifiers or timestamp precision")
					}
				case "chat.scheduleMessage":
					if got.channel != "C123" || got.id != "Q123" {
						t.Fatal("chat guard lost scheduled identifier")
					}
				case "chat.scheduledMessages.list":
					if got.messages == nil || len(got.messages) != 1 || got.messages[0].ID != "Q123" || got.messages[0].Channel != "C123" || got.messages[0].PostAt != 1780000000 || got.messages[0].Text != "Server normalized" || got.cursor != "NEXT" {
						t.Fatal("chat guard lost scheduled data or pagination")
					}
				case "chat.deleteScheduledMessage":
					if !got.deleted {
						t.Fatal("chat guard lost deletion acknowledgement")
					}
				}
				if method == "chat.postMessage" && got.message.Text != "Server normalized" {
					t.Fatal("chat guard replaced server message content")
				}
				if method == "chat.update" && got.text != "Server normalized" {
					t.Fatal("chat guard replaced server edit content")
				}
			})
		}
	}
}

func TestSDKMessagePayloadsDistinguishMissingAndEmptyData(t *testing.T) {
	for _, tc := range []struct {
		name, method, body string
		valid              bool
	}{
		{"post missing identifiers", "chat.postMessage", `{"ok":true}`, false},
		{"post missing channel", "chat.postMessage", `{"ok":true,"ts":"1746284582.000001"}`, false},
		{"post missing timestamp", "chat.postMessage", `{"ok":true,"channel":"C123"}`, false},
		{"post blank channel", "chat.postMessage", `{"ok":true,"channel":" ","ts":"1746284582.000001"}`, false},
		{"post alternate SDK timestamp", "chat.postMessage", `{"ok":true,"channel":"C123","message_ts":"1746284582.000001"}`, true},
		{"schedule missing id", "chat.scheduleMessage", `{"ok":true,"channel":"C123"}`, false},
		{"schedule wrong fallback id", "chat.scheduleMessage", `{"ok":true,"channel":"C123","ts":"1746284582.000001"}`, false},
		{"schedule blank id", "chat.scheduleMessage", `{"ok":true,"channel":"C123","scheduled_message_id":" "}`, false},
		{"list missing", "chat.scheduledMessages.list", `{"ok":true}`, false},
		{"list null", "chat.scheduledMessages.list", `{"ok":true,"scheduled_messages":null}`, false},
		{"list empty", "chat.scheduledMessages.list", `{"ok":true,"scheduled_messages":[],"response_metadata":{"next_cursor":"NEXT"}}`, true},
		{"list missing row id", "chat.scheduledMessages.list", `{"ok":true,"scheduled_messages":[{"channel_id":"C123","post_at":1780000000}]}`, false},
		{"list missing row channel", "chat.scheduledMessages.list", `{"ok":true,"scheduled_messages":[{"id":"Q123","post_at":1780000000}]}`, false},
		{"list missing row time", "chat.scheduledMessages.list", `{"ok":true,"scheduled_messages":[{"id":"Q123","channel_id":"C123"}]}`, false},
		{"list zero row time", "chat.scheduledMessages.list", `{"ok":true,"scheduled_messages":[{"id":"Q123","channel_id":"C123","post_at":0}]}`, false},
		{"list partial page", "chat.scheduledMessages.list", `{"ok":true,"scheduled_messages":[{"id":"Q123","channel_id":"C123","post_at":1780000000},{}]}`, false},
		{"edit ack only", "chat.update", `{"ok":true}`, true},
		{"delete ack only", "chat.delete", `{"ok":true}`, true},
		{"scheduled delete ack only", "chat.deleteScheduledMessage", `{"ok":true}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); _, _ = io.WriteString(w, tc.body) }))
			defer server.Close()
			client := api.NewSDKClient("xoxp-synthetic-message-response", api.SDKOptions{APIURL: server.URL, NoThrottle: true})
			got, err := invokeSDKMessage(t.Context(), client, tc.method)
			if (err == nil) != tc.valid || calls.Load() != 1 {
				t.Fatalf("chat payload lost missing/empty distinction or replayed: %v", err)
			}
			if tc.name == "list empty" && (got.messages == nil || len(got.messages) != 0 || got.cursor != "NEXT") {
				t.Fatal("empty scheduled page lost continuation")
			}
		})
	}
}

func TestSDKMessageResponseBodyFailureIsPrivateAndCloses(t *testing.T) {
	var calls, closes atomic.Int32
	transport := sdkTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: &sdkReactionBody{Reader: iotest.ErrReader(errors.New("synthetic-message-response-canary")), closes: &closes}, Request: r}, nil
	})
	client := api.NewSDKClient("xoxp-synthetic-message-response", api.SDKOptions{HTTPClient: &http.Client{Transport: transport}, NoThrottle: true})
	_, err := invokeSDKMessage(t.Context(), client, "chat.postMessage")
	if err == nil || strings.Contains(err.Error(), "synthetic-message-response-canary") || calls.Load() != 1 || closes.Load() != 1 {
		t.Fatal("chat read failure leaked data, body or replayed mutation")
	}
}

func TestSDKMessageResponseHonorsBodyDeadline(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":`)
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Error(err)
			return
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	client := api.NewSDKClient("xoxp-synthetic-message-response", api.SDKOptions{APIURL: server.URL, NoThrottle: true})
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_, err := invokeSDKMessage(ctx, client, "chat.postMessage")
	if !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 1 {
		t.Fatalf("chat body deadline was lost or replayed: %v", err)
	}
}

type sdkMessageResult struct {
	channel, id, text, cursor string
	message                   slackgo.Message
	messages                  []slackgo.ScheduledMessage
	deleted                   bool
}

func invokeSDKMessage(ctx context.Context, client *slackgo.Client, method string) (r sdkMessageResult, err error) {
	switch method {
	case "chat.postMessage":
		r.channel, r.id, r.message, err = client.PostMessageWithResponseContext(ctx, "C123", slackgo.MsgOptionText("PR review", false))
	case "chat.update":
		r.channel, r.id, r.text, err = client.UpdateMessageContext(ctx, "C123", "1746284582.000001", slackgo.MsgOptionText("PR review", false))
	case "chat.delete":
		r.channel, r.id, err = client.DeleteMessageContext(ctx, "C123", "1746284582.000001")
	case "chat.scheduleMessage":
		r.channel, r.id, err = client.ScheduleMessageContext(ctx, "C123", "1780000000", slackgo.MsgOptionText("PR review", false))
	case "chat.scheduledMessages.list":
		r.messages, r.cursor, err = client.GetScheduledMessagesContext(ctx, &slackgo.GetScheduledMessagesParameters{Channel: "C123", Limit: 1, Cursor: "CUR"})
	case "chat.deleteScheduledMessage":
		r.deleted, err = client.DeleteScheduledMessageContext(ctx, &slackgo.DeleteScheduledMessageParameters{Channel: "C123", ScheduledMessageID: "Q123"})
	}
	return r, err
}

func sdkMessageSuccess(method string) string {
	switch method {
	case "chat.postMessage":
		return `{"ok":true,"channel":"C123","ts":"1746284582.000001","message":{"type":"message","ts":"1746284582.000001","text":"Server normalized"}}`
	case "chat.update":
		return `{"ok":true,"channel":"C123","ts":"1746284582.000001","text":"Server normalized"}`
	case "chat.delete":
		return `{"ok":true,"channel":"C123","ts":"1746284582.000001"}`
	case "chat.scheduleMessage":
		return `{"ok":true,"channel":"C123","scheduled_message_id":"Q123"}`
	case "chat.scheduledMessages.list":
		return `{"ok":true,"scheduled_messages":[{"id":"Q123","channel_id":"C123","post_at":1780000000,"text":"Server normalized"}],"response_metadata":{"next_cursor":"NEXT"}}`
	default:
		return `{"ok":true}`
	}
}

func TestSDKScheduledListPreservesLegacyErrorMetadata(t *testing.T) {
	const token = "xoxp-synthetic-scheduled-error"
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api/chat.scheduledMessages.list" || r.PostForm.Get("token") != token {
			t.Error("scheduled error request changed")
		}
		_, _ = io.WriteString(w, `{"ok":false,"error":"missing_scope","response_metadata":{"messages":["[ERROR] missing required scope: chat:write"],"warnings":["fixture warning"]}}`)
	}))
	defer server.Close()
	client := api.NewSDKClient(token, api.SDKOptions{APIURL: server.URL, NoThrottle: true})
	_, _, err := client.GetScheduledMessagesContext(t.Context(), &slackgo.GetScheduledMessagesParameters{})
	var slackErr slackgo.SlackErrorResponse
	if !errors.As(err, &slackErr) || slackErr.Err != "missing_scope" || len(slackErr.ResponseMetadata.Messages) != 1 || slackErr.ResponseMetadata.Messages[0] != "[ERROR] missing required scope: chat:write" || len(slackErr.ResponseMetadata.Warnings) != 1 {
		t.Fatal("scheduled-list decoder lost native Slack error metadata")
	}
	if calls.Load() != 1 {
		t.Fatal("scheduled-list permission error retried")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatal("typed error leaked synthetic credentials")
	}
}
