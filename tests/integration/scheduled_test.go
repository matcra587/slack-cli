//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIntegrationScheduledMessagesPipeContracts(t *testing.T) {
	binary := buildSlackBinary(t)
	configPath := writePipeConfig(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/auth.test":
			writeJSON(w, `{"ok":true,"user_id":"U123"}`)
		case "/api/chat.scheduleMessage":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse schedule form: %v", err)
			}
			wantChannel := "C123"
			if r.Form.Get("text") == "scheduled dm" {
				wantChannel = "D123"
			}
			if got := r.Form.Get("channel"); got != wantChannel {
				t.Fatalf("schedule channel = %q, want %s", got, wantChannel)
			}
			if got := r.Form.Get("post_at"); got == "" {
				t.Fatalf("schedule post_at is empty")
			}
			writeJSON(w, `{"ok":true,"channel":"`+wantChannel+`","scheduled_message_id":"Q123","text":"scheduled"}`)
		case "/api/users.lookupByEmail":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse email lookup form: %v", err)
			}
			if got := r.Form.Get("email"); got != "dev@example.com" {
				t.Fatalf("email = %q, want dev@example.com", got)
			}
			writeJSON(w, `{"ok":true,"user":{"id":"UDEV","name":"dev"}}`)
		case "/api/conversations.open":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse conversation open form: %v", err)
			}
			if got := r.Form.Get("users"); got != "UDEV" {
				t.Fatalf("users = %q, want UDEV", got)
			}
			writeJSON(w, `{"ok":true,"channel":{"id":"D123"}}`)
		case "/api/chat.scheduledMessages.list":
			writeJSON(w, `{"ok":true,"scheduled_messages":[{"id":"Q123","channel_id":"C123","post_at":1780000000,"text":"scheduled"}],"response_metadata":{"next_cursor":"cur-2"}}`)
		case "/api/conversations.info":
			writeJSON(w, `{"ok":true,"channel":{"id":"C123","name":"alerts","is_channel":true}}`)
		case "/api/chat.deleteScheduledMessage":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse delete form: %v", err)
			}
			if got := r.Form.Get("scheduled_message_id"); got != "Q123" {
				t.Fatalf("scheduled_message_id = %q, want Q123", got)
			}
			writeJSON(w, `{"ok":true}`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	tests := []struct {
		name string
		args []string
		in   string
	}{
		{name: "scheduled send stdin", args: []string{"message", "send", "--channel", "C123", "--file", "-", "--schedule", "90m"}, in: "scheduled"},
		{name: "scheduled dm by email", args: []string{"message", "send", "--user", "dev@example.com", "--message", "scheduled dm", "--schedule", "90m"}},
		{name: "scheduled list", args: []string{"message", "scheduled", "list", "--channel", "C123", "--cursor", "cur-1", "--limit", "1"}},
		{name: "scheduled delete", args: []string{"message", "scheduled", "delete", "--channel", "C123", "--scheduled-id", "Q123"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, err := runSlackBinary(t, binary, configPath, server.URL, tt.in, tt.args...)
			if err != nil {
				t.Fatalf("command returned error: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
			}
			var payload map[string]any
			if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
				t.Fatalf("stdout is not JSON data: %v\n%s", err, stdout)
			}
			if stderr != "" {
				t.Fatalf("stderr = %q, want empty", stderr)
			}
		})
	}
}
