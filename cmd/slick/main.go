package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"charm.land/lipgloss/v2"
	cobracli "github.com/gechr/clib/cli/cobra"
	"github.com/gechr/clib/help"
	"github.com/gechr/clib/theme"
	"github.com/gechr/clog"
	clogstyle "github.com/gechr/clog/style"
	termansi "github.com/gechr/x/ansi"
	"github.com/gechr/x/human"
	"github.com/gechr/x/shell"
	"github.com/gechr/x/terminal"
	"github.com/matcra587/slack-cli/internal/config"
	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

type RenderMode int

const (
	RenderModePlain    RenderMode = iota // human-readable clog fields
	RenderModeEnvelope                   // JSON with meta envelope (default non-TTY)
	RenderModeCompact                    // JSON data only, no envelope
	RenderModeRaw                        // raw Slack JSON pass-through
)

type OutputFlags struct {
	JSON    bool
	Plain   bool
	Compact bool
	Raw     bool
}

func (f OutputFlags) Resolve(isTTY, agentMode bool) RenderMode {
	switch {
	case f.Raw:
		return RenderModeRaw
	case f.Compact:
		return RenderModeCompact
	case f.Plain:
		return RenderModePlain
	case f.JSON || !isTTY || agentMode:
		return RenderModeEnvelope
	default:
		return RenderModePlain
	}
}

const (
	ExitCodeAuthFailure = 1
	ExitCodeNotFound    = 2
	ExitCodeRateLimit   = 3
	ExitCodeValidation  = 4
	ExitCodeServer      = 5
	ExitCodeCanceled    = 6
	ExitCodeTimeout     = 7
)

const (
	ErrorTypeAuth       = "auth_failure"
	ErrorTypeNotFound   = "not_found"
	ErrorTypeRateLimit  = "rate_limit"
	ErrorTypeValidation = "validation_error"
	ErrorTypeServer     = "server_error"
	ErrorTypeCanceled   = "canceled"
	ErrorTypeTimeout    = "timeout"
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

type RootRuntime struct {
	Config          *config.Config
	ConfigLoadError error
	ConfigExplicit  bool
	ConfigPath      string
	CredentialStore config.CredentialStore
	TokenResolver   TokenResolver
	SlackBaseURL    string
	HTTPClient      *http.Client
	OpenURL         func(string) error
	OAuthTimeout    time.Duration
	Timeout         time.Duration
	CancelTimeout   context.CancelFunc
	Stdin           io.Reader
	Stdout          io.Writer
	Stderr          io.Writer
	IsTTY           bool
	ColorMode       clog.ColorMode
	Now             func() time.Time
	RequestID       func() string
	Theme           *theme.Theme
	HelpRenderer    *help.Renderer
}

type RootOption func(*RootRuntime)

func WithConfig(cfg *config.Config) RootOption {
	return func(runtime *RootRuntime) {
		runtime.Config = cfg
		runtime.ConfigLoadError = nil
		runtime.ConfigExplicit = true
	}
}

func WithConfigPath(path string) RootOption {
	return func(runtime *RootRuntime) {
		runtime.ConfigPath = path
		if !runtime.ConfigExplicit {
			runtime.Config, runtime.ConfigLoadError = loadDefaultConfig(path)
		}
	}
}

func WithCredentialStore(store config.CredentialStore) RootOption {
	return func(runtime *RootRuntime) {
		runtime.CredentialStore = store
	}
}

func WithTokenResolver(resolver TokenResolver) RootOption {
	return func(runtime *RootRuntime) {
		runtime.TokenResolver = resolver
	}
}

func WithSlackBaseURL(baseURL string) RootOption {
	return func(runtime *RootRuntime) {
		runtime.SlackBaseURL = baseURL
	}
}

func WithIO(stdin io.Reader, stdout, stderr io.Writer) RootOption {
	return func(runtime *RootRuntime) {
		runtime.Stdin = stdin
		runtime.Stdout = stdout
		runtime.Stderr = stderr
	}
}

func WithTTY(isTTY bool) RootOption {
	return func(runtime *RootRuntime) {
		runtime.IsTTY = isTTY
	}
}

func WithNow(now func() time.Time) RootOption {
	return func(runtime *RootRuntime) {
		runtime.Now = now
	}
}

func WithRequestID(requestID func() string) RootOption {
	return func(runtime *RootRuntime) {
		runtime.RequestID = requestID
	}
}

func WithURLOpener(openURL func(string) error) RootOption {
	return func(runtime *RootRuntime) {
		runtime.OpenURL = openURL
	}
}

func WithOAuthTimeout(timeout time.Duration) RootOption {
	return func(runtime *RootRuntime) {
		runtime.OAuthTimeout = timeout
	}
}

type TokenResolver interface {
	ResolveToken(ctx context.Context, profile config.WorkspaceProfile) (string, error)
}

type TokenResolverFunc func(ctx context.Context, profile config.WorkspaceProfile) (string, error)

func (f TokenResolverFunc) ResolveToken(ctx context.Context, profile config.WorkspaceProfile) (string, error) {
	return f(ctx, profile)
}

type EnvTokenResolver struct{}

func (EnvTokenResolver) ResolveToken(_ context.Context, profile config.WorkspaceProfile) (string, error) {
	if after, ok := strings.CutPrefix(profile.TokenRef, "env:"); ok {
		name := after
		token := os.Getenv(name)
		if token == "" {
			return "", errors.New("token environment variable is empty")
		}
		return token, nil
	}
	return "", errors.New("unsupported token reference")
}

type CredentialTokenResolver struct {
	Store        config.CredentialStore
	SlackBaseURL string
	HTTPClient   *http.Client
	Now          func() time.Time
	SecretReader SecretReader
}

type SecretReader interface {
	Read(ctx context.Context, ref string) (string, error)
}

type SecretReaderFunc func(ctx context.Context, ref string) (string, error)

func (f SecretReaderFunc) Read(ctx context.Context, ref string) (string, error) { return f(ctx, ref) }

type opSecretReader struct{}

func (opSecretReader) Read(ctx context.Context, ref string) (string, error) {
	out, err := exec.CommandContext(ctx, "op", "read", ref).Output() //nolint:gosec // The 1Password reference is explicit user configuration, not shell-expanded input.
	if err != nil {
		return "", fmt.Errorf("reading 1Password secret: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (r CredentialTokenResolver) secretReader() SecretReader {
	if r.SecretReader != nil {
		return r.SecretReader
	}
	return opSecretReader{}
}

func (r CredentialTokenResolver) ResolveToken(ctx context.Context, profile config.WorkspaceProfile) (string, error) {
	if token, ok := runtimeEnvToken(profile.Name); ok {
		return token, nil
	}
	if strings.TrimSpace(profile.TokenRef) == "" {
		return "", config.ErrCredentialNotFound
	}
	if strings.HasPrefix(profile.TokenRef, "env:") {
		return EnvTokenResolver{}.ResolveToken(ctx, profile)
	}
	if strings.HasPrefix(profile.TokenRef, "keychain:slack-cli/") {
		if r.Store == nil {
			return "", errors.New("credential store is unavailable")
		}
		name := strings.TrimPrefix(profile.TokenRef, "keychain:slack-cli/")
		secret, err := r.Store.Get("slack-cli", name)
		if err != nil {
			return "", err
		}
		return r.resolveStoredCredential(ctx, name, secret)
	}
	if strings.HasPrefix(profile.TokenRef, "op://") {
		secret, err := r.secretReader().Read(ctx, profile.TokenRef)
		if err != nil {
			return "", err
		}
		return accessTokenFromStructuredSecret(secret)
	}
	return "", errors.New("unsupported token reference")
}

func runtimeEnvToken(profileName string) (string, bool) {
	for _, name := range runtimeTokenEnvNames(profileName) {
		if token := strings.TrimSpace(os.Getenv(name)); token != "" {
			return token, true
		}
	}
	return "", false
}

func runtimeTokenEnvNames(profileName string) []string {
	names := make([]string, 0, 2)
	if normalized := normalizeProfileEnvSuffix(profileName); normalized != "" {
		names = append(names, "SLACK_CLI_TOKEN_"+normalized)
	}
	names = append(names, "SLACK_CLI_TOKEN")
	return names
}

func normalizeProfileEnvSuffix(profileName string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(profileName)) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	return strings.Trim(b.String(), "_")
}

const credentialRefreshBuffer = 5 * time.Minute

func (r CredentialTokenResolver) resolveStoredCredential(ctx context.Context, name, secret string) (string, error) {
	credential, err := config.DecodeCredential(secret)
	if err != nil {
		return "", err
	}
	if !r.credentialNeedsRefresh(credential) {
		return credential.AccessToken, nil
	}
	if credential.ClientID == "" || credential.RefreshToken == "" {
		return "", errors.New("credential is expiring and cannot be refreshed")
	}
	response, err := oauthRefreshToken(ctx, r.oauthHTTPClient(), r.SlackBaseURL, credential.ClientID, credential.RefreshToken)
	if err != nil {
		return "", err
	}
	updated := credentialPayloadFromOAuthRefresh(credential.ClientID, r.now(), response)
	encoded, err := config.EncodeCredential(updated)
	if err != nil {
		return "", err
	}
	if r.Store != nil && name != "" {
		if err := r.Store.Set("slack-cli", name, encoded); err != nil {
			return "", err
		}
	}
	return updated.AccessToken, nil
}

func (r CredentialTokenResolver) credentialNeedsRefresh(credential config.CredentialPayload) bool {
	if credential.ExpiresAt == nil {
		return false
	}
	return !r.now().Before(credential.ExpiresAt.Add(-credentialRefreshBuffer))
}

func (r CredentialTokenResolver) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r CredentialTokenResolver) oauthHTTPClient() *http.Client {
	if r.HTTPClient != nil {
		return r.HTTPClient
	}
	return &http.Client{}
}

func credentialPayloadFromOAuthRefresh(clientID string, now time.Time, response *slackgo.OAuthV2Response) config.CredentialPayload {
	payload := config.CredentialPayload{ClientID: clientID}
	if response.AuthedUser.AccessToken != "" {
		payload.AccessToken = response.AuthedUser.AccessToken
		payload.RefreshToken = response.AuthedUser.RefreshToken
		if response.AuthedUser.ExpiresIn > 0 {
			expiresAt := now.Add(time.Duration(response.AuthedUser.ExpiresIn) * time.Second)
			payload.ExpiresAt = &expiresAt
		}
		return payload
	}
	payload.AccessToken = response.AccessToken
	payload.RefreshToken = response.RefreshToken
	if response.ExpiresIn > 0 {
		expiresAt := now.Add(time.Duration(response.ExpiresIn) * time.Second)
		payload.ExpiresAt = &expiresAt
	}
	return payload
}

func accessTokenFromStructuredSecret(secret string) (string, error) {
	credential, err := config.DecodeCredential(secret)
	if err != nil {
		return "", err
	}
	return credential.AccessToken, nil
}

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
	runtime.Config, runtime.ConfigLoadError = loadDefaultConfig(runtime.ConfigPath)
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

func loadDefaultConfig(path string) (*config.Config, error) {
	if path == "" {
		return nil, nil
	}
	cfg, err := config.LoadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, missingConfigError(path)
		}
		return nil, err
	}
	return cfg, nil
}

func missingConfigError(path string) error {
	return fmt.Errorf("config file not found at %s; run `slick config init`", human.ContractHome(path))
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

func buildBaseLoggers(stdout, stderr io.Writer, colorMode clog.ColorMode) (*clog.Logger, *clog.Logger) {
	sl := clog.New(clog.NewOutput(stdout, colorMode))
	sl.SetOmitZero(true)
	sl.SetParts(clog.PartLevel, clog.PartMessage, clog.PartFields)

	el := clog.New(clog.NewOutput(stderr, colorMode))
	el.SetOmitZero(true)
	el.SetParts(clog.PartLevel, clog.PartMessage, clog.PartFields)
	el.SetNonTTYLevel(clog.LevelWarn)
	el.SetJSONPrintMode(clog.JSONFlat)

	return sl, el
}

func applyRenderMode(sl *clog.Logger, mode RenderMode) {
	switch mode {
	case RenderModeRaw:
		sl.SetJSONPrintMode(clog.JSONPreserve)
	case RenderModeCompact, RenderModeEnvelope:
		sl.SetJSONPrintMode(clog.JSONFlat)
	}
	// RenderModePlain: no JSON print mode needed; logger emits human-readable clog events.
}

func profileAttributionSetting(profile config.WorkspaceProfile) *bool {
	if profile.Attribution.Enabled != nil {
		return profile.Attribution.Enabled
	}
	return profile.AgentAttribution
}

type Envelope struct {
	Meta   EnvelopeMeta `json:"meta"`
	Data   any          `json:"data"`
	Errors []CLIError   `json:"errors"`
}

type EnvelopeMeta struct {
	Command    string      `json:"command"`
	Workspace  string      `json:"workspace"`
	Timestamp  string      `json:"timestamp"`
	RequestID  string      `json:"request_id"`
	Pagination *Pagination `json:"pagination,omitempty"`
}

type Pagination struct {
	Cursor        *string `json:"cursor,omitempty"`
	NextCursor    *string `json:"next_cursor,omitempty"`
	HasMore       bool    `json:"has_more"`
	MaxItems      *int    `json:"max_items,omitempty"`
	ItemsReturned *int    `json:"items_returned,omitempty"`
}

type CLIError struct {
	Type              string         `json:"type"`
	Message           string         `json:"message"`
	Details           map[string]any `json:"details,omitempty"`
	RetryAfterSeconds *int           `json:"retry_after_seconds,omitempty"`
	ExitCode          int            `json:"exit_code"`
}

type CommandError struct {
	CLIError CLIError
}

func (e CommandError) Error() string {
	return e.CLIError.Message
}

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
	switch typed := data.(type) {
	case authStatusData:
		return c.WriteAuthStatus(typed)
	case authWorkspaceData:
		return c.WriteAuthWorkspace(command, typed)
	case sendCommandData:
		return c.WriteSend(command, typed)
	case deleteMessageData:
		return c.WriteDelete(command, typed)
	case uploadFileResult:
		return c.WriteUpload(command, typed)
	case reactionCommandData:
		return c.WriteReaction(command, typed)
	case statusCommandData:
		return c.WriteStatus(command, typed)
	case cacheUsersData:
		return c.WriteUsers(command, typed.Users, nil)
	case cacheChannelsData:
		return c.WriteChannels(command, typed.Channels, nil)
	case cacheClearData:
		return c.WriteCacheClear(command, typed)
	case historyCommandData:
		return c.WriteMessages(command, typed.Messages, pagination)
	case searchCommandData:
		return c.WriteSearch(command, typed, pagination)
	case channelListData:
		return c.WriteChannels(command, typed.Channels, pagination)
	case channelInfoData:
		return c.WriteChannelInfo(command, typed.Channel)
	case userListData:
		return c.WriteUsers(command, typed.Users, pagination)
	case userInfoData:
		return c.WriteUserInfo(command, typed.User)
	case workspaceListData:
		return c.WriteWorkspaces(command, typed.Workspaces, pagination)
	case versionData:
		return c.WriteVersion(typed)
	case configInitData:
		return c.WriteConfigInit(typed)
	case configPathData:
		return c.WriteConfigPath(command, typed)
	case configListData:
		return c.WriteConfigList(command, typed)
	case configGetData:
		return c.WriteConfigGet(command, typed)
	case configMutationData:
		return c.WriteConfigMutation(command, typed)
	default:
		event := c.resultEvent(command).Any("data", data)
		addPaginationFields(event, pagination)
		event.Send()
		return nil
	}
}

func (c *CommandContext) WriteCacheClear(command string, data cacheClearData) error {
	c.resultEvent(command).
		Str("profile", data.Profile).
		When(data.Resource != "", func(e *clog.Event) {
			e.Str("resource", data.Resource).Bool("removed", data.Removed)
		}).
		Int("removed_count", data.RemovedCount).
		Send()
	return nil
}

func (c *CommandContext) resultEvent(command string) *clog.Event {
	return c.stdoutLogger().Info().
		Str("command", command)
}

type fieldStyle struct {
	Field string
	Seed  string
}

func entityFieldStyle(field, value string) fieldStyle {
	return fieldStyle{Field: field, Seed: field + ":" + value}
}

func (c *CommandContext) resultEventWithStyles(command string, styles ...fieldStyle) *clog.Event {
	logger := c.stdoutLogger()
	applyFieldStyles(logger, c.Theme, styles...)
	return logger.Info().
		Str("command", command)
}

func addPaginationFields(event *clog.Event, pagination *Pagination) *clog.Event {
	if pagination == nil {
		return event
	}
	return event.
		When(pagination.Cursor != nil, func(e *clog.Event) {
			e.Str("cursor", *pagination.Cursor)
		}).
		When(pagination.NextCursor != nil, func(e *clog.Event) {
			e.Str("next_cursor", *pagination.NextCursor)
		}).
		Bool("has_more", pagination.HasMore).
		When(pagination.MaxItems != nil, func(e *clog.Event) {
			e.Int("max_items", *pagination.MaxItems)
		}).
		When(pagination.ItemsReturned != nil, func(e *clog.Event) {
			e.Int("items_returned", *pagination.ItemsReturned)
		})
}

func addSlackTimestampFields(event *clog.Event, ts string, now time.Time) *clog.Event {
	return event.Str("ts", ts).
		When(clog.IsVerbose(), func(e *clog.Event) {
			parsed, ok := parseSlackTimestamp(ts)
			if !ok {
				return
			}
			e.Time("time", parsed).
				Str("age", human.FormatTimeAgoCompactFrom(parsed, now))
		})
}

func parseSlackTimestamp(ts string) (time.Time, bool) {
	secondsText, fractionText, ok := strings.Cut(strings.TrimSpace(ts), ".")
	if !ok {
		return time.Time{}, false
	}
	seconds, err := strconv.ParseInt(secondsText, 10, 64)
	if err != nil || seconds < 0 {
		return time.Time{}, false
	}
	if len(fractionText) > 9 {
		fractionText = fractionText[:9]
	}
	for len(fractionText) < 9 {
		fractionText += "0"
	}
	nanos, err := strconv.ParseInt(fractionText, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(seconds, nanos).UTC(), true
}

func addBoolField(event *clog.Event, key string, value *bool) *clog.Event {
	return event.When(value != nil, func(e *clog.Event) {
		e.Bool(key, *value)
	})
}

func addIntField(event *clog.Event, key string, value *int) *clog.Event {
	return event.When(value != nil, func(e *clog.Event) {
		e.Int(key, *value)
	})
}

func (c *CommandContext) WriteAuthWorkspace(command string, workspace authWorkspaceData) error {
	logger := c.stdoutLogger()
	if workspace.TeamID != "" {
		applyTeamIDStyle(logger, c.Theme, workspace.TeamID)
	}
	event := logger.Info().
		Str("command", command).
		Str("workspace", workspace.Workspace).
		Bool("authenticated", workspace.Authenticated).
		Str("token_type", string(workspace.TokenType)).
		Str("team_id", workspace.TeamID).
		Str("team_name", workspace.TeamName).
		Str("validation_error", workspace.ValidationError)
	event.Send()
	return nil
}

func (c *CommandContext) WriteSend(command string, data sendCommandData) error {
	channel := ""
	if data.Message.Channel != nil {
		channel = *data.Message.Channel
	}
	event := c.resultEventWithStyles(command, entityFieldStyle("channel", channel))
	event = addSlackTimestampFields(event, data.Message.TS, c.now()).
		Bool("dry_run", data.DryRun).
		When(clog.IsVerbose(), func(e *clog.Event) {
			e.Bool("attribution", data.Attribution)
			if data.Message.ThreadTS != nil {
				e.Str("thread_ts", *data.Message.ThreadTS)
			}
			if data.Permalink != nil {
				e.Str("permalink", *data.Permalink)
			}
		})
	if data.Message.Channel != nil {
		event = event.Str("channel", *data.Message.Channel)
	}
	event.Send()
	return nil
}

func (c *CommandContext) WriteDelete(command string, data deleteMessageData) error {
	event := c.resultEventWithStyles(command, entityFieldStyle("channel", data.Channel)).
		Str("channel", data.Channel)
	event = addSlackTimestampFields(event, data.Timestamp, c.now()).
		Bool("deleted", data.Deleted).
		Bool("dry_run", data.DryRun)
	event.Send()
	return nil
}

func (c *CommandContext) WriteUpload(command string, data uploadFileResult) error {
	c.resultEventWithStyles(command, entityFieldStyle("channel", data.Channel)).
		Str("channel", data.Channel).
		Str("file_id", data.File.ID).
		Str("file_name", data.File.Name).
		Int("size", data.File.Size).
		Str("size_human", human.FormatIECBytes(float64(data.File.Size))).
		Bool("dry_run", data.DryRun).
		Send()
	return nil
}

func (c *CommandContext) WriteReaction(command string, data reactionCommandData) error {
	if data.Reaction != nil {
		event := c.resultEventWithStyles(command, entityFieldStyle("channel", data.Reaction.Channel)).
			Str("channel", data.Reaction.Channel)
		event = addSlackTimestampFields(event, data.Reaction.Timestamp, c.now()).
			Str("emoji", data.Reaction.Emoji).
			Bool("removed", data.Reaction.Removed).
			Bool("dry_run", data.Reaction.DryRun)
		event.Send()
		return nil
	}
	if len(data.Reactions) > 0 {
		return c.WriteReactionTable(data.Reactions)
	}
	if len(data.Reactions) == 0 {
		event := c.resultEventWithStyles(command, entityFieldStyle("channel", data.Target.Channel)).
			Str("channel", data.Target.Channel)
		addSlackTimestampFields(event, data.Target.Timestamp, c.now()).
			Send()
		return nil
	}
	for _, reaction := range data.Reactions {
		event := c.resultEventWithStyles(command, entityFieldStyle("channel", data.Target.Channel)).
			Str("channel", data.Target.Channel)
		event = addSlackTimestampFields(event, data.Target.Timestamp, c.now()).
			Str("emoji", reaction.Name).
			Int("count", reaction.Count).
			Strs("users", reaction.Users)
		event.Send()
	}
	return nil
}

func (c *CommandContext) WriteStatus(command string, data statusCommandData) error {
	event := c.resultEvent(command).
		Str("text", data.Text).
		Str("emoji", data.Emoji).
		Bool("cleared", data.Cleared).
		Bool("dry_run", data.DryRun).
		When(data.Expiration > 0, func(e *clog.Event) {
			e.Int64("expiration", data.Expiration)
		})
	event.Send()
	return nil
}

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

func (c *CommandContext) WriteChannelInfo(command string, channel cliChannel) error {
	event := c.resultEventWithStyles(command, entityFieldStyle("channel", channel.ID)).
		Str("channel", channel.ID).
		Str("name", channel.Name).
		Str("type", channel.Type)
	event = addBoolField(event, "is_member", channel.IsMember)
	event = addBoolField(event, "is_im", channel.IsIM)
	event = addBoolField(event, "is_archived", channel.IsArchived)
	if channel.User != nil {
		event = event.Str("user", *channel.User)
	}
	if channel.Topic != nil {
		event = event.Str("topic", *channel.Topic)
	}
	event = addIntField(event, "num_members", channel.NumMembers)
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

func (c *CommandContext) WriteUserInfo(command string, user cliUser) error {
	event := c.resultEventWithStyles(command, entityFieldStyle("user", user.ID)).
		Str("user", user.ID).
		Str("name", user.Name)
	event = addBoolField(event, "deleted", user.Deleted)
	if user.Timezone != nil {
		event = event.Str("timezone", *user.Timezone)
	}
	if user.Presence != nil {
		event = event.Str("presence", *user.Presence)
	}
	if user.StatusText != nil {
		event = event.Str("status_text", *user.StatusText)
	}
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

func (c *CommandContext) WriteVersion(data versionData) error {
	var b strings.Builder
	b.WriteString("slick " + data.Version + "\n")
	b.WriteString("  commit:  " + data.Commit + "\n")
	b.WriteString("  branch:  " + data.Branch + "\n")
	b.WriteString("  built:   " + data.BuildTime + "\n")
	b.WriteString("  built by: " + data.BuildBy)
	return c.WritePlain(b.String())
}

func (c *CommandContext) WriteConfigInit(data configInitData) error {
	c.resultEvent("config.init").
		Link("path", data.Path, human.ContractHome(data.Path)).
		Str("profile", data.Profile).
		Str("workspace", data.Workspace).
		Bool("written", data.Written).
		Send()
	return nil
}

func (c *CommandContext) WriteConfigPath(command string, data configPathData) error {
	c.resultEvent(command).
		Link("path", data.Path, human.ContractHome(data.Path)).
		Bool("exists", data.Exists).
		Send()
	return nil
}

func (c *CommandContext) WriteConfigList(command string, data configListData) error {
	c.resultEvent(command).
		Link("path", data.Path, human.ContractHome(data.Path)).
		Str("default_workspace", data.DefaultWorkspace).
		Int("settings", len(data.Settings)).
		Send()
	if len(data.Settings) == 0 {
		return nil
	}
	for _, setting := range data.Settings {
		c.resultEvent(command).
			Str("key", setting.Key).
			Str("value", setting.Value).
			Msg("config setting")
	}
	return nil
}

func (c *CommandContext) WriteConfigGet(command string, data configGetData) error {
	c.resultEvent(command).
		Str("key", data.Key).
		Str("value", data.Value).
		Send()
	return nil
}

func (c *CommandContext) WriteConfigMutation(command string, data configMutationData) error {
	c.resultEvent(command).
		Link("path", data.Path, human.ContractHome(data.Path)).
		Str("key", data.Key).
		Str("value", data.Value).
		Send()
	return nil
}

func truncateText(value string, limit int) string {
	return termansi.Truncate(value, limit, "...")
}

func (c *CommandContext) WritePlain(message string) error {
	c.stdoutLogger().Info().Parts(clog.PartMessage).Msg(message)
	return nil
}

func (c *CommandContext) WriteAuthStatus(data authStatusData) error {
	logger := c.stdoutLogger()
	for _, workspace := range data.Workspaces {
		state := workspace.ValidationState
		if state == "" {
			if workspace.Authenticated {
				state = "valid"
			} else {
				state = "missing"
			}
		}
		if workspace.TeamID != "" {
			applyTeamIDStyle(logger, c.Theme, workspace.TeamID)
		}
		event := logger.Info().
			Str("workspace", workspace.Workspace).
			Bool("authenticated", workspace.Authenticated).
			Str("token_type", string(workspace.TokenType)).
			Str("team_id", workspace.TeamID).
			Str("team_name", workspace.TeamName).
			Bool("valid", state == "valid").
			Str("validation_error", workspace.ValidationError)
		event.Msg("auth status")
	}
	return nil
}

func applyTeamIDStyle(logger *clog.Logger, th *theme.Theme, teamID string) {
	applyFieldStyles(logger, th, entityFieldStyle("team_id", teamID))
}

func applyFieldStyles(logger *clog.Logger, th *theme.Theme, fields ...fieldStyle) {
	styles := clogstyle.Map{}
	for _, field := range fields {
		if field.Field == "" || field.Seed == "" {
			continue
		}
		if style := hashEntityStyle(th, field.Seed); style != nil {
			styles[field.Field] = style
		}
	}
	if len(styles) > 0 {
		logger.SetStyles(&clogstyle.Config{Keys: styles})
	}
}

func hashEntityStyle(th *theme.Theme, key string) *lipgloss.Style {
	if th == nil {
		th = theme.Default()
	}
	if len(th.EntityColors) == 0 || strings.TrimSpace(key) == "" {
		return nil
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(strings.TrimSpace(key))))
	style := lipgloss.NewStyle().Foreground(th.EntityColors[h.Sum32()%uint32(len(th.EntityColors))])
	return &style
}

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

func addCLIErrorDetails(event *clog.Event, details map[string]any) *clog.Event {
	if len(details) == 0 {
		return event
	}
	keys := make([]string, 0, len(details))
	for key := range details {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		switch value := details[key].(type) {
		case string:
			event = event.Str(key, value)
		case bool:
			event = event.Bool(key, value)
		case int:
			event = event.Int(key, value)
		default:
			event = event.Any(key, value)
		}
	}
	return event
}

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

func validationCLIError(message string) CLIError {
	return CLIError{Type: ErrorTypeValidation, Message: message, ExitCode: ExitCodeValidation}
}

func authCLIError(message string) CLIError {
	return CLIError{Type: ErrorTypeAuth, Message: message, ExitCode: ExitCodeAuthFailure}
}

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
