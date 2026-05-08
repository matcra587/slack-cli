package main

import (
	"errors"
	"slices"
	"strconv"
	"strings"

	cobracli "github.com/gechr/clib/cli/cobra"
	"github.com/gechr/clib/help"
	"github.com/gechr/clib/theme"
	"github.com/gechr/clog"
	xslices "github.com/gechr/x/slices"
	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

func newManifestCommand(runtime *RootRuntime) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "manifest",
		Short:        "Generate Slack app manifests",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newManifestTemplateCommand(runtime))
	return cmd
}

type manifestTemplateOptions struct {
	Name            string
	Description     string
	LongDescription string
	Preset          string
	Type            string
	BotScopes       []string
	UserScopes      []string
	RedirectURLs    []string
	CallbackPort    string
	BackgroundColor string
	Format          string
}

type generatedManifest struct {
	Display     slackgo.Display            `json:"display_information" yaml:"display_information"`
	Features    *generatedFeatures         `json:"features,omitempty" yaml:"features,omitempty"`
	OAuthConfig generatedOAuthConfig       `json:"oauth_config,omitempty" yaml:"oauth_config,omitempty"`
	Settings    *generatedManifestSettings `json:"settings,omitempty" yaml:"settings,omitempty"`
}

type generatedFeatures struct {
	BotUser *slackgo.BotUser `json:"bot_user,omitempty" yaml:"bot_user,omitempty"`
}

type generatedOAuthConfig struct {
	RedirectUrls []string            `json:"redirect_urls,omitempty" yaml:"redirect_urls,omitempty"`
	Scopes       slackgo.OAuthScopes `json:"scopes,omitempty" yaml:"scopes,omitempty"`
	PKCEEnabled  bool                `json:"pkce_enabled" yaml:"pkce_enabled"`
}

type generatedManifestSettings struct {
	TokenRotationEnabled bool `json:"token_rotation_enabled" yaml:"token_rotation_enabled"`
}

type manifestScopeReason struct {
	Scope  string
	Reason string
}

type manifestTemplatePreset struct {
	Name        string
	Description string
	Scopes      []string
}

const defaultManifestPreset = "messaging"

func newManifestTemplateCommand(runtime *RootRuntime) *cobra.Command {
	opts := manifestTemplateOptions{
		Name:            "slack-cli",
		Description:     "Slack CLI app for agent-friendly messaging and automation.",
		Preset:          defaultManifestPreset,
		Type:            "user",
		RedirectURLs:    []string{defaultManifestOAuthRedirectURL()},
		BackgroundColor: "#4A154B",
	}
	cmd := &cobra.Command{
		Use:          "template",
		Aliases:      []string{"generate"},
		Short:        "Output the Slack app manifest to import",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runManifestTemplate(cmd, runtime, opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Name, "name", "n", opts.Name, "App display name")
	cmd.Flags().StringVarP(&opts.Description, "description", "d", opts.Description, "Short app description")
	cmd.Flags().StringVarP(&opts.LongDescription, "long-description", "L", "", "Long app description")
	cmd.Flags().StringVarP(&opts.Preset, "preset", "p", opts.Preset, "Scope preset: readonly, messaging, files, search, or full")
	cmd.Flags().StringVar(&opts.Preset, "template", opts.Preset, "Alias for --preset")
	_ = cmd.Flags().MarkHidden("template")
	cmd.Flags().StringVarP(&opts.Type, "type", "t", opts.Type, "Auth shape: user, bot, or both")
	cmd.Flags().StringVarP(&opts.BackgroundColor, "background-color", "B", opts.BackgroundColor, "App background color")
	cmd.Flags().StringArrayVarP(&opts.BotScopes, "bot-scope", "S", nil, "Override bot OAuth scope")
	cmd.Flags().StringArrayVarP(&opts.UserScopes, "user-scope", "U", nil, "Override user OAuth scope")
	cmd.Flags().StringArrayVarP(&opts.RedirectURLs, "redirect-url", "r", opts.RedirectURLs, "OAuth redirect URL")
	cmd.Flags().StringVarP(&opts.CallbackPort, "callback-port", "C", "", "Local OAuth callback port for the generated redirect URL")
	cmd.Flags().StringVarP(&opts.Format, "format", "f", "json", "Output format: json or yaml")
	cmd.SetHelpFunc(manifestTemplateHelpFunc())
	return cmd
}

func manifestTemplateHelpFunc() func(*cobra.Command, []string) {
	renderer := help.NewRenderer(theme.Default())
	return cobracli.HelpFunc(renderer, func(cmd *cobra.Command) []help.Section {
		sections := cobracli.Sections(cmd)
		sections = append(sections, help.Section{
			Title: "Templates",
			Content: []help.Content{
				help.Text("Presets pick least-privilege scope sets. Use --user-scope or --bot-scope to replace the selected preset."),
				manifestTemplatePresetHelp(),
			},
		})
		sections = append(sections, help.Section{
			Title: "Messaging Template User Scopes",
			Content: []help.Content{
				help.Text("This is the default user-token manifest. It supports reading, sending, replies, DMs, and reactions without file upload or search access."),
				manifestScopeHelp(defaultManifestPreset),
			},
		})
		return sections
	})
}

func runManifestTemplate(cmd *cobra.Command, runtime *RootRuntime, opts manifestTemplateOptions) error {
	ctx := localManifestContext(cmd, runtime)
	if opts.CallbackPort != "" && !cmd.Flags().Changed("redirect-url") {
		opts.RedirectURLs = []string{manifestOAuthRedirectURLForPort(opts.CallbackPort)}
	}
	manifest, err := manifestTemplate(opts)
	if err != nil {
		return writeCommandError(ctx, validationCLIError(err.Error()))
	}
	switch strings.ToLower(strings.TrimSpace(opts.Format)) {
	case "", "json":
		ctx.stdoutLogger().Print().Mode(clog.JSONFlat).JSON(manifest)
	case "yaml", "yml":
		return ctx.WritePlain(renderManifestYAML(manifest))
	default:
		return writeCommandError(ctx, validationCLIError("format must be json or yaml"))
	}
	return nil
}

func manifestOAuthRedirectURLForPort(port string) string {
	if strings.TrimSpace(port) == osAssignedCallbackPort {
		if allocated, err := allocateLocalOAuthCallbackPort(); err == nil {
			port = allocated
		}
	}
	return oauthRedirectURLForPort(port)
}

func localManifestContext(cmd *cobra.Command, runtime *RootRuntime) *CommandContext {
	opts := rootOptionsFromCommand(cmd, runtime)
	return &CommandContext{
		Workspace: "manifest",
		Mode:      opts.Output.Resolve(runtime.IsTTY, DetectAgentOutputMode(opts.Agent)),
		Stdout:    runtime.Stdout,
		Stderr:    runtime.Stderr,
		Now:       runtime.Now,
		RequestID: runtime.RequestID,
	}
}

func manifestTemplate(opts manifestTemplateOptions) (*generatedManifest, error) {
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		return nil, errors.New("name is required")
	}
	authType := strings.ToLower(strings.TrimSpace(opts.Type))
	botScopes, userScopes, err := manifestScopes(authType, opts.Preset, opts.BotScopes, opts.UserScopes)
	if err != nil {
		return nil, err
	}
	var features *generatedFeatures
	if len(botScopes) > 0 {
		features = &generatedFeatures{}
		features.BotUser = &slackgo.BotUser{
			DisplayName:  botDisplayName(name),
			AlwaysOnline: false,
		}
	}
	return &generatedManifest{
		Display: slackgo.Display{
			Name:            name,
			Description:     strings.TrimSpace(opts.Description),
			LongDescription: strings.TrimSpace(opts.LongDescription),
			BackgroundColor: strings.TrimSpace(opts.BackgroundColor),
		},
		Features: features,
		OAuthConfig: generatedOAuthConfig{
			RedirectUrls: cleanStrings(opts.RedirectURLs),
			Scopes: slackgo.OAuthScopes{
				Bot:  botScopes,
				User: userScopes,
			},
			PKCEEnabled: true,
		},
		Settings: &generatedManifestSettings{TokenRotationEnabled: true},
	}, nil
}

func manifestScopes(authType, template string, botScopes, userScopes []string) ([]string, []string, error) {
	if authType == "" {
		authType = "user"
	}
	presetScopes, err := manifestPresetScopes(template)
	if err != nil {
		return nil, nil, err
	}
	switch authType {
	case "user":
		return nil, scopesOrPreset(userScopes, presetScopes), nil
	case "bot":
		return botCompatibleScopes(scopesOrPreset(botScopes, presetScopes)), nil, nil
	case "both":
		return botCompatibleScopes(scopesOrPreset(botScopes, presetScopes)), scopesOrPreset(userScopes, presetScopes), nil
	default:
		return nil, nil, errors.New("type must be user, bot, or both")
	}
}

func scopesOrPreset(scopes, preset []string) []string {
	out := normalizeScopes(scopes)
	if len(out) == 0 {
		return slices.Clone(preset)
	}
	return out
}

func botCompatibleScopes(scopes []string) []string {
	out := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		if userTokenOnlyManifestScope(scope) {
			continue
		}
		out = append(out, scope)
	}
	return out
}

func userTokenOnlyManifestScope(scope string) bool {
	return scope == "search:read" || scope == "users.profile:write"
}

func manifestTemplatePresets() []manifestTemplatePreset {
	return []manifestTemplatePreset{
		{
			Name:        "readonly",
			Description: "Read conversations, reactions, and users without mutation scopes.",
			Scopes: []string{
				"channels:history",
				"channels:read",
				"groups:history",
				"groups:read",
				"im:history",
				"im:read",
				"mpim:history",
				"mpim:read",
				"reactions:read",
				"users:read",
			},
		},
		{
			Name:        "messaging",
			Description: "Default. Read conversations, send messages and DMs, and manage reactions.",
			Scopes: []string{
				"channels:history",
				"channels:read",
				"chat:write",
				"groups:history",
				"groups:read",
				"im:history",
				"im:read",
				"im:write",
				"mpim:history",
				"mpim:read",
				"mpim:write",
				"reactions:read",
				"reactions:write",
				"users:read",
				"users:read.email",
			},
		},
		{
			Name:        "files",
			Description: "Messaging plus file upload support.",
			Scopes: []string{
				"channels:history",
				"channels:read",
				"chat:write",
				"files:write",
				"groups:history",
				"groups:read",
				"im:history",
				"im:read",
				"im:write",
				"mpim:history",
				"mpim:read",
				"mpim:write",
				"reactions:read",
				"reactions:write",
				"users:read",
				"users:read.email",
			},
		},
		{
			Name:        "search",
			Description: "Readonly plus workspace message search.",
			Scopes: []string{
				"channels:history",
				"channels:read",
				"groups:history",
				"groups:read",
				"im:history",
				"im:read",
				"mpim:history",
				"mpim:read",
				"reactions:read",
				"search:read",
				"users:read",
			},
		},
		{
			Name:        "full",
			Description: "Messaging, file upload, and search scopes.",
			Scopes: []string{
				"channels:history",
				"channels:read",
				"chat:write",
				"files:write",
				"groups:history",
				"groups:read",
				"im:history",
				"im:read",
				"im:write",
				"mpim:history",
				"mpim:read",
				"mpim:write",
				"reactions:read",
				"reactions:write",
				"search:read",
				"users:read",
				"users:read.email",
				"users.profile:write",
			},
		},
	}
}

func manifestPresetScopes(template string) ([]string, error) {
	template = strings.ToLower(strings.TrimSpace(template))
	if template == "" {
		template = defaultManifestPreset
	}
	for _, preset := range manifestTemplatePresets() {
		if preset.Name == template {
			return slices.Clone(preset.Scopes), nil
		}
	}
	return nil, errors.New("template must be readonly, messaging, files, search, or full")
}

func manifestTemplatePresetHelp() help.CommandGroup {
	presets := manifestTemplatePresets()
	out := make(help.CommandGroup, 0, len(presets))
	for _, preset := range presets {
		out = append(out, help.Command{Name: preset.Name, Desc: preset.Description})
	}
	return out
}

func manifestScopeReasons() []manifestScopeReason {
	return []manifestScopeReason{
		{Scope: "channels:history", Reason: "Read public channel history and thread replies."},
		{Scope: "channels:read", Reason: "List public channels and resolve public channel metadata."},
		{Scope: "chat:write", Reason: "Send, edit, and delete Slack CLI messages."},
		{Scope: "files:write", Reason: "Upload files and stdin payloads as Slack files."},
		{Scope: "groups:history", Reason: "Read private channel history and thread replies."},
		{Scope: "groups:read", Reason: "List private channels and resolve private channel metadata."},
		{Scope: "im:history", Reason: "Read direct message history and thread replies."},
		{Scope: "im:read", Reason: "List and inspect direct message conversations."},
		{Scope: "im:write", Reason: "Open direct messages and send Slack CLI DMs."},
		{Scope: "mpim:history", Reason: "Read group direct message history and thread replies."},
		{Scope: "mpim:read", Reason: "List and inspect group direct message conversations."},
		{Scope: "mpim:write", Reason: "Open group direct messages when Slack requires a write scope."},
		{Scope: "reactions:read", Reason: "List emoji reactions on messages."},
		{Scope: "reactions:write", Reason: "Add and remove emoji reactions."},
		{Scope: "search:read", Reason: "Search workspace messages."},
		{Scope: "users:read", Reason: "List users, inspect user metadata, and read presence."},
		{Scope: "users:read.email", Reason: "Resolve direct-message recipients by email address."},
		{Scope: "users.profile:write", Reason: "Set and clear the authenticated user's Slack status."},
	}
}

func manifestScopeHelp(template string) help.CommandGroup {
	scopes, err := manifestPresetScopes(template)
	if err != nil {
		return nil
	}
	reasonsByScope := make(map[string]string)
	for _, reason := range manifestScopeReasons() {
		reasonsByScope[reason.Scope] = reason.Reason
	}
	out := make(help.CommandGroup, 0, len(scopes))
	for _, scope := range scopes {
		out = append(out, help.Command{Name: scope, Desc: reasonsByScope[scope]})
	}
	return out
}

func normalizeScopes(scopes []string) []string {
	out := xslices.Unique(cleanStrings(scopes))
	slices.Sort(out)
	return out
}

func cleanStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func botDisplayName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, " ", "-")
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "slack-cli"
	}
	return b.String()
}

func renderManifestYAML(manifest *generatedManifest) string {
	var b strings.Builder
	b.WriteString("display_information:\n")
	b.WriteString("  name: " + yamlString(manifest.Display.Name) + "\n")
	if manifest.Display.Description != "" {
		b.WriteString("  description: " + yamlString(manifest.Display.Description) + "\n")
	}
	if manifest.Display.LongDescription != "" {
		b.WriteString("  long_description: " + yamlString(manifest.Display.LongDescription) + "\n")
	}
	if manifest.Display.BackgroundColor != "" {
		b.WriteString("  background_color: " + yamlString(manifest.Display.BackgroundColor) + "\n")
	}
	if manifest.Features != nil && manifest.Features.BotUser != nil && manifest.Features.BotUser.DisplayName != "" {
		b.WriteString("features:\n")
		b.WriteString("  bot_user:\n")
		b.WriteString("    display_name: " + yamlString(manifest.Features.BotUser.DisplayName) + "\n")
	}
	b.WriteString("oauth_config:\n")
	if len(manifest.OAuthConfig.RedirectUrls) > 0 {
		b.WriteString("  redirect_urls:\n")
		for _, redirectURL := range manifest.OAuthConfig.RedirectUrls {
			b.WriteString("    - " + yamlString(redirectURL) + "\n")
		}
	}
	b.WriteString("  pkce_enabled: true\n")
	b.WriteString("  scopes:\n")
	if len(manifest.OAuthConfig.Scopes.Bot) > 0 {
		b.WriteString("    bot:\n")
		for _, scope := range manifest.OAuthConfig.Scopes.Bot {
			b.WriteString("      - " + yamlString(scope) + "\n")
		}
	}
	if len(manifest.OAuthConfig.Scopes.User) > 0 {
		b.WriteString("    user:\n")
		for _, scope := range manifest.OAuthConfig.Scopes.User {
			b.WriteString("      - " + yamlString(scope) + "\n")
		}
	}
	if manifest.Settings != nil {
		b.WriteString("settings:\n")
		if manifest.Settings.TokenRotationEnabled {
			b.WriteString("  token_rotation_enabled: true\n")
		}
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func yamlString(value string) string {
	return strconv.Quote(value)
}
