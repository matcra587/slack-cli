package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	cobracli "github.com/gechr/clib/cli/cobra"
	"github.com/gechr/clib/help"
	"github.com/gechr/clib/theme"
	"github.com/gechr/clog"
	"github.com/gechr/x/human"
	"github.com/gechr/x/shell"
	"github.com/gechr/x/terminal"
	"github.com/matcra587/slack-cli/internal/agent"
	cliagent "github.com/matcra587/slack-cli/internal/cli/agent"
	cliauth "github.com/matcra587/slack-cli/internal/cli/auth"
	clicache "github.com/matcra587/slack-cli/internal/cli/cache"
	clichannel "github.com/matcra587/slack-cli/internal/cli/channel"
	clicompletion "github.com/matcra587/slack-cli/internal/cli/completion"
	cliconfig "github.com/matcra587/slack-cli/internal/cli/config"
	clifile "github.com/matcra587/slack-cli/internal/cli/file"
	clihistory "github.com/matcra587/slack-cli/internal/cli/history"
	climanifest "github.com/matcra587/slack-cli/internal/cli/manifest"
	climessage "github.com/matcra587/slack-cli/internal/cli/message"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	clireaction "github.com/matcra587/slack-cli/internal/cli/reaction"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	slackclient "github.com/matcra587/slack-cli/internal/cli/slackclient"
	clithread "github.com/matcra587/slack-cli/internal/cli/thread"
	clitoken "github.com/matcra587/slack-cli/internal/cli/token"
	cliuser "github.com/matcra587/slack-cli/internal/cli/user"
	cliworkspace "github.com/matcra587/slack-cli/internal/cli/workspace"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/spf13/cobra"
)

type RenderMode = clioutput.RenderMode

const (
	RenderModePlain    = clioutput.RenderModePlain
	RenderModeEnvelope = clioutput.RenderModeEnvelope
	RenderModeCompact  = clioutput.RenderModeCompact
	RenderModeRaw      = clioutput.RenderModeRaw
)

type OutputFlags = clioutput.OutputFlags

const (
	ExitCodeAuthFailure = clioutput.ExitCodeAuthFailure
	ExitCodeNotFound    = clioutput.ExitCodeNotFound
	ExitCodeRateLimit   = clioutput.ExitCodeRateLimit
	ExitCodeValidation  = clioutput.ExitCodeValidation
	ExitCodeServer      = clioutput.ExitCodeServer
	ExitCodeCanceled    = clioutput.ExitCodeCanceled
	ExitCodeTimeout     = clioutput.ExitCodeTimeout
)

const (
	ErrorTypeAuth       = clioutput.ErrorTypeAuth
	ErrorTypeNotFound   = clioutput.ErrorTypeNotFound
	ErrorTypeRateLimit  = clioutput.ErrorTypeRateLimit
	ErrorTypeValidation = clioutput.ErrorTypeValidation
	ErrorTypeServer     = clioutput.ErrorTypeServer
	ErrorTypeCanceled   = clioutput.ErrorTypeCanceled
	ErrorTypeTimeout    = clioutput.ErrorTypeTimeout
)

// CommandContext is the shared per-command rendering state.
type CommandContext = clioutput.CommandContext

// PlainRenderer is implemented by each command data type for plain-mode output.
type PlainRenderer = clioutput.PlainRenderer

// DTO type aliases so existing cmd/slick files compile without changes.
type (
	cliMessage = clioutput.CliMessage
	cliChannel = clioutput.CliChannel
	cliUser    = clioutput.CliUser
)

// Command data type aliases so existing cmd/slick code compiles unchanged.
type (
	sendCommandData     = climessage.SendData
	deleteMessageData   = climessage.DeleteData
	reactionCommandData = clireaction.Data
	reactionResult      = clireaction.Result
	channelInfoData     = clichannel.InfoData
	userInfoData        = cliuser.InfoData
)

// Auth DTO aliases used by tests and output_test.go.
type (
	authWorkspaceData = cliauth.WorkspaceData
	authStatusData    = cliauth.StatusData
)

// DTO converter aliases.
var (
	cliErrorFromSlack = clioutput.CliErrorFromSlack
)

// Slack client aliases — all cmd/slick command files call slackClient(cmd, profile, runtime).
var (
	slackClient = slackclient.Client
)

type RootOptions struct {
	Config    *config.Config
	Workspace string
	Output    OutputFlags
	Agent     AgentFlags
	Stdout    io.Writer
	Stderr    io.Writer
	IsTTY     bool
	ColorMode clog.ColorMode
	Now       func() time.Time
	RequestID func() string
	Theme     *theme.Theme
}

// AgentFlags carries the per-command agent/attribution overrides parsed from
// cobra persistent flags.
type AgentFlags = cliruntime.AgentFlags

// Attribution describes the agent attribution payload for sent messages.
type Attribution = agent.Attribution

// DetectAgentMode computes the attribution payload for the supplied flags.
func DetectAgentMode(flags AgentFlags) Attribution {
	return cliagent.DetectAttribution(flags)
}

// DetectAgentOutputMode reports whether output should be rendered in agent mode.
func DetectAgentOutputMode(flags AgentFlags) bool {
	return cliagent.DetectOutputMode(flags)
}

type RootRuntime = cliruntime.RootRuntime

type RootOption = cliruntime.RootOption

var (
	WithConfig          = cliruntime.WithConfig
	WithConfigPath      = cliruntime.WithConfigPath
	WithCredentialStore = cliruntime.WithCredentialStore
	WithTokenResolver   = cliruntime.WithTokenResolver
	WithSlackBaseURL    = cliruntime.WithSlackBaseURL
	WithIO              = cliruntime.WithIO
	WithTTY             = cliruntime.WithTTY
	WithNow             = cliruntime.WithNow
	WithRequestID       = cliruntime.WithRequestID
	WithURLOpener       = cliruntime.WithURLOpener
	WithOAuthTimeout    = cliruntime.WithOAuthTimeout
)

type TokenResolver = cliruntime.TokenResolver

type TokenResolverFunc = cliruntime.TokenResolverFunc

type EnvTokenResolver = clitoken.EnvTokenResolver

type CredentialTokenResolver = clitoken.CredentialTokenResolver

type SecretReader = clitoken.SecretReader

type SecretReaderFunc = clitoken.SecretReaderFunc

func NewRootCommandWithRuntime(options ...RootOption) (*cobra.Command, *RootRuntime) {
	var rt *RootRuntime
	cmd := NewRootCommand(append([]RootOption{func(r *RootRuntime) { rt = r }}, options...)...)
	return cmd, rt
}

func NewRootCommand(options ...RootOption) *cobra.Command {
	runtime := &RootRuntime{
		ConfigPath:      defaultConfigPath(),
		CredentialStore: config.NewKeyringCredentialStore(),
		SlackBaseURL:    os.Getenv("SLACK_CLI_BASE_URL"),
		OAuthTimeout:    2 * time.Minute,
		Stdin:           os.Stdin,
		Stdout:          os.Stdout,
		Stderr:          os.Stderr,
		IsTTY:           terminal.Is(os.Stdout),
	}
	runtime.Config, runtime.ConfigLoadError = cliruntime.LoadDefaultConfig(runtime.ConfigPath)
	for _, option := range options {
		option(runtime)
	}
	if runtime.TokenResolver == nil {
		runtime.TokenResolver = clitoken.CredentialTokenResolver{
			Store:        runtime.CredentialStore,
			SlackBaseURL: runtime.SlackBaseURL,
			HTTPClient:   runtime.HTTPClient,
			Now:          runtime.Now,
		}
	}

	root := &cobra.Command{
		Use:           "slick",
		Short:         "Slack command line interface",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetIn(runtime.Stdin)
	root.SetOut(runtime.Stdout)
	root.SetErr(runtime.Stderr)
	clicompletion.Setup(root, runtime)

	clog.SetEnvPrefix("SLICK")
	theme.SetEnvPrefix("SLICK")
	getTheme := sync.OnceValue(theme.Default)
	th := getTheme()
	renderer := help.NewRenderer(th)
	runtime.Theme = th
	runtime.HelpRenderer = renderer
	root.SetHelpFunc(cobracli.HelpFunc(renderer, cobracli.Sections))
	flags := root.PersistentFlags()
	flags.StringP("workspace", "w", "", "Workspace profile")
	flags.BoolP("json", "j", false, "Force JSON output")
	flags.BoolP("plain", "P", false, "Force plain text output")
	flags.BoolP("compact", "k", false, "Output command data without envelope")
	flags.BoolP("raw", "X", false, "Output Slack-native data")
	flags.BoolP("agent", "a", false, "Force agent mode")
	flags.BoolP("no-agent-attribution", "z", false, "Disable agent attribution for this command")
	flags.StringP("agent-label", "G", "", "Override agent attribution label")
	flags.StringP("agent-emoji", "Y", "", "Override agent attribution emoji")
	flags.StringP("agent-message", "O", "", "Override agent attribution message")
	flags.BoolP("no-throttle", "Q", false, "Disable proactive Slack API throttling")
	flags.BoolP("debug", "D", false, "Enable debug-level output")
	flags.DurationP("timeout", "I", 30*time.Second, "Slack API call timeout")
	flags.TextVarP(&runtime.ColorMode, "color", "V", clog.ColorAuto, "Color mode (auto, always, never)")
	root.MarkFlagsMutuallyExclusive("json", "plain", "compact", "raw")
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		if debug, _ := cmd.Root().PersistentFlags().GetBool("debug"); debug {
			clog.SetVerbose(true)
		}
		if timeout, err := cmd.Root().PersistentFlags().GetDuration("timeout"); err == nil && timeout > 0 {
			runtime.Timeout = timeout
			var timeoutCtx context.Context
			timeoutCtx, runtime.CancelTimeout = context.WithTimeout(cmd.Context(), timeout)
			cmd.SetContext(timeoutCtx)
		}
		if err := cmd.ValidateFlagGroups(); err != nil {
			sl, el := buildBaseLoggers(runtime.Stdout, runtime.Stderr, runtime.ColorMode)
			applyRenderMode(sl, RenderModeEnvelope)
			ctx := &CommandContext{
				Workspace:     "default",
				Mode:          RenderModeEnvelope,
				Stdout:        runtime.Stdout,
				Stderr:        runtime.Stderr,
				NowFunc:       runtime.Now,
				RequestIDFunc: runtime.RequestID,
				StdoutLog:     sl,
				StderrLog:     el,
			}
			return writeCommandError(ctx, validationCLIError(err.Error()))
		}
		return nil
	}

	root.AddGroup(
		&cobra.Group{ID: "messaging", Title: "Messaging Commands:"},
		&cobra.Group{ID: "discovery", Title: "Discovery Commands:"},
		&cobra.Group{ID: "admin", Title: "Auth & Config Commands:"},
		&cobra.Group{ID: "meta", Title: "Agent & Tools:"},
	)

	msgCmd := climessage.NewCommand(runtime)
	msgCmd.GroupID = "messaging"
	root.AddCommand(msgCmd)

	historyCmd := clihistory.NewCommand(runtime)
	historyCmd.GroupID = "messaging"
	root.AddCommand(historyCmd)

	replyCmd := clithread.NewCommand(runtime)
	replyCmd.GroupID = "messaging"
	root.AddCommand(replyCmd)

	reactCmd := clireaction.NewCommand(runtime)
	reactCmd.GroupID = "messaging"
	root.AddCommand(reactCmd)

	statusCmd := newStatusCommand(runtime)
	statusCmd.GroupID = "messaging"
	root.AddCommand(statusCmd)

	lookupCmd := clichannel.NewCommand(runtime)
	lookupCmd.GroupID = "discovery"
	root.AddCommand(lookupCmd)

	cacheCmd := clicache.NewCommand(runtime)
	cacheCmd.GroupID = "discovery"
	root.AddCommand(cacheCmd)

	authCmd := cliauth.NewCommand(runtime)
	authCmd.GroupID = "admin"
	root.AddCommand(authCmd)

	configCmd := cliconfig.NewCommand(runtime)
	configCmd.GroupID = "admin"
	root.AddCommand(configCmd)

	workspaceCmd := cliworkspace.NewCommand(runtime)
	workspaceCmd.GroupID = "admin"
	root.AddCommand(workspaceCmd)

	manifestCmd := climanifest.NewCommand(runtime)
	manifestCmd.GroupID = "admin"
	root.AddCommand(manifestCmd)

	agentCmd := cliagent.NewCommand(runtime)
	agentCmd.GroupID = "meta"
	root.AddCommand(agentCmd)

	fileCmd := clifile.NewCommand(runtime)
	fileCmd.GroupID = "meta"
	root.AddCommand(fileCmd)

	versionCmd := newVersionCommand(runtime)
	versionCmd.GroupID = "meta"
	root.AddCommand(versionCmd)
	clicompletion.ExtendSlackMetadata(root)
	clicompletion.AddCommand(root)

	return root
}

func defaultConfigPath() string {
	if path := os.Getenv("SLICK_CONFIG"); path != "" {
		return human.ExpandPath(path)
	}
	if path := os.Getenv("SLACK_CLI_CONFIG"); path != "" {
		return human.ExpandPath(path)
	}
	dir, err := shell.XDGConfigHome()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "slick", "config.toml")
}

func rootOptionsFromCommand(cmd *cobra.Command, runtime *RootRuntime) RootOptions {
	flags := cmd.Root().PersistentFlags()
	workspace, _ := flags.GetString("workspace")
	jsonMode, _ := flags.GetBool("json")
	plain, _ := flags.GetBool("plain")
	compact, _ := flags.GetBool("compact")
	raw, _ := flags.GetBool("raw")
	agent, _ := flags.GetBool("agent")
	noAttribution, _ := flags.GetBool("no-agent-attribution")
	agentLabel, _ := flags.GetString("agent-label")
	agentEmoji, _ := flags.GetString("agent-emoji")
	agentMessage, _ := flags.GetString("agent-message")

	return RootOptions{
		Config:    runtime.Config,
		Workspace: workspace,
		Output: OutputFlags{
			JSON:    jsonMode,
			Plain:   plain,
			Compact: compact,
			Raw:     raw,
		},
		Agent: AgentFlags{
			Agent:              agent,
			NoAgentAttribution: noAttribution,
			AgentLabel:         agentLabel,
			AgentEmoji:         agentEmoji,
			AgentMessage:       agentMessage,
		},
		Stdout:    runtime.Stdout,
		Stderr:    runtime.Stderr,
		IsTTY:     runtime.IsTTY,
		ColorMode: runtime.ColorMode,
		Now:       runtime.Now,
		RequestID: runtime.RequestID,
		Theme:     runtime.Theme,
	}
}

func commandContext(cmd *cobra.Command, runtime *RootRuntime) (*CommandContext, config.WorkspaceProfile, Attribution, error) {
	return cliruntime.CommandContext(cmd, runtime)
}

func NewCommandContext(opts RootOptions) (*CommandContext, Attribution, error) {
	workspace := "default"
	agentFlags := opts.Agent
	if opts.Config != nil {
		profile, err := opts.Config.ResolveWorkspace(opts.Workspace)
		if err != nil {
			return nil, Attribution{}, err
		}
		workspace = profile.Name
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
	} else if opts.Workspace != "" {
		workspace = opts.Workspace
	}

	mode := opts.Output.Resolve(opts.IsTTY, DetectAgentOutputMode(agentFlags))
	attribution := DetectAgentMode(agentFlags)
	stdoutLog, stderrLog := buildBaseLoggers(opts.Stdout, opts.Stderr, opts.ColorMode)
	applyRenderMode(stdoutLog, mode)
	return &CommandContext{
		Workspace:     workspace,
		Mode:          mode,
		Stdout:        opts.Stdout,
		Stderr:        opts.Stderr,
		NowFunc:       opts.Now,
		RequestIDFunc: opts.RequestID,
		IsTTY:         opts.IsTTY,
		ColorMode:     opts.ColorMode,
		Theme:         opts.Theme,
		StdoutLog:     stdoutLog,
		StderrLog:     stderrLog,
	}, attribution, nil
}

var buildBaseLoggers = clioutput.BuildBaseLoggers

var applyRenderMode = clioutput.ApplyRenderMode

func profileAttributionSetting(profile config.WorkspaceProfile) *bool {
	if profile.Attribution.Enabled != nil {
		return profile.Attribution.Enabled
	}
	return profile.AgentAttribution
}

type Envelope = clioutput.Envelope

type EnvelopeMeta = clioutput.EnvelopeMeta

type Pagination = clioutput.Pagination

type CLIError = clioutput.CLIError

type CommandError = clioutput.CommandError

var validationCLIError = clioutput.ValidationCLIError

var authCLIError = clioutput.AuthCLIError

func writeCommandError(ctx *CommandContext, err CLIError) error {
	return clioutput.WriteCommandError(ctx, err)
}

func writeRuntimeError(runtime *RootRuntime, err CLIError) error {
	return cliruntime.WriteRuntimeError(runtime, err)
}

func truncateText(value string, limit int) string {
	return clioutput.TruncateText(value, limit)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root, runtime := NewRootCommandWithRuntime()
	defer func() {
		if runtime.CancelTimeout != nil {
			runtime.CancelTimeout()
		}
	}()
	root.SetContext(ctx)
	if err := root.ExecuteContext(ctx); err != nil {
		var commandErr CommandError
		if errors.As(err, &commandErr) {
			os.Exit(commandErr.CLIError.ExitCode)
		}
		mode := outputFlagsFromCommand(root).Resolve(terminal.Is(os.Stdout), false)
		sl, el := buildBaseLoggers(os.Stdout, os.Stderr, clog.ColorAuto)
		applyRenderMode(sl, mode)
		cmdCtx := &CommandContext{
			Workspace: "default",
			Mode:      mode,
			Stdout:    os.Stdout,
			Stderr:    os.Stderr,
			StdoutLog: sl,
			StderrLog: el,
		}
		os.Exit(cmdCtx.WriteError(validationCLIError(err.Error())))
	}
}

func outputFlagsFromCommand(cmd *cobra.Command) OutputFlags {
	flags := cmd.PersistentFlags()
	jsonMode, _ := flags.GetBool("json")
	plain, _ := flags.GetBool("plain")
	compact, _ := flags.GetBool("compact")
	raw, _ := flags.GetBool("raw")
	return OutputFlags{JSON: jsonMode, Plain: plain, Compact: compact, Raw: raw}
}
