package main

import (
	"encoding/json"
	"flag"
	"path/filepath"
	"testing"

	"github.com/matcra587/docent/docenttest"
	"github.com/matcra587/slack-cli/internal/agentguides"
	"github.com/matcra587/slack-cli/internal/agenthelp"
	"github.com/spf13/cobra"
)

var updateDocentGolden = flag.Bool("update-docent-golden", false, "Update the agent schema contract fixture")

func TestDocentGuidesFollowStandard(t *testing.T) {
	t.Setenv("SLICK_CONFIG", filepath.Join(t.TempDir(), "config.toml"))
	root := NewRootCommand()
	docenttest.Validate(t, agentguides.Files(), agenthelp.GenerateSchema(root))
}

func TestDocentSchemaGolden(t *testing.T) {
	t.Setenv("SLICK_CONFIG", filepath.Join(t.TempDir(), "config.toml"))
	root := NewRootCommand()
	root.InitDefaultHelpCmd()
	tree := docenttest.MaskFlagDefaults(agenthelp.GenerateSchema(root), "redirect-url")
	docenttest.SchemaGolden(t, tree.StripShapes(), "testdata/agent-schema.json", *updateDocentGolden)
}

func TestCommandGroupsUseCommonJSONFrame(t *testing.T) {
	root := NewRootCommand(WithConfig(nil))
	var visit func(*cobra.Command)
	visit = func(parent *cobra.Command) {
		for _, cmd := range parent.Commands() {
			visit(cmd)
			if !cmd.HasSubCommands() {
				continue
			}
			t.Run(cmd.CommandPath(), func(t *testing.T) {
				args := append(commandWords(cmd), "--output=json")
				stdout, stderr, err := executeTestRoot(t, nil, "http://example.invalid", "", args)
				if err != nil || stderr != "" {
					t.Fatalf("error=%v stderr=%s", err, stderr)
				}
				var frame map[string]json.RawMessage
				if err := json.Unmarshal([]byte(stdout), &frame); err != nil {
					t.Fatal(err)
				}
				if len(frame) != 4 || string(frame["ok"]) != "true" {
					t.Fatalf("not the common success frame: %s", stdout)
				}
				var meta map[string]json.RawMessage
				if err := json.Unmarshal(frame["meta"], &meta); err != nil {
					t.Fatal(err)
				}
				if string(meta["exit_code"]) != "0" {
					t.Fatalf("success omitted exit_code: %s", frame["meta"])
				}
			})
		}
	}
	visit(root)
}

func commandWords(cmd *cobra.Command) []string {
	var words []string
	for cmd.Parent() != nil {
		words = append([]string{cmd.Name()}, words...)
		cmd = cmd.Parent()
	}
	return words
}
