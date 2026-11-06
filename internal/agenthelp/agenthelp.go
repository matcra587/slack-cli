// Package agenthelp joins docent's command introspection with slick's contract.
package agenthelp

import (
	_ "embed"
	"encoding/json"
	"strings"

	"github.com/matcra587/docent"
	docentcobra "github.com/matcra587/docent/cobra"
	"github.com/matcra587/slack-cli/internal/agentguides"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	"github.com/spf13/cobra"
)

const ContractVersion = "2"

//go:embed contract.json
var contractJSON []byte

// GenerateSchema derives framework facts from the fully assembled command tree.
func GenerateSchema(root *cobra.Command) docent.Command {
	materializeHelp(root.Root())
	tree := docentcobra.Tree(root.Root())
	if root.Parent() != nil {
		if selected, ok := docent.FindByPath(tree, root.CommandPath()); ok {
			tree = selected
		}
	}
	var extensions map[string]any
	if err := json.Unmarshal(contractJSON, &extensions); err != nil {
		panic(err) // embedded contract JSON is validated by the test suite
	}
	tree.Extensions = extensions
	guides, err := agentguides.Load()
	if err == nil {
		summaries := []map[string]any{}
		for guide := range guides.All() {
			summaries = append(summaries, map[string]any{"slug": guide.Slug, "aliases": guide.Aliases, "title": guide.Title, "description": guide.Description, "commands": guide.Commands})
		}
		tree.Extensions["guides"] = summaries
	}
	var attach func(*docent.Command)
	attach = func(cmd *docent.Command) {
		if cmd.Name != "help" && !strings.Contains(cmd.Path, "completion ") {
			cmd.OutputSchema = clioutput.EnvelopeSchema()
		}
		switch cmd.Name {
		case "list", "get", "path", "info", "schema", "guide", "version", "current", "check", "api-test", "watch":
			cmd.ReadOnly = true
		}
		if cmd.Path == "slick auth status" {
			cmd.ReadOnly = true
		}
		for i := range cmd.Children {
			attach(&cmd.Children[i])
		}
	}
	attach(&tree)
	return tree
}

func materializeHelp(cmd *cobra.Command) {
	cmd.InitDefaultHelpFlag()
	for _, child := range cmd.Commands() {
		materializeHelp(child)
	}
}

// GenerateCompactSchema retains routing facts while omitting verbose metadata.
func GenerateCompactSchema(root *cobra.Command) docent.Command {
	tree := GenerateSchema(root).StripShapes()
	var compact func(*docent.Command)
	compact = func(cmd *docent.Command) {
		cmd.Description = ""
		cmd.Extensions = nil
		for i := range cmd.Flags {
			cmd.Flags[i].Description = ""
			cmd.Flags[i].Extensions = nil
		}
		for i := range cmd.Children {
			compact(&cmd.Children[i])
		}
	}
	compact(&tree)
	tree.Extensions = map[string]any{"contract_version": ContractVersion}
	return tree
}

// WorkflowNames includes canonical slugs and legacy names for completions.
func WorkflowNames() []string { return agentguides.Names() }

// GetGuideSection resolves both legacy names and canonical guide slugs.
func GetGuideSection(name string) string {
	guides, err := agentguides.Load()
	if err != nil {
		return ""
	}
	guide, ok := guides.Get(name)
	if !ok {
		return ""
	}
	return string(guide.Raw)
}

// GetGuide returns all embedded source documents for host contract tests.
func GetGuide() string {
	guides, err := agentguides.Load()
	if err != nil {
		return ""
	}
	var documents []string
	for guide := range guides.All() {
		documents = append(documents, string(guide.Raw))
	}
	return strings.Join(documents, "\n")
}
