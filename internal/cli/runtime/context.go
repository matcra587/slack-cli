package runtime

import (
	"github.com/matcra587/slack-cli/internal/agent"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/spf13/cobra"
)

// AgentFlags carries the per-command agent/attribution overrides parsed from
// cobra persistent flags.
type AgentFlags struct {
	Agent              bool
	NoAgentAttribution bool
	AgentLabel         string
	AgentEmoji         string
	AgentMessage       string
	ProfileAttribution *bool
}

// CommandContextFromCmd resolves the rendering context, workspace profile, and
// attribution state from a cobra command and runtime. It is the canonical
// entry point for command handlers in internal packages.
func CommandContext(cmd *cobra.Command, runtime *RootRuntime) (*clioutput.CommandContext, config.WorkspaceProfile, agent.Attribution, error) {
	flags := cmd.Root().PersistentFlags()
	workspace, _ := flags.GetString("workspace")
	jsonMode, _ := flags.GetBool("json")
	plain, _ := flags.GetBool("plain")
	compact, _ := flags.GetBool("compact")
	raw, _ := flags.GetBool("raw")
	forceAgent, _ := flags.GetBool("agent")
	noAttribution, _ := flags.GetBool("no-agent-attribution")
	agentLabel, _ := flags.GetString("agent-label")
	agentEmoji, _ := flags.GetString("agent-emoji")
	agentMessage, _ := flags.GetString("agent-message")

	outputFlags := clioutput.OutputFlags{
		JSON:    jsonMode,
		Plain:   plain,
		Compact: compact,
		Raw:     raw,
	}
	agentFlags := AgentFlags{
		Agent:              forceAgent,
		NoAgentAttribution: noAttribution,
		AgentLabel:         agentLabel,
		AgentEmoji:         agentEmoji,
		AgentMessage:       agentMessage,
	}

	if runtime.ConfigLoadError != nil {
		mode := outputFlags.Resolve(runtime.IsTTY, detectAgentOutputMode(agentFlags))
		sl, el := clioutput.BuildBaseLoggers(runtime.Stdout, runtime.Stderr, runtime.ColorMode)
		clioutput.ApplyRenderMode(sl, mode)
		ctx := &clioutput.CommandContext{
			Workspace:     "default",
			Mode:          mode,
			Stdout:        runtime.Stdout,
			Stderr:        runtime.Stderr,
			NowFunc:       runtime.Now,
			RequestIDFunc: runtime.RequestID,
			Theme:         runtime.Theme,
			StdoutLog:     sl,
			StderrLog:     el,
		}
		return ctx, config.WorkspaceProfile{}, agent.Attribution{}, runtime.ConfigLoadError
	}

	resolvedWorkspace := "default"
	var resolvedProfile config.WorkspaceProfile

	if runtime.Config != nil {
		profile, err := runtime.Config.ResolveWorkspace(workspace)
		if err != nil {
			return nil, config.WorkspaceProfile{}, agent.Attribution{}, err
		}
		resolvedWorkspace = profile.Name
		resolvedProfile = profile
		agentFlags.ProfileAttribution = profileAttributionSetting(profile)
		settings := profile.AgentSettings()
		if agentFlags.AgentLabel == "" {
			agentFlags.AgentLabel = settings.Label
		}
		if agentFlags.AgentEmoji == "" {
			agentFlags.AgentEmoji = settings.Emoji
		}
		if agentFlags.AgentMessage == "" {
			agentFlags.AgentMessage = settings.Message
		}
	} else if workspace != "" {
		resolvedWorkspace = workspace
	}

	mode := outputFlags.Resolve(runtime.IsTTY, detectAgentOutputMode(agentFlags))
	attribution := detectAgentMode(agentFlags)
	sl, el := clioutput.BuildBaseLoggers(runtime.Stdout, runtime.Stderr, runtime.ColorMode)
	clioutput.ApplyRenderMode(sl, mode)
	ctx := &clioutput.CommandContext{
		Workspace:     resolvedWorkspace,
		Mode:          mode,
		Stdout:        runtime.Stdout,
		Stderr:        runtime.Stderr,
		NowFunc:       runtime.Now,
		RequestIDFunc: runtime.RequestID,
		IsTTY:         runtime.IsTTY,
		ColorMode:     runtime.ColorMode,
		Theme:         runtime.Theme,
		StdoutLog:     sl,
		StderrLog:     el,
	}

	if runtime.Config == nil {
		return ctx, config.WorkspaceProfile{Name: resolvedWorkspace, TokenType: config.TokenTypeBot}, attribution, nil
	}
	return ctx, resolvedProfile, attribution, nil
}

// ExtractFlags reads the standard persistent output and agent flags from the
// cobra root.
func ExtractFlags(cmd *cobra.Command) (clioutput.OutputFlags, AgentFlags) {
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
		}, AgentFlags{
			Agent:              forceAgent,
			NoAgentAttribution: noAttribution,
			AgentLabel:         agentLabel,
			AgentEmoji:         agentEmoji,
			AgentMessage:       agentMessage,
		}
}

// LocalContext builds a minimal CommandContext for commands that do not
// resolve a workspace profile (e.g. config, manifest). The workspace
// argument labels the context for telemetry.
func LocalContext(cmd *cobra.Command, runtime *RootRuntime, workspace string) *clioutput.CommandContext {
	output, agentFlags := ExtractFlags(cmd)
	mode := output.Resolve(runtime.IsTTY, detectAgentOutputMode(agentFlags))
	return buildLocalContext(runtime, workspace, mode)
}

// LocalContextForceAgent is like LocalContext but forces agent-mode output
// resolution. Used by the `slick agent` subcommands which exist exclusively
// to serve agents.
func LocalContextForceAgent(cmd *cobra.Command, runtime *RootRuntime, workspace string) *clioutput.CommandContext {
	output, _ := ExtractFlags(cmd)
	mode := output.Resolve(runtime.IsTTY, true)
	return buildLocalContext(runtime, workspace, mode)
}

func buildLocalContext(runtime *RootRuntime, workspace string, mode clioutput.RenderMode) *clioutput.CommandContext {
	sl, el := clioutput.BuildBaseLoggers(runtime.Stdout, runtime.Stderr, runtime.ColorMode)
	clioutput.ApplyRenderMode(sl, mode)
	return &clioutput.CommandContext{
		Workspace:     workspace,
		Mode:          mode,
		Stdout:        runtime.Stdout,
		Stderr:        runtime.Stderr,
		NowFunc:       runtime.Now,
		RequestIDFunc: runtime.RequestID,
		StdoutLog:     sl,
		StderrLog:     el,
	}
}

// WriteRuntimeError emits a structured CLI error using a minimal context built
// from the runtime (before a full CommandContext is available).
func WriteRuntimeError(runtime *RootRuntime, err clioutput.CLIError) error {
	mode := clioutput.OutputFlags{}.Resolve(runtime.IsTTY, false)
	sl, el := clioutput.BuildBaseLoggers(runtime.Stdout, runtime.Stderr, runtime.ColorMode)
	clioutput.ApplyRenderMode(sl, mode)
	ctx := &clioutput.CommandContext{
		Workspace:     "default",
		Mode:          mode,
		Stdout:        runtime.Stdout,
		Stderr:        runtime.Stderr,
		NowFunc:       runtime.Now,
		RequestIDFunc: runtime.RequestID,
		StdoutLog:     sl,
		StderrLog:     el,
	}
	return clioutput.WriteCommandError(ctx, err)
}

func profileAttributionSetting(profile config.WorkspaceProfile) *bool {
	if profile.Attribution.Enabled != nil {
		return profile.Attribution.Enabled
	}
	return profile.AgentAttribution
}

func detectAgentOutputMode(flags AgentFlags) bool {
	return agent.Detect(agent.Options{Force: flags.Agent}).Active
}

func detectAgentMode(flags AgentFlags) agent.Attribution {
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
