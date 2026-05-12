// Package agent implements the `slick agent` cobra command tree, which
// exposes machine-readable CLI schema and workflow guides for AI agents.
package agent

import (
	cobracli "github.com/gechr/clib/cli/cobra"
	"github.com/gechr/clib/help"
	agentpkg "github.com/matcra587/slack-cli/internal/agent"
	"github.com/matcra587/slack-cli/internal/agenthelp"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	"github.com/spf13/cobra"
)

// NewCommand returns the `slick agent` parent command.
func NewCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "agent",
		Short:        "Agent tooling: schema and guides",
		Long:         "Provides machine-readable CLI schema and workflow guides for AI agents.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newSchemaCommand(runtime))
	cmd.AddCommand(newGuideCommand(runtime))
	return cmd
}

func newSchemaCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	var compact bool
	cmd := &cobra.Command{
		Use:          "schema",
		Short:        "Output command schema as JSON",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cliruntime.LocalContextForceAgent(cmd, runtime, "agent")
			if compact {
				ctx.StdoutLogger().Print().JSON(agenthelp.GenerateCompactSchema(cmd.Root()))
				return nil
			}
			ctx.StdoutLogger().Print().JSON(agenthelp.GenerateSchema(cmd.Root()))
			return nil
		},
	}
	cmd.Flags().BoolVarP(&compact, "compact", "c", false, "Output minimal schema")
	return cmd
}

func newGuideCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "guide [workflow]",
		Short:        "Output workflow instructions for agents",
		ValidArgs:    agenthelp.WorkflowNames(),
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cliruntime.LocalContextForceAgent(cmd, runtime, "agent")
			if len(args) == 0 {
				return ctx.WriteString(agenthelp.GetGuide())
			}
			return ctx.WriteString(agenthelp.GetGuideSection(args[0]))
		},
	}
	cmd.SetHelpFunc(guideHelpFunc(runtime.HelpRenderer))
	return cmd
}

func guideHelpFunc(renderer *help.Renderer) func(*cobra.Command, []string) {
	return cobracli.HelpFunc(renderer, func(cmd *cobra.Command) []help.Section {
		sections := cobracli.Sections(cmd)
		sections = append(sections, help.Section{
			Title: "Workflows",
			Content: []help.Content{
				help.Text("Available workflows:"),
				agenthelp.WorkflowCommandGroup(),
			},
		})
		return sections
	})
}

// DetectAttribution computes the attribution settings (label/emoji/message)
// for the supplied flags. Used by main.go and tests that exercise attribution
// fallthrough.
func DetectAttribution(flags cliruntime.AttributionFlags) agentpkg.Attribution {
	detection := agentpkg.Detect(agentpkg.Options{
		Attribution:        flags.Attribution,
		ProfileAttribution: flags.ProfileAttribution,
		Label:              flags.Label,
		Emoji:              flags.Emoji,
		Message:            flags.Message,
	})
	return agentpkg.NewAttribution(detection, agentpkg.Options{
		Label:   flags.Label,
		Emoji:   flags.Emoji,
		Message: flags.Message,
	})
}

// DetectOutputMode reports whether output should be rendered in agent mode.
// True when an agent/CI env trigger is set; the user can also pick a
// machine-friendly mode explicitly via --output=json|compact.
func DetectOutputMode() bool {
	return agentpkg.Detect(agentpkg.Options{}).Active
}
