package runtime

import (
	"fmt"

	"github.com/matcra587/slack-cli/internal/agent"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// presenceFlag is a pflag.Value used for switches that must be valueless.
// `--attribution` and `--no-attribution` exist solely to signal direction;
// any `=value` form would either silently invert the documented direction
// (`--attribution=false`) or duplicate the bare form (`--attribution=true`).
// Both are rejected.
//
// The trick: pflag substitutes NoOptDefVal when the user passes the flag
// bare, so RegisterAttributionFlags sets NoOptDefVal to presenceSentinel
// (a string the user can't type). Set accepts only that sentinel; every
// real `=value` form fails parsing with a usage error.
type presenceFlag bool

const presenceSentinel = "\x00slick-presence-flag\x00"

func (p *presenceFlag) String() string {
	if p != nil && *p {
		return "true"
	}
	return "false"
}

func (p *presenceFlag) Set(value string) error {
	if value != presenceSentinel {
		return fmt.Errorf("presence-only switch: pass the flag bare instead of %q", value)
	}
	*p = true
	return nil
}

func (p *presenceFlag) Type() string { return "bool" }

// IsBoolFlag signals to pflag that the flag's bare form is valid (no value
// required after the flag name), so `--attribution --dry-run` parses cleanly
// rather than consuming `--dry-run` as the value.
func (p *presenceFlag) IsBoolFlag() bool { return true }

// AttributionFlags carries the per-command attribution overrides parsed from
// cobra flags. Attribution is tri-state: nil defers to env detection and the
// resolved profile default; a non-nil bool forces the per-command choice.
// ProfileAttribution comes from the resolved workspace profile and reflects
// the persistent default.
type AttributionFlags struct {
	Attribution        *bool
	Label              string
	Emoji              string
	Message            string
	ProfileAttribution *bool
}

// RegisterAttributionFlags adds the local attribution flags onto a mutating
// command. Call this from each command that actually emits an attribution
// block (message send/edit, reply, file upload). --attribution and
// --no-attribution are mutually exclusive; passing either is an explicit
// per-command override that wins over both env detection and profile defaults.
func RegisterAttributionFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	on := new(presenceFlag)
	off := new(presenceFlag)
	onFlag := flags.VarPF(on, "attribution", "", "Force attribution on for this command (overrides config and env)")
	offFlag := flags.VarPF(off, "no-attribution", "z", "Disable attribution for this command (overrides config and env)")
	// NoOptDefVal tells pflag the flag is valueless. Set to presenceSentinel
	// (not "true") so Set() can distinguish the bare form from any explicit
	// `=value` form: bare → pflag invokes Set(sentinel) → accepted; explicit
	// → pflag invokes Set(userValue) → rejected by presenceFlag.Set.
	onFlag.NoOptDefVal = presenceSentinel
	offFlag.NoOptDefVal = presenceSentinel
	flags.String("attribution-label", "", "Override attribution label")
	flags.String("attribution-emoji", "", "Override attribution emoji")
	flags.String("attribution-message", "", "Override attribution message")
	cmd.MarkFlagsMutuallyExclusive("attribution", "no-attribution")
}

// CommandContextFromCmd resolves the rendering context, workspace profile, and
// attribution state from a cobra command and runtime. It is the canonical
// entry point for command handlers in internal packages.
func CommandContext(cmd *cobra.Command, runtime *RootRuntime) (*clioutput.CommandContext, config.WorkspaceProfile, agent.Attribution, error) {
	rootFlags := cmd.Root().PersistentFlags()
	flagWorkspace, _ := rootFlags.GetString("workspace")
	// The --workspace flag arrives raw from cobra; trim before the
	// resolver sees it so a stray space doesn't manifest as "not found".
	// Logger isn't built yet at this point, so the trim is silent here;
	// auth.go's command handlers log via TrimInputName when they take
	// the same kind of input through their own flags/args.
	workspace := clioutput.TrimInputName(nil, "workspace", flagWorkspace)
	output, _ := rootFlags.GetString("output")
	attrFlags := readAttributionFlags(cmd)

	outputFlags := clioutput.OutputFlags{Output: output}

	if runtime.ConfigLoadError != nil {
		mode := outputFlags.Resolve(runtime.IsTTY, detectAgentOutputMode())
		sl, el := clioutput.BuildBaseLoggers(runtime.Stdout, runtime.Stderr, runtime.ColorMode)
		clioutput.ApplyRenderMode(sl, mode)
		ctx := &clioutput.CommandContext{
			Workspace:     "default",
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
		attrFlags.ProfileAttribution = profileAttributionSetting(profile)
		settings := profile.AgentSettings()
		if attrFlags.Label == "" {
			attrFlags.Label = settings.Label
		}
		if attrFlags.Emoji == "" {
			attrFlags.Emoji = settings.Emoji
		}
		if attrFlags.Message == "" {
			attrFlags.Message = settings.Message
		}
	} else if workspace != "" {
		resolvedWorkspace = workspace
	}

	mode := outputFlags.Resolve(runtime.IsTTY, detectAgentOutputMode())
	attribution := detectAttribution(attrFlags)
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

// ExtractFlags reads the standard output flag and attribution flags from the
// cobra command. Output comes from root persistent flags; attribution flags
// come from the local command (and return zero values for commands that do
// not register them).
func ExtractFlags(cmd *cobra.Command) (clioutput.OutputFlags, AttributionFlags) {
	output, _ := cmd.Root().PersistentFlags().GetString("output")
	return clioutput.OutputFlags{Output: output}, readAttributionFlags(cmd)
}

func readAttributionFlags(cmd *cobra.Command) AttributionFlags {
	flags := cmd.LocalFlags()
	return AttributionFlags{
		Attribution: readAttributionToggle(flags),
		Label:       localString(flags, "attribution-label"),
		Emoji:       localString(flags, "attribution-emoji"),
		Message:     localString(flags, "attribution-message"),
	}
}

// readAttributionToggle folds --attribution and --no-attribution into a
// single tri-state. Returns nil when neither flag was set, so the caller
// defers to env detection and profile defaults. Both flags are presence-only
// switches (see presenceFlag), so Changed alone is the override signal — the
// value form `--attribution=false` is rejected at parse time. Cobra's mutex
// registered in RegisterAttributionFlags prevents both being set at once.
func readAttributionToggle(fs *pflag.FlagSet) *bool {
	if f := fs.Lookup("attribution"); f != nil && f.Changed {
		t := true
		return &t
	}
	if f := fs.Lookup("no-attribution"); f != nil && f.Changed {
		off := false
		return &off
	}
	return nil
}

func localString(fs *pflag.FlagSet, name string) string {
	if fs.Lookup(name) == nil {
		return ""
	}
	v, _ := fs.GetString(name)
	return v
}

// LocalContext builds a minimal CommandContext for commands that do not
// resolve a workspace profile (e.g. config, manifest). The workspace
// argument labels the context for telemetry.
func LocalContext(cmd *cobra.Command, runtime *RootRuntime, workspace string) *clioutput.CommandContext {
	output, _ := ExtractFlags(cmd)
	mode := output.Resolve(runtime.IsTTY, detectAgentOutputMode())
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
		IsTTY:         runtime.IsTTY,
		ColorMode:     runtime.ColorMode,
		Theme:         runtime.Theme,
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
		IsTTY:         runtime.IsTTY,
		ColorMode:     runtime.ColorMode,
		Theme:         runtime.Theme,
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

func detectAgentOutputMode() bool {
	return agent.Detect(agent.Options{}).Active
}

func detectAttribution(flags AttributionFlags) agent.Attribution {
	detection := agent.Detect(agent.Options{
		Attribution:        flags.Attribution,
		ProfileAttribution: flags.ProfileAttribution,
		Label:              flags.Label,
		Emoji:              flags.Emoji,
		Message:            flags.Message,
	})
	return agent.NewAttribution(detection, agent.Options{
		Label:   flags.Label,
		Emoji:   flags.Emoji,
		Message: flags.Message,
	})
}
