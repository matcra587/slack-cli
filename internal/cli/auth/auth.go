package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"charm.land/huh/v2"
	clibtheme "github.com/gechr/clib/theme"
	"github.com/gechr/x/human"
	"github.com/gechr/x/shell"
	clitheme "github.com/matcra587/slack-cli/internal/cli/clitheme"
	climanifest "github.com/matcra587/slack-cli/internal/cli/manifest"
	clioauth "github.com/matcra587/slack-cli/internal/cli/oauth"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	slackclient "github.com/matcra587/slack-cli/internal/cli/slackclient"
	clitoken "github.com/matcra587/slack-cli/internal/cli/token"
	"github.com/matcra587/slack-cli/internal/config"
	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type WorkspaceData struct {
	Workspace       string           `json:"workspace"`
	Authenticated   bool             `json:"authenticated"`
	TokenType       config.TokenType `json:"token_type,omitempty"`
	TeamID          string           `json:"team_id,omitempty"`
	TeamName        string           `json:"team_name,omitempty"`
	ValidationState string           `json:"validation_state,omitempty"`
	ValidationError string           `json:"validation_error,omitempty"`
}

type StatusData struct {
	Workspaces []WorkspaceData `json:"workspaces"`
}

var _ clioutput.PlainRenderer = WorkspaceData{}

func (d WorkspaceData) WritePlain(c *clioutput.CommandContext, command string, _ *clioutput.Pagination) error {
	if d.TeamID != "" {
		clioutput.ApplyTeamIDStyle(c.StdoutLogger(), c.Theme, d.TeamID)
	}
	event := c.ResultEvent(command).
		Str("workspace", d.Workspace).
		Bool("authenticated", d.Authenticated).
		Str("token_type", string(d.TokenType)).
		Str("team_id", d.TeamID).
		Str("team_name", d.TeamName).
		Str("validation_error", d.ValidationError)
	event.Msg(clioutput.ActionLabel(command))
	return nil
}

var _ clioutput.PlainRenderer = StatusData{}

func (d StatusData) WritePlain(c *clioutput.CommandContext, _ string, _ *clioutput.Pagination) error {
	logger := c.StdoutLogger()
	for _, workspace := range d.Workspaces {
		state := workspace.ValidationState
		if state == "" {
			if workspace.Authenticated {
				state = "valid"
			} else {
				state = "missing"
			}
		}
		if workspace.TeamID != "" {
			clioutput.ApplyTeamIDStyle(logger, c.Theme, workspace.TeamID)
		}
		event := logger.Info().
			Str("workspace", workspace.Workspace).
			Str("token_type", string(workspace.TokenType)).
			Str("team_id", workspace.TeamID).
			Str("team_name", workspace.TeamName)
		if workspace.ValidationError != "" {
			event = event.Str("validation_error", workspace.ValidationError)
		}
		event.Msg(authStatusMessage(c.Theme, state))
	}
	return nil
}

// authStatusMessage returns a state-specific message painted with the
// theme's BoldGreen / Red so the label alone communicates auth state.
func authStatusMessage(th *clibtheme.Theme, state string) string {
	switch state {
	case "valid":
		if th != nil && th.BoldGreen != nil {
			return th.BoldGreen.Render("Authenticated")
		}
		return "Authenticated"
	case "invalid":
		if th != nil && th.Red != nil {
			return th.Red.Render("Authentication invalid")
		}
		return "Authentication invalid"
	default:
		if th != nil && th.Red != nil {
			return th.Red.Render("Credentials missing")
		}
		return "Credentials missing"
	}
}

// NewCommand returns the auth cobra command tree.
func NewCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
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
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if oauthCallbackPort != "" {
				oauthRedirectURL = clioauth.RedirectURLForPort(oauthCallbackPort)
			}
			method := authMethod
			if !cmd.Flags().Changed("method") {
				method = ""
			}
			return runAuthLogin(cmd, runtime, loginInput{
				WorkspaceName: workspaceName,
				TokenStdin:    tokenStdin,
				TokenFile:     tokenFile,
				TokenEnv:      tokenEnv,
				TeamID:        teamID,
				TeamName:      teamName,
				AuthMethod:    method,
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
	loginCmd.Flags().StringVarP(&authMethod, "method", "m", "token", "Auth mechanism: oauth or token")
	loginCmd.Flags().StringVarP(&clientID, "oauth-client-id", "C", "", "Slack OAuth client ID")
	loginCmd.Flags().StringVarP(&oauthRedirectURL, "oauth-redirect-url", "r", defaultOAuthRedirectURL(), "Slack OAuth redirect URL configured on the app")
	loginCmd.Flags().StringVarP(&oauthCallbackPort, "oauth-callback-port", "p", "", "Local OAuth callback port; use 0 for an OS-assigned port")
	loginCmd.Flags().BoolVarP(&force, "force", "F", false, "Overwrite an existing authenticated profile")
	_ = loginCmd.Flags().MarkHidden("workspace-name")
	loginCmd.Flags().SetNormalizeFunc(func(_ *pflag.FlagSet, name string) pflag.NormalizedName {
		switch name {
		case "auth-method":
			return pflag.NormalizedName("method")
		case "client-id":
			return pflag.NormalizedName("oauth-client-id")
		}
		return pflag.NormalizedName(name)
	})

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
	logoutCmd.Flags().BoolP("keep-token", "K", false, "skip token revocation and local credential deletion; only removes the workspace auth fields from config")

	authCmd.AddCommand(loginCmd, statusCmd, switchCmd, logoutCmd)
	return authCmd
}

func runAuthLogin(cmd *cobra.Command, runtime *cliruntime.RootRuntime, input loginInput) error {
	input.WorkspaceName = strings.TrimSpace(input.WorkspaceName)
	if input.WorkspaceName == "" {
		flagWorkspace, _ := cmd.Root().PersistentFlags().GetString("workspace")
		input.WorkspaceName = strings.TrimSpace(flagWorkspace)
	}
	ctx, _, _, err := cliruntime.CommandContext(cmd, runtime)
	if err != nil {
		ctx = cliruntime.LocalContext(cmd, runtime, "default")
	}
	interactive := runtime.IsTTY && input.WorkspaceName == "" && !input.HasTokenSource() && input.AuthMethod == ""
	if interactive {
		if err := runAuthLoginForm(ctx, runtime, &input); err != nil {
			return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
		}
	}
	input.WorkspaceName = strings.TrimSpace(input.WorkspaceName)
	if input.WorkspaceName == "" {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("workspace-name is required"))
	}
	if input.AuthMethod == "" {
		input.AuthMethod = "token"
	}
	switch input.AuthMethod {
	case "oauth":
		complete, err := completeOAuthLogin(cmd.Context(), ctx, runtime, &input)
		if err != nil {
			return clioutput.WriteCommandError(ctx, authCLIErrorFromError(err))
		}
		if !complete {
			return nil
		}
	case "token":
		if err := resolveLoginTokenSource(runtime, &input); err != nil {
			return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
		}
		auth, err := validateLoginToken(cmd.Context(), runtime, input.Token)
		if err != nil {
			return clioutput.WriteCommandError(ctx, clioutput.AuthCLIError(err.Error()))
		}
		input.TokenType = tokenType(input.Token)
		if input.TeamID == "" {
			input.TeamID = auth.TeamID
		}
		if input.TeamName == "" {
			input.TeamName = auth.Team
		}
	default:
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("auth-method must be oauth or token"))
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
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("workspace id is required"))
	}
	if input.TeamName == "" {
		input.TeamName = profileName
	}
	if existing, ok := cfg.Workspaces[profileName]; ok && workspaceHasAuth(existing) && !input.Force && hasStoredCredential(runtime, profileName) {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("workspace profile is already authenticated; rerun with --force to overwrite auth fields"))
	}
	secret, err := encodeLoginCredential(ctx, input)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.AuthCLIError(err.Error()))
	}
	if err := runtime.CredentialStore.Set("slack-cli", profileName, secret); err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.AuthCLIError(err.Error()))
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
			return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
		}
	}
	runtime.Config = cfg
	return ctx.WriteResult("auth.login", WorkspaceData{Workspace: profileName, Authenticated: true, TokenType: input.resolvedTokenType(), TeamID: input.TeamID, TeamName: input.TeamName})
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

// hasStoredCredential reports whether the credential store actually holds
// a non-empty secret for profileName. Used to avoid blocking re-login when
// the keychain entry is gone but the workspace metadata still lingers in
// config.toml.
func hasStoredCredential(runtime *cliruntime.RootRuntime, profileName string) bool {
	if runtime == nil || runtime.CredentialStore == nil {
		return false
	}
	secret, err := runtime.CredentialStore.Get("slack-cli", profileName)
	if err != nil {
		return false
	}
	return strings.TrimSpace(secret) != ""
}

type loginInput struct {
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

func (input loginInput) HasTokenSource() bool {
	return input.Token != "" || input.TokenStdin || input.TokenFile != "" || input.TokenEnv != ""
}

func (input loginInput) resolvedTokenType() config.TokenType {
	if input.TokenType != "" {
		return input.TokenType
	}
	return tokenType(input.Token)
}

func resolveLoginTokenSource(runtime *cliruntime.RootRuntime, input *loginInput) error {
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

func runAuthLoginForm(ctx *clioutput.CommandContext, runtime *cliruntime.RootRuntime, input *loginInput) error {
	accessible := !clioauth.UsesTerminalFiles(runtime)
	help := authLoginFieldHelp()
	if strings.TrimSpace(input.WorkspaceName) == "" {
		input.WorkspaceName = "default"
	}
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Profile name").
				Description(help["workspace"].Description).
				Placeholder(help["workspace"].Placeholder).
				Value(&input.WorkspaceName).
				Validate(clioauth.RequiredField("profile name")),
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
		WithTheme(clitheme.LoginHuhTheme(clibtheme.Default())).
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
		Validate(clioauth.RequiredField("slack token"))
	if !accessible {
		field.EchoMode(huh.EchoModePassword)
	}
	return field
}

func runTokenLoginForm(runtime *cliruntime.RootRuntime, input *loginInput, help map[string]authFieldHelp, accessible bool) error {
	form := huh.NewForm(
		huh.NewGroup(authTokenInput(&input.Token, help["token"], accessible)),
	).
		WithTheme(clitheme.LoginHuhTheme(clibtheme.Default())).
		WithInput(runtime.Stdin).
		WithOutput(runtime.Stderr)
	if accessible {
		form.WithAccessible(true)
	}
	return form.Run()
}

func runOAuthLoginForm(ctx *clioutput.CommandContext, runtime *cliruntime.RootRuntime, input *loginInput, help map[string]authFieldHelp) error {
	accessible := !clioauth.UsesTerminalFiles(runtime)
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Slack OAuth client ID").
				Description(help["client_id"].Description).
				Placeholder(help["client_id"].Placeholder).
				Value(&input.ClientID).
				Validate(clioauth.RequiredField("oauth client id")),
			huh.NewInput().
				Title("OAuth redirect URL").
				Description(help["oauth_redirect"].Description).
				Placeholder(help["oauth_redirect"].Placeholder).
				Value(&input.OAuthRedirect).
				Validate(validateOAuthRedirectField),
		),
	).
		WithTheme(clitheme.LoginHuhTheme(clibtheme.Default())).
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

func completeOAuthLogin(reqCtx context.Context, ctx *clioutput.CommandContext, runtime *cliruntime.RootRuntime, input *loginInput) (bool, error) {
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
	openURL := runtime.OpenURL
	if openURL == nil {
		openURL = defaultOpenURL
	}

	timeout := runtime.OAuthTimeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	exchangeTimeout := runtime.Timeout
	if exchangeTimeout <= 0 {
		exchangeTimeout = 30 * time.Second
	}

	// Detach from reqCtx so the OAuth wait isn't bound to the global
	// --timeout deadline (default 30s). The browser round-trip easily
	// exceeds that; OAuthTimeout governs the human-interaction budget.
	oauthCtx, cancelOAuth := context.WithTimeout(context.WithoutCancel(reqCtx), timeout)
	defer cancelOAuth()
	var response *slackgo.OAuthV2Response
	spinner := ctx.StderrLogger().Spinner("Waiting for Slack OAuth callback").
		Link("authorize_url", authorizeURL, clioutput.HyperlinkText(authorizeURL)).
		Link("redirect_url", redirectURL.String(), clioutput.HyperlinkText(redirectURL.String()))
	spinner.ClearOnCancel = true
	spinErr := spinner.
		Wait(oauthCtx, func(taskCtx context.Context) error {
			_ = openURL(authorizeURL)
			var callback oauthCallbackResult
			select {
			case callback = <-resultCh:
			case <-taskCtx.Done():
				if errors.Is(taskCtx.Err(), context.DeadlineExceeded) {
					return oauthTimeoutError{RedirectURL: redirectURL.String()}
				}
				return taskCtx.Err()
			}
			if callback.Err != nil {
				return callback.Err
			}
			// The exchange is a single Slack POST that runs after the user has
			// already interacted; give it its own fresh per-request timeout
			// rather than racing with what's left of OAuthTimeout.
			exchangeCtx, cancelExchange := context.WithTimeout(context.WithoutCancel(reqCtx), exchangeTimeout)
			defer cancelExchange()
			var err error
			response, err = oauthExchangeCode(exchangeCtx, runtime, input.ClientID, callback.Code, redirectURL.String(), verifier)
			return err
		}).Silent()
	if spinErr != nil {
		// runAnimation races the task return against ctx.Done(); if the ctx
		// branch wins we get a bare DeadlineExceeded. Re-wrap as the
		// structured oauth-timeout error so callers see the redirect_url.
		if errors.Is(spinErr, context.DeadlineExceeded) {
			return false, oauthTimeoutError{RedirectURL: redirectURL.String()}
		}
		return false, spinErr
	}
	if err := applyOAuthResponse(input, response); err != nil {
		return false, err
	}
	return true, nil
}

func applyOAuthResponse(input *loginInput, response *slackgo.OAuthV2Response) error {
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

func authCLIErrorFromError(err error) clioutput.CLIError {
	var timeout oauthTimeoutError
	if errors.As(err, &timeout) {
		return clioutput.CLIError{
			Type:     clioutput.ErrorTypeAuth,
			Message:  timeout.Error(),
			Details:  map[string]any{"redirect_url": timeout.RedirectURL},
			ExitCode: clioutput.ExitCodeAuthFailure,
		}
	}
	return clioutput.AuthCLIError(err.Error())
}

func encodeLoginCredential(ctx *clioutput.CommandContext, input loginInput) (string, error) {
	payload := config.CredentialPayload{AccessToken: input.Token, RefreshToken: input.RefreshToken, ClientID: input.ClientID}
	if input.ExpiresIn > 0 {
		expiresAt := ctx.Now().Add(time.Duration(input.ExpiresIn) * time.Second)
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
	scopes, _ := climanifest.PresetScopes(climanifest.DefaultPreset)
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

func defaultOAuthRedirectURL() string {
	return clioauth.RedirectURLForPort(clioauth.DefaultCallbackPort())
}

func oauthRedirectURLForListener(redirectURL *url.URL, listener net.Listener) (*url.URL, error) {
	if redirectURL.Port() != clioauth.OSAssignedCallbackPort {
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
		parsed.Path = clioauth.DefaultCallbackPath
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

func validateLoginToken(ctx context.Context, runtime *cliruntime.RootRuntime, token string) (*slackgo.AuthTestResponse, error) {
	return slackAuthClient(ctx, token, runtime).AuthTestContext(ctx)
}

func slackAuthClient(ctx context.Context, token string, runtime *cliruntime.RootRuntime) *slackgo.Client {
	return slackclient.New(ctx, nil, runtime, token,
		slackgo.OptionHTTPClient(slackclient.NoThrottle(runtime)),
	)
}

func runAuthStatus(cmd *cobra.Command, runtime *cliruntime.RootRuntime) error {
	ctx, _, _, err := cliruntime.CommandContext(cmd, runtime)
	if err != nil {
		return cliruntime.WriteRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
	}
	var workspaces []WorkspaceData
	if runtime.Config != nil {
		for name, profile := range runtime.Config.Workspaces {
			validationState := "missing"
			validationError := ""
			authenticated := false
			client, clientErr := slackclient.Client(cmd, profile, runtime)
			if clientErr != nil {
				if !errors.Is(clientErr, config.ErrCredentialNotFound) {
					validationState = "invalid"
					validationError = clientErr.Error()
				}
			} else if _, callErr := client.AuthTestContext(cmd.Context()); callErr != nil {
				validationState = "invalid"
				validationError = callErr.Error()
			} else {
				authenticated = true
				validationState = "valid"
			}
			workspaces = append(workspaces, WorkspaceData{
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
	return ctx.WriteResult("auth.status", StatusData{Workspaces: workspaces})
}

func runAuthSwitch(cmd *cobra.Command, runtime *cliruntime.RootRuntime, workspace string) error {
	workspace = strings.TrimSpace(workspace)
	ctx, _, _, err := cliruntime.CommandContext(cmd, runtime)
	if err != nil {
		return cliruntime.WriteRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
	}
	if runtime.Config == nil {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("config is required"))
	}
	if _, ok := runtime.Config.Workspaces[workspace]; !ok {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("workspace not configured"))
	}
	runtime.Config.DefaultWorkspace = workspace
	if runtime.ConfigPath != "" {
		if err := config.SaveFile(runtime.ConfigPath, runtime.Config); err != nil {
			return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
		}
	}
	return ctx.WriteResult("auth.switch", WorkspaceData{Workspace: workspace})
}

func runAuthLogout(cmd *cobra.Command, runtime *cliruntime.RootRuntime, workspace string) error {
	workspace = strings.TrimSpace(workspace)
	ctx, _, _, err := cliruntime.CommandContext(cmd, runtime)
	if err != nil {
		return cliruntime.WriteRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
	}

	keepToken, _ := cmd.Flags().GetBool("keep-token")

	if keepToken {
		ctx.StderrLogger().Warn().Str("workspace", workspace).Msg("--keep-token preserves the credential in keychain; the token is still valid on Slack's side until manually revoked or it naturally expires")
	}

	if !keepToken {
		// Resolve token before deleting credentials so we can revoke it.
		token := resolveTokenForRevoke(cmd.Context(), runtime, workspace)
		if token != "" {
			client := slackAuthClient(cmd.Context(), token, runtime)
			if _, revokeErr := client.SendAuthRevokeContext(cmd.Context(), token); revokeErr != nil {
				ctx.StderrLogger().Warn().Err(revokeErr).Str("workspace", workspace).Msg("token revocation failed; proceeding with local cleanup")
			}
		}
		_ = runtime.CredentialStore.Delete("slack-cli", workspace)
	}

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
				return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
			}
		}
	}
	return ctx.WriteResult("auth.logout", WorkspaceData{Workspace: workspace, Authenticated: false})
}

func resolveTokenForRevoke(ctx context.Context, runtime *cliruntime.RootRuntime, workspace string) string {
	if runtime.Config == nil || runtime.Config.Workspaces == nil {
		return ""
	}
	profile, ok := runtime.Config.Workspaces[workspace]
	if !ok {
		return ""
	}
	resolver := runtime.TokenResolver
	if resolver == nil {
		resolver = clitoken.CredentialTokenResolver{
			Store:        runtime.CredentialStore,
			SlackBaseURL: runtime.SlackBaseURL,
			HTTPClient:   runtime.HTTPClient,
			Now:          runtime.Now,
		}
	}
	token, err := resolver.ResolveToken(ctx, profile)
	if err != nil {
		return ""
	}
	return token
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

func oauthExchangeCode(ctx context.Context, runtime *cliruntime.RootRuntime, clientID, code, redirectURI, verifier string) (*slackgo.OAuthV2Response, error) {
	oauthHTTPClient := runtime.HTTPClient
	if oauthHTTPClient == nil {
		oauthHTTPClient = &http.Client{Timeout: runtime.Timeout}
	}
	opts := []slackgo.OAuthOption{slackgo.OAuthOptionCodeVerifier(verifier)}
	if runtime.SlackBaseURL != "" {
		opts = append(opts, slackgo.OAuthOptionAPIURL(slackAPIURL(runtime.SlackBaseURL)))
	}
	resp, err := slackgo.GetOAuthV2ResponseContext(ctx, oauthHTTPClient, clientID, "", code, redirectURI, opts...)
	if err != nil {
		return nil, wrapBadClientSecret(err)
	}
	return resp, nil
}

// slackAPIURL mirrors internal/cli/token.slackAPIURL; collapse when auth/token packages consolidate.
func slackAPIURL(baseURL string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(baseURL, "/api") {
		return baseURL + "/"
	}
	return baseURL + "/api/"
}

// wrapBadClientSecret mirrors internal/cli/token.wrapBadClientSecret; collapse when auth/token packages consolidate.
func wrapBadClientSecret(err error) error {
	var slackErr slackgo.SlackErrorResponse
	if errors.As(err, &slackErr) && slackErr.Err == "bad_client_secret" {
		return fmt.Errorf("bad_client_secret: Slack treated this as a client-secret OAuth flow. Enable PKCE for the Slack app, or import a manifest with oauth_config.pkce_enabled=true; slack-cli local OAuth intentionally omits the client secret: %w", err)
	}
	return err
}
