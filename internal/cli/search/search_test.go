package search_test

import (
	"encoding/json"
	"strings"
	"testing"

	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	"github.com/matcra587/slack-cli/internal/cli/runtime/runtimetest"
	clisearch "github.com/matcra587/slack-cli/internal/cli/search"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
	"github.com/spf13/cobra"
)

func TestSearchMessagesCommandWritesPaginatedEnvelope(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"search.messages": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"messages":{"matches":[{"channel":{"id":"C123","name":"alerts"},"user":"U1","text":"deploy failed in prod","ts":"1746284582.123456","permalink":"https://example.slack.com/archives/C123/p1746284582123456","snippet":"deploy failed"}],"pagination":{"page":1,"page_count":2}}}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(),
		"",
		[]string{"lookup", "messages", "--query", "deploy failed", "--max-items", "1", "--cursor", "1"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if envelope["meta"].(map[string]any)["command"] != "lookup.messages" {
		t.Fatalf("meta.command = %q, want lookup.messages", envelope["meta"].(map[string]any)["command"])
	}
	if !strings.Contains(stdout, "deploy failed in prod") {
		t.Fatalf("stdout = %s, want full message text", stdout)
	}
}

func TestSearchUsesSDKDefaultsAndExplicitPageArguments(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(map[bool]string{false: "defaults", true: "explicit"}[explicit], func(t *testing.T) {
			server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
				"search.messages": func(req testutil.SlackRequest) testutil.SlackResponse {
					if req.Form.Get("query") != "release check" {
						t.Error("search query changed")
					}
					if explicit {
						if req.Form.Get("count") != "5" || req.Form.Get("page") != "2" {
							t.Error("explicit count or page was ignored")
						}
					} else {
						for _, key := range []string{"count", "page", "sort", "sort_dir"} {
							if _, exists := req.Form[key]; exists {
								t.Errorf("default search unexpectedly sent %s=%q", key, req.Form.Get(key))
							}
						}
					}
					page := "1"
					if explicit {
						page = "2"
					}
					return testutil.JSONResponse(`{"ok":true,"messages":{"matches":[],"pagination":{"page":` + page + `,"page_count":3}}}`)
				},
			})
			args := []string{"lookup", "messages", "--query", "release check"}
			if explicit {
				args = append(args, "--max-items", "5", "--cursor", "2")
			}
			stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(), "", args)
			next := `"next_cursor":"2"`
			if explicit {
				next = `"next_cursor":"3"`
			}
			if err != nil || stderr != "" || !strings.Contains(stdout, next) {
				t.Fatalf("search failed or lost next page: err=%v stdout=%s stderr=%s", err, stdout, stderr)
			}
		})
	}
}

func TestSearchMessagesCommandReturnsEmptyMatchesForNoResults(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"search.messages": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"messages":{"matches":[],"pagination":{"page":1,"page_count":1}}}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(),
		"",
		[]string{"lookup", "messages", "--query", "no such message", "--max-items", "10"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	var envelope struct {
		Data struct {
			Matches []clioutput.SearchMessage `json:"matches"`
		} `json:"data"`
		Errors []clioutput.CLIError `json:"errors"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if len(envelope.Data.Matches) != 0 {
		t.Fatalf("matches = %#v, want empty", envelope.Data.Matches)
	}
	if len(envelope.Errors) != 0 {
		t.Fatalf("errors = %#v, want empty", envelope.Errors)
	}
}

func TestSearchMessagesCommandRejectsMissingQuery(t *testing.T) {
	server := testutil.NewSlackServer(t, nil)
	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(),
		"",
		[]string{"lookup", "messages"},
	)
	if err == nil {
		t.Fatal("Execute returned nil error, want validation error")
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, `"type":"validation_error"`) {
		t.Fatalf("stdout = %s, want validation_error", stdout)
	}
}

func TestSearchMessagesRequiresUserToken(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"search.messages": func(testutil.SlackRequest) testutil.SlackResponse {
			t.Fatal("search.messages should not be called for bot-token profiles")
			return testutil.JSONResponse(`{"ok":true}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
		"",
		[]string{"lookup", "messages", "--query", "deploy failed"},
	)
	if err == nil {
		t.Fatal("Execute returned nil error, want auth failure")
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	if !strings.Contains(stdout, `"type":"auth_failure"`) || !strings.Contains(stdout, "user token") || !strings.Contains(stdout, "search:read") {
		t.Fatalf("stdout = %s, want user-token search scope auth failure", stdout)
	}
}

func executeTestRoot(t *testing.T, cfg *config.Config, baseURL, stdin string, args []string) (string, string, error) {
	t.Helper()
	runtime, stdout, stderr := runtimetest.NewRuntime(t, runtimetest.Options{
		Config:       cfg,
		SlackBaseURL: baseURL,
		Stdin:        strings.NewReader(stdin),
	})
	root := runtimetest.NewRoot(runtime, stdout, stderr)
	lookupCmd := &cobra.Command{Use: "lookup", Short: "Look up Slack channels and users"}
	lookupCmd.AddCommand(clisearch.NewLookupMessagesCommand(runtime))
	root.AddCommand(lookupCmd)
	// These fixtures assert requests/results; dedicated SDK and binary tests own pacing.
	args = append([]string{"--no-throttle"}, args...)
	return runtimetest.Run(t, root, args, stdout, stderr)
}

func workspaceConfig(tokenType config.TokenType) *config.Config {
	return &config.Config{
		SchemaVersion:    config.SchemaVersion,
		DefaultWorkspace: "default",
		Workspaces: map[string]config.WorkspaceProfile{
			"default": {
				Name:      "default",
				TeamID:    "T123",
				TokenType: tokenType,
				TokenRef:  "env:SLACK_TEST_TOKEN",
			},
		},
	}
}
