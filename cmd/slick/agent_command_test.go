package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/agenthelp"
)

func TestAgentSchemaCompactOutputsCommandTreeForAgents(t *testing.T) {
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
	if !compactSchemaHasCommand(schema.Commands, "message", "send") {
		t.Fatalf("schema commands = %#v, want message send", schema.Commands)
	}
	if !compactSchemaHasCommand(schema.Commands, "reply") {
		t.Fatalf("schema commands = %#v, want public reply command", schema.Commands)
	}
	if !compactSchemaHasCommand(schema.Commands, "react", "add") ||
		!compactSchemaHasCommand(schema.Commands, "react", "remove") ||
		!compactSchemaHasCommand(schema.Commands, "react", "list") {
		t.Fatalf("schema commands = %#v, want public react add/remove/list commands", schema.Commands)
	}
	if compactSchemaHasCommand(schema.Commands, "reaction") {
		t.Fatalf("schema commands = %#v, legacy reaction command should not exist", schema.Commands)
	}
	if compactSchemaHasCommand(schema.Commands, "thread") {
		t.Fatalf("schema commands = %#v, legacy thread command should not exist", schema.Commands)
	}
	if compactSchemaHasCommand(schema.Commands, "dm") {
		t.Fatalf("schema commands = %#v, dm command should not be exposed; use message send --user", schema.Commands)
	}
	if compactSchemaHasCommand(schema.Commands, "schema") {
		t.Fatalf("schema commands = %#v, root schema alias should not be exposed", schema.Commands)
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

func TestAgentSchemaIncludesBlocksAndOutputOnlyRawContract(t *testing.T) {
	stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", []string{"agent", "schema"})
	if err != nil {
		t.Fatalf("agent schema returned error: %v\nstderr=%s", err, stderr)
	}
	var schema agenthelp.Schema
	if err := json.Unmarshal([]byte(stdout), &schema); err != nil {
		t.Fatalf("unmarshal schema: %v\nstdout=%s", err, stdout)
	}
	for _, path := range [][]string{
		{"message", "send"},
		{"message", "edit"},
	} {
		command := findSchemaCommand(schema.Commands, path...)
		if command == nil {
			t.Fatalf("schema missing command path %v", path)
		}
		if !schemaCommandHasFlag(*command, "blocks") {
			t.Fatalf("schema command %v flags = %#v, want --blocks", path, command.Flags)
		}
	}
	if command := findSchemaCommand(schema.Commands, "dm"); command != nil {
		t.Fatalf("schema exposed dm command %#v; direct messages must use message send --user", *command)
	}
	for _, name := range []string{"channel", "user"} {
		if command := findSchemaCommand(schema.Commands, name); command != nil {
			t.Fatalf("schema exposed %s command %#v; discovery must use lookup", name, *command)
		}
	}
	for _, path := range [][]string{{"lookup", "channel"}, {"lookup", "messages"}, {"lookup", "user"}} {
		if command := findSchemaCommand(schema.Commands, path...); command == nil {
			t.Fatalf("schema missing lookup command path %v", path)
		}
	}
	if !schemaGlobalFlag(schema.GlobalFlags, "raw") || !schemaGlobalFlag(schema.GlobalFlags, "json") {
		t.Fatalf("global flags = %#v, want --raw and --json output flags", schema.GlobalFlags)
	}
	messageShape := strings.Join(schema.InputShapes["message.send"], "\n")
	if !strings.Contains(messageShape, "--blocks Block Kit JSON array") {
		t.Fatalf("message.send input shape = %#v, want --blocks raw input contract", schema.InputShapes["message.send"])
	}
	for _, fragment := range []string{"source-preserving Markdown fallback", "validates Slack Block Kit JSON rules", "missing_scope", "not_in_channel", "no_permission"} {
		if !strings.Contains(messageShape, fragment) && !strings.Contains(strings.Join(schema.Output.Notes, "\n"), fragment) {
			t.Fatalf("schema missing clarified contract %q\ninput=%#v\nnotes=%#v", fragment, schema.InputShapes["message.send"], schema.Output.Notes)
		}
	}
	if !strings.Contains(messageShape, "--channel and --user are mutually exclusive") {
		t.Fatalf("message.send input shape = %#v, want mutually-exclusive target contract", schema.InputShapes["message.send"])
	}
	if strings.Contains(messageShape, "--raw Block Kit JSON array") {
		t.Fatalf("message.send input shape = %#v, --raw must not select raw input", schema.InputShapes["message.send"])
	}
	if !strings.Contains(strings.Join(schema.Output.Notes, "\n"), "--raw is output-only") {
		t.Fatalf("output notes = %#v, want output-only --raw note", schema.Output.Notes)
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

func findSchemaCommand(commands []agenthelp.CommandInfo, path ...string) *agenthelp.CommandInfo {
	if len(path) == 0 {
		return nil
	}
	for i := range commands {
		if commands[i].Name != path[0] {
			continue
		}
		if len(path) == 1 {
			return &commands[i]
		}
		return findSchemaCommand(commands[i].Subcommands, path[1:]...)
	}
	return nil
}

func schemaCommandHasFlag(command agenthelp.CommandInfo, name string) bool {
	for _, flag := range command.Flags {
		if flag.Name == name {
			return true
		}
	}
	return false
}

func schemaGlobalFlag(flags []agenthelp.FlagInfo, name string) bool {
	for _, flag := range flags {
		if flag.Name == name {
			return true
		}
	}
	return false
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

func compactSchemaHasCommand(commands []agenthelp.CompactCommand, path ...string) bool {
	if len(path) == 0 {
		return true
	}
	for _, command := range commands {
		if command.Name == path[0] {
			return compactSchemaHasCommand(command.Subcommands, path[1:]...)
		}
	}
	return false
}
