package completion_test

import (
	"bufio"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/gechr/clib/complete"
	"github.com/matcra587/slack-cli/internal/agent"
	slackcache "github.com/matcra587/slack-cli/internal/cache"
	clicompletion "github.com/matcra587/slack-cli/internal/cli/completion"
	cliconfig "github.com/matcra587/slack-cli/internal/cli/config"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestMain(m *testing.M) {
	for _, key := range agent.KnownEnvVars() {
		_ = os.Unsetenv(key)
	}
	os.Exit(m.Run())
}

func TestCompletionHandlerCompletesSlackResourcesAndLocalConfig(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"conversations.list": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"channels":[{"id":"C123","name":"alerts"},{"id":"D123","is_im":true,"user":"U123"}]}`)
		},
		"users.list": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"members":[{"id":"U123","name":"matt","deleted":false},{"id":"U456","name":"deploy-bot","deleted":false},{"id":"UDELETED","name":"gone","deleted":true}]}`)
		},
	})

	cfg := &config.Config{
		SchemaVersion:    config.SchemaVersion,
		DefaultWorkspace: "default",
		Workspaces: map[string]config.WorkspaceProfile{
			"default": {Name: "default"},
			"ci":      {Name: "ci"},
		},
	}
	handler := clicompletion.Handler("xox-test", cfg, &cliruntime.RootRuntime{SlackBaseURL: server.BaseURL()})

	tests := []struct {
		name string
		kind string
		args []string
		want []string
	}{
		{name: "channel IDs", kind: "channel", want: []string{"C123", "D123"}},
		{name: "fish channel descriptions", kind: "channel", want: []string{"C123\talerts", "D123\tU123"}},
		{name: "user IDs", kind: "user", want: []string{"U123", "U456"}},
		{name: "workspace profiles", kind: "workspace", want: []string{"ci", "default"}},
		{name: "config keys", kind: "config_key", want: []string{"default_workspace", "workspaces.default.default_channel", "workspaces.ci.attribution.enabled"}},
		{name: "fish config key descriptions", kind: "config_key", want: []string{"default_workspace\tDefault workspace profile name", "workspaces.default.default_channel\tFallback message channel ID or alias"}},
		{name: "config workspace values", kind: "config_value", args: []string{"default_workspace"}, want: []string{"ci", "default"}},
		{name: "config channel values", kind: "config_value", args: []string{"workspaces.default.default_channel"}, want: []string{"C123", "D123"}},
		{name: "config bool values", kind: "config_value", args: []string{"workspaces.default.attribution.enabled"}, want: []string{"true", "false"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shell := "zsh"
			if strings.HasPrefix(tt.name, "fish") {
				shell = "fish"
			}
			got := captureSlackCompletion(t, handler, shell, tt.kind, tt.args)
			if tt.kind == "user" && slices.Contains(got, "UDELETED") {
				t.Fatalf("completion %s/%s = %#v, did not want deleted users", shell, tt.kind, got)
			}
			for _, want := range tt.want {
				if !slices.Contains(got, want) {
					t.Fatalf("completion %s/%s = %#v, want %q", shell, tt.kind, got, want)
				}
			}
		})
	}
}

func TestClibConfigValueCompletionsCompletesSecondArg(t *testing.T) {
	values := cliconfig.ValueCompletions("workspaces.default.attribution.enabled", nil)
	if !slices.Contains(values, "true") || !slices.Contains(values, "false") {
		t.Fatalf("values = %#v, want bool suggestions", values)
	}
}

func TestCompletionUsesCachedUsersAndChannelsBeforeSlackRequests(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if _, err := slackcache.Write("default", "users", json.RawMessage(`{"users":[{"id":"UCACHED","name":"cached-user","deleted":false},{"id":"UDELETED","name":"gone","deleted":true}]}`)); err != nil {
		t.Fatalf("write users cache: %v", err)
	}
	if _, err := slackcache.Write("default", "channels", json.RawMessage(`{"channels":[{"id":"CCACHED","name":"cached-channel"}]}`)); err != nil {
		t.Fatalf("write channels cache: %v", err)
	}
	server := testutil.NewSlackServer(t, nil)

	cfg := workspaceConfig(config.TokenTypeBot)
	handler := clicompletion.Handler("xox-test", cfg, &cliruntime.RootRuntime{SlackBaseURL: server.BaseURL()})

	userCandidates := captureSlackCompletion(t, handler, "zsh", "user", nil)
	if !slices.Contains(userCandidates, "UCACHED") {
		t.Fatalf("user completion = %#v, want cached user", userCandidates)
	}
	if slices.Contains(userCandidates, "UDELETED") {
		t.Fatalf("user completion = %#v, did not want deleted cached user", userCandidates)
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
	got := captureSlackCompletion(t, clicompletion.Handler("", workspaceConfig(config.TokenTypeBot), &cliruntime.RootRuntime{}), "zsh", "cache_resource", nil)
	for _, want := range []string{"users", "channels"} {
		if !slices.Contains(got, want) {
			t.Fatalf("cache_resource completion = %#v, want %q", got, want)
		}
	}
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

func captureSlackCompletion(t *testing.T, handler complete.Handler, shell, kind string, args []string) []string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}

	orig := os.Stdout
	os.Stdout = w
	handler(shell, kind, args)
	os.Stdout = orig
	_ = w.Close()

	var lines []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan completions: %v", err)
	}
	return lines
}
