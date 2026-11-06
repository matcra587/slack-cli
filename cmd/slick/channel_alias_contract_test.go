package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestChannelAliasesUseSelectedWorkspaceBeforeSlackRequests(t *testing.T) {
	t.Setenv("SLICK_CONFIG", filepath.Join(t.TempDir(), "unused.toml"))
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	file := filepath.Join(t.TempDir(), "fixture.txt")
	if err := os.WriteFile(file, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, method, parameter string
		args                    []string
	}{
		{"channel history", "conversations.history", "channel", []string{"history", "list"}},
		{"thread history", "conversations.replies", "channel", []string{"history", "list", "--thread", "1746284582.123456"}},
		{"reply", "chat.postMessage", "channel", []string{"reply", "--parent", "1746284582.123456", "--message", "Review complete"}},
		{"reaction add", "reactions.add", "channel", []string{"react", "add", "--timestamp", "1746284582.123456", "--emoji", "rocket"}},
		{"reaction remove", "reactions.remove", "channel", []string{"react", "remove", "--timestamp", "1746284582.123456", "--emoji", "rocket"}},
		{"reaction list", "reactions.get", "channel", []string{"react", "list", "--timestamp", "1746284582.123456"}},
		{"reaction watch", "reactions.get", "channel", []string{"react", "watch", "--timestamp", "1746284582.123456", "--emoji", "rocket"}},
		{"file upload", "files.completeUploadExternal", "channel_id", []string{"file", "upload", "--file", file}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := workspaceConfig(config.TokenTypeUser)
			profile := cfg.Workspaces["default"]
			profile.Aliases = map[string]string{"ops": "CDEFAULT"}
			cfg.Workspaces["default"] = profile
			profile.Name = "selected"
			profile.Aliases = map[string]string{"ops": "CSELECTED"}
			cfg.Workspaces["selected"] = profile
			before, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			var server *testutil.SlackServer
			server = testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
				"auth.test": func(testutil.SlackRequest) testutil.SlackResponse {
					return testutil.JSONResponse(`{"ok":true,"user_id":"U1"}`)
				},
				"conversations.history": func(testutil.SlackRequest) testutil.SlackResponse {
					return testutil.JSONResponse(`{"ok":true,"messages":[]}`)
				},
				"conversations.replies": func(testutil.SlackRequest) testutil.SlackResponse {
					return testutil.JSONResponse(`{"ok":true,"messages":[]}`)
				},
				"chat.postMessage": func(testutil.SlackRequest) testutil.SlackResponse {
					return testutil.JSONResponse(`{"ok":true,"channel":"CSELECTED","ts":"1746284590.123456","message":{"text":"Review complete","thread_ts":"1746284582.123456"}}`)
				},
				"reactions.add":    func(testutil.SlackRequest) testutil.SlackResponse { return testutil.JSONResponse(`{"ok":true}`) },
				"reactions.remove": func(testutil.SlackRequest) testutil.SlackResponse { return testutil.JSONResponse(`{"ok":true}`) },
				"reactions.get": func(testutil.SlackRequest) testutil.SlackResponse {
					return testutil.JSONResponse(`{"ok":true,"type":"message","message":{"reactions":[{"name":"rocket","count":1,"users":["U1"]}]}}`)
				},
				"conversations.info": func(testutil.SlackRequest) testutil.SlackResponse {
					return testutil.JSONResponse(`{"ok":true,"channel":{"id":"CSELECTED","name":"ops"}}`)
				},
				"chat.getPermalink": func(testutil.SlackRequest) testutil.SlackResponse {
					return testutil.JSONResponse(`{"ok":true,"permalink":"https://example.slack.com/archives/CSELECTED/p1746284590123456"}`)
				},
				"files.getUploadURLExternal": func(testutil.SlackRequest) testutil.SlackResponse {
					return testutil.JSONResponse(`{"ok":true,"upload_url":"` + server.BaseURL() + `/upload","file_id":"F123"}`)
				},
				"/upload": func(testutil.SlackRequest) testutil.SlackResponse {
					return testutil.SlackResponse{Status: 200, Body: "ok"}
				},
				"files.completeUploadExternal": func(testutil.SlackRequest) testutil.SlackResponse {
					return testutil.JSONResponse(`{"ok":true,"files":[{"id":"F123","title":"Fixture"}]}`)
				},
				"files.info": func(testutil.SlackRequest) testutil.SlackResponse {
					return testutil.JSONResponse(`{"ok":true,"file":{"id":"F123","name":"fixture.txt"}}`)
				},
			})
			args := append(append([]string(nil), tc.args...), "--channel", " ops ", "--workspace", "selected", "--output=json", "--no-throttle")
			stdout, stderr, err := executeTestRoot(t, cfg, server.BaseURL(), "", args)
			if err != nil || stderr != "" {
				t.Fatalf("aliased command failed: err=%v stdout=%s stderr=%s", err, stdout, stderr)
			}
			requests := server.Requests(tc.method)
			if len(requests) != 1 || requests[0].Form.Get(tc.parameter) != "CSELECTED" {
				t.Fatalf("%s did not receive the selected profile's channel ID", tc.method)
			}
			if strings.Contains(stdout, `"channel":" ops "`) || strings.Contains(stdout, "CDEFAULT") {
				t.Fatal("result leaked an unresolved alias or the unselected workspace target")
			}
			var frame struct {
				Meta struct{ Workspace string } `json:"meta"`
			}
			if err := json.Unmarshal([]byte(stdout), &frame); err != nil || frame.Meta.Workspace != "selected" {
				t.Fatalf("receipt lost the selected workspace: %s", stdout)
			}
			after, err := json.Marshal(cfg)
			if err != nil || string(before) != string(after) {
				t.Fatal("alias selection changed workspace settings")
			}
		})
	}
}

func TestChannelAliasDryRunsPreviewIDsWithoutSlack(t *testing.T) {
	t.Setenv("SLICK_CONFIG", filepath.Join(t.TempDir(), "unused.toml"))
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	file := filepath.Join(t.TempDir(), "fixture.txt")
	if err := os.WriteFile(file, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"reply", "--parent", "1746284582.123456", "--message", "Review complete"},
		{"react", "add", "--timestamp", "1746284582.123456", "--emoji", "rocket"},
		{"react", "remove", "--timestamp", "1746284582.123456", "--emoji", "rocket"},
		{"file", "upload", "--file", file},
	} {
		t.Run(strings.Join(args[:1], " "), func(t *testing.T) {
			cfg := workspaceConfig(config.TokenTypeUser)
			profile := cfg.Workspaces["default"]
			profile.Aliases = map[string]string{"ops": "C123"}
			cfg.Workspaces["default"] = profile
			server := testutil.NewSlackServer(t, nil)
			command := append(append([]string(nil), args...), "--channel", "ops", "--dry-run", "--output=json", "--no-throttle")
			stdout, stderr, err := executeTestRoot(t, cfg, server.BaseURL(), "", command)
			if err != nil || stderr != "" || !strings.Contains(stdout, `"channel":"C123"`) {
				t.Fatalf("dry run failed to preview the resolved ID: err=%v stdout=%s stderr=%s", err, stdout, stderr)
			}
			for _, method := range []string{"auth.test", "chat.postMessage", "reactions.add", "reactions.remove", "files.getUploadURLExternal", "files.completeUploadExternal"} {
				if len(server.Requests(method)) != 0 {
					t.Errorf("dry run called %s", method)
				}
			}
		})
	}
}
