package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	cobracli "github.com/gechr/clib/cli/cobra"
	"github.com/gechr/clib/help"
	"github.com/gechr/clib/theme"
	"github.com/gechr/clog"
	xfilepath "github.com/gechr/x/filepath"
	"github.com/gechr/x/terminal"
	"github.com/google/uuid"
	"github.com/matcra587/slack-cli/internal/agent"
	"github.com/matcra587/slack-cli/internal/agenthelp"
	cliagent "github.com/matcra587/slack-cli/internal/cli/agent"
	cliauth "github.com/matcra587/slack-cli/internal/cli/auth"
	clibookmark "github.com/matcra587/slack-cli/internal/cli/bookmark"
	clicache "github.com/matcra587/slack-cli/internal/cli/cache"
	clichannel "github.com/matcra587/slack-cli/internal/cli/channel"
	"github.com/matcra587/slack-cli/internal/cli/clitheme"
	clicompletion "github.com/matcra587/slack-cli/internal/cli/completion"
	cliconfig "github.com/matcra587/slack-cli/internal/cli/config"
	clidnd "github.com/matcra587/slack-cli/internal/cli/dnd"
	clifile "github.com/matcra587/slack-cli/internal/cli/file"
	clihealth "github.com/matcra587/slack-cli/internal/cli/health"
	clihistory "github.com/matcra587/slack-cli/internal/cli/history"
	climanifest "github.com/matcra587/slack-cli/internal/cli/manifest"
	climessage "github.com/matcra587/slack-cli/internal/cli/message"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	clipin "github.com/matcra587/slack-cli/internal/cli/pin"
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
		Args: func(cmd *cobra.Command, args []string) error {
			if flag := cmd.Flags().Lookup("@complete"); flag != nil && flag.Changed {
				return nil
			}
			return cobra.NoArgs(cmd, args)
		},
	}
	root.RunE = func(cmd *cobra.Command, _ []string) error {
		ctx := cliruntime.LocalContext(cmd, runtime, "default")
		if ctx.Mode == clioutput.RenderModePlain {
			return cmd.Help()
		}
		return writeDiscovery(ctx, "slick", cmd.Root())
	}
	root.SetIn(runtime.Stdin)
	root.SetOut(runtime.Stdout)
	root.SetErr(runtime.Stderr)
	clicompletion.Setup(root, runtime)

	clog.SetEnvPrefix("SLICK")
	theme.SetEnvPrefix("SLICK")
	getTheme := sync.OnceValue(clitheme.Default)
	th := getTheme()
	renderer := help.NewRenderer(th)
	runtime.Theme = th
	runtime.HelpRenderer = renderer
	root.SetHelpFunc(cobracli.HelpFunc(renderer, cobracli.Sections))
	flags := root.PersistentFlags()
	flags.StringP("workspace", "w", "", "Workspace profile")
	flags.StringP("output", "o", clioutput.OutputAuto, "Output format: auto, human, json, compact")
	flags.BoolP("dry-run", "n", false, "Preview mutations without changing Slack or local state")
	flags.BoolP("no-throttle", "Q", false, "Disable proactive Slack API throttling")
	flags.BoolP("debug", "D", false, "Enable debug-level output")
	flags.DurationP("timeout", "I", 30*time.Second, "Slack API call timeout")
	flags.TextVarP(&runtime.ColorMode, "color", "V", clog.ColorAuto, "Color mode (auto, always, never)")
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		runtime.OutputMode, _ = cmd.Root().PersistentFlags().GetString("output")
		runtime.WorkspaceName, _ = cmd.Root().PersistentFlags().GetString("workspace")
		runtime.CommandName = strings.TrimPrefix(strings.ReplaceAll(cmd.CommandPath(), " ", "."), "slick.")
		if output, _ := cmd.Root().PersistentFlags().GetString("output"); output != "" {
			if err := clioutput.ValidateOutputMode(output); err != nil {
				return cliruntime.WriteRuntimeError(runtime, validationCLIError(err.Error()))
			}
		}
		if err := validateCompletionPlacement(cmd); err != nil {
			return cliruntime.WriteRuntimeError(runtime, validationCLIError(err.Error()))
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
	for _, cmd := range []*cobra.Command{clidnd.NewCommand(runtime), clipin.NewCommand(runtime), clibookmark.NewCommand(runtime)} {
		cmd.GroupID = "messaging"
		root.AddCommand(cmd)
	}

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

	fileCmd := clifile.NewCommand(runtime)
	fileCmd.GroupID = "meta"
	root.AddCommand(fileCmd)

	versionCmd := newVersionCommand(runtime)
	versionCmd.GroupID = "meta"
	root.AddCommand(versionCmd)
	agentCmd := cliagent.NewCommand(runtime)
	agentCmd.GroupID = "meta"
	root.AddCommand(agentCmd)
	clicompletion.ExtendSlackMetadata(root)
	clicompletion.AddCommand(root, runtime)
	configureGroupDiscovery(root, runtime)
	protectHelpOutput(root, runtime)

	return root
}

func defaultConfigPath() string {
	if path := os.Getenv("SLICK_CONFIG"); path != "" {
		return xfilepath.Expand(path)
	}
	if path := os.Getenv("SLACK_CLI_CONFIG"); path != "" {
		return xfilepath.Expand(path)
	}
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".config")
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
	// Return write failures through the output contract instead of dying on SIGPIPE.
	signal.Ignore(syscall.SIGPIPE)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root, runtime := NewRootCommandWithRuntime()
	defer func() {
		if runtime.CancelTimeout != nil {
			runtime.CancelTimeout()
		}
	}()
	root.SetContext(ctx)
	runtime.OutputMode = failureOutputMode(root, os.Args[1:])
	runtime.WorkspaceName = failureStringFlag(root, os.Args[1:], "workspace", "w", "")
	command, err := executeRoot(ctx, root, runtime)
	if err != nil {
		if commandErr, ok := errors.AsType[CommandError](err); ok {
			os.Exit(commandErr.CLIError.ExitCode)
		}
		if command != nil {
			runtime.CommandName = strings.TrimPrefix(strings.ReplaceAll(command.CommandPath(), " ", "."), "slick.")
		}
		commandErr := cliruntime.WriteRuntimeError(runtime, validationCLIError(err.Error()))
		if classified, ok := errors.AsType[CommandError](commandErr); ok {
			os.Exit(classified.CLIError.ExitCode)
		}
		os.Exit(ExitCodeValidation)
	}
}

// failureOutputMode resolves the global selector before Cobra can fail parsing.
// Known value-taking flags consume their next token so message text is not
// mistaken for an output selector. A literal -- terminates flag processing.
func failureOutputMode(root *cobra.Command, args []string) string {
	return failureStringFlag(root, args, "output", "o", clioutput.OutputAuto)
}

func failureStringFlag(root *cobra.Command, args []string, longName, shortName, defaultValue string) string {
	command, _, _ := root.Find(args)
	if command == nil {
		command = root
	}
	selected := defaultValue
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if flagArg, long := strings.CutPrefix(arg, "--"); long {
			name, value, attached := strings.Cut(flagArg, "=")
			flag := command.Flags().Lookup(name)
			if flag == nil {
				flag = root.PersistentFlags().Lookup(name)
			}
			if flag != nil && !attached && flag.NoOptDefVal == "" {
				if i+1 >= len(args) {
					continue
				}
				i++
				value = args[i]
			}
			if name == longName {
				selected = value
			}
		} else if strings.HasPrefix(arg, "-") && len(arg) > 1 {
			for j := 1; j < len(arg); j++ {
				short := arg[j : j+1]
				flag := command.Flags().ShorthandLookup(short)
				if flag == nil {
					flag = root.PersistentFlags().ShorthandLookup(short)
				}
				if flag == nil {
					break
				}
				if flag.NoOptDefVal != "" {
					continue
				}
				tail := arg[j+1:]
				value := strings.TrimPrefix(tail, "=")
				if tail == "" {
					if i+1 >= len(args) {
						break
					}
					i++
					value = args[i]
				}
				if short == shortName {
					selected = value
				}
				break
			}
		}
	}
	return selected
}

// configureGroupDiscovery keeps explicit machine selection meaningful on groups.
func configureGroupDiscovery(root *cobra.Command, runtime *RootRuntime) {
	var visit func(*cobra.Command)
	visit = func(parent *cobra.Command) {
		for _, child := range parent.Commands() {
			visit(child)
			if !child.HasSubCommands() {
				continue
			}
			fallback := child.RunE
			if child.Args == nil {
				child.Args = cobra.NoArgs
			}
			child.RunE = func(cmd *cobra.Command, args []string) error {
				ctx := cliruntime.LocalContext(cmd, runtime, "default")
				if ctx.Mode == clioutput.RenderModePlain {
					if fallback != nil {
						return fallback(cmd, args)
					}
					return cmd.Help()
				}
				command := strings.TrimPrefix(strings.ReplaceAll(cmd.CommandPath(), " ", "."), "slick.")
				return writeDiscovery(ctx, command, cmd)
			}
		}
	}
	visit(root)
}

func writeDiscovery(ctx *CommandContext, command string, cmd *cobra.Command) error {
	tree := agenthelp.GenerateSchema(cmd)
	raw, err := json.Marshal(tree.StripShapes())
	if err != nil {
		return err
	}
	return ctx.WriteResult(command, cliagent.SchemaData{Schema: raw})
}
