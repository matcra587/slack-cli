package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBinaryAgentAttributionAddsContextBlock(t *testing.T) {
	binary := buildSlackBinary(t)
	configPath := writePipeConfig(t)
	var sawAttribution bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/chat.postMessage":
			var blocks []map[string]any
			if err := json.Unmarshal([]byte(r.FormValue("blocks")), &blocks); err != nil {
				t.Fatalf("blocks is not JSON: %v", err)
			}
			last := blocks[len(blocks)-1]
			if last["type"] == "context" && strings.Contains(r.FormValue("blocks"), ":robot_face: _Sent via slack-cli (agent mode)_") {
				sawAttribution = true
			}
			writeJSON(w, `{"ok":true,"channel":"C123","ts":"1746284582.123456","message":{"type":"message","text":"hello","ts":"1746284582.123456"}}`)
		case "/api/chat.getPermalink":
			writeJSON(w, `{"ok":true,"permalink":"https://example.slack.com/archives/C123/p1746284582123456"}`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	_, stderr, err := runSlackBinary(t, binary, configPath, server.URL, "hello", "message", "send", "--channel", "C123", "--file", "-")
	if err != nil {
		t.Fatalf("command returned error: %v\nstderr=%s", err, stderr)
	}
	if !sawAttribution {
		t.Fatal("attribution context block was not sent")
	}
}
