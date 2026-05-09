// Package agent implements the `slick agent` cobra command tree, which
// exposes machine-readable CLI schema and workflow guides for AI agents.
package agent

import (
	cobracli "github.com/gechr/clib/cli/cobra"
	"github.com/gechr/clib/help"
	agentpkg "github.com/matcra587/slack-cli/internal/agent"
	"github.com/matcra587/slack-cli/internal/agenthelp"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
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
			ctx := localContext(cmd, runtime)
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
			ctx := localContext(cmd, runtime)
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

func localContext(cmd *cobra.Command, runtime *cliruntime.RootRuntime) *clioutput.CommandContext {
	output, _ := commandFlags(cmd)
	mode := output.Resolve(runtime.IsTTY, true)
	sl, el := clioutput.BuildBaseLoggers(runtime.Stdout, runtime.Stderr, runtime.ColorMode)
	clioutput.ApplyRenderMode(sl, mode)
	return &clioutput.CommandContext{
		Workspace:     "agent",
		Mode:          mode,
		Stdout:        runtime.Stdout,
		Stderr:        runtime.Stderr,
		NowFunc:       runtime.Now,
		RequestIDFunc: runtime.RequestID,
		StdoutLog:     sl,
		StderrLog:     el,
	}
}

func commandFlags(cmd *cobra.Command) (clioutput.OutputFlags, cliruntime.AgentFlags) {
	flags := cmd.Root().PersistentFlags()
	jsonMode, _ := flags.GetBool("json")
	plain, _ := flags.GetBool("plain")
	compact, _ := flags.GetBool("compact")
	raw, _ := flags.GetBool("raw")
	forceAgent, _ := flags.GetBool("agent")
	noAttribution, _ := flags.GetBool("no-agent-attribution")
	agentLabel, _ := flags.GetString("agent-label")
	agentEmoji, _ := flags.GetString("agent-emoji")
	agentMessage, _ := flags.GetString("agent-message")
	return clioutput.OutputFlags{
			JSON:    jsonMode,
			Plain:   plain,
			Compact: compact,
			Raw:     raw,
		}, cliruntime.AgentFlags{
			Agent:              forceAgent,
			NoAgentAttribution: noAttribution,
			AgentLabel:         agentLabel,
			AgentEmoji:         agentEmoji,
			AgentMessage:       agentMessage,
		}
}

// DetectAttribution computes the agent attribution settings (label/emoji/message)
// for the supplied flags. Used by main.go and tests that exercise attribution
// fallthrough.
func DetectAttribution(flags cliruntime.AgentFlags) agentpkg.Attribution {
	detection := agentpkg.Detect(agentpkg.Options{
		Force:              flags.Agent,
		NoAttribution:      flags.NoAgentAttribution,
		ProfileAttribution: flags.ProfileAttribution,
		Label:              flags.AgentLabel,
		Emoji:              flags.AgentEmoji,
		Message:            flags.AgentMessage,
	})
	return agentpkg.NewAttribution(detection, agentpkg.Options{
		Label:   flags.AgentLabel,
		Emoji:   flags.AgentEmoji,
		Message: flags.AgentMessage,
	})
}

// DetectOutputMode reports whether output should be rendered in agent mode for
// the supplied flags.
func DetectOutputMode(flags cliruntime.AgentFlags) bool {
	return agentpkg.Detect(agentpkg.Options{Force: flags.Agent}).Active
}
