package slackmeta_test

import (
	"context"
	"encoding/json"
	"testing"

	slackcache "github.com/matcra587/slack-cli/internal/cache"
	"github.com/matcra587/slack-cli/internal/cli/slackmeta"
	"github.com/matcra587/slack-cli/internal/testutil"
	slackgo "github.com/slack-go/slack"
)

func TestResolveConversationUsesFreshCacheBeforeSlack(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	writeCache(t, "default", "channels", map[string]any{
		"channels": []map[string]any{{
			"id":   "C123",
			"name": "slack_test",
			"type": "channel",
		}},
	})
	server := testutil.NewSlackServer(t, nil)
	client := slackgo.New("xoxb-test", slackgo.OptionAPIURL(server.BaseURL()+"/api/"))

	ref := slackmeta.ResolveConversation(context.Background(), client, "default", "C123")

	if ref.ID != "C123" || ref.Name != "slack_test" || ref.Type != "channel" {
		t.Fatalf("ref = %#v, want cached channel metadata", ref)
	}
	if got := len(server.Requests("conversations.info")); got != 0 {
		t.Fatalf("conversations.info requests = %d, want cache hit without Slack API call", got)
	}
}

func TestResolveConversationFallsBackToSlackAndResolvesDMUserName(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"conversations.info": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("channel"); got != "D123" {
				t.Fatalf("conversations.info channel = %q, want D123", got)
			}
			return testutil.JSONResponse(`{"ok":true,"channel":{"id":"D123","is_im":true,"user":"U123"}}`)
		},
		"users.info": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("user"); got != "U123" {
				t.Fatalf("users.info user = %q, want U123", got)
			}
			return testutil.JSONResponse(`{"ok":true,"user":{"id":"U123","name":"matt","profile":{"display_name":"matcra587"}}}`)
		},
	})
	client := slackgo.New("xoxb-test", slackgo.OptionAPIURL(server.BaseURL()+"/api/"))

	ref := slackmeta.ResolveConversation(context.Background(), client, "default", "D123")

	if ref.ID != "D123" || ref.Name != "matcra587" || ref.User != "U123" || ref.IsDM == nil || !*ref.IsDM {
		t.Fatalf("ref = %#v, want Slack-resolved DM metadata", ref)
	}
}

func writeCache(t *testing.T, profile, resource string, payload any) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal cache payload: %v", err)
	}
	if _, err := slackcache.Write(profile, resource, body); err != nil {
		t.Fatalf("write cache: %v", err)
	}
}
