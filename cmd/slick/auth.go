package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"image/color"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	clibtheme "github.com/gechr/clib/theme"
	"github.com/gechr/clog"
	"github.com/gechr/x/human"
	"github.com/gechr/x/shell"
	"github.com/matcra587/slack-cli/internal/config"
	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

type authWorkspaceData struct {
	Workspace       string           `json:"workspace"`
	Authenticated   bool             `json:"authenticated"`
	TokenType       config.TokenType `json:"token_type,omitempty"`
	TeamID          string           `json:"team_id,omitempty"`
	TeamName        string           `json:"team_name,omitempty"`
	ValidationState string           `json:"validation_state,omitempty"`
	ValidationError string           `json:"validation_error,omitempty"`
}

type authStatusData struct {
	Workspaces []authWorkspaceData `json:"workspaces"`
}

func newAuthCommand(runtime *RootRuntime) *cobra.Command {
	authCmd := &cobra.Command{Use: "auth", Short: "Manage Slack authentication"}

	var workspaceName string
	var tokenStdin bool
	var tokenFile string
	var tokenEnv string
	var teamID string
	var teamName string
	var authMethod string
	var clientID string
	var oauthRedirectURL string
	var oauthCallbackPort string
	var force bool
	loginCmd := &cobra.Command{
		Use:          "login",
		Short:        "Create a workspace profile",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if oauthCallbackPort != "" {
				oauthRedirectURL = oauthRedirectURLForPort(oauthCallbackPort)
			}
			return runAuthLogin(cmd, runtime, authLoginInput{
				WorkspaceName: workspaceName,
				TokenStdin:    tokenStdin,
				TokenFile:     tokenFile,
				TokenEnv:      tokenEnv,
				TeamID:        teamID,
				TeamName:      teamName,
				AuthMethod:    authMethod,
				ClientID:      clientID,
				OAuthRedirect: oauthRedirectURL,
				Force:         force,
			})
		},
	}
	loginCmd.Flags().StringVar(&workspaceName, "workspace-name", "", "Workspace profile name")
	loginCmd.Flags().BoolVarP(&tokenStdin, "token-stdin", "s", false, "Read Slack token from stdin")
	loginCmd.Flags().StringVarP(&tokenFile, "token-file", "f", "", "Read Slack token from file")
	loginCmd.Flags().StringVarP(&tokenEnv, "token-env", "e", "", "Read Slack token from named environment variable")
	loginCmd.Flags().StringVarP(&teamID, "team-id", "T", "", "Slack workspace ID")
	loginCmd.Flags().StringVarP(&teamName, "team-name", "N", "", "Slack workspace display name")
	loginCmd.Flags().StringVarP(&authMethod, "method", "m", "", "Auth mechanism: oauth or token")
	loginCmd.Flags().StringVar(&authMethod, "auth-method", "", "Auth mechanism: oauth or token")
	loginCmd.Flags().StringVarP(&clientID, "oauth-client-id", "C", "", "Slack OAuth client ID")
	loginCmd.Flags().StringVar(&clientID, "client-id", "", "Slack OAuth client ID")
	loginCmd.Flags().StringVarP(&oauthRedirectURL, "oauth-redirect-url", "r", defaultOAuthRedirectURL(), "Slack OAuth redirect URL configured on the app")
	loginCmd.Flags().StringVarP(&oauthCallbackPort, "oauth-callback-port", "p", "", "Local OAuth callback port; use 0 for an OS-assigned port")
	loginCmd.Flags().BoolVarP(&force, "force", "F", false, "Overwrite an existing authenticated profile")
	_ = loginCmd.Flags().MarkHidden("workspace-name")
	_ = loginCmd.Flags().MarkHidden("auth-method")
	_ = loginCmd.Flags().MarkHidden("client-id")

	statusCmd := &cobra.Command{
		Use:          "status",
		Short:        "Show auth status",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAuthStatus(cmd, runtime)
		},
	}

	switchCmd := &cobra.Command{
		Use:          "switch <workspace>",
		Short:        "Switch default workspace",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthSwitch(cmd, runtime, args[0])
		},
	}

	logoutCmd := &cobra.Command{
		Use:          "logout <workspace>",
		Short:        "Remove workspace credentials",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuthLogout(cmd, runtime, args[0])
		},
	}

	authCmd.AddCommand(loginCmd, statusCmd, switchCmd, logoutCmd)
	return authCmd
}

func runAuthLogin(cmd *cobra.Command, runtime *RootRuntime, input authLoginInput) error {
	opts := rootOptionsFromCommand(cmd, runtime)
	if input.WorkspaceName == "" {
		input.WorkspaceName = opts.Workspace
	}
	ctx, _, _, err := commandContext(cmd, runtime)
	if err != nil {
		ctx = &CommandContext{
			Workspace: "default",
			Mode:      opts.Output.Resolve(runtime.IsTTY, false),
			Stdout:    runtime.Stdout,
			Stderr:    runtime.Stderr,
			Now:       runtime.Now,
			RequestID: runtime.RequestID,
		}
	}
	interactive := runtime.IsTTY && input.WorkspaceName == "" && !input.HasTokenSource() && input.AuthMethod == ""
	if interactive {
		if err := runAuthLoginForm(ctx, runtime, &input); err != nil {
			return writeCommandError(ctx, validationCLIError(err.Error()))
		}
	}
	if input.WorkspaceName == "" {
		return writeCommandError(ctx, validationCLIError("workspace-name is required"))
	}
	if input.AuthMethod == "" {
		input.AuthMethod = "token"
	}
	switch input.AuthMethod {
	case "oauth":
		complete, err := completeOAuthLogin(ctx, runtime, &input)
		if err != nil {
			return writeCommandError(ctx, authCLIErrorFromError(err))
		}
		if !complete {
			return nil
		}
	case "token":
		if err := resolveLoginTokenSource(runtime, &input); err != nil {
			return writeCommandError(ctx, validationCLIError(err.Error()))
		}
		auth, err := validateLoginToken(runtime, input.Token)
		if err != nil {
			return writeCommandError(ctx, authCLIError(err.Error()))
		}
		input.TokenType = tokenType(input.Token)
		if input.TeamID == "" {
			input.TeamID = auth.TeamID
		}
		if input.TeamName == "" {
			input.TeamName = auth.Team
		}
	default:
		return writeCommandError(ctx, validationCLIError("auth-method must be oauth or token"))
	}
	cfg := runtime.Config
	if cfg == nil {
		cfg = &config.Config{SchemaVersion: config.SchemaVersion, Workspaces: map[string]config.WorkspaceProfile{}}
	}
	if cfg.Workspaces == nil {
		cfg.Workspaces = map[string]config.WorkspaceProfile{}
	}
	profileName := canonicalProfileName(cfg, input.WorkspaceName)
	if input.TeamID == "" {
		return writeCommandError(ctx, validationCLIError("workspace id is required"))
	}
	if input.TeamName == "" {
		input.TeamName = profileName
	}
	if existing, ok := cfg.Workspaces[profileName]; ok && workspaceHasAuth(existing) && !input.Force {
		return writeCommandError(ctx, validationCLIError("workspace profile is already authenticated; rerun with --force to overwrite auth fields"))
	}
	secret, err := encodeLoginCredential(ctx, input)
	if err != nil {
		return writeCommandError(ctx, authCLIError(err.Error()))
	}
	if err := runtime.CredentialStore.Set("slack-cli", profileName, secret); err != nil {
		return writeCommandError(ctx, authCLIError(err.Error()))
	}

	if cfg.DefaultWorkspace == "" {
		cfg.DefaultWorkspace = profileName
	}
	profile := cfg.Workspaces[profileName]
	profile.Name = profileName
	profile.TeamID = input.TeamID
	profile.TeamName = input.TeamName
	profile.TokenType = input.resolvedTokenType()
	profile.TokenRef = "keychain:slack-cli/" + profileName
	cfg.Workspaces[profileName] = profile
	if runtime.ConfigPath != "" {
		if err := config.SaveFile(runtime.ConfigPath, cfg); err != nil {
			return writeCommandError(ctx, validationCLIError(err.Error()))
		}
	}
	runtime.Config = cfg
	return ctx.WriteResult("auth.login", authWorkspaceData{Workspace: profileName, Authenticated: true, TokenType: input.resolvedTokenType(), TeamID: input.TeamID, TeamName: input.TeamName})
}

func canonicalProfileName(cfg *config.Config, name string) string {
	for existing := range cfg.Workspaces {
		if strings.EqualFold(existing, name) {
			return existing
		}
	}
	return name
}

func workspaceHasAuth(profile config.WorkspaceProfile) bool {
	return profile.TeamID != "" || profile.TeamName != "" || profile.TokenType != "" || profile.TokenRef != ""
}

type authLoginInput struct {
	WorkspaceName string
	Token         string
	TokenStdin    bool
	TokenFile     string
	TokenEnv      string
	TokenType     config.TokenType
	TeamID        string
	TeamName      string
	AuthMethod    string
	ClientID      string
	OAuthRedirect string
	RefreshToken  string
	ExpiresIn     int
	Force         bool
}

func (input authLoginInput) HasTokenSource() bool {
	return input.Token != "" || input.TokenStdin || input.TokenFile != "" || input.TokenEnv != ""
}

func (input authLoginInput) resolvedTokenType() config.TokenType {
	if input.TokenType != "" {
		return input.TokenType
	}
	return tokenType(input.Token)
}

func resolveLoginTokenSource(runtime *RootRuntime, input *authLoginInput) error {
	sourceCount := 0
	if input.TokenStdin {
		sourceCount++
	}
	if input.TokenFile != "" {
		sourceCount++
	}
	if input.TokenEnv != "" {
		sourceCount++
	}
	if sourceCount > 1 {
		return errors.New("token source flags are mutually exclusive")
	}
	if input.Token == "" {
		switch {
		case input.TokenStdin:
			raw, err := io.ReadAll(runtime.Stdin)
			if err != nil {
				return fmt.Errorf("reading token from stdin: %w", err)
			}
			input.Token = trimTokenSource(raw)
		case input.TokenFile != "":
			raw, err := os.ReadFile(human.ExpandPath(input.TokenFile))
			if err != nil {
				return fmt.Errorf("reading token file: %w", err)
			}
			input.Token = trimTokenSource(raw)
		case input.TokenEnv != "":
			name := strings.TrimSpace(input.TokenEnv)
			if name == "" {
				return errors.New("token-env is required")
			}
			input.Token = strings.TrimSpace(os.Getenv(name))
			if input.Token == "" {
				return fmt.Errorf("token environment variable %s is empty", name)
			}
		default:
			return errors.New("token source is required; use --token-stdin, --token-file, --token-env, or interactive login")
		}
	}
	return validateLoginTokenContent(input.Token)
}

func trimTokenSource(raw []byte) string {
	return strings.TrimRight(string(raw), "\r\n")
}

func validateLoginTokenContent(token string) error {
	if token == "" {
		return errors.New("token is empty")
	}
	if strings.TrimSpace(token) != token || strings.ContainsAny(token, "\r\n\t ") {
		return errors.New("token content is malformed")
	}
	if !strings.HasPrefix(token, "xoxb-") && !strings.HasPrefix(token, "xoxp-") {
		return errors.New("token content is malformed; expected xoxb- or xoxp- token")
	}
	return nil
}

type authFieldHelp struct {
	Description string
	Placeholder string
}

func runAuthLoginForm(ctx *CommandContext, runtime *RootRuntime, input *authLoginInput) error {
	accessible := !usesTerminalFiles(runtime)
	help := authLoginFieldHelp()
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Profile name").
				Description(help["workspace"].Description).
				Placeholder(help["workspace"].Placeholder).
				Value(&input.WorkspaceName).
				Validate(requiredField("profile name")),
			huh.NewSelect[string]().
				Title("Auth mechanism").
				Description(help["auth_method"].Description).
				Options(
					huh.NewOption("Slack OAuth", "oauth"),
					huh.NewOption("Paste token", "token"),
				).
				Value(&input.AuthMethod),
		),
	).
		WithTheme(authLoginHuhTheme(clibtheme.Default())).
		WithInput(runtime.Stdin).
		WithOutput(runtime.Stderr)
	if accessible {
		form.WithAccessible(true)
	}
	if err := form.Run(); err != nil {
		return err
	}
	switch input.AuthMethod {
	case "oauth":
		return runOAuthLoginForm(ctx, runtime, input, help)
	default:
		input.AuthMethod = "token"
		return runTokenLoginForm(runtime, input, help, accessible)
	}
}

func authTokenInput(token *string, help authFieldHelp, accessible bool) *huh.Input {
	field := huh.NewInput().
		Title("Slack token").
		Description(help.Description).
		Placeholder(help.Placeholder).
		Value(token).
		Validate(requiredField("slack token"))
	if !accessible {
		field.EchoMode(huh.EchoModePassword)
	}
	return field
}

func runTokenLoginForm(runtime *RootRuntime, input *authLoginInput, help map[string]authFieldHelp, accessible bool) error {
	form := huh.NewForm(
		huh.NewGroup(authTokenInput(&input.Token, help["token"], accessible)),
	).
		WithTheme(authLoginHuhTheme(clibtheme.Default())).
		WithInput(runtime.Stdin).
		WithOutput(runtime.Stderr)
	if accessible {
		form.WithAccessible(true)
	}
	return form.Run()
}

func runOAuthLoginForm(ctx *CommandContext, runtime *RootRuntime, input *authLoginInput, help map[string]authFieldHelp) error {
	accessible := !usesTerminalFiles(runtime)
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Slack OAuth client ID").
				Description(help["client_id"].Description).
				Placeholder(help["client_id"].Placeholder).
				Value(&input.ClientID).
				Validate(requiredField("oauth client id")),
			huh.NewInput().
				Title("OAuth redirect URL").
				Description(help["oauth_redirect"].Description).
				Placeholder(help["oauth_redirect"].Placeholder).
				Value(&input.OAuthRedirect).
				Validate(validateOAuthRedirectField),
		),
	).
		WithTheme(authLoginHuhTheme(clibtheme.Default())).
		WithInput(runtime.Stdin).
		WithOutput(runtime.Stderr)
	if accessible {
		form.WithAccessible(true)
	}
	return form.Run()
}

func authLoginFieldHelp() map[string]authFieldHelp {
	return map[string]authFieldHelp{
		"workspace": {
			Description: "Local Slack CLI profile. Select it later with --workspace; use default if you only need one profile.",
			Placeholder: "default",
		},
		"auth_method": {
			Description: "Use Slack OAuth for guided setup, or paste an existing xoxp-/xoxb- token when automation provisions credentials for you.",
			Placeholder: "oauth",
		},
		"token": {
			Description: "Slack issues tokens through an app install. An xoxp- user token acts as you; an xoxb- bot token acts as the app bot. The raw value is stored in keychain.",
			Placeholder: "xoxb-...",
		},
		"team_id": {
			Description: "Workspace ID. In https://app.slack.com/client/T8KQ42P9D/C7N2Q8L4P, use T8KQ42P9D. auth.test also returns it as team_id.",
			Placeholder: "T8KQ42P9D",
		},
		"client_id": {
			Description: "OAuth client ID from your Slack app Basic Information page.",
			Placeholder: "1234567890.1234567890",
		},
		"oauth_redirect": {
			Description: "Local HTTP redirect URL configured in the Slack app OAuth settings. Slack returns to this loopback callback after approval.",
			Placeholder: defaultOAuthRedirectURL(),
		},
	}
}

func validateOAuthRedirectField(value string) error {
	_, err := oauthRedirectURL(value)
	return err
}

func authLoginHuhTheme(th *clibtheme.Theme) huh.Theme {
	if th == nil {
		th = clibtheme.Default()
	}
	return huh.ThemeFunc(func(isDark bool) *huh.Styles {
		if th.String() == "plain" || th.String() == "monochrome" {
			return huh.ThemeBase(isDark)
		}
		resolved := th.Init()

		t := huh.ThemeBase(isDark)
		helpCommand := authLoginHuhStyle(resolved.HelpCommand)
		helpDim := authLoginHuhStyle(resolved.HelpDim)
		helpPlaceholder := authLoginHuhStyle(resolved.HelpValuePlaceholder)
		red := authLoginHuhStyle(resolved.Red)

		t.Focused.Title = helpCommand
		t.Focused.NoteTitle = helpCommand
		t.Focused.Description = helpDim
		t.Focused.ErrorIndicator = authLoginMergeHuhStyle(t.Focused.ErrorIndicator, red)
		t.Focused.ErrorMessage = red
		t.Focused.TextInput.Cursor = authLoginMergeHuhStyle(t.Focused.TextInput.Cursor, helpCommand)
		t.Focused.TextInput.Placeholder = helpPlaceholder
		t.Focused.TextInput.Prompt = helpCommand

		t.Blurred = t.Focused
		t.Blurred.Base = t.Focused.Base.BorderStyle(lipgloss.HiddenBorder())
		t.Blurred.Card = t.Blurred.Base
		t.Blurred.NextIndicator = lipgloss.NewStyle()
		t.Blurred.PrevIndicator = lipgloss.NewStyle()

		t.Group.Title = t.Focused.Title
		t.Group.Description = t.Focused.Description
		return t
	})
}

func authLoginMergeHuhStyle(base, override lipgloss.Style) lipgloss.Style {
	if foreground := override.GetForeground(); foreground != nil {
		base = base.Foreground(foreground)
	}
	if background := override.GetBackground(); background != nil {
		base = base.Background(background)
	}
	if override.GetBold() {
		base = base.Bold(true)
	}
	if override.GetFaint() {
		base = base.Faint(true)
	}
	if override.GetItalic() {
		base = base.Italic(true)
	}
	if override.GetUnderline() {
		base = base.Underline(true)
	}
	return base
}

func authLoginHuhStyle(style *lipgloss.Style) lipgloss.Style {
	if style == nil {
		return lipgloss.NewStyle()
	}
	converted := lipgloss.NewStyle()
	if style.GetBold() {
		converted = converted.Bold(true)
	}
	if style.GetFaint() {
		converted = converted.Faint(true)
	}
	if style.GetItalic() {
		converted = converted.Italic(true)
	}
	if style.GetUnderline() {
		converted = converted.Underline(true)
	}
	if foreground := authLoginHuhColor(style.GetForeground()); foreground != nil {
		converted = converted.Foreground(foreground)
	}
	if background := authLoginHuhColor(style.GetBackground()); background != nil {
		converted = converted.Background(background)
	}
	return converted
}

const colorComponentShift = 8

func authLoginHuhColor(c color.Color) color.Color {
	if c == nil {
		return nil
	}
	if _, ok := c.(lipgloss.NoColor); ok {
		return nil
	}
	r, g, b, _ := c.RGBA()
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", uint8(r>>colorComponentShift), uint8(g>>colorComponentShift), uint8(b>>colorComponentShift)))
}

func usesTerminalFiles(runtime *RootRuntime) bool {
	_, stdinIsFile := runtime.Stdin.(*os.File)
	_, stderrIsFile := runtime.Stderr.(*os.File)
	return stdinIsFile && stderrIsFile
}

func requiredField(name string) func(string) error {
	return func(value string) error {
		if strings.TrimSpace(value) == "" {
			return errors.New(name + " is required")
		}
		return nil
	}
}

func completeOAuthLogin(ctx *CommandContext, runtime *RootRuntime, input *authLoginInput) (bool, error) {
	if input.ClientID == "" {
		return false, errors.New("oauth client id is required")
	}
	redirectURL, err := oauthRedirectURL(input.OAuthRedirect)
	if err != nil {
		return false, err
	}
	verifier, err := slackgo.GenerateCodeVerifier()
	if err != nil {
		return false, err
	}
	state, err := oauthRandomState()
	if err != nil {
		return false, err
	}
	listener, err := net.Listen("tcp", redirectURL.Host)
	if err != nil {
		return false, fmt.Errorf("oauth redirect listener failed for %s: %w", redirectURL.String(), err)
	}
	defer func() { _ = listener.Close() }()
	redirectURL, err = oauthRedirectURLForListener(redirectURL, listener)
	if err != nil {
		return false, err
	}

	resultCh := make(chan oauthCallbackResult, 1)
	server := &http.Server{Handler: oauthCallbackHandler(state, redirectURL.Path, resultCh)}
	defer func() { _ = server.Close() }()
	go func() {
		if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			resultCh <- oauthCallbackResult{Err: serveErr}
		}
	}()

	authorizeURL := oauthAuthorizeURL(oauthAuthorizeParams{
		ClientID:      input.ClientID,
		RedirectURI:   redirectURL.String(),
		State:         state,
		CodeChallenge: slackgo.GenerateCodeChallenge(verifier),
	})
	ctx.stderrLogger().Info().
		Parts(clog.PartLevel, clog.PartMessage, clog.PartFields).
		URL("authorize_url", authorizeURL).
		URL("redirect_url", redirectURL.String()).
		Msg("open OAuth authorize URL")
	openURL := runtime.OpenURL
	if openURL == nil {
		openURL = defaultOpenURL
	}
	_ = openURL(authorizeURL)

	timeout := runtime.OAuthTimeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	var callback oauthCallbackResult
	select {
	case callback = <-resultCh:
	case <-time.After(timeout):
		return false, oauthTimeoutError{RedirectURL: redirectURL.String()}
	}
	if callback.Err != nil {
		return false, callback.Err
	}
	response, err := exchangeOAuthUserCode(runtime, input.ClientID, callback.Code, redirectURL.String(), verifier)
	if err != nil {
		return false, err
	}
	if err := applyOAuthResponse(input, response); err != nil {
		return false, err
	}
	ctx.stderrLogger().Info().
		Parts(clog.PartLevel, clog.PartMessage, clog.PartFields).
		Bool("authenticated", true).
		Str("token_type", string(input.resolvedTokenType())).
		Str("team_id", input.TeamID).
		Str("team_name", input.TeamName).
		Msg("oauth token exchange completed")
	return true, nil
}

func applyOAuthResponse(input *authLoginInput, response *slackgo.OAuthV2Response) error {
	if response.AuthedUser.AccessToken != "" {
		input.Token = response.AuthedUser.AccessToken
		input.RefreshToken = response.AuthedUser.RefreshToken
		input.ExpiresIn = response.AuthedUser.ExpiresIn
		input.TokenType = config.TokenTypeUser
	} else {
		input.Token = response.AccessToken
		input.RefreshToken = response.RefreshToken
		input.ExpiresIn = response.ExpiresIn
		switch strings.ToLower(response.TokenType) {
		case "user":
			input.TokenType = config.TokenTypeUser
		case "bot":
			input.TokenType = config.TokenTypeBot
		}
	}
	if input.Token == "" {
		return errors.New("oauth response did not include an access token")
	}
	if input.TeamID == "" {
		input.TeamID = response.Team.ID
	}
	if input.TeamName == "" {
		input.TeamName = response.Team.Name
	}
	input.ClientID = strings.TrimSpace(input.ClientID)
	return nil
}

type oauthTimeoutError struct {
	RedirectURL string
}

func (e oauthTimeoutError) Error() string {
	return "oauth flow timed out waiting"
}

func authCLIErrorFromError(err error) CLIError {
	var timeout oauthTimeoutError
	if errors.As(err, &timeout) {
		return CLIError{
			Type:     ErrorTypeAuth,
			Message:  timeout.Error(),
			Details:  map[string]any{"redirect_url": timeout.RedirectURL},
			ExitCode: ExitCodeAuthFailure,
		}
	}
	return authCLIError(err.Error())
}

func encodeLoginCredential(ctx *CommandContext, input authLoginInput) (string, error) {
	payload := config.CredentialPayload{AccessToken: input.Token, RefreshToken: input.RefreshToken, ClientID: input.ClientID}
	if input.ExpiresIn > 0 {
		expiresAt := ctx.now().Add(time.Duration(input.ExpiresIn) * time.Second)
		payload.ExpiresAt = &expiresAt
	}
	return config.EncodeCredential(payload)
}

type oauthAuthorizeParams struct {
	ClientID      string
	RedirectURI   string
	State         string
	CodeChallenge string
}

func oauthAuthorizeURL(params oauthAuthorizeParams) string {
	scopes, _ := manifestPresetScopes(defaultManifestPreset)
	values := url.Values{
		"client_id":  {params.ClientID},
		"user_scope": {strings.Join(scopes, ",")},
	}
	if params.RedirectURI != "" {
		values.Set("redirect_uri", params.RedirectURI)
	}
	if params.State != "" {
		values.Set("state", params.State)
	}
	if params.CodeChallenge != "" {
		values.Set("code_challenge", params.CodeChallenge)
		values.Set("code_challenge_method", "S256")
	}
	return "https://slack.com/oauth/v2/authorize?" + values.Encode()
}

type oauthCallbackResult struct {
	Code string
	Err  error
}

const (
	defaultOAuthCallbackPath = "/callback"
	osAssignedCallbackPort   = "0"
)

func defaultOAuthRedirectURL() string {
	return oauthRedirectURLForPort(defaultOAuthCallbackPort())
}

func defaultManifestOAuthRedirectURL() string {
	port := defaultOAuthCallbackPort()
	if port == osAssignedCallbackPort {
		if allocated, err := allocateLocalOAuthCallbackPort(); err == nil {
			port = allocated
		}
	}
	return oauthRedirectURLForPort(port)
}

func defaultOAuthCallbackPort() string {
	if port := strings.TrimSpace(os.Getenv("SLACK_CLI_CALLBACK_PORT")); port != "" {
		return port
	}
	return osAssignedCallbackPort
}

func oauthRedirectURLForPort(port string) string {
	port = strings.TrimSpace(port)
	if port == "" {
		port = defaultOAuthCallbackPort()
	}
	return "http://localhost:" + port + defaultOAuthCallbackPath
}

func oauthRedirectURLForListener(redirectURL *url.URL, listener net.Listener) (*url.URL, error) {
	if redirectURL.Port() != osAssignedCallbackPort {
		return redirectURL, nil
	}
	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		return nil, fmt.Errorf("reading oauth listener port: %w", err)
	}
	copyURL := *redirectURL
	copyURL.Host = net.JoinHostPort(redirectURL.Hostname(), port)
	return &copyURL, nil
}

func allocateLocalOAuthCallbackPort() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	_, port, splitErr := net.SplitHostPort(listener.Addr().String())
	closeErr := listener.Close()
	if splitErr != nil {
		return "", splitErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	return port, nil
}

func oauthRedirectURL(raw string) (*url.URL, error) {
	if strings.TrimSpace(raw) == "" {
		raw = defaultOAuthRedirectURL()
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != "http" {
		return nil, errors.New("oauth redirect url must use local http for PKCE callback")
	}
	if !isLoopbackHost(parsed.Hostname()) {
		return nil, errors.New("oauth redirect url host must be localhost or a loopback address")
	}
	if parsed.Port() == "" {
		return nil, errors.New("oauth redirect url must include a port")
	}
	if parsed.Path == "" || parsed.Path == "/" {
		parsed.Path = defaultOAuthCallbackPath
	}
	return parsed, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func oauthCallbackHandler(state, expectedPath string, resultCh chan<- oauthCallbackResult) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if r.URL.Path != expectedPath {
			http.NotFound(w, r)
			return
		}
		if errValue := query.Get("error"); errValue != "" {
			sendOAuthCallbackResult(resultCh, oauthCallbackResult{Err: errors.New(errValue)})
			writeOAuthCallbackHTML(w, http.StatusBadRequest, "Slack OAuth failed", errValue)
			return
		}
		if query.Get("state") != state {
			sendOAuthCallbackResult(resultCh, oauthCallbackResult{Err: errors.New("oauth callback state mismatch")})
			writeOAuthCallbackHTML(w, http.StatusBadRequest, "OAuth state mismatch", "Return to the terminal and retry the login command.")
			return
		}
		code := query.Get("code")
		if code == "" {
			sendOAuthCallbackResult(resultCh, oauthCallbackResult{Err: errors.New("oauth callback missing code")})
			writeOAuthCallbackHTML(w, http.StatusBadRequest, "OAuth code missing", "Return to the terminal and retry the login command.")
			return
		}
		sendOAuthCallbackResult(resultCh, oauthCallbackResult{Code: code})
		writeOAuthCallbackHTML(w, http.StatusOK, "Authorisation successful", "You can close this tab and return to the terminal.")
	})
}

func writeOAuthCallbackHTML(w http.ResponseWriter, status int, title, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `<!doctype html><html><body style="font-family:sans-serif"><h2>%s</h2><p>%s</p></body></html>`, html.EscapeString(title), html.EscapeString(body))
}

func sendOAuthCallbackResult(resultCh chan<- oauthCallbackResult, result oauthCallbackResult) {
	select {
	case resultCh <- result:
	default:
	}
}

func oauthRandomState() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func defaultOpenURL(target string) error {
	if opener := os.Getenv("BROWSER"); opener != "" {
		parts, err := shell.Split(opener)
		if err != nil {
			return fmt.Errorf("parsing BROWSER: %w", err)
		}
		if len(parts) == 0 {
			return nil
		}
		return exec.Command(parts[0], append(parts[1:], target)...).Start() //nolint:gosec // BROWSER is explicit user configuration and exec.Command does not invoke a shell.
	}
	for _, opener := range []string{"xdg-open", "open"} {
		if path, err := exec.LookPath(opener); err == nil {
			return exec.Command(path, target).Start() //nolint:gosec // Path is resolved from a fixed opener allowlist.
		}
	}
	return nil
}

func validateLoginToken(runtime *RootRuntime, token string) (*slackgo.AuthTestResponse, error) {
	return slackAuthClient(token, runtime).AuthTestContext(context.Background())
}

func slackAuthClient(token string, runtime *RootRuntime) *slackgo.Client {
	options := []slackgo.Option{
		slackgo.OptionHTTPClient(slackHTTPClientForRuntime(runtime)),
		slackgo.OptionRetryConfig(slackRetryConfig()),
	}
	if runtime.SlackBaseURL != "" {
		options = append(options, slackgo.OptionAPIURL(slackAPIURL(runtime.SlackBaseURL)))
	}
	return slackgo.New(token, options...)
}

func runAuthStatus(cmd *cobra.Command, runtime *RootRuntime) error {
	ctx, _, _, err := commandContext(cmd, runtime)
	if err != nil {
		return writeRuntimeError(runtime, validationCLIError(err.Error()))
	}
	var workspaces []authWorkspaceData
	if runtime.Config != nil {
		for name, profile := range runtime.Config.Workspaces {
			validationState := "missing"
			validationError := ""
			authenticated := false
			client, clientErr := slackClient(cmd, profile, runtime)
			if clientErr != nil {
				if !errors.Is(clientErr, config.ErrCredentialNotFound) {
					validationState = "invalid"
					validationError = clientErr.Error()
				}
			} else if _, callErr := client.AuthTestContext(context.Background()); callErr != nil {
				validationState = "invalid"
				validationError = callErr.Error()
			} else {
				authenticated = true
				validationState = "valid"
			}
			workspaces = append(workspaces, authWorkspaceData{
				Workspace:       name,
				Authenticated:   authenticated,
				TokenType:       profile.TokenType,
				TeamID:          profile.TeamID,
				TeamName:        profile.TeamName,
				ValidationState: validationState,
				ValidationError: validationError,
			})
		}
	}
	return ctx.WriteResult("auth.status", authStatusData{Workspaces: workspaces})
}

func runAuthSwitch(cmd *cobra.Command, runtime *RootRuntime, workspace string) error {
	ctx, _, _, err := commandContext(cmd, runtime)
	if err != nil {
		return writeRuntimeError(runtime, validationCLIError(err.Error()))
	}
	if runtime.Config == nil {
		return writeCommandError(ctx, validationCLIError("config is required"))
	}
	if _, ok := runtime.Config.Workspaces[workspace]; !ok {
		return writeCommandError(ctx, validationCLIError("workspace not configured"))
	}
	runtime.Config.DefaultWorkspace = workspace
	if runtime.ConfigPath != "" {
		if err := config.SaveFile(runtime.ConfigPath, runtime.Config); err != nil {
			return writeCommandError(ctx, validationCLIError(err.Error()))
		}
	}
	return ctx.WriteResult("auth.switch", authWorkspaceData{Workspace: workspace})
}

func runAuthLogout(cmd *cobra.Command, runtime *RootRuntime, workspace string) error {
	ctx, _, _, err := commandContext(cmd, runtime)
	if err != nil {
		return writeRuntimeError(runtime, validationCLIError(err.Error()))
	}
	_ = runtime.CredentialStore.Delete("slack-cli", workspace)
	if runtime.Config != nil && runtime.Config.Workspaces != nil {
		profile, ok := runtime.Config.Workspaces[workspace]
		if ok {
			profile.TeamID = ""
			profile.TeamName = ""
			profile.TokenType = ""
			profile.TokenRef = ""
			if configManagedProfile(profile) {
				runtime.Config.Workspaces[workspace] = profile
			} else {
				delete(runtime.Config.Workspaces, workspace)
			}
		}
		if runtime.Config.DefaultWorkspace == workspace && !configManagedProfile(runtime.Config.Workspaces[workspace]) {
			runtime.Config.DefaultWorkspace = firstWorkspaceName(runtime.Config.Workspaces)
		}
		if runtime.ConfigPath != "" && len(runtime.Config.Workspaces) > 0 {
			if err := config.SaveFile(runtime.ConfigPath, runtime.Config); err != nil {
				return writeCommandError(ctx, validationCLIError(err.Error()))
			}
		}
	}
	return ctx.WriteResult("auth.logout", authWorkspaceData{Workspace: workspace, Authenticated: false})
}

func configManagedProfile(profile config.WorkspaceProfile) bool {
	if profile.Name == "" {
		return false
	}
	return profile.DefaultChannel != "" ||
		profile.AgentAttribution != nil ||
		profile.AgentLabel != "" ||
		profile.AgentEmoji != "" ||
		profile.AgentMessage != "" ||
		profile.Attribution.Label != "" ||
		profile.Attribution.Emoji != "" ||
		profile.Attribution.Message != "" ||
		profile.RateLimitTier != "" ||
		len(profile.Aliases) > 0
}

func firstWorkspaceName(workspaces map[string]config.WorkspaceProfile) string {
	for name := range workspaces {
		return name
	}
	return ""
}

func tokenType(token string) config.TokenType {
	if strings.HasPrefix(token, "xoxp-") || strings.Contains(token, "xoxp-") {
		return config.TokenTypeUser
	}
	return config.TokenTypeBot
}

func slackHTTPClientForRuntime(runtime *RootRuntime) *http.Client {
	if runtime.SlackBaseURL != "" {
		_ = runtime.SlackBaseURL
	}
	if runtime.HTTPClient != nil {
		return runtime.HTTPClient
	}
	return http.DefaultClient
}

func slackOAuthHTTPClient(runtime *RootRuntime) *http.Client {
	return slackHTTPClientForRuntime(runtime)
}
