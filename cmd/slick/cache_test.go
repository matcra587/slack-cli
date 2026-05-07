package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	slackcache "github.com/matcra587/slack-cli/internal/cache"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestCacheUsersPrimesActiveUsersAndReusesFreshCache(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"users.list": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"members":[{"id":"UACTIVE","name":"active","deleted":false},{"id":"UDELETED","name":"deleted","deleted":true}]}`)
		},
	})
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "", []string{"cache", "users"})
	if err != nil {
		t.Fatalf("cache users returned error: %v\nstderr=%s", err, stderr)
	}
	data := envelopeData(t, stdout)
	if data["from_cache"] != false {
		t.Fatalf("from_cache = %v, want false", data["from_cache"])
	}
	users := data["users"].([]any)
	if len(users) != 1 || users[0].(map[string]any)["id"] != "UACTIVE" {
		t.Fatalf("users = %#v, want active user only", users)
	}

	stdout, stderr, err = executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "", []string{"cache", "users"})
	if err != nil {
		t.Fatalf("cached cache users returned error: %v\nstderr=%s", err, stderr)
	}
	data = envelopeData(t, stdout)
	if data["from_cache"] != true {
		t.Fatalf("from_cache = %v, want true", data["from_cache"])
	}
	if got := len(server.Requests("users.list")); got != 1 {
		t.Fatalf("users.list requests = %d, want 1", got)
	}
}

func TestCacheChannelsPrimesAllConversationTypesAndReusesFreshCache(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"conversations.list": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("types"); got != "public_channel,private_channel,im,mpim" {
				t.Fatalf("types = %q, want all conversation types", got)
			}
			return testutil.JSONResponse(`{"ok":true,"channels":[{"id":"C123","name":"alerts"},{"id":"D123","is_im":true,"user":"U123"}]}`)
		},
	})
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "", []string{"cache", "channels"})
	if err != nil {
		t.Fatalf("cache channels returned error: %v\nstderr=%s", err, stderr)
	}
	data := envelopeData(t, stdout)
	if data["from_cache"] != false {
		t.Fatalf("from_cache = %v, want false", data["from_cache"])
	}
	channels := data["channels"].([]any)
	if len(channels) != 2 {
		t.Fatalf("channels = %#v, want two cached conversations", channels)
	}

	stdout, stderr, err = executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "", []string{"cache", "channels"})
	if err != nil {
		t.Fatalf("cached cache channels returned error: %v\nstderr=%s", err, stderr)
	}
	data = envelopeData(t, stdout)
	if data["from_cache"] != true {
		t.Fatalf("from_cache = %v, want true", data["from_cache"])
	}
	if got := len(server.Requests("conversations.list")); got != 1 {
		t.Fatalf("conversations.list requests = %d, want 1", got)
	}
}

func TestCacheClearRemovesOneResource(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if _, err := slackcache.Write("default", "users", json.RawMessage(`{"users":[{"id":"U123"}]}`)); err != nil {
		t.Fatalf("write users cache: %v", err)
	}
	if _, err := slackcache.Write("default", "channels", json.RawMessage(`{"channels":[{"id":"C123"}]}`)); err != nil {
		t.Fatalf("write channels cache: %v", err)
	}

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), "http://example.invalid", "", []string{"cache", "clear", "users"})
	if err != nil {
		t.Fatalf("cache clear users returned error: %v\nstderr=%s", err, stderr)
	}
	data := envelopeData(t, stdout)
	if data["removed"] != true {
		t.Fatalf("removed = %v, want true", data["removed"])
	}
	if _, ok, _, _ := slackcache.Read("default", "users", 0); ok {
		t.Fatal("users cache still exists")
	}
	if _, ok, _, _ := slackcache.Read("default", "channels", 0); !ok {
		t.Fatal("channels cache was removed")
	}
}

func TestCompletionUsesCachedUsersAndChannelsBeforeSlackRequests(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if _, err := slackcache.Write("default", "users", json.RawMessage(`{"users":[{"id":"UCACHED","name":"cached-user"}]}`)); err != nil {
		t.Fatalf("write users cache: %v", err)
	}
	if _, err := slackcache.Write("default", "channels", json.RawMessage(`{"channels":[{"id":"CCACHED","name":"cached-channel"}]}`)); err != nil {
		t.Fatalf("write channels cache: %v", err)
	}
	server := testutil.NewSlackServer(t, nil)
	defer server.Close()

	cfg := workspaceConfig(config.TokenTypeBot)
	handler := slackCompletionHandler("xox-test", cfg, server.BaseURL())

	userCandidates := captureSlackCompletion(t, handler, "zsh", "user", nil)
	if !slices.Contains(userCandidates, "UCACHED") {
		t.Fatalf("user completion = %#v, want cached user", userCandidates)
	}
	channelCandidates := captureSlackCompletion(t, handler, "fish", "channel", nil)
	if !slices.Contains(channelCandidates, "CCACHED\tcached-channel") {
		t.Fatalf("channel completion = %#v, want cached channel with description", channelCandidates)
	}
	if len(server.Requests("users.list")) != 0 || len(server.Requests("conversations.list")) != 0 {
		t.Fatalf("completion hit Slack despite cache: users=%d channels=%d", len(server.Requests("users.list")), len(server.Requests("conversations.list")))
	}
}

func TestCompletionHandlerCompletesCacheResourceArgs(t *testing.T) {
	got := captureSlackCompletion(t, slackCompletionHandler("", workspaceConfig(config.TokenTypeBot), ""), "zsh", "cache_resource", nil)
	for _, want := range []string{"users", "channels"} {
		if !slices.Contains(got, want) {
			t.Fatalf("cache_resource completion = %#v, want %q", got, want)
		}
	}
}

func envelopeData(t *testing.T, stdout string) map[string]any {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("stdout data has unexpected shape:\n%s", stdout)
	}
	return data
}

func TestCacheCommandIsVisibleOnRoot(t *testing.T) {
	root := NewRootCommand()
	cacheCmd := findDirectChild(root, "cache")
	if cacheCmd == nil {
		t.Fatal("root command missing cache command")
	}
	if cacheCmd.Hidden {
		t.Fatal("cache command should be visible")
	}
	if got := strings.TrimSpace(cacheCmd.Short); got == "" {
		t.Fatal("cache command needs a short description")
	}
}
