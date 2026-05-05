package main

import (
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestPlainOutputForHistorySearchListsAndReactions(t *testing.T) {
	longText := strings.Repeat("x", 360)
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"conversations.history": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"messages":[{"type":"message","user":"U1","text":"hello","ts":"1746284582.123456"}]}`)
		},
		"search.messages": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"messages":{"matches":[{"channel":{"id":"C123","name":"alerts"},"user":"U1","text":"` + longText + `","ts":"1746284582.123456"}],"pagination":{"page":1,"page_count":1}}}`)
		},
		"conversations.list": func(req testutil.SlackRequest) testutil.SlackResponse {
			if req.Form.Get("types") == "im" {
				return testutil.JSONResponse(`{"ok":true,"channels":[{"id":"D123","is_im":true,"user":"U123"}]}`)
			}
			return testutil.JSONResponse(`{"ok":true,"channels":[{"id":"C123","name":"alerts","topic":{"value":"Ops alerts"}}]}`)
		},
		"users.list": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"members":[{"id":"U123","name":"matt","profile":{"status_text":"Deploying"}}]}`)
		},
		"reactions.get": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"type":"message","message":{"reactions":[{"name":"thumbsup","count":1,"users":["U1"]}]}}`)
		},
	})
	defer server.Close()

	commands := []struct {
		args    []string
		headers []string
		row     string
	}{
		{args: []string{"--plain", "history", "list", "--channel", "C123"}, headers: []string{"TS", "USER", "TEXT"}, row: "hello"},
		{args: []string{"--plain", "lookup", "messages", "--query", "xxx"}, headers: []string{"TS", "CHANNEL", "USER", "TEXT"}, row: "alerts"},
		{args: []string{"--plain", "lookup", "channel"}, headers: []string{"CHANNEL", "NAME", "TYPE", "MEMBERS", "TOPIC"}, row: "Ops alerts"},
		{args: []string{"--plain", "lookup", "channel", "--types", "im"}, headers: []string{"CHANNEL", "TYPE", "USER"}, row: "D123"},
		{args: []string{"--plain", "lookup", "user"}, headers: []string{"USER", "NAME", "PRESENCE", "TZ", "STATUS"}, row: "Deploying"},
		{args: []string{"--plain", "react", "list", "--channel", "C123", "--timestamp", "1746284582.123456"}, headers: []string{"EMOJI", "COUNT", "USERS"}, row: "thumbsup"},
	}
	for _, tt := range commands {
		stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "", tt.args)
		if err != nil {
			t.Fatalf("%v returned error: %v\nstderr=%s", tt.args, err, stderr)
		}
		if strings.Contains(stdout, "{") {
			t.Fatalf("%v stdout looks like JSON: %s", tt.args, stdout)
		}
		if strings.Contains(stdout, "INF ") {
			t.Fatalf("%v stdout should be table output, got clog event rows: %s", tt.args, stdout)
		}
		for _, header := range tt.headers {
			if !strings.Contains(stdout, header) {
				t.Fatalf("%v stdout missing header %q:\n%s", tt.args, header, stdout)
			}
		}
		if !strings.Contains(stdout, tt.row) {
			t.Fatalf("%v stdout missing row fragment %q:\n%s", tt.args, tt.row, stdout)
		}
	}
}

func TestSearchPlainOutputTruncatesUnlessFull(t *testing.T) {
	longText := strings.Repeat("x", 360)
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"search.messages": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"messages":{"matches":[{"channel":{"id":"C123","name":"alerts"},"user":"U1","text":"` + longText + `","ts":"1746284582.123456"}],"pagination":{"page":1,"page_count":1}}}`)
		},
	})
	defer server.Close()

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"--plain", "lookup", "messages", "--query", "xxx"},
	)
	if err != nil {
		t.Fatalf("search returned error: %v\nstderr=%s", err, stderr)
	}
	if strings.Contains(stdout, longText) {
		t.Fatalf("plain search was not truncated: %s", stdout)
	}

	stdout, stderr, err = executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"--plain", "lookup", "messages", "--query", "xxx", "--full"},
	)
	if err != nil {
		t.Fatalf("search --full returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, longText) {
		t.Fatalf("plain search --full missing full text: %s", stdout)
	}
}
