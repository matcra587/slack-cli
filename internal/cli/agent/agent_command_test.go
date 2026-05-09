package agent_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gechr/clib/help"
	"github.com/gechr/clib/theme"
	agentpkg "github.com/matcra587/slack-cli/internal/agent"
	"github.com/matcra587/slack-cli/internal/agenthelp"
	cliagent "github.com/matcra587/slack-cli/internal/cli/agent"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/spf13/cobra"
)

func TestMain(m *testing.M) {
	for _, key := range agentpkg.KnownEnvVars() {
		_ = os.Unsetenv(key)
	}
	os.Exit(m.Run())
}

func TestAgentSchemaCompactReturnsRawJSONWithoutEnvelope(t *testing.T) {
	stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", []string{"agent", "schema", "--compact"})
	if err != nil {
		t.Fatalf("agent schema returned error: %v\nstderr=%s", err, stderr)
	}
	if strings.Contains(stdout, `"meta"`) {
		t.Fatalf("stdout = %s, want direct agent schema without JSON envelope", stdout)
	}

	var schema agenthelp.CompactSchema
	if err := json.Unmarshal([]byte(stdout), &schema); err != nil {
		t.Fatalf("unmarshal schema: %v\nstdout=%s", err, stdout)
	}
	if schema.Version == "" {
		t.Fatal("schema version is empty")
	}
}

func TestAgentSchemaOutputsWorkflowsAlphabetically(t *testing.T) {
	stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", []string{"agent", "schema"})
	if err != nil {
		t.Fatalf("agent schema returned error: %v\nstderr=%s", err, stderr)
	}
	var schema agenthelp.Schema
	if err := json.Unmarshal([]byte(stdout), &schema); err != nil {
		t.Fatalf("unmarshal schema: %v\nstdout=%s", err, stdout)
	}
	var previous string
	for _, workflow := range schema.Workflows {
		if previous != "" && workflow.Name < previous {
			t.Fatalf("workflows are not sorted: %q appeared after %q", workflow.Name, previous)
		}
		previous = workflow.Name
	}
}

func TestAgentSchemaIncludesRootSchemaContract(t *testing.T) {
	stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", []string{"agent", "schema"})
	if err != nil {
		t.Fatalf("agent schema returned error: %v\nstderr=%s", err, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("unmarshal schema: %v\nstdout=%s", err, stdout)
	}
	for _, key := range []string{"input_shapes", "output_schemas", "env", "exit_codes", "examples"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("schema missing %s: %s", key, stdout)
		}
	}
	envRaw, ok := payload["env"].([]any)
	if !ok {
		t.Fatalf("env = %#v, want array", payload["env"])
	}
	env := map[string]bool{}
	for _, value := range envRaw {
		env[value.(string)] = true
	}
	for _, key := range []string{"SLACK_CLI_AGENT", "WINDSURF_AGENT", "TF_BUILD", "OPENAI_CODEX"} {
		if !env[key] {
			t.Fatalf("schema env missing %s in %#v", key, envRaw)
		}
	}
	examples, ok := payload["examples"].(map[string]any)
	if !ok {
		t.Fatalf("examples = %#v, want object", payload["examples"])
	}
	schemaExamples, ok := examples["schema"].([]any)
	if !ok || len(schemaExamples) == 0 {
		t.Fatalf("schema examples = %#v, want non-empty array", examples["schema"])
	}
	if !strings.Contains(schemaExamples[0].(string), "slick agent schema") {
		t.Fatalf("schema examples = %#v, want agent schema command", schemaExamples)
	}
}

func TestAgentSchemaDoesNotDocumentRawTokenArgv(t *testing.T) {
	stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", []string{"agent", "schema"})
	if err != nil {
		t.Fatalf("agent schema returned error: %v\nstderr=%s", err, stderr)
	}
	if strings.Contains(stdout, "--token <xox") || strings.Contains(stdout, "--token xox") {
		t.Fatalf("schema documents raw token argv usage: %s", stdout)
	}
	var schema agenthelp.Schema
	if err := json.Unmarshal([]byte(stdout), &schema); err != nil {
		t.Fatalf("unmarshal schema: %v\nstdout=%s", err, stdout)
	}
	for _, fragment := range []string{"--token-stdin", "--token-file", "--token-env"} {
		if !strings.Contains(stdout, fragment) {
			t.Fatalf("schema missing safe token source %q: %s", fragment, stdout)
		}
	}
	env := map[string]bool{}
	for _, value := range schema.Env {
		env[value] = true
	}
	if !env["SLACK_CLI_TOKEN_<PROFILE>"] {
		t.Fatalf("schema env = %#v, want profile-scoped runtime token env", schema.Env)
	}
}

func TestAgentGuideOutputsNamedWorkflowInstructions(t *testing.T) {
	stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", []string{"agent", "guide", "send_msg"})
	if err != nil {
		t.Fatalf("agent guide returned error: %v\nstderr=%s", err, stderr)
	}
	for _, fragment := range []string{
		"## send_msg",
		"slick message send",
		"--channel",
		"--file -",
		"--blocks",
		"JSON",
		"Agent attribution",
		"--agent-emoji",
		"--agent-message",
		"--raw",
		"output-only",
		"attribution.enabled",
	} {
		if !strings.Contains(stdout, fragment) {
			t.Fatalf("stdout = %q, want fragment %q", stdout, fragment)
		}
	}
}

func TestAgentGuideOutputsReactionInstructions(t *testing.T) {
	stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", []string{"agent", "guide", "react"})
	if err != nil {
		t.Fatalf("agent guide returned error: %v\nstderr=%s", err, stderr)
	}
	for _, fragment := range []string{
		"## react",
		"slick react add",
		"--timestamp",
		":thumbsup:",
	} {
		if !strings.Contains(stdout, fragment) {
			t.Fatalf("stdout = %q, want fragment %q", stdout, fragment)
		}
	}
}

func TestAgentGuideHelpShowsAvailableWorkflows(t *testing.T) {
	stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", []string{"agent", "guide", "--help"})
	if err != nil {
		t.Fatalf("agent guide --help returned error: %v\nstderr=%s", err, stderr)
	}
	for _, fragment := range []string{
		"Available workflows:",
		"send_msg",
		"react",
		"reply",
		"auth_setup",
		"config_prefs",
		"core_contract",
		"discover_destination",
		"inspect_schema",
		"safe_mutation",
	} {
		if !strings.Contains(stdout, fragment) {
			t.Fatalf("stdout = %q, want fragment %q", stdout, fragment)
		}
	}
}

func TestAgentGuideHelpShowsWorkflowsAlphabetically(t *testing.T) {
	stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", []string{"agent", "guide", "--help"})
	if err != nil {
		t.Fatalf("agent guide --help returned error: %v\nstderr=%s", err, stderr)
	}
	assertBefore(t, stdout, "auth_setup", "delete_msg")
	assertBefore(t, stdout, "auth_setup", "config_prefs")
	assertBefore(t, stdout, "config_prefs", "core_contract")
	assertBefore(t, stdout, "core_contract", "delete_msg")
	assertBefore(t, stdout, "delete_msg", "discover_destination")
	assertBefore(t, stdout, "discover_destination", "edit_msg")
	assertBefore(t, stdout, "edit_msg", "lookup_user")
	assertBefore(t, stdout, "inspect_schema", "lookup_user")
	assertBefore(t, stdout, "safe_mutation", "search_msgs")
	assertBefore(t, stdout, "search_msgs", "send_dm")
	assertBefore(t, stdout, "send_dm", "send_msg")
}

func TestAgentGuideOutputsAdditionalWorkflowInstructions(t *testing.T) {
	tests := map[string][]string{
		"auth_setup": {
			"## auth_setup",
			"PKCE",
			"manifest template",
			"keychain",
		},
		"config_prefs": {
			"## config_prefs",
			"slick config init",
			"preferences",
			"auth commands",
		},
		"core_contract": {
			"## core_contract",
			"stdout is command data only",
			"stderr is diagnostics",
			"mutually exclusive",
			"Exit codes",
		},
		"reply": {
			"## reply",
			"slick reply",
			"--parent",
			"parent message timestamp",
		},
		"edit_msg": {
			"## edit_msg",
			"slick message edit",
			"--timestamp",
			"own messages",
		},
		"delete_msg": {
			"## delete_msg",
			"slick message delete",
			"--force",
			"--dry-run",
		},
		"discover_destination": {
			"## discover_destination",
			"slick lookup channel",
			"--types",
			"prefer IDs",
			"plain mode renders tables",
		},
		"inspect_schema": {
			"## inspect_schema",
			"slick agent schema",
			"--compact",
			"slick schema",
		},
		"lookup_user": {
			"## lookup_user",
			"slick lookup user",
			"--user",
			"timezone",
		},
		"send_dm": {
			"## send_dm",
			"slick message send --user",
			"Slack decides",
			"bot-token",
		},
		"safe_mutation": {
			"## safe_mutation",
			"--dry-run",
			"destructive",
			"JSON",
		},
	}
	for section, fragments := range tests {
		t.Run(section, func(t *testing.T) {
			stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", []string{"agent", "guide", section})
			if err != nil {
				t.Fatalf("agent guide returned error: %v\nstderr=%s", err, stderr)
			}
			for _, fragment := range fragments {
				if !strings.Contains(stdout, fragment) {
					t.Fatalf("stdout = %q, want fragment %q", stdout, fragment)
				}
			}
		})
	}
}

func assertBefore(t *testing.T, output, first, second string) {
	t.Helper()
	firstIndex := strings.Index(output, first)
	secondIndex := strings.Index(output, second)
	if firstIndex == -1 || secondIndex == -1 {
		t.Fatalf("output missing ordering targets %q or %q:\n%s", first, second, output)
	}
	if firstIndex >= secondIndex {
		t.Fatalf("output order: %q should appear before %q:\n%s", first, second, output)
	}
}

func buildTestRoot(cfg *config.Config, baseURL string, stdin interface{ Read([]byte) (int, error) }, stdout, stderr *bytes.Buffer) *cobra.Command {
	th := theme.Default()
	runtime := &cliruntime.RootRuntime{
		Stdin:        stdin,
		Stdout:       stdout,
		Stderr:       stderr,
		IsTTY:        false,
		Now:          func() time.Time { return time.Date(2026, 5, 3, 13, 8, 0, 0, time.UTC) },
		RequestID:    func() string { return "test-request" },
		Theme:        th,
		HelpRenderer: help.NewRenderer(th),
	}
	if cfg != nil {
		runtime.Config = cfg
	}
	if baseURL != "" {
		runtime.SlackBaseURL = baseURL
	}
	runtime.TokenResolver = cliruntime.TokenResolverFunc(func(_ context.Context, _ config.WorkspaceProfile) (string, error) {
		return "xox-test", nil
	})

	root := &cobra.Command{
		Use:           "slick",
		Short:         "Slack command line interface",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetIn(stdin)
	root.SetOut(stdout)
	root.SetErr(stderr)

	flags := root.PersistentFlags()
	flags.StringP("workspace", "w", "", "Workspace profile")
	flags.BoolP("json", "j", false, "Force JSON output")
	flags.BoolP("plain", "P", false, "Force plain text output")
	flags.BoolP("compact", "k", false, "Output command data without envelope")
	flags.BoolP("raw", "X", false, "Output Slack-native data")
	flags.BoolP("agent", "a", false, "Force agent mode")
	flags.BoolP("no-agent-attribution", "z", false, "Disable agent attribution for this command")
	flags.StringP("agent-label", "G", "", "Override agent attribution label")
	flags.StringP("agent-emoji", "Y", "", "Override agent attribution emoji")
	flags.StringP("agent-message", "O", "", "Override agent attribution message")
	flags.BoolP("no-throttle", "Q", false, "Disable proactive Slack API throttling")
	flags.BoolP("debug", "D", false, "Enable debug-level output")
	root.MarkFlagsMutuallyExclusive("json", "plain", "compact", "raw")

	root.AddCommand(cliagent.NewCommand(runtime))
	return root
}

func executeTestRoot(t *testing.T, cfg *config.Config, baseURL, stdin string, args []string) (string, string, error) {
	t.Helper()
	stdoutBuf := &bytes.Buffer{}
	stderrBuf := &bytes.Buffer{}
	cmd := buildTestRoot(cfg, baseURL, strings.NewReader(stdin), stdoutBuf, stderrBuf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdoutBuf.String(), stderrBuf.String(), err
}
