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
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	cobracli "github.com/gechr/clib/cli/cobra"
	"github.com/gechr/clib/help"
	"github.com/gechr/clib/theme"
	"github.com/gechr/clog"
	clogstyle "github.com/gechr/clog/style"
	"github.com/gechr/x/human"
	"github.com/gechr/x/shell"
	"github.com/gechr/x/terminal"
	"github.com/matcra587/slack-cli/internal/config"
	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

type OutputMode string

const (
	OutputModeJSON    OutputMode = "json"
	OutputModePlain   OutputMode = "plain"
	OutputModeCompact OutputMode = "compact"
	OutputModeRaw     OutputMode = "raw"
)

type OutputFlags struct {
	JSON    bool
	Plain   bool
	Compact bool
	Raw     bool
}

func (f OutputFlags) Resolve(isTTY, agentMode bool) OutputMode {
	switch {
	case f.Raw:
		return OutputModeRaw
	case f.Compact:
		return OutputModeCompact
	case f.Plain:
		return OutputModePlain
	case f.JSON || !isTTY || agentMode:
		return OutputModeJSON
	default:
		return OutputModePlain
	}
}

const (
	ExitCodeAuthFailure = 1
	ExitCodeNotFound    = 2
	ExitCodeRateLimit   = 3
	ExitCodeValidation  = 4
	ExitCodeServer      = 5
)

const (
	ErrorTypeAuth       = "auth_failure"
	ErrorTypeNotFound   = "not_found"
	ErrorTypeRateLimit  = "rate_limit"
	ErrorTypeValidation = "validation_error"
	ErrorTypeServer     = "server_error"
)

type CommandContext struct {
	Workspace string
	Mode      OutputMode
	Stdout    io.Writer
	Stderr    io.Writer
	Now       func() time.Time
	RequestID func() string
	ColorMode clog.ColorMode
	IsTTY     bool
}

type RootOptions struct {
	Config    *config.Config
	Workspace string
	Output    OutputFlags
	Agent     AgentFlags
	Stdout    io.Writer
	Stderr    io.Writer
	IsTTY     bool
	Now       func() time.Time
	RequestID func() string
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
	Stdin           io.Reader
	Stdout          io.Writer
	Stderr          io.Writer
	IsTTY           bool
	Now             func() time.Time
	RequestID       func() string
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
	ResolveToken(config.WorkspaceProfile) (string, error)
}

type TokenResolverFunc func(config.WorkspaceProfile) (string, error)

func (f TokenResolverFunc) ResolveToken(profile config.WorkspaceProfile) (string, error) {
	return f(profile)
}

type EnvTokenResolver struct{}

func (EnvTokenResolver) ResolveToken(profile config.WorkspaceProfile) (string, error) {
	if strings.HasPrefix(profile.TokenRef, "env:") {
		name := strings.TrimPrefix(profile.TokenRef, "env:")
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
}

var readOnePasswordSecret = func(ref string) (string, error) {
	out, err := exec.Command("op", "read", ref).Output() //nolint:gosec // The 1Password reference is explicit user configuration, not shell-expanded input.
	if err != nil {
		return "", fmt.Errorf("reading 1Password secret: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (r CredentialTokenResolver) ResolveToken(profile config.WorkspaceProfile) (string, error) {
	if token, ok := runtimeEnvToken(profile.Name); ok {
		return token, nil
	}
	if strings.TrimSpace(profile.TokenRef) == "" {
		return "", config.ErrCredentialNotFound
	}
	if strings.HasPrefix(profile.TokenRef, "env:") {
		return EnvTokenResolver{}.ResolveToken(profile)
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
		return r.resolveStoredCredential(name, secret)
	}
	if strings.HasPrefix(profile.TokenRef, "op://") {
		secret, err := readOnePasswordSecret(profile.TokenRef)
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

func (r CredentialTokenResolver) resolveStoredCredential(name, secret string) (string, error) {
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
	response, err := refreshOAuthUserToken(context.Background(), r.oauthHTTPClient(), r.SlackBaseURL, credential.ClientID, credential.RefreshToken)
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
	return http.DefaultClient
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

	th := theme.Default()
	renderer := help.NewRenderer(th)
	root.SetHelpFunc(cobracli.HelpFunc(renderer, cobracli.Sections))
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		if err := validateOutputModeFlags(cmd.Root()); err != nil {
			ctx := &CommandContext{
				Workspace: "default",
				Mode:      OutputModeJSON,
				Stdout:    runtime.Stdout,
				Stderr:    runtime.Stderr,
				Now:       runtime.Now,
				RequestID: runtime.RequestID,
			}
			return writeCommandError(ctx, validationCLIError(err.Error()))
		}
		return nil
	}

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

	root.AddCommand(newMessageCommand(runtime))
	root.AddCommand(newHistoryCommand(runtime))
	root.AddCommand(newReplyCommand(runtime))
	root.AddCommand(newReactCommand(runtime))
	root.AddCommand(newLookupCommand(runtime))
	root.AddCommand(newFileCommand(runtime))
	root.AddCommand(newStatusCommand(runtime))
	root.AddCommand(newCacheCommand(runtime))
	root.AddCommand(newManifestCommand(runtime))
	root.AddCommand(newConfigCommand(runtime))
	root.AddCommand(newAgentCommand(runtime))
	root.AddCommand(newAuthCommand(runtime))
	root.AddCommand(newWorkspaceCommand(runtime))
	root.AddCommand(newVersionCommand(runtime))
	extendSlackCompletionMetadata(root)
	addClibCompletionCommand(root)

	return root
}

func validateOutputModeFlags(root *cobra.Command) error {
	if root == nil {
		return nil
	}
	flags := root.PersistentFlags()
	selected := make([]string, 0, 4)
	for _, name := range []string{"json", "plain", "compact", "raw"} {
		value, err := flags.GetBool(name)
		if err != nil {
			return err
		}
		if value {
			selected = append(selected, "--"+name)
		}
	}
	if len(selected) > 1 {
		return fmt.Errorf("output mode flags are mutually exclusive: %s", strings.Join(selected, ", "))
	}
	return nil
}

func defaultConfigPath() string {
	if path := os.Getenv("SLICK_CONFIG"); path != "" {
		return shell.ExpandPath(path)
	}
	if path := os.Getenv("SLACK_CLI_CONFIG"); path != "" {
		return shell.ExpandPath(path)
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
		Now:       runtime.Now,
		RequestID: runtime.RequestID,
	}
}

func commandContext(cmd *cobra.Command, runtime *RootRuntime) (*CommandContext, config.WorkspaceProfile, Attribution, error) {
	opts := rootOptionsFromCommand(cmd, runtime)
	if runtime.ConfigLoadError != nil {
		ctx := &CommandContext{
			Workspace: "default",
			Mode:      opts.Output.Resolve(runtime.IsTTY, DetectAgentOutputMode(opts.Agent)),
			Stdout:    runtime.Stdout,
			Stderr:    runtime.Stderr,
			Now:       runtime.Now,
			RequestID: runtime.RequestID,
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

	attribution := DetectAgentMode(agentFlags)
	return &CommandContext{
		Workspace: workspace,
		Mode:      opts.Output.Resolve(opts.IsTTY, DetectAgentOutputMode(agentFlags)),
		Stdout:    opts.Stdout,
		Stderr:    opts.Stderr,
		Now:       opts.Now,
		RequestID: opts.RequestID,
		IsTTY:     opts.IsTTY,
	}, attribution, nil
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
	case OutputModePlain:
		return c.WritePlainResult(command, data, pagination)
	case OutputModeCompact:
		c.stdoutLogger().Print().Mode(clog.JSONFlat).JSON(data)
	case OutputModeRaw:
		if raw, ok := data.([]byte); ok {
			c.stdoutLogger().Print().Mode(clog.JSONPreserve).RawJSON(raw)
			return nil
		}
		if raw, ok := data.(json.RawMessage); ok {
			c.stdoutLogger().Print().Mode(clog.JSONPreserve).RawJSON(raw)
			return nil
		}
		c.stdoutLogger().Print().Mode(clog.JSONPreserve).JSON(data)
	default:
		c.stdoutLogger().Print().Mode(clog.JSONFlat).JSON(Envelope{
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
		event.Msg(commandMessage(command))
		return nil
	}
}

func (c *CommandContext) WriteCacheClear(command string, data cacheClearData) error {
	c.resultEvent(command).
		Str("profile", data.Profile).
		When(data.Resource != "", func(e *clog.Event) {
			e.Str("resource", data.Resource).Bool("removed", data.Removed)
		}).
		When(data.RemovedCount > 0, func(e *clog.Event) {
			e.Int("removed_count", data.RemovedCount)
		}).
		Msg(commandMessage(command))
	return nil
}

func (c *CommandContext) resultEvent(command string) *clog.Event {
	return c.stdoutLogger().Info().
		Parts(clog.PartLevel, clog.PartMessage, clog.PartFields).
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
	applyFieldStyles(logger, styles...)
	return logger.Info().
		Parts(clog.PartLevel, clog.PartMessage, clog.PartFields).
		Str("command", command)
}

func commandMessage(command string) string {
	return strings.ReplaceAll(command, ".", " ")
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

func addStringField(event *clog.Event, key string, value *string) *clog.Event {
	return event.When(value != nil && *value != "", func(e *clog.Event) {
		e.Str(key, *value)
	})
}

func addSlackTimestampFields(event *clog.Event, ts string, debug bool, now time.Time) *clog.Event {
	event = event.Str("ts", ts)
	if !debug {
		return event
	}
	parsed, ok := parseSlackTimestamp(ts)
	if !ok {
		return event
	}
	return event.
		Time("time", parsed).
		Str("age", human.FormatTimeAgoCompactFrom(parsed, now))
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
		applyTeamIDStyle(logger, workspace.TeamID)
	}
	event := logger.Info().
		Parts(clog.PartLevel, clog.PartMessage, clog.PartFields).
		Str("command", command).
		Str("workspace", workspace.Workspace).
		Bool("authenticated", workspace.Authenticated).
		When(workspace.TokenType != "", func(e *clog.Event) {
			e.Str("token_type", string(workspace.TokenType))
		}).
		When(workspace.TeamID != "", func(e *clog.Event) {
			e.Str("team_id", workspace.TeamID)
		}).
		When(workspace.TeamName != "", func(e *clog.Event) {
			e.Str("team_name", workspace.TeamName)
		}).
		When(workspace.ValidationError != "", func(e *clog.Event) {
			e.Str("validation_error", workspace.ValidationError)
		})
	event.Msg(commandMessage(command))
	return nil
}

func (c *CommandContext) WriteSend(command string, data sendCommandData) error {
	channel := ""
	if data.Message.Channel != nil {
		channel = *data.Message.Channel
	}
	event := c.resultEventWithStyles(command, entityFieldStyle("channel", channel))
	event = addSlackTimestampFields(event, data.Message.TS, c.debugOutput(), c.now()).
		Bool("dry_run", data.DryRun).
		When(c.debugOutput(), func(e *clog.Event) {
			e.Bool("attribution", data.Attribution)
			addStringField(e, "thread_ts", data.Message.ThreadTS)
			addStringField(e, "permalink", data.Permalink)
		})
	event = addStringField(event, "channel", data.Message.Channel)
	event.Msg(commandMessage(command))
	return nil
}

func (c *CommandContext) WriteDelete(command string, data deleteMessageData) error {
	event := c.resultEventWithStyles(command, entityFieldStyle("channel", data.Channel)).
		Str("channel", data.Channel)
	event = addSlackTimestampFields(event, data.Timestamp, c.debugOutput(), c.now()).
		Bool("deleted", data.Deleted).
		Bool("dry_run", data.DryRun)
	event.Msg(commandMessage(command))
	return nil
}

func (c *CommandContext) WriteUpload(command string, data uploadFileResult) error {
	c.resultEventWithStyles(command, entityFieldStyle("channel", data.Channel)).
		Str("channel", data.Channel).
		Str("file_id", data.File.ID).
		Str("file_name", data.File.Name).
		Int("size", data.File.Size).
		Bool("dry_run", data.DryRun).
		Msg(commandMessage(command))
	return nil
}

func (c *CommandContext) WriteReaction(command string, data reactionCommandData) error {
	if data.Reaction != nil {
		event := c.resultEventWithStyles(command, entityFieldStyle("channel", data.Reaction.Channel)).
			Str("channel", data.Reaction.Channel)
		event = addSlackTimestampFields(event, data.Reaction.Timestamp, c.debugOutput(), c.now()).
			Str("emoji", data.Reaction.Emoji).
			Bool("removed", data.Reaction.Removed).
			Bool("dry_run", data.Reaction.DryRun)
		event.Msg(commandMessage(command))
		return nil
	}
	if len(data.Reactions) > 0 {
		return c.WriteReactionTable(data.Reactions)
	}
	if len(data.Reactions) == 0 {
		event := c.resultEventWithStyles(command, entityFieldStyle("channel", data.Target.Channel)).
			Str("channel", data.Target.Channel)
		addSlackTimestampFields(event, data.Target.Timestamp, c.debugOutput(), c.now()).
			Msg(commandMessage(command))
		return nil
	}
	for _, reaction := range data.Reactions {
		event := c.resultEventWithStyles(command, entityFieldStyle("channel", data.Target.Channel)).
			Str("channel", data.Target.Channel)
		event = addSlackTimestampFields(event, data.Target.Timestamp, c.debugOutput(), c.now()).
			Str("emoji", reaction.Name).
			Int("count", reaction.Count).
			Strs("users", reaction.Users)
		event.Msg(commandMessage(command))
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
	event.Msg(commandMessage(command))
	return nil
}

func (c *CommandContext) WriteMessages(command string, messages []cliMessage, pagination *Pagination) error {
	if len(messages) > 0 {
		return c.WriteMessageTable(messages)
	}
	if len(messages) == 0 {
		event := c.resultEvent(command)
		addPaginationFields(event, pagination)
		event.Msg(commandMessage(command))
		return nil
	}
	for _, message := range messages {
		channel := ""
		if message.Channel != nil {
			channel = *message.Channel
		}
		event := c.resultEventWithStyles(command, entityFieldStyle("channel", channel))
		event = addSlackTimestampFields(event, message.TS, c.debugOutput(), c.now())
		event = addStringField(event, "channel", message.Channel)
		event = addStringField(event, "user", message.User)
		event = addStringField(event, "bot_id", message.BotID)
		event = addStringField(event, "thread_ts", message.ThreadTS)
		event = addStringField(event, "text", message.Text)
		event = addIntField(event, "reply_count", message.ReplyCount)
		event.Msg(commandMessage(command))
	}
	return nil
}

func (c *CommandContext) WriteSearch(command string, data searchCommandData, pagination *Pagination) error {
	if len(data.Matches) > 0 {
		return c.WriteSearchTable(data)
	}
	if len(data.Matches) == 0 {
		event := c.resultEvent(command)
		addPaginationFields(event, pagination)
		event.Msg(commandMessage(command))
		return nil
	}
	for _, match := range data.Matches {
		text := match.Text
		if !data.Full {
			text = truncateText(text, 300)
		}
		event := c.resultEventWithStyles(command, entityFieldStyle("channel", match.Channel.ID)).
			Str("channel", match.Channel.ID).
			Str("channel_name", match.Channel.Name).
			Str("user", match.User)
		event = addSlackTimestampFields(event, match.TS, c.debugOutput(), c.now()).
			Str("text", text).
			Str("permalink", match.Permalink)
		event.Msg(commandMessage(command))
	}
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
	event = addStringField(event, "user", channel.User)
	event = addStringField(event, "topic", channel.Topic)
	event = addIntField(event, "num_members", channel.NumMembers)
	event.Msg(commandMessage(command))
	return nil
}

func (c *CommandContext) WriteChannels(command string, channels []cliChannel, pagination *Pagination) error {
	if len(channels) > 0 {
		return c.WriteChannelTable(command, channels)
	}
	if len(channels) == 0 {
		event := c.resultEvent(command)
		addPaginationFields(event, pagination)
		event.Msg(commandMessage(command))
		return nil
	}
	for _, channel := range channels {
		event := c.resultEventWithStyles(command, entityFieldStyle("channel", channel.ID)).
			Str("channel", channel.ID).
			Str("name", channel.Name).
			Str("type", channel.Type)
		event = addBoolField(event, "is_member", channel.IsMember)
		event = addBoolField(event, "is_im", channel.IsIM)
		event = addBoolField(event, "is_archived", channel.IsArchived)
		event = addStringField(event, "user", channel.User)
		event = addStringField(event, "topic", channel.Topic)
		event = addIntField(event, "num_members", channel.NumMembers)
		event.Msg(commandMessage(command))
	}
	return nil
}

func (c *CommandContext) WriteUserInfo(command string, user cliUser) error {
	event := c.resultEventWithStyles(command, entityFieldStyle("user", user.ID)).
		Str("user", user.ID).
		Str("name", user.Name)
	event = addBoolField(event, "deleted", user.Deleted)
	event = addStringField(event, "timezone", user.Timezone)
	event = addStringField(event, "presence", user.Presence)
	event = addStringField(event, "status_text", user.StatusText)
	event.Msg(commandMessage(command))
	return nil
}

func (c *CommandContext) WriteUsers(command string, users []cliUser, pagination *Pagination) error {
	if len(users) > 0 {
		return c.WriteUserTable(users)
	}
	if len(users) == 0 {
		event := c.resultEvent(command)
		addPaginationFields(event, pagination)
		event.Msg(commandMessage(command))
		return nil
	}
	for _, user := range users {
		event := c.resultEvent(command).
			Str("user", user.ID).
			Str("name", user.Name)
		event = addBoolField(event, "deleted", user.Deleted)
		event = addStringField(event, "timezone", user.Timezone)
		event = addStringField(event, "presence", user.Presence)
		event = addStringField(event, "status_text", user.StatusText)
		event.Msg(commandMessage(command))
	}
	return nil
}

func (c *CommandContext) WriteWorkspaces(command string, workspaces []config.WorkspaceProfile, pagination *Pagination) error {
	if len(workspaces) > 0 {
		return c.WriteWorkspaceTable(workspaces)
	}
	if len(workspaces) == 0 {
		event := c.resultEvent(command)
		addPaginationFields(event, pagination)
		event.Msg(commandMessage(command))
		return nil
	}
	for _, workspace := range workspaces {
		logger := c.stdoutLogger()
		if workspace.TeamID != "" {
			applyTeamIDStyle(logger, workspace.TeamID)
		}
		event := logger.Info().
			Parts(clog.PartLevel, clog.PartMessage, clog.PartFields).
			Str("command", command).
			Str("workspace", workspace.Name).
			Str("team_id", workspace.TeamID).
			Str("token_type", string(workspace.TokenType)).
			Str("token", workspace.TokenRef).
			When(workspace.TeamName != "", func(e *clog.Event) {
				e.Str("team_name", workspace.TeamName)
			})
		event.Msg(commandMessage(command))
	}
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
		Str("path", human.ContractHome(data.Path)).
		Str("profile", data.Profile).
		Str("workspace", data.Workspace).
		Bool("written", data.Written).
		Msg("config init")
	return nil
}

func (c *CommandContext) WriteConfigPath(command string, data configPathData) error {
	c.resultEvent(command).
		Str("path", human.ContractHome(data.Path)).
		Bool("exists", data.Exists).
		Msg(commandMessage(command))
	return nil
}

func (c *CommandContext) WriteConfigList(command string, data configListData) error {
	c.resultEvent(command).
		Str("path", human.ContractHome(data.Path)).
		Str("default_workspace", data.DefaultWorkspace).
		Int("settings", len(data.Settings)).
		Msg(commandMessage(command))
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
		Msg(commandMessage(command))
	return nil
}

func (c *CommandContext) WriteConfigMutation(command string, data configMutationData) error {
	c.resultEvent(command).
		Str("path", human.ContractHome(data.Path)).
		Str("key", data.Key).
		When(data.Value != "", func(e *clog.Event) {
			e.Str("value", data.Value)
		}).
		Msg(commandMessage(command))
	return nil
}

func truncateText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return value[:limit]
	}
	return value[:limit-3] + "..."
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
		event := logger.Info().
			Parts(clog.PartLevel, clog.PartMessage, clog.PartFields).
			Str("workspace", workspace.Workspace).
			Bool("authenticated", workspace.Authenticated).
			When(workspace.TokenType != "", func(e *clog.Event) {
				e.Str("token_type", string(workspace.TokenType))
			}).
			When(workspace.TeamID != "", func(e *clog.Event) {
				applyTeamIDStyle(logger, workspace.TeamID)
				e.Str("team_id", workspace.TeamID)
			}).
			When(workspace.TeamName != "", func(e *clog.Event) {
				e.Str("team_name", workspace.TeamName)
			}).
			Bool("valid", state == "valid").
			When(workspace.ValidationError != "", func(e *clog.Event) {
				e.Str("validation_error", workspace.ValidationError)
			})
		event.Msg("auth status")
	}
	return nil
}

func applyTeamIDStyle(logger *clog.Logger, teamID string) {
	applyFieldStyles(logger, entityFieldStyle("team_id", teamID))
}

func applyFieldStyles(logger *clog.Logger, fields ...fieldStyle) {
	styles := clogstyle.Map{}
	for _, field := range fields {
		if field.Field == "" || field.Seed == "" {
			continue
		}
		if style := hashEntityStyle(theme.Default(), field.Seed); style != nil {
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
	if c.Mode == OutputModePlain {
		event := c.stderrLogger().Error().
			Str("type", err.Type).
			Int("exit_code", err.ExitCode)
		event = addCLIErrorDetails(event, err.Details)
		event.Msg(err.Message)
	} else {
		c.stderrLogger().Print().Mode(clog.JSONFlat).JSON(struct {
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

func (c *CommandContext) debugOutput() bool {
	for _, key := range []string{"DEBUG", "SLACK_CLI_DEBUG"} {
		if truthyEnv(os.Getenv(key)) {
			return true
		}
	}
	return false
}

func truthyEnv(value string) bool {
	if value == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func writeCommandError(ctx *CommandContext, err CLIError) error {
	ctx.WriteError(err)
	return CommandError{CLIError: err}
}

func writeRuntimeError(runtime *RootRuntime, err CLIError) error {
	ctx := &CommandContext{
		Workspace: "default",
		Mode:      OutputFlags{}.Resolve(runtime.IsTTY, false),
		Stdout:    runtime.Stdout,
		Stderr:    runtime.Stderr,
		Now:       runtime.Now,
		RequestID: runtime.RequestID,
	}
	return writeCommandError(ctx, err)
}

func validationCLIError(message string) CLIError {
	return CLIError{Type: ErrorTypeValidation, Message: message, ExitCode: ExitCodeValidation}
}

func authCLIError(message string) CLIError {
	return CLIError{Type: ErrorTypeAuth, Message: message, ExitCode: ExitCodeAuthFailure}
}

func (c *CommandContext) stdoutLogger() *clog.Logger {
	logger := clog.New(clog.NewOutput(c.stdout(), c.ColorMode))
	logger.SetOmitZero(true)
	return logger
}

func (c *CommandContext) stderrLogger() *clog.Logger {
	logger := clog.New(clog.NewOutput(c.stderr(), c.ColorMode))
	logger.SetOmitZero(true)
	return logger
}

func (c *CommandContext) stdout() io.Writer {
	if c.Stdout != nil {
		return c.Stdout
	}
	return io.Discard
}

func (c *CommandContext) stderr() io.Writer {
	if c.Stderr != nil {
		return c.Stderr
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
	root := NewRootCommand()
	if err := root.Execute(); err != nil {
		var commandErr CommandError
		if errors.As(err, &commandErr) {
			os.Exit(commandErr.CLIError.ExitCode)
		}
		ctx := &CommandContext{
			Workspace: "default",
			Mode:      outputFlagsFromCommand(root).Resolve(terminal.Is(os.Stdout), false),
			Stdout:    os.Stdout,
			Stderr:    os.Stderr,
		}
		os.Exit(ctx.WriteError(validationCLIError(err.Error())))
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
