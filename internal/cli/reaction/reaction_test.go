package reaction_test

import (
	"os"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/agent"
	clireaction "github.com/matcra587/slack-cli/internal/cli/reaction"
	"github.com/matcra587/slack-cli/internal/cli/runtime/runtimetest"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestMain(m *testing.M) {
	for _, key := range agent.KnownEnvVars() {
		_ = os.Unsetenv(key)
	}
	os.Exit(m.Run())
}

func TestReactionCommandAddRemoveAndList(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"reactions.add": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("name"); got != "thumbsup" {
				t.Fatalf("add name = %q, want thumbsup", got)
			}
			return testutil.JSONResponse(`{"ok":true}`)
		},
		"reactions.remove": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("name"); got != "thumbsup" {
				t.Fatalf("remove name = %q, want thumbsup", got)
			}
			return testutil.JSONResponse(`{"ok":true}`)
		},
		"reactions.get": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"type":"message","channel":"C123","message":{"reactions":[{"name":"thumbsup","count":1,"users":["U1"]}]}}`)
		},
	})

	for _, args := range [][]string{
		{"react", "add", "--channel", "C123", "--timestamp", "1746284582.123456", "--emoji", ":thumbsup:"},
		{"react", "remove", "--channel", "C123", "--timestamp", "1746284582.123456", "--emoji", "thumbsup"},
		{"react", "list", "--channel", "C123", "--timestamp", "1746284582.123456"},
	} {
		stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "", args)
		if err != nil {
			t.Fatalf("Execute %v returned error: %v\nstderr=%s", args, err, stderr)
		}
		expect := `"mutations"`
		if args[0] == "react" && args[1] == "list" {
			expect = `"reactions"`
		}
		if !strings.Contains(stdout, expect) {
			t.Fatalf("stdout for %v = %s, want %s", args, stdout, expect)
		}
	}
}

func TestReactionCommandDryRunSkipsMutation(t *testing.T) {
	for _, tt := range []struct {
		name   string
		action string
		method string
		want   string
	}{
		{name: "add", action: "add", method: "reactions.add", want: `"dry_run":true`},
		{name: "remove", action: "remove", method: "reactions.remove", want: `"removed":true`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := testutil.NewSlackServer(t, nil)

			stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(),
				"",
				[]string{"react", tt.action, "--channel", "C123", "--timestamp", "1746284582.123456", "--emoji", "thumbsup", "--dry-run"},
			)
			if err != nil {
				t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
			}
			if got := len(server.Requests(tt.method)); got != 0 {
				t.Fatalf("%s requests = %d, want 0", tt.method, got)
			}
			if !strings.Contains(stdout, `"dry_run":true`) || !strings.Contains(stdout, tt.want) {
				t.Fatalf("stdout = %s, want dry_run true and %s", stdout, tt.want)
			}
		})
	}
}

func TestReactionAddAppliesMultipleEmojisInOrder(t *testing.T) {
	var got []string
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"reactions.add": func(req testutil.SlackRequest) testutil.SlackResponse {
			got = append(got, req.Form.Get("name"))
			return testutil.JSONResponse(`{"ok":true}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "",
		[]string{"react", "add", "--channel", "C123", "--timestamp", "1746284582.123456", "--emoji", "thumbsup,white_check_mark,rocket"},
	)
	if err != nil {
		t.Fatalf("execute returned error: %v\nstderr=%s", err, stderr)
	}
	if want := []string{"thumbsup", "white_check_mark", "rocket"}; !equalSlices(got, want) {
		t.Fatalf("reactions.add order = %v, want %v", got, want)
	}
	for _, want := range []string{`"thumbsup"`, `"white_check_mark"`, `"rocket"`} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %s\n%s", want, stdout)
		}
	}
}

func TestReactionAddRepeatedFlagApplyInOrder(t *testing.T) {
	var got []string
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"reactions.add": func(req testutil.SlackRequest) testutil.SlackResponse {
			got = append(got, req.Form.Get("name"))
			return testutil.JSONResponse(`{"ok":true}`)
		},
	})

	_, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "",
		[]string{
			"react", "add", "--channel", "C123", "--timestamp", "1746284582.123456",
			"--emoji", ":alpha:", "--emoji", "beta", "--emoji", "gamma",
		},
	)
	if err != nil {
		t.Fatalf("execute returned error: %v\nstderr=%s", err, stderr)
	}
	if want := []string{"alpha", "beta", "gamma"}; !equalSlices(got, want) {
		t.Fatalf("reactions.add order = %v, want %v", got, want)
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestReactionCommandIsNotRegistered(t *testing.T) {
	stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", []string{"reaction", "add", "--help"})
	if err == nil {
		t.Fatal("Execute returned nil error, want unknown legacy command")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	if !strings.Contains(err.Error(), `unknown command "reaction"`) {
		t.Fatalf("err = %v, want unknown legacy command", err)
	}
}

// --- helpers ---

func executeTestRoot(t *testing.T, cfg *config.Config, baseURL, stdin string, args []string) (string, string, error) {
	t.Helper()
	runtime, stdout, stderr := runtimetest.NewRuntime(t, runtimetest.Options{
		Config:       cfg,
		SlackBaseURL: baseURL,
		Stdin:        strings.NewReader(stdin),
	})
	root := runtimetest.NewRoot(runtime, stdout, stderr)
	root.AddCommand(clireaction.NewCommand(runtime))
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
