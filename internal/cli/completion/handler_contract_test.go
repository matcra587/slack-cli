package completion_test

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	slackcache "github.com/matcra587/slack-cli/internal/cache"
	clicompletion "github.com/matcra587/slack-cli/internal/cli/completion"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestCompletionLocalAndEmptyBranchesWithoutToken(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	server := testutil.NewSlackServer(t, nil)
	for _, tc := range []struct {
		name, kind string
		args, want []string
		cfg        *config.Config
	}{
		{"workspace", "workspace", nil, []string{"default"}, workspaceConfig(config.TokenTypeBot)},
		{"nil workspace", "workspace", nil, nil, nil},
		{"bool value", "config_value", []string{"workspaces.default.attribution.enabled"}, []string{"true", "false"}, workspaceConfig(config.TokenTypeBot)},
		{"missing value key", "config_value", nil, nil, nil},
		{"unknown value key", "config_value", []string{"unknown"}, nil, nil},
		{"cache resources", "cache_resource", nil, []string{"users", "channels"}, nil},
		{"channel", "channel", nil, nil, nil},
		{"user", "user", nil, nil, nil},
		{"channel value", "config_value", []string{"workspaces.default.default_channel"}, nil, nil},
		{"unknown kind", "unknown", nil, nil, nil},
		{"invalid default", "channel", nil, nil, &config.Config{SchemaVersion: "1", DefaultWorkspace: "missing", Workspaces: map[string]config.WorkspaceProfile{"default": {Name: "default"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := clicompletion.Handler("", tc.cfg, &cliruntime.RootRuntime{SlackBaseURL: server.BaseURL()})
			got := captureSlackCompletion(t, handler, "zsh", tc.kind, tc.args)
			if !slices.Equal(got, tc.want) {
				t.Fatal("local/empty completion branch changed")
			}
		})
	}
	for _, method := range []string{"users.list", "conversations.list", "auth.test"} {
		if len(server.Requests(method)) != 0 {
			t.Fatal("local completion reached Slack")
		}
	}
	handler := clicompletion.Handler("", nil, &cliruntime.RootRuntime{SlackBaseURL: server.BaseURL()})
	if got := captureSlackCompletion(t, handler, "fish", "config_key", nil); !slices.Contains(got, "default_workspace\tDefault workspace profile name") {
		t.Fatal("configuration key descriptions lost")
	}
	if got := captureSlackCompletion(t, handler, "zsh", "guide_workflow", nil); !slices.Contains(got, "core-contract") {
		t.Fatal("guide workflow suggestions lost")
	}
}

func TestCompletionRemoteFailuresProduceNoCandidatesOrReplay(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, tc := range []struct {
		kind, method string
		args         []string
	}{
		{"channel", "conversations.list", nil}, {"user", "users.list", nil}, {"config_value", "conversations.list", []string{"workspaces.default.default_channel"}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{tc.method: func(req testutil.SlackRequest) testutil.SlackResponse {
				if req.Method != "POST" || req.Form.Get("token") != "xox-test" {
					t.Error("completion changed method/authentication")
				}
				return testutil.JSONResponse(`{"ok":false,"error":"missing_scope"}`)
			}})
			handler := clicompletion.Handler("xox-test", workspaceConfig(config.TokenTypeBot), &cliruntime.RootRuntime{SlackBaseURL: server.BaseURL()})
			if got := captureSlackCompletion(t, handler, "zsh", tc.kind, tc.args); len(got) != 0 || len(server.Requests(tc.method)) != 1 {
				t.Fatal("failed completion emitted candidates or replayed request")
			}
		})
	}
}

func TestCompletionCacheBranchCharacterization(t *testing.T) {
	for _, tc := range []struct {
		name, resource, payload, kind string
		args, want                    []string
		remote                        int
	}{
		{"empty channels", "channels", `{"channels":[]}`, "channel", nil, nil, 0},
		{"cached channel value", "channels", `{"channels":[{"id":"CCACHED","name":"cached"}]}`, "config_value", []string{"workspaces.default.default_channel"}, []string{"CCACHED"}, 0},
		{"empty users", "users", `{"users":[]}`, "user", nil, nil, 0},
		{"deleted users", "users", `{"users":[{"id":"UGONE","name":"gone","deleted":true}]}`, "user", nil, nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			entry, err := slackcache.Write("default", tc.resource, json.RawMessage(tc.payload))
			if err != nil {
				t.Fatal(err)
			}
			before, err := json.Marshal(entry)
			if err != nil {
				t.Fatal(err)
			}
			server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{"users.list": func(testutil.SlackRequest) testutil.SlackResponse {
				return testutil.JSONResponse(`{"ok":true,"members":[{"id":"UREMOTE","name":"remote","deleted":false}]}`)
			}})
			handler := clicompletion.Handler("xox-test", workspaceConfig(config.TokenTypeBot), &cliruntime.RootRuntime{SlackBaseURL: server.BaseURL()})
			got := captureSlackCompletion(t, handler, "zsh", tc.kind, tc.args)
			if !slices.Equal(got, tc.want) || len(server.Requests("users.list")) != tc.remote || len(server.Requests("conversations.list")) != 0 {
				t.Fatal("cache fallback/output behavior changed")
			}
			after, ok, _, err := slackcache.Read("default", tc.resource, 0)
			if err != nil || !ok {
				t.Fatal("completion lost cache entry")
			}
			encoded, err := json.Marshal(after)
			if err != nil || !reflect.DeepEqual(before, encoded) {
				t.Fatal("completion changed cache state")
			}
		})
	}
}
