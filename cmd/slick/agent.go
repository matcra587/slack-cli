package main

import (
	cobracli "github.com/gechr/clib/cli/cobra"
	"github.com/gechr/clib/help"
	"github.com/gechr/clib/theme"
	"github.com/gechr/clog"
	"github.com/matcra587/slack-cli/internal/agent"
	"github.com/matcra587/slack-cli/internal/agenthelp"
	"github.com/spf13/cobra"
)

type Attribution = agent.Attribution

type AgentFlags struct {
	Agent              bool
	NoAgentAttribution bool
	AgentLabel         string
	AgentEmoji         string
	AgentMessage       string
	ProfileAttribution *bool
}

func DetectAgentMode(flags AgentFlags) agent.Attribution {
	detection := agent.Detect(agent.Options{
		Force:              flags.Agent,
		NoAttribution:      flags.NoAgentAttribution,
		ProfileAttribution: flags.ProfileAttribution,
		Label:              flags.AgentLabel,
		Emoji:              flags.AgentEmoji,
		Message:            flags.AgentMessage,
	})
	return agent.NewAttribution(detection, agent.Options{
		Label:   flags.AgentLabel,
		Emoji:   flags.AgentEmoji,
		Message: flags.AgentMessage,
	})
}

func DetectAgentOutputMode(flags AgentFlags) bool {
	return agent.Detect(agent.Options{Force: flags.Agent}).Active
}

func newAgentCommand(runtime *RootRuntime) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "agent",
		Short:        "Agent tooling: schema and guides",
		Long:         "Provides machine-readable CLI schema and workflow guides for AI agents.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newAgentSchemaCommand(runtime))
	cmd.AddCommand(newAgentGuideCommand(runtime))
	return cmd
}

func newAgentSchemaCommand(runtime *RootRuntime) *cobra.Command {
	var compact bool
	cmd := &cobra.Command{
		Use:          "schema",
		Short:        "Output command schema as JSON",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := agentCommandContext(cmd, runtime)
			if compact {
				ctx.stdoutLogger().Print().Mode(clog.JSONFlat).JSON(agenthelp.GenerateCompactSchema(cmd.Root()))
				return nil
			}
			ctx.stdoutLogger().Print().Mode(clog.JSONFlat).JSON(agenthelp.GenerateSchema(cmd.Root()))
			return nil
		},
	}
	cmd.Flags().BoolVarP(&compact, "compact", "c", false, "Output minimal schema")
	return cmd
}

func newAgentGuideCommand(runtime *RootRuntime) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "guide [workflow]",
		Short:        "Output workflow instructions for agents",
		ValidArgs:    agenthelp.WorkflowNames(),
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := agentCommandContext(cmd, runtime)
			if len(args) == 0 {
				return ctx.WritePlain(agenthelp.GetGuide())
			}
			return ctx.WritePlain(agenthelp.GetGuideSection(args[0]))
		},
	}
	cmd.SetHelpFunc(agentGuideHelpFunc())
	return cmd
}

func agentGuideHelpFunc() func(*cobra.Command, []string) {
	renderer := help.NewRenderer(theme.Default())
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

func agentCommandContext(cmd *cobra.Command, runtime *RootRuntime) *CommandContext {
	opts := rootOptionsFromCommand(cmd, runtime)
	return &CommandContext{
		Workspace: "agent",
		Mode:      opts.Output.Resolve(runtime.IsTTY, true),
		Stdout:    runtime.Stdout,
		Stderr:    runtime.Stderr,
		Now:       runtime.Now,
		RequestID: runtime.RequestID,
	}
}
