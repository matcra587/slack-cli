package agent_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/cli/clitheme"

	"github.com/matcra587/docent"
	agentpkg "github.com/matcra587/slack-cli/internal/agent"
	cliagent "github.com/matcra587/slack-cli/internal/cli/agent"
	"github.com/matcra587/slack-cli/internal/cli/runtime/runtimetest"
	"github.com/matcra587/slack-cli/internal/config"
)

func TestMain(m *testing.M) {
	for _, key := range agentpkg.KnownEnvVars() {
		_ = os.Unsetenv(key)
	}
	os.Exit(m.Run())
}

func TestAgentSchemaCompactUsesSharedEnvelope(t *testing.T) {
	stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", []string{"agent", "schema", "--compact"})
	if err != nil {
		t.Fatalf("agent schema returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"meta"`) {
		t.Fatalf("stdout = %s, want shared JSON envelope", stdout)
	}

	var schema docent.Command
	if err := json.Unmarshal(schemaArtifact(t, stdout), &schema); err != nil {
		t.Fatalf("unmarshal schema: %v\nstdout=%s", err, stdout)
	}
	if schema.Extensions["contract_version"] == nil {
		t.Fatal("schema version is empty")
	}
}

func TestAgentSchemaOutputsWorkflowsAlphabetically(t *testing.T) {
	stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", []string{"agent", "schema"})
	if err != nil {
		t.Fatalf("agent schema returned error: %v\nstderr=%s", err, stderr)
	}
	var schema docent.Command
	if err := json.Unmarshal(schemaArtifact(t, stdout), &schema); err != nil {
		t.Fatalf("unmarshal schema: %v\nstdout=%s", err, stdout)
	}
	guides, ok := schema.Extensions["guides"].([]any)
	if !ok || len(guides) == 0 {
		t.Fatal("schema omitted guide summaries")
	}
	var previous string
	for _, v := range guides {
		guide := v.(map[string]any)
		name := guide["slug"].(string)
		if previous != "" && name < previous {
			t.Fatalf("guide order %q before %q", previous, name)
		}
		previous = name
	}
}

func TestAgentSchemaIncludesRootSchemaContract(t *testing.T) {
	stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", []string{"agent", "schema"})
	if err != nil {
		t.Fatalf("agent schema returned error: %v\nstderr=%s", err, stderr)
	}
	var payload map[string]any
	if err := json.Unmarshal(schemaArtifact(t, stdout), &payload); err != nil {
		t.Fatalf("unmarshal schema: %v\nstdout=%s", err, stdout)
	}
	payload = payload["extensions"].(map[string]any)
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
	var schema docent.Command
	if err := json.Unmarshal(schemaArtifact(t, stdout), &schema); err != nil {
		t.Fatalf("unmarshal schema: %v\nstdout=%s", err, stdout)
	}
	for _, fragment := range []string{"--token-stdin", "--token-file", "--token-env"} {
		if !strings.Contains(stdout, fragment) {
			t.Fatalf("schema missing safe token source %q: %s", fragment, stdout)
		}
	}
	env := map[string]bool{}
	for _, raw := range schema.Extensions["env"].([]any) {
		value := raw.(string)
		env[value] = true
	}
	if !env["SLACK_CLI_TOKEN_<PROFILE>"] {
		t.Fatalf("schema env = %#v, want profile-scoped runtime token env", schema.Extensions["env"])
	}
}

func TestAgentGuideOutputsNamedWorkflowInstructions(t *testing.T) {
	stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", []string{"agent", "guide", "--output=human", "send_msg"})
	if err != nil {
		t.Fatalf("agent guide returned error: %v\nstderr=%s", err, stderr)
	}
	for _, fragment := range []string{
		"## Decide",
		"slick message send",
		"--channel",
		"--file -",
		"--blocks",
		"--output=json",
		// Attribution surface now lives in core_contract; send_msg
		// references it instead of duplicating the flag list.
		"Attribution",
		"core_contract",
		// Lower-case "raw Block Kit" — the capitalised form was dropped
		// along with the Behavior bullet during the token squeeze.
		"raw Block Kit",
	} {
		if !strings.Contains(stdout, fragment) {
			t.Fatalf("stdout = %q, want fragment %q", stdout, fragment)
		}
	}
}

func TestAgentGuideOutputsReactionInstructions(t *testing.T) {
	stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", []string{"agent", "guide", "--output=human", "react"})
	if err != nil {
		t.Fatalf("agent guide returned error: %v\nstderr=%s", err, stderr)
	}
	for _, fragment := range []string{
		"## Decide",
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
	stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", []string{"agent", "guide", "--output=human", "--help"})
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
	stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", []string{"agent", "guide", "--output=human", "--help"})
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
			"## Decide",
			"PKCE",
			"manifest template",
			"keychain",
		},
		"config_prefs": {
			"## Decide",
			"slick config init",
			// R4 schema: the section's Goal line uses "preferences",
			// and the Behavior block points back at the auth commands.
			"preferences",
			"slick auth login",
		},
		"core_contract": {
			"## Decide",
			"Machine success and failure are JSON records on stdout",
			"stderr is diagnostics",
			"--output",
			"Exit codes",
		},
		"reply": {
			"## Decide",
			"slick reply",
			"--parent",
			"parent message timestamp",
		},
		"edit_msg": {
			"## Decide",
			"slick message edit",
			"--timestamp",
			"own messages",
		},
		"delete_msg": {
			"## Decide",
			"slick message delete",
			"--force",
			"--dry-run",
		},
		"discover_destination": {
			"## Decide",
			"slick lookup channel",
			"--types",
			// R4 Behavior: "Prefer over display names" (covers the
			// stable-ID guidance the old assertion was after).
			"display names",
			// R4 Behavior: "Plain mode renders human tables".
			"Plain mode",
		},
		"inspect_schema": {
			"## Decide",
			"slick agent schema",
			"--compact",
			"slick schema",
		},
		"lookup_user": {
			"## Decide",
			"slick lookup user",
			"--user",
			"timezone",
		},
		"send_dm": {
			"## Decide",
			"slick message send --user",
			"Slack decides",
			"bot-token",
		},
		"safe_mutation": {
			"## Decide",
			"--dry-run",
			// R4 Decide: "high-impact" replaces the old "destructive" framing
			// while preserving the safety intent.
			"high-impact",
			"--output=json",
		},
	}
	for section, fragments := range tests {
		t.Run(section, func(t *testing.T) {
			stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", []string{"agent", "guide", "--output=human", section})
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

func executeTestRoot(t *testing.T, cfg *config.Config, baseURL, stdin string, args []string) (string, string, error) {
	t.Helper()
	runtime, stdout, stderr := runtimetest.NewRuntime(t, runtimetest.Options{
		Config:       cfg,
		SlackBaseURL: baseURL,
		Stdin:        strings.NewReader(stdin),
		Theme:        clitheme.Default(),
	})
	root := runtimetest.NewRoot(runtime, stdout, stderr)
	root.AddCommand(cliagent.NewCommand(runtime))
	return runtimetest.Run(t, root, args, stdout, stderr)
}

func schemaArtifact(t *testing.T, stdout string) json.RawMessage {
	t.Helper()
	var frame struct {
		OK   bool `json:"ok"`
		Data struct {
			Schema json.RawMessage `json:"schema"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &frame); err != nil {
		t.Fatal(err)
	}
	if !frame.OK || len(frame.Data.Schema) == 0 {
		t.Fatalf("missing schema envelope: %s", stdout)
	}
	return frame.Data.Schema
}
