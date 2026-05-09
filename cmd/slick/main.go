package main

import (
	"context"
	"encoding/json"
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
	termansi "github.com/gechr/x/ansi"
	"github.com/gechr/x/human"
	"github.com/gechr/x/shell"
	"github.com/gechr/x/terminal"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	clitoken "github.com/matcra587/slack-cli/internal/cli/token"
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

type CommandContext struct {
	Workspace string
	Mode      RenderMode
	Stdout    io.Writer
	Stderr    io.Writer
	Now       func() time.Time
	RequestID func() string
	ColorMode clog.ColorMode
	IsTTY     bool
	Theme     *theme.Theme
	stdoutLog *clog.Logger
	stderrLog *clog.Logger
}

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

var runtimeEnvToken = clitoken.RuntimeEnvToken

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

	root := &cobra.Command{
		Use:           "slick",
		Short:         "Slack command line interface",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetIn(runtime.Stdin)
	root.SetOut(runtime.Stdout)
	root.SetErr(runtime.Stderr)
	setupClibCompletion(root, runtime)

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
				Workspace: "default",
				Mode:      RenderModeEnvelope,
				Stdout:    runtime.Stdout,
				Stderr:    runtime.Stderr,
				Now:       runtime.Now,
				RequestID: runtime.RequestID,
				stdoutLog: sl,
				stderrLog: el,
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

	msgCmd := newMessageCommand(runtime)
	msgCmd.GroupID = "messaging"
	root.AddCommand(msgCmd)

	historyCmd := newHistoryCommand(runtime)
	historyCmd.GroupID = "messaging"
	root.AddCommand(historyCmd)

	replyCmd := newReplyCommand(runtime)
	replyCmd.GroupID = "messaging"
	root.AddCommand(replyCmd)

	reactCmd := newReactCommand(runtime)
	reactCmd.GroupID = "messaging"
	root.AddCommand(reactCmd)

	statusCmd := newStatusCommand(runtime)
	statusCmd.GroupID = "messaging"
	root.AddCommand(statusCmd)

	lookupCmd := newLookupCommand(runtime)
	lookupCmd.GroupID = "discovery"
	root.AddCommand(lookupCmd)

	cacheCmd := newCacheCommand(runtime)
	cacheCmd.GroupID = "discovery"
	root.AddCommand(cacheCmd)

	authCmd := newAuthCommand(runtime)
	authCmd.GroupID = "admin"
	root.AddCommand(authCmd)

	configCmd := newConfigCommand(runtime)
	configCmd.GroupID = "admin"
	root.AddCommand(configCmd)

	workspaceCmd := newWorkspaceCommand(runtime)
	workspaceCmd.GroupID = "admin"
	root.AddCommand(workspaceCmd)

	manifestCmd := newManifestCommand(runtime)
	manifestCmd.GroupID = "admin"
	root.AddCommand(manifestCmd)

	agentCmd := newAgentCommand(runtime)
	agentCmd.GroupID = "meta"
	root.AddCommand(agentCmd)

	fileCmd := newFileCommand(runtime)
	fileCmd.GroupID = "meta"
	root.AddCommand(fileCmd)

	versionCmd := newVersionCommand(runtime)
	versionCmd.GroupID = "meta"
	root.AddCommand(versionCmd)
	extendSlackCompletionMetadata(root)
	addClibCompletionCommand(root)

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
	opts := rootOptionsFromCommand(cmd, runtime)
	if runtime.ConfigLoadError != nil {
		mode := opts.Output.Resolve(runtime.IsTTY, DetectAgentOutputMode(opts.Agent))
		sl, el := buildBaseLoggers(runtime.Stdout, runtime.Stderr, runtime.ColorMode)
		applyRenderMode(sl, mode)
		ctx := &CommandContext{
			Workspace: "default",
			Mode:      mode,
			Stdout:    runtime.Stdout,
			Stderr:    runtime.Stderr,
			Now:       runtime.Now,
			RequestID: runtime.RequestID,
			Theme:     runtime.Theme,
			stdoutLog: sl,
			stderrLog: el,
		}
		return ctx, config.WorkspaceProfile{}, Attribution{}, runtime.ConfigLoadError
	}
	ctx, attribution, err := NewCommandContext(opts)
	if err != nil {
		return nil, config.WorkspaceProfile{}, Attribution{}, err
	}
	if runtime.Config == nil {
		return ctx, config.WorkspaceProfile{Name: ctx.Workspace, TokenType: config.TokenTypeBot}, attribution, nil
	}
	profile, err := runtime.Config.ResolveWorkspace(opts.Workspace)
	if err != nil {
		return nil, config.WorkspaceProfile{}, Attribution{}, err
	}
	return ctx, profile, attribution, nil
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
		Workspace: workspace,
		Mode:      mode,
		Stdout:    opts.Stdout,
		Stderr:    opts.Stderr,
		Now:       opts.Now,
		RequestID: opts.RequestID,
		IsTTY:     opts.IsTTY,
		ColorMode: opts.ColorMode,
		Theme:     opts.Theme,
		stdoutLog: stdoutLog,
		stderrLog: stderrLog,
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

func (c *CommandContext) WriteResult(command string, data any) error {
	return c.WriteResultWithPagination(command, data, nil)
}

func (c *CommandContext) WriteResultWithPagination(command string, data any, pagination *Pagination) error {
	switch c.Mode {
	case RenderModePlain:
		return c.WritePlainResult(command, data, pagination)
	case RenderModeCompact:
		c.stdoutLogger().Print().JSON(data)
	case RenderModeRaw:
		switch raw := data.(type) {
		case []byte:
			c.stdoutLogger().Print().RawJSON(raw)
		case json.RawMessage:
			c.stdoutLogger().Print().RawJSON(raw)
		default:
			c.stdoutLogger().Print().JSON(data)
		}
	default:
		c.stdoutLogger().Print().JSON(Envelope{
			Meta: EnvelopeMeta{
				Command:    command,
				Workspace:  c.workspace(),
				Timestamp:  c.now().Format(time.RFC3339),
				RequestID:  c.requestID(),
				Pagination: pagination,
			},
			Data:   data,
			Errors: []CLIError{},
		})
	}
	return nil
}

func (c *CommandContext) WritePlainResult(command string, data any, pagination *Pagination) error {
	if r, ok := data.(PlainRenderer); ok {
		return r.WritePlain(c, command, pagination)
	}
	event := c.resultEvent(command).Any("data", data)
	addPaginationFields(event, pagination)
	event.Send()
	return nil
}

func (c *CommandContext) resultEvent(command string) *clog.Event {
	return c.stdoutLogger().Info().
		Str("command", command)
}

type fieldStyle = clioutput.FieldStyle

var entityFieldStyle = clioutput.EntityFieldStyle

func (c *CommandContext) resultEventWithStyles(command string, styles ...fieldStyle) *clog.Event {
	logger := c.stdoutLogger()
	clioutput.ApplyFieldStyles(logger, c.Theme, styles...)
	return logger.Info().
		Str("command", command)
}

var addPaginationFields = clioutput.AddPaginationFields

var addSlackTimestampFields = clioutput.AddSlackTimestampFields

var addBoolField = clioutput.AddBoolField

var addIntField = clioutput.AddIntField

func (c *CommandContext) WriteMessages(command string, messages []cliMessage, pagination *Pagination) error {
	if len(messages) > 0 {
		return c.WriteMessageTable(messages)
	}
	event := c.resultEvent(command)
	addPaginationFields(event, pagination)
	event.Send()
	return nil
}

func (c *CommandContext) WriteSearch(command string, data searchCommandData, pagination *Pagination) error {
	if len(data.Matches) > 0 {
		return c.WriteSearchTable(data)
	}
	event := c.resultEvent(command)
	addPaginationFields(event, pagination)
	event.Send()
	return nil
}

func (c *CommandContext) WriteChannels(command string, channels []cliChannel, pagination *Pagination) error {
	if len(channels) > 0 {
		return c.WriteChannelTable(command, channels)
	}
	event := c.resultEvent(command)
	addPaginationFields(event, pagination)
	event.Send()
	return nil
}

func (c *CommandContext) WriteUsers(command string, users []cliUser, pagination *Pagination) error {
	if len(users) > 0 {
		return c.WriteUserTable(users)
	}
	event := c.resultEvent(command)
	addPaginationFields(event, pagination)
	event.Send()
	return nil
}

func (c *CommandContext) WriteWorkspaces(command string, workspaces []config.WorkspaceProfile, pagination *Pagination) error {
	if len(workspaces) > 0 {
		return c.WriteWorkspaceTable(workspaces)
	}
	event := c.resultEvent(command)
	addPaginationFields(event, pagination)
	event.Send()
	return nil
}

func truncateText(value string, limit int) string {
	return termansi.Truncate(value, limit, "...")
}

func (c *CommandContext) WriteString(message string) error {
	c.stdoutLogger().Info().Parts(clog.PartMessage).Msg(message)
	return nil
}

var applyTeamIDStyle = clioutput.ApplyTeamIDStyle

func (c *CommandContext) WriteError(err CLIError) int {
	if c.Mode == RenderModePlain {
		event := c.stderrLogger().Error().
			Str("type", err.Type).
			Int("exit_code", err.ExitCode)
		event = addCLIErrorDetails(event, err.Details)
		event.Msg(err.Message)
	} else {
		c.stderrLogger().Print().JSON(struct {
			Errors []CLIError `json:"errors"`
		}{
			Errors: []CLIError{err},
		})
	}
	return err.ExitCode
}

var addCLIErrorDetails = clioutput.AddCLIErrorDetails

func writeCommandError(ctx *CommandContext, err CLIError) error {
	ctx.WriteError(err)
	return CommandError{CLIError: err}
}

func writeRuntimeError(runtime *RootRuntime, err CLIError) error {
	mode := OutputFlags{}.Resolve(runtime.IsTTY, false)
	sl, el := buildBaseLoggers(runtime.Stdout, runtime.Stderr, runtime.ColorMode)
	applyRenderMode(sl, mode)
	ctx := &CommandContext{
		Workspace: "default",
		Mode:      mode,
		Stdout:    runtime.Stdout,
		Stderr:    runtime.Stderr,
		Now:       runtime.Now,
		RequestID: runtime.RequestID,
		stdoutLog: sl,
		stderrLog: el,
	}
	return writeCommandError(ctx, err)
}

var validationCLIError = clioutput.ValidationCLIError

var authCLIError = clioutput.AuthCLIError

func (c *CommandContext) stdoutLogger() *clog.Logger { return c.stdoutLog }
func (c *CommandContext) stderrLogger() *clog.Logger { return c.stderrLog }

func (c *CommandContext) stdout() io.Writer {
	if c.Stdout != nil {
		return c.Stdout
	}
	return io.Discard
}

func (c *CommandContext) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now().UTC()
}

func (c *CommandContext) requestID() string {
	if c.RequestID != nil {
		return c.RequestID()
	}
	return ""
}

func (c *CommandContext) workspace() string {
	if c.Workspace != "" {
		return c.Workspace
	}
	return "default"
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
			stdoutLog: sl,
			stderrLog: el,
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
