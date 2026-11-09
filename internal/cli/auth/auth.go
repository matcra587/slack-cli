package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"io"
	"maps"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"charm.land/huh/v2"
	clibtheme "github.com/gechr/clib/theme"
	"github.com/gechr/clog/fx"
	xfilepath "github.com/gechr/x/filepath"
	"github.com/gechr/x/shell"
	clitheme "github.com/matcra587/slack-cli/internal/cli/clitheme"
	climanifest "github.com/matcra587/slack-cli/internal/cli/manifest"
	clioauth "github.com/matcra587/slack-cli/internal/cli/oauth"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	slackclient "github.com/matcra587/slack-cli/internal/cli/slackclient"
	clitoken "github.com/matcra587/slack-cli/internal/cli/token"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/credentials"
	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type WorkspaceData struct {
	Workspace       string           `json:"workspace"`
	DryRun          bool             `json:"dry_run,omitempty"`
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
	logger := c.StdoutLogger()
	if d.TeamID != "" {
		clioutput.ApplyTeamIDStyle(logger, c.Theme, d.TeamID)
	}
	clioutput.ApplyFieldStyles(logger, c.Theme,
		clioutput.EntityFieldStyle("workspace", d.Workspace),
		clioutput.HashedFieldStyle("token_type", "token_type:"+string(d.TokenType)),
	)
	// clog's OmitZero=true would strip a literal Bool("authenticated", false),
	// hiding the field after `auth logout`. Render it as a string so the field
	// always appears, and apply state coloring (green=true, red=false) so the
	// auth state is visually obvious.
	clioutput.ApplyBoolValueStyle(logger, c.Theme, "authenticated", d.Authenticated)
	event := c.ResultEvent(command).
		Str("workspace", d.Workspace).
		Str("team_id", d.TeamID).
		Str("team_name", d.TeamName).
		Str("token_type", string(d.TokenType)).
		Str("authenticated", strconv.FormatBool(d.Authenticated)).
		Bool("dry_run", d.DryRun).
		Str("validation_error", d.ValidationError)
	event.Msg(clioutput.ActionLabel(command))
	return nil
}

var _ clioutput.PlainRenderer = StatusData{}

func (d StatusData) WritePlain(c *clioutput.CommandContext, command string, _ *clioutput.Pagination) error {
	if len(d.Workspaces) == 0 {
		c.ResultEvent(command).Msg("No workspace profiles configured")
		return nil
	}
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
		clioutput.ApplyFieldStyles(logger, c.Theme,
			clioutput.EntityFieldStyle("workspace", workspace.Workspace),
			clioutput.HashedFieldStyle("token_type", "token_type:"+string(workspace.TokenType)),
		)
		// Render authenticated as a string so OmitZero=true does not strip
		// the false case, and color it (green=true, red=false) so the auth
		// state is visible in the field list as well as the message label.
		clioutput.ApplyBoolValueStyle(logger, c.Theme, "authenticated", workspace.Authenticated)
		event := logger.Info().
			Str("workspace", workspace.Workspace).
			Str("team_id", workspace.TeamID).
			Str("team_name", workspace.TeamName).
			Str("token_type", string(workspace.TokenType)).
			Str("authenticated", strconv.FormatBool(workspace.Authenticated))
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
	var scopePreset string
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
				ScopePreset:   scopePreset,
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
	loginCmd.Flags().StringVarP(&scopePreset, "scope-preset", "P", climanifest.DefaultPreset, "OAuth user scopes: readonly, messaging, files, search, organize, or full")
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
		Args:         cobra.NoArgs,
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
	if input.WorkspaceName == "" {
		flagWorkspace, _ := cmd.Root().PersistentFlags().GetString("workspace")
		input.WorkspaceName = flagWorkspace
	}
	ctx, _, _, err := cliruntime.CommandContext(cmd, runtime)
	if runtime.ConfigLoadError != nil {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("could not load configuration; repair it before logging in"))
	}
	if err != nil {
		ctx = cliruntime.LocalContext(cmd, runtime, "default")
	}
	if err := cmd.Context().Err(); err != nil {
		return clioutput.WriteCommandError(ctx, slackclient.CLIError(cmd.Context(), err))
	}
	input.WorkspaceName = clioutput.TrimInputName(ctx.StderrLogger(), "workspace", input.WorkspaceName)
	interactive := runtime.IsTTY && !cliruntime.DryRun(cmd) && input.WorkspaceName == "" && !input.HasTokenSource() && input.AuthMethod == ""
	if interactive {
		if err := runAuthLoginForm(ctx, runtime, &input); err != nil {
			return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
		}
		input.WorkspaceName = clioutput.TrimInputName(ctx.StderrLogger(), "workspace", input.WorkspaceName)
	}
	if input.WorkspaceName == "" {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("workspace-name is required"))
	}
	if input.AuthMethod == "" {
		input.AuthMethod = "token"
	}
	if runtime.Config != nil {
		if err := runtime.Config.Validate(); err != nil {
			return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
		}
		input.WorkspaceName = canonicalProfileName(runtime.Config, input.WorkspaceName)
	}
	ctx.Workspace = input.WorkspaceName
	if err := validateLoginOptions(cmd, &input); err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
	}
	if cliruntime.DryRun(cmd) {
		return ctx.WriteResult("auth.login", WorkspaceData{Workspace: input.WorkspaceName, DryRun: true})
	}
	switch input.AuthMethod {
	case "oauth":
		// Browser approval has a separate budget. Keep the original caller's
		// cancellation for the flow and local persistence after the exchange.
		cmd.SetContext(cmd.Root().Context())
		complete, err := completeOAuthLogin(cmd.Context(), ctx, runtime, &input)
		if err != nil {
			return clioutput.WriteCommandError(ctx, authCLIErrorFromError(cmd.Context(), err))
		}
		if !complete {
			return nil
		}
	case "token":
		if err := resolveLoginTokenSource(runtime, &input); err != nil {
			return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
		}
		auth, err := validateLoginToken(cmd, runtime, input.Token)
		if err != nil {
			return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err, "auth"))
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
	if err := cmd.Context().Err(); err != nil {
		return clioutput.WriteCommandError(ctx, slackclient.CLIError(cmd.Context(), err))
	}
	cfg := &config.Config{SchemaVersion: config.SchemaVersion}
	if runtime.Config != nil {
		staged := *runtime.Config
		staged.Workspaces = maps.Clone(runtime.Config.Workspaces)
		cfg = &staged
	}
	if cfg.Workspaces == nil {
		cfg.Workspaces = map[string]config.WorkspaceProfile{}
	}
	profileName := canonicalProfileName(cfg, input.WorkspaceName)
	ctx.Workspace = profileName
	if input.TeamID == "" {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("workspace id is required"))
	}
	if input.TeamName == "" {
		input.TeamName = profileName
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
	if err := cfg.Validate(); err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
	}
	previous, snapshotErr := runtime.CredentialStore.Get("slack-cli", profileName)
	if err := cmd.Context().Err(); err != nil {
		return clioutput.WriteCommandError(ctx, slackclient.CLIError(cmd.Context(), err))
	}
	missing := errors.Is(snapshotErr, config.ErrCredentialNotFound)
	var existing config.WorkspaceProfile
	var exists bool
	if runtime.Config != nil {
		existing, exists = runtime.Config.Workspaces[profileName]
	}
	protected := existing.TokenRef != profile.TokenRef || (!missing && snapshotErr != nil) || strings.TrimSpace(previous) != ""
	if exists && workspaceHasAuth(existing) && !input.Force && protected {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("workspace profile is already authenticated; rerun with --force to overwrite auth fields"))
	}
	if snapshotErr != nil && !missing {
		failure := slackclient.CLIError(cmd.Context(), snapshotErr)
		if failure.ExitCode == clioutput.ExitCodeAuthFailure {
			failure.Message = "could not read the previous credential; config and saved credentials unchanged"
		}
		return clioutput.WriteCommandError(ctx, failure)
	}
	secret, err := encodeLoginCredential(ctx, input)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.AuthCLIError("could not encode login credential"))
	}
	if err := runtime.CredentialStore.Set("slack-cli", profileName, secret); err != nil {
		failure := slackclient.CLIError(cmd.Context(), err)
		if failure.ExitCode == clioutput.ExitCodeAuthFailure {
			failure.Message = "could not store login credential"
		}
		return clioutput.WriteCommandError(ctx, restoreLoginCredential(runtime, profileName, previous, missing, failure))
	}
	if err := cmd.Context().Err(); err != nil {
		failure := slackclient.CLIError(cmd.Context(), err)
		return clioutput.WriteCommandError(ctx, restoreLoginCredential(runtime, profileName, previous, missing, failure))
	}
	if runtime.ConfigPath != "" {
		if err := config.SaveFile(runtime.ConfigPath, cfg); err != nil {
			failure := clioutput.ConfigSaveCLIError(err, "could not save login configuration")
			if config.SaveWasPublished(err) {
				if runtime.Config == nil {
					runtime.Config = cfg
				} else {
					*runtime.Config = *cfg
				}
				return clioutput.WriteCommandError(ctx, failure)
			}
			return clioutput.WriteCommandError(ctx, restoreLoginCredential(runtime, profileName, previous, missing, failure))
		}
	}
	if runtime.Config == nil {
		runtime.Config = cfg
	} else {
		*runtime.Config = *cfg
	}
	return ctx.WriteResult("auth.login", WorkspaceData{Workspace: profileName, Authenticated: true, TokenType: input.resolvedTokenType(), TeamID: input.TeamID, TeamName: input.TeamName})
}

// A credential write can succeed even when the provider reports an error.
// Restore the snapshot after a failed credential write or an unpublished config save.
func restoreLoginCredential(runtime *cliruntime.RootRuntime, profile, previous string, missing bool, failure clioutput.CLIError) clioutput.CLIError {
	var err error
	if missing {
		err = runtime.CredentialStore.Delete("slack-cli", profile)
		if errors.Is(err, config.ErrCredentialNotFound) {
			err = nil
		}
	} else {
		err = runtime.CredentialStore.Set("slack-cli", profile, previous)
	}
	if err != nil {
		failure = failure.WithDetails(map[string]any{"credential_restore_failed": true})
	}
	return failure
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
	ScopePreset   string
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

func validateLoginOptions(cmd *cobra.Command, input *loginInput) error {
	switch input.AuthMethod {
	case "token":
		for _, name := range []string{"oauth-client-id", "oauth-redirect-url", "oauth-callback-port", "scope-preset"} {
			if cmd.Flags().Changed(name) {
				return errors.New("OAuth options require --method oauth")
			}
		}
		return validateLoginTokenSourceFlags(*input)
	case "oauth":
		if _, err := climanifest.PresetScopes(input.ScopePreset); err != nil {
			return err
		}
		if input.HasTokenSource() {
			return errors.New("token source flags require --method token")
		}
		input.ClientID = strings.TrimSpace(input.ClientID)
		if input.ClientID == "" {
			return errors.New("oauth client id is required")
		}
		if cmd.Flags().Changed("oauth-redirect-url") && cmd.Flags().Changed("oauth-callback-port") {
			return errors.New("oauth-redirect-url and oauth-callback-port are mutually exclusive")
		}
		_, err := oauthRedirectURL(input.OAuthRedirect)
		return err
	default:
		return errors.New("auth-method must be oauth or token")
	}
}

func validateLoginTokenSourceFlags(input loginInput) error {
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
	if input.Token == "" && sourceCount == 0 {
		return errors.New("token source is required; use --token-stdin, --token-file, --token-env, or interactive login")
	}
	if input.TokenFile != "" && strings.TrimSpace(input.TokenFile) == "" {
		return errors.New("token-file is required")
	}
	if input.TokenEnv != "" && strings.TrimSpace(input.TokenEnv) == "" {
		return errors.New("token-env is required")
	}
	return nil
}

func resolveLoginTokenSource(runtime *cliruntime.RootRuntime, input *loginInput) error {
	if err := validateLoginTokenSourceFlags(*input); err != nil {
		return err
	}
	if input.Token == "" {
		switch {
		case input.TokenStdin:
			raw, err := io.ReadAll(runtime.Stdin)
			if err != nil {
				return errors.New("reading token from stdin failed")
			}
			input.Token = trimTokenSource(raw)
		case input.TokenFile != "":
			raw, err := os.ReadFile(xfilepath.Expand(input.TokenFile))
			if err != nil {
				return errors.New("reading token file failed")
			}
			input.Token = trimTokenSource(raw)
		case input.TokenEnv != "":
			name := strings.TrimSpace(input.TokenEnv)
			if name == "" {
				return errors.New("token-env is required")
			}
			input.Token = strings.TrimSpace(os.Getenv(name))
			if input.Token == "" {
				return errors.New("token environment variable is unset or empty")
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
	accessToken := strings.TrimPrefix(token, "xoxe.")
	if !strings.HasPrefix(accessToken, "xoxb-") && !strings.HasPrefix(accessToken, "xoxp-") {
		return errors.New("token content is malformed; expected a bot or user access token (xoxb-, xoxp-, or rotating xoxe. form)")
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
		WithTheme(clitheme.LoginHuhTheme(clitheme.Default())).
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
		WithTheme(clitheme.LoginHuhTheme(clitheme.Default())).
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
		WithTheme(clitheme.LoginHuhTheme(clitheme.Default())).
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
	if err := reqCtx.Err(); err != nil {
		return false, err
	}
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
		return false, oauthListenerError{Address: redirectURL.Host}
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
			sendOAuthCallbackResult(resultCh, oauthCallbackResult{Err: serveErr})
		}
	}()

	authorizeURL := oauthAuthorizeURL(oauthAuthorizeParams{
		ClientID:      input.ClientID,
		RedirectURI:   redirectURL.String(),
		State:         state,
		CodeChallenge: slackgo.GenerateCodeChallenge(verifier),
		ScopePreset:   input.ScopePreset,
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

	// Caller deadlines and cancellation apply to both browser approval and
	// the exchange. Only the browser wait uses OAuthTimeout.
	signalCtx, signalStop := signal.NotifyContext(reqCtx, os.Interrupt, syscall.SIGTERM)
	defer signalStop()
	oauthCtx, cancelOAuth := context.WithTimeout(signalCtx, timeout)
	defer cancelOAuth()
	var callbackPtr atomic.Pointer[oauthCallbackResult]
	spinner := ctx.StderrLogger().Spinner("Waiting for Slack OAuth callback").
		Link("authorize_url", authorizeURL, ctx.HyperlinkText(authorizeURL)).
		Link("redirect_url", redirectURL.String(), ctx.HyperlinkText(redirectURL.String()))
	group := ctx.StderrLogger().Group(oauthCtx, fx.WithClearOnCancel())
	group.Add(spinner).Run(func(taskCtx context.Context) error {
		if err := taskCtx.Err(); err != nil {
			return err
		}
		_ = openURL(authorizeURL)
		var callback oauthCallbackResult
		select {
		case callback = <-resultCh:
		case <-taskCtx.Done():
			return taskCtx.Err()
		}
		if callback.Err != nil {
			return callback.Err
		}
		callbackPtr.Store(&callback)
		return nil
	})
	spinErr := group.Wait().Silent()
	if spinErr != nil {
		if err := signalCtx.Err(); err != nil {
			return false, err
		}
		if errors.Is(spinErr, context.DeadlineExceeded) && errors.Is(oauthCtx.Err(), context.DeadlineExceeded) {
			return false, oauthTimeoutError{RedirectURL: redirectURL.String()}
		}
		return false, spinErr
	}
	if err := signalCtx.Err(); err != nil {
		return false, err
	}
	callback := callbackPtr.Load()
	if callback == nil {
		return false, errors.New("oauth callback did not complete")
	}
	// The browser budget ends at callback acceptance. The exchange gets a
	// fresh request timeout while still honoring the original caller.
	exchangeCtx, cancelExchange := context.WithTimeout(signalCtx, exchangeTimeout)
	defer cancelExchange()
	response, err := oauthExchangeCode(exchangeCtx, runtime, input.ClientID, callback.Code, redirectURL.String(), verifier)
	if contextErr := exchangeCtx.Err(); contextErr != nil {
		return false, contextErr
	}
	if err != nil {
		return false, err
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

func authCLIErrorFromError(ctx context.Context, err error) clioutput.CLIError {
	if listener, ok := errors.AsType[oauthListenerError](err); ok {
		failure := clioutput.RuntimeCLIError(listener.Error())
		failure.Details = map[string]any{"callback_address": listener.Address}
		return failure
	}
	if timeout, ok := errors.AsType[oauthTimeoutError](err); ok {
		return clioutput.CLIError{
			Type:     clioutput.ErrorTypeAuth,
			Message:  timeout.Error(),
			Details:  map[string]any{"redirect_url": timeout.RedirectURL},
			ExitCode: clioutput.ExitCodeAuthFailure,
		}
	}
	if _, ok := errors.AsType[oauthDeniedError](err); ok {
		return clioutput.AuthCLIError(err.Error())
	}
	if slackErr, ok := errors.AsType[slackgo.SlackErrorResponse](err); ok {
		switch slackErr.Err {
		case "bad_client_secret":
			return clioutput.AuthCLIError("Slack requires PKCE for this flow; enable oauth_config.pkce_enabled in the app manifest. Local OAuth omits the client secret")
		case "invalid_client_id", "invalid_code", "invalid_code_verifier", "bad_redirect_uri", "pkce_not_allowed":
			// Preserve known OAuth auth failures without copying arbitrary
			// remote error text or response metadata into diagnostics.
			return clioutput.AuthCLIError(slackErr.Err)
		}
	}
	return clioutput.CliErrorFromSlack(ctx, err, "auth")
}

type oauthDeniedError struct{}

func (oauthDeniedError) Error() string {
	return "oauth authorization failed; retry login to authorize access"
}

type oauthListenerError struct{ Address string }

func (oauthListenerError) Error() string {
	return "oauth redirect listener failed; check the callback port is available"
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
	ScopePreset   string
}

func oauthAuthorizeURL(params oauthAuthorizeParams) string {
	scopes, _ := climanifest.PresetScopes(params.ScopePreset)
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
		return nil, errors.New("oauth redirect url is malformed")
	}
	if parsed.User != nil || parsed.Fragment != "" {
		return nil, errors.New("oauth redirect url must not contain credentials or a fragment")
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
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port < 0 || port > 65535 {
		return nil, errors.New("oauth redirect port must be between 0 and 65535")
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
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeOAuthCallbackHTML(w, http.StatusMethodNotAllowed, "Invalid callback method", "Use the callback link from Slack.")
			return
		}
		// An unsolicited or ambiguous callback must not consume the pending
		// login. Authenticate both successful and denied callbacks first.
		if len(query["state"]) != 1 || query.Get("state") != state {
			writeOAuthCallbackHTML(w, http.StatusBadRequest, "OAuth state mismatch", "Use the callback link from the current Slack authorization.")
			return
		}
		code := query.Get("code")
		errValue := query.Get("error")
		if len(query["code"]) > 1 || len(query["error"]) > 1 || (code != "" && errValue != "") || (code == "" && errValue == "") {
			writeOAuthCallbackHTML(w, http.StatusBadRequest, "Invalid OAuth callback", "Use the callback link from Slack.")
			return
		}
		if errValue != "" {
			denied := oauthDeniedError{}
			sendOAuthCallbackResult(resultCh, oauthCallbackResult{Err: denied})
			writeOAuthCallbackHTML(w, http.StatusBadRequest, "Slack OAuth failed", denied.Error())
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

func validateLoginToken(cmd *cobra.Command, runtime *cliruntime.RootRuntime, token string) (*slackgo.AuthTestResponse, error) {
	return slackAuthClient(cmd, token, runtime).AuthTestContext(cmd.Context())
}

func slackAuthClient(cmd *cobra.Command, token string, runtime *cliruntime.RootRuntime) *slackgo.Client {
	return slackclient.New(cmd.Context(), cmd, runtime, token)
}

func runAuthStatus(cmd *cobra.Command, runtime *cliruntime.RootRuntime) error {
	ctx, _, _, err := cliruntime.CommandContext(cmd, runtime)
	if err != nil {
		return cliruntime.WriteRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
	}
	if err := cmd.Context().Err(); err != nil {
		return clioutput.WriteCommandError(ctx, slackclient.CLIError(cmd.Context(), err))
	}
	workspaces := make([]WorkspaceData, 0)
	if runtime.Config != nil {
		for _, name := range slices.Sorted(maps.Keys(runtime.Config.Workspaces)) {
			if err := cmd.Context().Err(); err != nil {
				return clioutput.WriteCommandError(ctx, slackclient.CLIError(cmd.Context(), err))
			}
			profile := runtime.Config.Workspaces[name]
			validationState := "missing"
			validationError := ""
			authenticated := false
			client, clientErr := slackclient.Client(cmd, profile, runtime)
			if clientErr != nil {
				failure := slackclient.CLIError(cmd.Context(), clientErr)
				if failure.ExitCode == clioutput.ExitCodeCanceled || failure.ExitCode == clioutput.ExitCodeTimeout {
					return clioutput.WriteCommandError(ctx, failure)
				}
				if !errors.Is(clientErr, config.ErrCredentialNotFound) {
					validationState = "invalid"
					validationError = failure.Message
				}
			} else if _, callErr := client.AuthTestContext(cmd.Context()); callErr != nil {
				failure := clioutput.CliErrorFromSlack(cmd.Context(), callErr, "auth")
				if failure.ExitCode == clioutput.ExitCodeCanceled || failure.ExitCode == clioutput.ExitCodeTimeout {
					return clioutput.WriteCommandError(ctx, failure)
				}
				validationState = "invalid"
				validationError = failure.Message
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
	ctx, _, _, err := cliruntime.CommandContext(cmd, runtime)
	if err != nil {
		return cliruntime.WriteRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
	}
	if err := cmd.Context().Err(); err != nil {
		return clioutput.WriteCommandError(ctx, slackclient.CLIError(cmd.Context(), err))
	}
	workspace = clioutput.TrimInputName(ctx.StderrLogger(), "workspace", workspace)
	if runtime.Config == nil {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("config is required"))
	}
	workspace = canonicalProfileName(runtime.Config, workspace)
	if _, ok := runtime.Config.Workspaces[workspace]; !ok {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("workspace not configured"))
	}
	ctx.Workspace = workspace
	ctx.TeamID = runtime.Config.Workspaces[workspace].TeamID
	if cliruntime.DryRun(cmd) {
		return ctx.WriteResult("auth.switch", WorkspaceData{Workspace: workspace, DryRun: true})
	}
	updated := *runtime.Config
	updated.DefaultWorkspace = workspace
	if runtime.ConfigPath != "" {
		if err := config.SaveFile(runtime.ConfigPath, &updated); err != nil {
			if config.SaveWasPublished(err) {
				*runtime.Config = updated
			}
			return clioutput.WriteCommandError(ctx, clioutput.ConfigSaveCLIError(err, "could not save default workspace"))
		}
	}
	*runtime.Config = updated
	return ctx.WriteResult("auth.switch", WorkspaceData{Workspace: workspace})
}

func runAuthLogout(cmd *cobra.Command, runtime *cliruntime.RootRuntime, workspace string) error {
	ctx, _, _, err := cliruntime.CommandContext(cmd, runtime)
	if err != nil {
		return cliruntime.WriteRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
	}
	if err := cmd.Context().Err(); err != nil {
		return clioutput.WriteCommandError(ctx, slackclient.CLIError(cmd.Context(), err))
	}
	workspace = clioutput.TrimInputName(ctx.StderrLogger(), "workspace", workspace)
	if runtime.Config != nil {
		workspace = canonicalProfileName(runtime.Config, workspace)
	}
	ctx.Workspace = workspace
	if runtime.Config != nil {
		ctx.TeamID = runtime.Config.Workspaces[workspace].TeamID
	}
	if cliruntime.DryRun(cmd) {
		return ctx.WriteResult("auth.logout", WorkspaceData{Workspace: workspace, DryRun: true})
	}
	keepToken, _ := cmd.Flags().GetBool("keep-token")

	// Commit local configuration before irreversible token revocation or deletion.
	// Keep the original runtime profile available for credential resolution below.
	var updated *config.Config
	if runtime.Config != nil && runtime.Config.Workspaces != nil {
		staged := *runtime.Config
		staged.Workspaces = maps.Clone(runtime.Config.Workspaces)
		updated = &staged
		profile, ok := updated.Workspaces[workspace]
		if ok {
			profile.TeamID = ""
			profile.TeamName = ""
			profile.TokenType = ""
			profile.TokenRef = ""
			if configManagedProfile(profile) {
				updated.Workspaces[workspace] = profile
			} else {
				delete(updated.Workspaces, workspace)
			}
		}
		if strings.EqualFold(updated.DefaultWorkspace, workspace) && !configManagedProfile(updated.Workspaces[workspace]) {
			updated.DefaultWorkspace = firstWorkspaceName(updated.Workspaces)
		}
		if runtime.ConfigPath != "" {
			var err error
			if len(updated.Workspaces) == 0 {
				err = os.Remove(runtime.ConfigPath)
				if errors.Is(err, os.ErrNotExist) {
					err = nil
				}
			} else {
				err = config.SaveFile(runtime.ConfigPath, updated)
			}
			if err != nil {
				failure := clioutput.ConfigSaveCLIError(err, "could not save logout configuration")
				if config.SaveWasPublished(err) {
					*runtime.Config = *updated
					failure = failure.WithDetails(map[string]any{"credential_cleanup_attempted": false})
				}
				return clioutput.WriteCommandError(ctx, failure)
			}
		}
	}

	if keepToken {
		ctx.StderrLogger().Info().Str("workspace", workspace).Msg("--keep-token preserves the credential in keychain; the token is still valid on Slack's side until manually revoked or it naturally expires")
	} else {
		token, resolveErr := resolveTokenForRevoke(cmd.Context(), runtime, workspace)
		if resolveErr != nil {
			ctx.StderrLogger().Warn().Str("workspace", workspace).Msg("could not resolve token for revocation; proceeding with local cleanup")
		}
		if token != "" {
			client := slackAuthClient(cmd, token, runtime)
			response, revokeErr := client.SendAuthRevokeContext(cmd.Context(), token)
			if revokeErr != nil {
				ctx.StderrLogger().Warn().Str("workspace", workspace).Msg("token revocation failed; proceeding with local cleanup")
			} else if response == nil || !response.Revoked {
				ctx.StderrLogger().Warn().Str("workspace", workspace).Msg("Slack did not confirm token revocation; proceeding with local cleanup")
			}
		}
		if err := runtime.CredentialStore.Delete("slack-cli", workspace); err != nil && !errors.Is(err, config.ErrCredentialNotFound) {
			ctx.StderrLogger().Warn().Str("workspace", workspace).Msg("keychain delete failed during logout")
		}
	}
	if updated != nil {
		*runtime.Config = *updated
	}
	return ctx.WriteResult("auth.logout", WorkspaceData{Workspace: workspace, Authenticated: false})
}

func resolveTokenForRevoke(ctx context.Context, runtime *cliruntime.RootRuntime, workspace string) (string, error) {
	if runtime.Config == nil || runtime.Config.Workspaces == nil {
		return "", nil
	}
	profile, ok := runtime.Config.Workspaces[workspace]
	if !ok {
		return "", nil
	}
	if profile.TokenRef == "" {
		if _, configured := clitoken.RuntimeEnvToken(profile.Name); !configured {
			return "", nil
		}
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
		return "", err
	}
	if strings.TrimSpace(token) == "" {
		return "", config.ErrCredentialNotFound
	}
	return token, nil
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
		profile.Attribution.Enabled != nil ||
		profile.Attribution.Label != "" ||
		profile.Attribution.Emoji != "" ||
		profile.Attribution.Message != "" ||
		profile.RateLimitTier != "" ||
		len(profile.Aliases) > 0
}

func firstWorkspaceName(workspaces map[string]config.WorkspaceProfile) string {
	for _, name := range slices.Sorted(maps.Keys(workspaces)) {
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
	oauthHTTPClient := credentials.NewOAuthHTTPClient(runtime.HTTPClient)
	if runtime.HTTPClient == nil {
		oauthHTTPClient.Timeout = runtime.Timeout
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
