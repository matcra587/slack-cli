package main

import (
	"bufio"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/gechr/clib/complete"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
	"github.com/spf13/cobra"
)

func TestRootUsesClibCompletionCommand(t *testing.T) {
	root := NewRootCommand()

	completion := findDirectChild(root, "completion")
	if completion == nil {
		t.Fatal("root command missing clib completion command")
	}
	if !completion.Hidden {
		t.Fatal("completion command should be hidden like pagerduty-client")
	}
	if !root.CompletionOptions.DisableDefaultCmd {
		t.Fatal("cobra default completion command should be disabled")
	}

	stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", []string{"completion", "fish"})
	if err != nil {
		t.Fatalf("completion fish returned error: %v\nstderr=%s", err, stderr)
	}
	for _, fragment := range []string{
		"slick",
		"--@complete=channel",
		"--@complete=user",
		"--@complete=workspace",
	} {
		if !strings.Contains(stdout, fragment) {
			t.Fatalf("completion script missing %q:\n%s", fragment, stdout)
		}
	}
	for _, hidden := range []string{
		`complete -c slick -n '__slick_needs_command' -a file `,
	} {
		if strings.Contains(stdout, hidden) {
			t.Fatalf("completion script exposes hidden command candidate %q:\n%s", hidden, stdout)
		}
	}
	if !strings.Contains(stdout, `-a messages -d "Search Slack messages"`) {
		t.Fatalf("completion script does not expose public lookup messages command:\n%s", stdout)
	}
}

func TestCompletionGeneratorAnnotatesCommonSlackFlags(t *testing.T) {
	gen := slackCompletionGenerator(NewRootCommand())

	messageSend := completionSub(t, gen.Subs, "message", "send")
	assertCompletionSpec(t, messageSend.Specs, "channel", func(spec complete.Spec) bool {
		return spec.Dynamic == "channel"
	})
	assertCompletionSpec(t, messageSend.Specs, "user", func(spec complete.Spec) bool {
		return spec.Dynamic == "user"
	})
	assertCompletionSpec(t, messageSend.Specs, "file", func(spec complete.Spec) bool {
		return spec.ValueHint == complete.HintFile
	})

	historyList := completionSub(t, gen.Subs, "history", "list")
	assertCompletionSpec(t, historyList.Specs, "channel", func(spec complete.Spec) bool {
		return spec.Dynamic == "channel"
	})
	assertCompletionSpec(t, historyList.Specs, "user", func(spec complete.Spec) bool {
		return spec.Dynamic == "user"
	})

	reactAdd := completionSub(t, gen.Subs, "react", "add")
	assertCompletionSpec(t, reactAdd.Specs, "emoji", func(spec complete.Spec) bool {
		return slices.Contains(spec.Values, "thumbsup")
	})

	authLogin := completionSub(t, gen.Subs, "auth", "login")
	assertCompletionSpec(t, authLogin.Specs, "method", func(spec complete.Spec) bool {
		return slices.Contains(spec.Values, "oauth") && slices.Contains(spec.Values, "token")
	})
	assertCompletionSpec(t, authLogin.Specs, "token-file", func(spec complete.Spec) bool {
		return spec.ValueHint == complete.HintFile
	})

	manifestTemplate := completionSub(t, gen.Subs, "manifest", "template")
	assertCompletionSpec(t, manifestTemplate.Specs, "preset", func(spec complete.Spec) bool {
		return slices.Contains(spec.Values, "messaging") && slices.Contains(spec.Values, "readonly")
	})
	assertCompletionSpec(t, manifestTemplate.Specs, "type", func(spec complete.Spec) bool {
		return slices.Contains(spec.Values, "user") && slices.Contains(spec.Values, "bot") && slices.Contains(spec.Values, "both")
	})
	assertCompletionSpec(t, manifestTemplate.Specs, "format", func(spec complete.Spec) bool {
		return slices.Contains(spec.Values, "json") && slices.Contains(spec.Values, "yaml")
	})

	lookupChannel := completionSub(t, gen.Subs, "lookup", "channel")
	assertCompletionSpec(t, lookupChannel.Specs, "types", func(spec complete.Spec) bool {
		return spec.CommaList && slices.Contains(spec.Values, "public_channel") && slices.Contains(spec.Values, "dm")
	})

	configSet := completionSub(t, gen.Subs, "config", "set")
	if got := configSet.DynamicArgs; !slices.Equal(got, []string{"config_key", "config_value"}) {
		t.Fatalf("config set dynamic args = %#v, want config_key/config_value", got)
	}
}

func TestCompletionDoesNotUseCobraNativeCompletionHooks(t *testing.T) {
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		t.Helper()
		if cmd.ValidArgsFunction != nil {
			t.Fatalf("%s uses Cobra ValidArgsFunction; completions must be clib metadata/handler only", cmd.CommandPath())
		}
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(NewRootCommand())
}

func TestCompletionHandlerCompletesSlackResourcesAndLocalConfig(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"conversations.list": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"channels":[{"id":"C123","name":"alerts"},{"id":"D123","is_im":true,"user":"U123"}]}`)
		},
		"users.list": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"members":[{"id":"U123","name":"matt"},{"id":"U456","name":"deploy-bot"}]}`)
		},
	})
	defer server.Close()

	cfg := &config.Config{
		SchemaVersion:    config.SchemaVersion,
		DefaultWorkspace: "default",
		Workspaces: map[string]config.WorkspaceProfile{
			"default": {Name: "default"},
			"ci":      {Name: "ci"},
		},
	}
	handler := slackCompletionHandler("xox-test", cfg, server.BaseURL())

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
			for _, want := range tt.want {
				if !slices.Contains(got, want) {
					t.Fatalf("completion %s/%s = %#v, want %q", shell, tt.kind, got, want)
				}
			}
		})
	}
}

func TestClibConfigValueCompletionsCompletesSecondArg(t *testing.T) {
	values := slackConfigValueCompletions("workspaces.default.attribution.enabled", nil)
	if !slices.Contains(values, "true") || !slices.Contains(values, "false") {
		t.Fatalf("values = %#v, want bool suggestions", values)
	}
}

func TestRootCompletionPassesPositionalArgsToClibHandler(t *testing.T) {
	got := captureRootStdout(t, []string{
		"--@complete=config_value",
		"--",
		"workspaces.default.attribution.enabled",
	})
	if !slices.Contains(got, "true") || !slices.Contains(got, "false") {
		t.Fatalf("root completion output = %#v, want boolean value completions", got)
	}
}

func completionSub(t *testing.T, subs []complete.SubSpec, path ...string) complete.SubSpec {
	t.Helper()
	if len(path) == 0 {
		t.Fatal("completionSub requires a path")
	}
	for _, sub := range subs {
		if sub.Name != path[0] {
			continue
		}
		if len(path) == 1 {
			return sub
		}
		return completionSub(t, sub.Subs, path[1:]...)
	}
	t.Fatalf("completion subcommand %q not found in %#v", strings.Join(path, " "), subs)
	return complete.SubSpec{}
}

func assertCompletionSpec(t *testing.T, specs []complete.Spec, name string, ok func(complete.Spec) bool) {
	t.Helper()
	for _, spec := range specs {
		if spec.LongFlag == name {
			if !ok(spec) {
				t.Fatalf("completion spec for --%s = %#v", name, spec)
			}
			return
		}
	}
	t.Fatalf("completion spec for --%s not found in %#v", name, specs)
}

func captureRootStdout(t *testing.T, args []string) []string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}

	orig := os.Stdout
	os.Stdout = w
	stdout := &strings.Builder{}
	stderr := &strings.Builder{}
	cmd := NewRootCommand(WithIO(strings.NewReader(""), stdout, stderr), WithTTY(false))
	cmd.SetArgs(args)
	err = cmd.Execute()
	os.Stdout = orig
	_ = w.Close()
	if err != nil {
		t.Fatalf("root completion returned error: %v\nstderr=%s", err, stderr.String())
	}

	var lines []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan root completions: %v", err)
	}
	return lines
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
