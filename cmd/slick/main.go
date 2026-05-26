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
	"github.com/google/uuid"
	"github.com/matcra587/slack-cli/internal/agent"
	cliagent "github.com/matcra587/slack-cli/internal/cli/agent"
	cliauth "github.com/matcra587/slack-cli/internal/cli/auth"
	clicache "github.com/matcra587/slack-cli/internal/cli/cache"
	clichannel "github.com/matcra587/slack-cli/internal/cli/channel"
	clicompletion "github.com/matcra587/slack-cli/internal/cli/completion"
	cliconfig "github.com/matcra587/slack-cli/internal/cli/config"
	clifile "github.com/matcra587/slack-cli/internal/cli/file"
	clihealth "github.com/matcra587/slack-cli/internal/cli/health"
	clihistory "github.com/matcra587/slack-cli/internal/cli/history"
	climanifest "github.com/matcra587/slack-cli/internal/cli/manifest"
	climessage "github.com/matcra587/slack-cli/internal/cli/message"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	clireaction "github.com/matcra587/slack-cli/internal/cli/reaction"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	clistatus "github.com/matcra587/slack-cli/internal/cli/status"
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
	cliMessage = clioutput.Message
	cliChannel = clioutput.Channel
	cliUser    = clioutput.User
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

type RootOptions struct {
	Config      *config.Config
	Workspace   string
	Output      OutputFlags
	Attribution AttributionFlags
	Stdout      io.Writer
	Stderr      io.Writer
	IsTTY       bool
	ColorMode   clog.ColorMode
	Now         func() time.Time
	RequestID   func() string
	Theme       *theme.Theme
}

// AttributionFlags carries the per-command attribution overrides parsed from
// the local mutating-command flags.
type AttributionFlags = cliruntime.AttributionFlags

// Attribution describes the attribution payload for sent messages.
type Attribution = agent.Attribution

// DetectAttribution computes the attribution payload for the supplied flags.
func DetectAttribution(flags AttributionFlags) Attribution {
	return cliagent.DetectAttribution(flags)
}

// DetectAgentOutputMode reports whether output should be rendered in agent mode.
func DetectAgentOutputMode() bool {
	return cliagent.DetectOutputMode()
}

type RootRuntime = cliruntime.RootRuntime

type RootOption = cliruntime.RootOption

var (
	WithConfig             = cliruntime.WithConfig
	WithConfigPath         = cliruntime.WithConfigPath
	WithCredentialStore    = cliruntime.WithCredentialStore
	WithTokenResolver      = cliruntime.WithTokenResolver
	WithSlackBaseURL       = cliruntime.WithSlackBaseURL
	WithSlackStatusBaseURL = cliruntime.WithSlackStatusBaseURL
	WithIO                 = cliruntime.WithIO
	WithTTY                = cliruntime.WithTTY
	WithNow                = cliruntime.WithNow
	WithRequestID          = cliruntime.WithRequestID
	WithURLOpener          = cliruntime.WithURLOpener
	WithOAuthTimeout       = cliruntime.WithOAuthTimeout
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
	if runtime.RequestID == nil {
		// Stable per-process UUID so every envelope from one invocation
		// shares the same request_id for log correlation.
		runtime.RequestID = sync.OnceValue(uuid.NewString)
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
	flags.StringP("output", "o", clioutput.OutputAuto, "Output format: auto, human, json, compact")
	flags.BoolP("dry-run", "n", false, "Preview mutating commands without changing Slack")
	flags.BoolP("no-throttle", "Q", false, "Disable proactive Slack API throttling")
	flags.BoolP("debug", "D", false, "Enable debug-level output")
	flags.DurationP("timeout", "I", 30*time.Second, "Slack API call timeout")
	flags.TextVarP(&runtime.ColorMode, "color", "V", clog.ColorAuto, "Color mode (auto, always, never)")
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		if output, _ := cmd.Root().PersistentFlags().GetString("output"); output != "" {
			if err := clioutput.ValidateOutputMode(output); err != nil {
				return cliruntime.WriteRuntimeError(runtime, validationCLIError(err.Error()))
			}
		}
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
			return cliruntime.WriteRuntimeError(runtime, validationCLIError(err.Error()))
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

	statusCmd := clistatus.NewCommand(runtime)
	statusCmd.GroupID = "messaging"
	root.AddCommand(statusCmd)

	healthCmd := clihealth.NewCommand(runtime)
	healthCmd.GroupID = "meta"
	root.AddCommand(healthCmd)

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

func NewCommandContext(opts RootOptions) (*CommandContext, Attribution, error) {
	workspace := "default"
	teamID := ""
	attrFlags := opts.Attribution
	if opts.Config != nil {
		profile, err := opts.Config.ResolveWorkspace(opts.Workspace)
		if err != nil {
			return nil, Attribution{}, err
		}
		workspace = profile.Name
		teamID = profile.TeamID
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
	} else if opts.Workspace != "" {
		workspace = opts.Workspace
	}

	mode := opts.Output.Resolve(opts.IsTTY, DetectAgentOutputMode())
	attribution := DetectAttribution(attrFlags)
	stdoutLog, stderrLog := buildBaseLoggers(opts.Stdout, opts.Stderr, opts.ColorMode)
	applyRenderMode(stdoutLog, mode)
	return &CommandContext{
		Workspace:     workspace,
		TeamID:        teamID,
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
		isTTY := terminal.Is(os.Stdout)
		mode := outputFlagsFromCommand(root).Resolve(isTTY, false)
		sl, el := buildBaseLoggers(os.Stdout, os.Stderr, clog.ColorAuto)
		applyRenderMode(sl, mode)
		cmdCtx := &CommandContext{
			Workspace: "default",
			Mode:      mode,
			Stdout:    os.Stdout,
			Stderr:    os.Stderr,
			IsTTY:     isTTY,
			ColorMode: clog.ColorAuto,
			Theme:     runtime.Theme,
			StdoutLog: sl,
			StderrLog: el,
		}
		os.Exit(cmdCtx.WriteError(validationCLIError(err.Error())))
	}
}

func outputFlagsFromCommand(cmd *cobra.Command) OutputFlags {
	flags := cmd.PersistentFlags()
	output, _ := flags.GetString("output")
	return OutputFlags{Output: output}
}
