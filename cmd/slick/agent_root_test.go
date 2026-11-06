package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/matcra587/docent"
)

func TestAgentSchemaCompactOutputsCommandTreeForAgents(t *testing.T) {
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
	if !compactSchemaHasCommand(schema.Children, "message", "send") {
		t.Fatalf("schema commands = %#v, want message send", schema.Children)
	}
	if !compactSchemaHasCommand(schema.Children, "reply") {
		t.Fatalf("schema commands = %#v, want public reply command", schema.Children)
	}
	if !compactSchemaHasCommand(schema.Children, "react", "add") ||
		!compactSchemaHasCommand(schema.Children, "react", "remove") ||
		!compactSchemaHasCommand(schema.Children, "react", "list") {
		t.Fatalf("schema commands = %#v, want public react add/remove/list commands", schema.Children)
	}
	if compactSchemaHasCommand(schema.Children, "reaction") {
		t.Fatalf("schema commands = %#v, legacy reaction command should not exist", schema.Children)
	}
	if compactSchemaHasCommand(schema.Children, "thread") {
		t.Fatalf("schema commands = %#v, legacy thread command should not exist", schema.Children)
	}
	if compactSchemaHasCommand(schema.Children, "dm") {
		t.Fatalf("schema commands = %#v, dm command should not be exposed; use message send --user", schema.Children)
	}
	if compactSchemaHasCommand(schema.Children, "schema") {
		t.Fatalf("schema commands = %#v, root schema alias should not be exposed", schema.Children)
	}
}

func TestAgentSchemaIncludesBlocksAndOutputOnlyRawContract(t *testing.T) {
	stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", []string{"agent", "schema"})
	if err != nil {
		t.Fatalf("agent schema returned error: %v\nstderr=%s", err, stderr)
	}
	var schema docent.Command
	if err := json.Unmarshal(schemaArtifact(t, stdout), &schema); err != nil {
		t.Fatalf("unmarshal schema: %v\nstdout=%s", err, stdout)
	}
	for _, path := range [][]string{
		{"message", "send"},
		{"message", "edit"},
	} {
		command := findSchemaCommand(schema.Children, path...)
		if command == nil {
			t.Fatalf("schema missing command path %v", path)
		}
		if !schemaCommandHasFlag(*command, "blocks") {
			t.Fatalf("schema command %v flags = %#v, want --blocks", path, command.Flags)
		}
	}
	if command := findSchemaCommand(schema.Children, "dm"); command != nil {
		t.Fatalf("schema exposed dm command %#v; direct messages must use message send --user", *command)
	}
	for _, name := range []string{"channel", "user"} {
		if command := findSchemaCommand(schema.Children, name); command != nil {
			t.Fatalf("schema exposed %s command %#v; discovery must use lookup", name, *command)
		}
	}
	for _, path := range [][]string{{"lookup", "channel"}, {"lookup", "messages"}, {"lookup", "user"}} {
		if command := findSchemaCommand(schema.Children, path...); command == nil {
			t.Fatalf("schema missing lookup command path %v", path)
		}
	}
	if !schemaGlobalFlag(schema.Flags, "output") {
		t.Fatalf("global flags = %#v, want --output flag", schema.Flags)
	}
	var contract struct {
		InputShapes map[string][]string `json:"input_shapes"`
		Output      struct {
			Notes []string `json:"notes"`
		} `json:"output"`
		ExitCodes map[string]int `json:"exit_codes"`
	}
	raw, _ := json.Marshal(schema.Extensions)
	if err := json.Unmarshal(raw, &contract); err != nil {
		t.Fatal(err)
	}
	messageShape := strings.Join(contract.InputShapes["message.send"], "\n")
	if !strings.Contains(messageShape, "--blocks Block Kit JSON array") {
		t.Fatalf("message.send input shape = %#v, want --blocks raw input contract", contract.InputShapes["message.send"])
	}
	for _, fragment := range []string{"source-preserving Markdown fallback", "validates Slack Block Kit JSON rules", "missing_scope", "not_in_channel", "no_permission"} {
		if !strings.Contains(messageShape, fragment) && !strings.Contains(strings.Join(contract.Output.Notes, "\n"), fragment) {
			t.Fatalf("schema missing clarified contract %q\ninput=%#v\nnotes=%#v", fragment, contract.InputShapes["message.send"], contract.Output.Notes)
		}
	}
	if !strings.Contains(messageShape, "--channel and --user are mutually exclusive") {
		t.Fatalf("message.send input shape = %#v, want mutually-exclusive target contract", contract.InputShapes["message.send"])
	}
	for _, fragment := range []string{
		"--schedule <RFC3339|duration|unix-seconds> for channel or user scheduled sends",
		"scheduled --user sends resolve/open the DM before chat.scheduleMessage",
	} {
		if !strings.Contains(messageShape, fragment) {
			t.Fatalf("message.send input shape = %#v, want scheduled DM target contract %q", contract.InputShapes["message.send"], fragment)
		}
	}
	if strings.Contains(messageShape, "--output Block Kit JSON array") {
		t.Fatalf("message.send input shape = %#v, --output must not select raw input", contract.InputShapes["message.send"])
	}
	if !strings.Contains(strings.Join(contract.Output.Notes, "\n"), "use command-local --blocks for raw Block Kit input") {
		t.Fatalf("output notes = %#v, want --blocks raw-input note", contract.Output.Notes)
	}
	for name, want := range map[string]int{"canceled": 6, "timeout": 7} {
		if got := contract.ExitCodes[name]; got != want {
			t.Fatalf("schema exit code %s = %d, want %d; codes=%#v", name, got, want, contract.ExitCodes)
		}
	}
}

func findSchemaCommand(commands []docent.Command, path ...string) *docent.Command {
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
		return findSchemaCommand(commands[i].Children, path[1:]...)
	}
	return nil
}

func schemaCommandHasFlag(command docent.Command, name string) bool {
	for _, flag := range command.Flags {
		if flag.Name == name {
			return true
		}
	}
	return false
}

func schemaGlobalFlag(flags []docent.Flag, name string) bool {
	for _, flag := range flags {
		if flag.Name == name {
			return true
		}
	}
	return false
}

func compactSchemaHasCommand(commands []docent.Command, path ...string) bool {
	if len(path) == 0 {
		return true
	}
	for _, command := range commands {
		if command.Name == path[0] {
			return compactSchemaHasCommand(command.Children, path[1:]...)
		}
	}
	return false
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
