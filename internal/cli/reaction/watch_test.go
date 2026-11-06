package reaction_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	clireaction "github.com/matcra587/slack-cli/internal/cli/reaction"
	"github.com/matcra587/slack-cli/internal/cli/runtime/runtimetest"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestReactionWatchWritesOneMatchWithoutMutation(t *testing.T) {
	for _, mode := range []string{"json", "compact"} {
		t.Run(mode, func(t *testing.T) {
			server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
				"reactions.get": func(testutil.SlackRequest) testutil.SlackResponse {
					return testutil.JSONResponse(`{"ok":true,"type":"message","message":{"reactions":[{"name":"+1","count":1,"users":["U123"]}]}}`)
				},
			})
			stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "", []string{
				"react", "watch", "--channel", "C123", "--timestamp", "1746284582.123456", "--emoji", ":+1:", "--user", "U123", "--output=" + mode,
			})
			if err != nil {
				t.Fatalf("watch failed: %v; stderr=%s", err, stderr)
			}
			if strings.Count(strings.TrimSpace(stdout), "\n") != 0 || stderr != "" {
				t.Fatalf("watch must write one stdout record and no diagnostics: stdout=%q stderr=%q", stdout, stderr)
			}
			var result map[string]any
			if err := json.Unmarshal([]byte(stdout), &result); err != nil {
				t.Fatal(err)
			}
			if mode == "json" {
				if result["ok"] != true {
					t.Fatal("watch did not report success")
				}
				result = result["data"].(map[string]any)
			}
			if result["target"].(map[string]any)["ts"] != "1746284582.123456" {
				t.Fatalf("timestamp was not preserved: %#v", result)
			}
			if result["reactions"].([]any)[0].(map[string]any)["name"] != "+1" {
				t.Fatalf("wrong reaction: %#v", result)
			}
			for _, method := range []string{"reactions.add", "reactions.remove"} {
				if len(server.Requests(method)) != 0 {
					t.Fatalf("watch invoked mutation %s", method)
				}
			}
		})
	}
}

func TestReactionWatchReportsDeadlineWithFailureEnvelope(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"reactions.get": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"type":"message","message":{"reactions":[]}}`)
		},
	})
	runtime, stdout, stderr := runtimetest.NewRuntime(t, runtimetest.Options{Config: workspaceConfig(config.TokenTypeBot), SlackBaseURL: server.BaseURL()})
	root := runtimetest.NewRoot(runtime, stdout, stderr)
	root.AddCommand(clireaction.NewCommand(runtime))
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	root.SetContext(ctx)
	_, _, err := runtimetest.Run(t, root, []string{"react", "watch", "--channel", "C123", "--timestamp", "1746284582.123456", "--emoji", "+1", "--no-throttle"}, stdout, stderr)
	if err == nil {
		t.Fatal("watch succeeded without a matching reaction")
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["ok"] != false || result["meta"].(map[string]any)["exit_code"] != float64(7) {
		t.Fatalf("deadline did not use the machine failure contract: %s", stdout)
	}
}

func TestReactionWatchRejectsInvalidFlagsBeforeRequest(t *testing.T) {
	for _, flags := range [][]string{
		{"--emoji", ""},
		{"--emoji", ":+1:,:heart:"},
		{"--emoji", "white check mark"},
		{"--emoji", "+1", "--interval", "0s"},
		{"--emoji", "+1", "--interval", "-1s"},
	} {
		t.Run(strings.Join(flags, " "), func(t *testing.T) {
			server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
				"auth.test": func(testutil.SlackRequest) testutil.SlackResponse {
					t.Error("invalid flags reached Slack authentication")
					return testutil.JSONResponse(`{"ok":true}`)
				},
				"reactions.get": func(testutil.SlackRequest) testutil.SlackResponse {
					t.Error("invalid flags reached Slack reactions")
					return testutil.JSONResponse(`{"ok":false}`)
				},
			})
			args := append([]string{"react", "watch", "--channel", "C123", "--timestamp", "1746284582.123456", "--output=json"}, flags...)
			stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "", args)
			if err == nil || stderr != "" || !strings.Contains(stdout, `"exit_code":4`) {
				t.Fatalf("invalid watch did not return validation failure: err=%v stdout=%s stderr=%s", err, stdout, stderr)
			}
		})
	}
}
