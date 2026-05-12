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
	if !schemaGlobalFlag(schema.GlobalFlags, "output") {
		t.Fatalf("global flags = %#v, want --output flag", schema.GlobalFlags)
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
	if strings.Contains(messageShape, "--output Block Kit JSON array") {
		t.Fatalf("message.send input shape = %#v, --output must not select raw input", schema.InputShapes["message.send"])
	}
	if !strings.Contains(strings.Join(schema.Output.Notes, "\n"), "use command-local --blocks for raw Block Kit input") {
		t.Fatalf("output notes = %#v, want --blocks raw-input note", schema.Output.Notes)
	}
	for name, want := range map[string]int{"canceled": 6, "timeout": 7} {
		if got := schema.ExitCodes[name]; got != want {
			t.Fatalf("schema exit code %s = %d, want %d; codes=%#v", name, got, want, schema.ExitCodes)
		}
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
