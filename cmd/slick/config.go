package main

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"charm.land/huh/v2"
	clib "github.com/gechr/clib/cli/cobra"
	clibtheme "github.com/gechr/clib/theme"
	xfs "github.com/gechr/x/fs"
	"github.com/gechr/x/human"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type configInitOptions struct {
	Profile          string
	DefaultChannel   string
	AgentAttribution bool
	AgentLabel       string
	AgentEmoji       string
	AgentMessage     string
	Force            bool
}

type configInitData struct {
	Path      string `json:"path"`
	Profile   string `json:"profile"`
	Workspace string `json:"workspace"`
	Written   bool   `json:"written"`
}

var _ PlainRenderer = configInitData{}

func (d configInitData) WritePlain(c *CommandContext, _ string, _ *Pagination) error {
	c.resultEvent("config.init").
		Link("path", d.Path, human.ContractHome(d.Path)).
		Str("profile", d.Profile).
		Str("workspace", d.Workspace).
		Bool("written", d.Written).
		Send()
	return nil
}

type configPathData struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
}

var _ PlainRenderer = configPathData{}

func (d configPathData) WritePlain(c *CommandContext, command string, _ *Pagination) error {
	c.resultEvent(command).
		Link("path", d.Path, human.ContractHome(d.Path)).
		Bool("exists", d.Exists).
		Send()
	return nil
}

type configEntry struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

type configListData struct {
	Path             string        `json:"path"`
	DefaultWorkspace string        `json:"default_workspace"`
	Settings         []configEntry `json:"settings"`
}

var _ PlainRenderer = configListData{}

func (d configListData) WritePlain(c *CommandContext, command string, _ *Pagination) error {
	c.resultEvent(command).
		Link("path", d.Path, human.ContractHome(d.Path)).
		Str("default_workspace", d.DefaultWorkspace).
		Int("settings", len(d.Settings)).
		Send()
	if len(d.Settings) == 0 {
		return nil
	}
	for _, setting := range d.Settings {
		c.resultEvent(command).
			Str("key", setting.Key).
			Str("value", setting.Value).
			Msg("config setting")
	}
	return nil
}

type configGetData struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

var _ PlainRenderer = configGetData{}

func (d configGetData) WritePlain(c *CommandContext, command string, _ *Pagination) error {
	c.resultEvent(command).
		Str("key", d.Key).
		Str("value", d.Value).
		Send()
	return nil
}

type configMutationData struct {
	Path  string `json:"path"`
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
}

var _ PlainRenderer = configMutationData{}

func (d configMutationData) WritePlain(c *CommandContext, command string, _ *Pagination) error {
	c.resultEvent(command).
		Link("path", d.Path, human.ContractHome(d.Path)).
		Str("key", d.Key).
		Str("value", d.Value).
		Send()
	return nil
}

var slackConfigKeys = []string{
	"default_workspace",
	"workspaces.<profile>.default_channel",
	"workspaces.<profile>.attribution.enabled",
	"workspaces.<profile>.attribution.label",
	"workspaces.<profile>.attribution.emoji",
	"workspaces.<profile>.attribution.message",
}

var slackConfigDescriptions = map[string]string{
	"default_workspace":                        "Default workspace profile name",
	"workspaces.<profile>.default_channel":     "Fallback message channel ID or alias",
	"workspaces.<profile>.attribution.enabled": "Add visible attribution by default",
	"workspaces.<profile>.attribution.label":   "Attribution label override",
	"workspaces.<profile>.attribution.emoji":   "Attribution emoji override",
	"workspaces.<profile>.attribution.message": "Attribution message override",
}

func newConfigCommand(runtime *RootRuntime) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "config",
		Short:        "Manage slack configuration",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newConfigInitCommand(runtime))
	cmd.AddCommand(newConfigPathCommand(runtime))
	cmd.AddCommand(newConfigListCommand(runtime))
	cmd.AddCommand(newConfigGetCommand(runtime))
	cmd.AddCommand(newConfigSetCommand(runtime))
	cmd.AddCommand(newConfigUnsetCommand(runtime))
	return cmd
}

func newConfigInitCommand(runtime *RootRuntime) *cobra.Command {
	opts := configInitOptions{
		Profile:          "default",
		AgentAttribution: true,
	}
	cmd := &cobra.Command{
		Use:          "init",
		Short:        "First-run config setup",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runConfigInit(cmd, runtime, opts)
		},
	}
	cmd.Flags().StringVarP(&opts.Profile, "profile", "p", opts.Profile, "Local workspace profile name")
	cmd.Flags().StringVarP(&opts.DefaultChannel, "default-channel", "c", "", "Default message channel ID or alias")
	cmd.Flags().BoolVarP(&opts.AgentAttribution, "attribution-enabled", "A", opts.AgentAttribution, "Enable visible attribution by default")
	cmd.Flags().StringVarP(&opts.AgentLabel, "attribution-label", "l", "", "Attribution label")
	cmd.Flags().StringVarP(&opts.AgentEmoji, "attribution-emoji", "e", "", "Attribution emoji")
	cmd.Flags().StringVarP(&opts.AgentMessage, "attribution-message", "m", "", "Attribution message")
	cmd.Flags().SetNormalizeFunc(func(_ *pflag.FlagSet, name string) pflag.NormalizedName {
		switch name {
		case "agent-attribution":
			return pflag.NormalizedName("attribution-enabled")
		case "agent-label":
			return pflag.NormalizedName("attribution-label")
		case "agent-emoji":
			return pflag.NormalizedName("attribution-emoji")
		case "agent-message":
			return pflag.NormalizedName("attribution-message")
		}
		return pflag.NormalizedName(name)
	})
	cmd.Flags().BoolVarP(&opts.Force, "force", "F", false, "Overwrite an existing config")
	extendConfigInitFlags(cmd)
	return cmd
}

func runConfigInit(cmd *cobra.Command, runtime *RootRuntime, opts configInitOptions) error {
	ctx := localConfigContext(cmd, runtime)
	path := runtime.ConfigPath
	if strings.TrimSpace(path) == "" {
		return writeCommandError(ctx, validationCLIError("config path is unavailable"))
	}
	exists, err := xfs.Exists(path)
	if err != nil {
		return writeCommandError(ctx, validationCLIError(err.Error()))
	}
	if exists && !opts.Force {
		if !runtime.IsTTY {
			return writeCommandError(ctx, validationCLIError("config already exists; rerun with --force to overwrite"))
		}
		overwrite, err := confirmConfigOverwrite(runtime, path)
		if err != nil {
			return writeCommandError(ctx, validationCLIError(err.Error()))
		}
		if !overwrite {
			return ctx.WriteResult("config.init", configInitData{Path: path, Profile: opts.Profile, Workspace: opts.Profile, Written: false})
		}
	}

	if runtime.IsTTY && shouldPromptConfigInit(cmd) {
		if err := runConfigInitForm(runtime, &opts); err != nil {
			return writeCommandError(ctx, validationCLIError(err.Error()))
		}
	}
	profile, err := configProfileFromInitOptions(opts)
	if err != nil {
		return writeCommandError(ctx, validationCLIError(err.Error()))
	}
	cfg := &config.Config{
		SchemaVersion:    config.SchemaVersion,
		DefaultWorkspace: profile.Name,
		Workspaces: map[string]config.WorkspaceProfile{
			profile.Name: profile,
		},
	}
	if err := config.SaveFile(path, cfg); err != nil {
		return writeCommandError(ctx, validationCLIError(err.Error()))
	}
	runtime.Config = cfg
	return ctx.WriteResult("config.init", configInitData{Path: path, Profile: profile.Name, Workspace: profile.Name, Written: true})
}

func shouldPromptConfigInit(cmd *cobra.Command) bool {
	flags := cmd.Flags()
	return !slices.ContainsFunc([]string{
		"profile",
		"default-channel",
		"attribution-enabled",
		"attribution-label",
		"attribution-emoji",
		"attribution-message",
	}, flags.Changed)
}

func confirmConfigOverwrite(runtime *RootRuntime, path string) (bool, error) {
	overwrite := false
	confirm := huh.NewConfirm().
		Title("Overwrite existing config?").
		Description(path).
		Affirmative("Overwrite").
		Negative("Keep existing").
		Value(&overwrite)
	if err := runConfigForm(runtime, huh.NewForm(huh.NewGroup(confirm))); err != nil {
		return false, err
	}
	return overwrite, nil
}

func runConfigInitForm(runtime *RootRuntime, opts *configInitOptions) error {
	help := configInitFieldHelp()
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Profile name").
				Description(help["profile"]).
				Placeholder("default").
				Value(&opts.Profile).
				Validate(requiredField("profile name")),
			huh.NewInput().
				Title("Default message channel").
				Description(help["default_channel"]).
				Placeholder("C7N2Q8L4P").
				Value(&opts.DefaultChannel),
			huh.NewConfirm().
				Title("Add visible attribution to sent messages?").
				Description(help["agent_attribution"]).
				Value(&opts.AgentAttribution),
			huh.NewInput().
				Title("Attribution emoji").
				Description(help["agent_emoji"]).
				Placeholder(":robot_face:").
				Value(&opts.AgentEmoji),
			huh.NewInput().
				Title("Attribution message").
				Description(help["agent_message"]).
				Placeholder("Sent via slick").
				Value(&opts.AgentMessage),
		),
	)
	return runConfigForm(runtime, form)
}

func runConfigForm(runtime *RootRuntime, form *huh.Form) error {
	form = form.
		WithTheme(authLoginHuhTheme(clibtheme.Default())).
		WithInput(runtime.Stdin).
		WithOutput(runtime.Stderr)
	if !usesTerminalFiles(runtime) {
		form.WithAccessible(true)
	}
	return form.Run()
}

func configInitFieldHelp() map[string]string {
	return map[string]string{
		"profile":           "Local Slack CLI profile. Select it later with --workspace; use default if you only need one profile.",
		"default_channel":   "Optional fallback destination for slick message send when no --channel or --user target is provided. Use a channel ID or configured alias.",
		"agent_attribution": "Default on. Sent messages include a visible Slack context block; detected agent and CI runs add agent-mode wording.",
		"agent_emoji":       "Emoji shown in the attribution context block.",
		"agent_message":     "Text shown in the attribution context block.",
	}
}

func configProfileFromInitOptions(opts configInitOptions) (config.WorkspaceProfile, error) {
	name := strings.TrimSpace(opts.Profile)
	if name == "" {
		return config.WorkspaceProfile{}, errors.New("profile is required")
	}
	agentAttribution := opts.AgentAttribution
	return config.WorkspaceProfile{
		Name:           name,
		DefaultChannel: strings.TrimSpace(opts.DefaultChannel),
		Attribution: config.AttributionConfig{
			Enabled: &agentAttribution,
			Label:   strings.TrimSpace(opts.AgentLabel),
			Emoji:   strings.TrimSpace(opts.AgentEmoji),
			Message: strings.TrimSpace(opts.AgentMessage),
		},
	}, nil
}

func newConfigPathCommand(runtime *RootRuntime) *cobra.Command {
	return &cobra.Command{
		Use:          "path",
		Short:        "Print the config file path",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := localConfigContext(cmd, runtime)
			exists, err := xfs.Exists(runtime.ConfigPath)
			return ctx.WriteResult("config.path", configPathData{Path: runtime.ConfigPath, Exists: err == nil && exists})
		},
	}
}

func newConfigListCommand(runtime *RootRuntime) *cobra.Command {
	return &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "Show current settings",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := localConfigContext(cmd, runtime)
			cfg, err := loadConfigForConfigCommand(runtime)
			if err != nil {
				return writeCommandError(ctx, validationCLIError(err.Error()))
			}
			return ctx.WriteResult("config.list", configListData{
				Path:             runtime.ConfigPath,
				DefaultWorkspace: cfg.DefaultWorkspace,
				Settings:         configEntries(cfg),
			})
		},
	}
}

func newConfigGetCommand(runtime *RootRuntime) *cobra.Command {
	return &cobra.Command{
		Use:          "get <key>",
		Short:        "Show a configuration value",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := localConfigContext(cmd, runtime)
			cfg, err := loadConfigForConfigCommand(runtime)
			if err != nil {
				return writeCommandError(ctx, validationCLIError(err.Error()))
			}
			value, err := getConfigValue(cfg, args[0])
			if err != nil {
				return writeCommandError(ctx, validationCLIError(err.Error()))
			}
			return ctx.WriteResult("config.get", configGetData{Key: args[0], Value: value})
		},
	}
}

func newConfigSetCommand(runtime *RootRuntime) *cobra.Command {
	return &cobra.Command{
		Use:          "set <key> <value>",
		Short:        "Add or update a setting",
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := localConfigContext(cmd, runtime)
			cfg, err := loadConfigForConfigCommand(runtime)
			if err != nil {
				return writeCommandError(ctx, validationCLIError(err.Error()))
			}
			if err := setConfigValue(cfg, args[0], args[1]); err != nil {
				return writeCommandError(ctx, validationCLIError(err.Error()))
			}
			if err := config.SaveFile(runtime.ConfigPath, cfg); err != nil {
				return writeCommandError(ctx, validationCLIError(err.Error()))
			}
			runtime.Config = cfg
			return ctx.WriteResult("config.set", configMutationData{Path: runtime.ConfigPath, Key: args[0], Value: args[1]})
		},
	}
}

func newConfigUnsetCommand(runtime *RootRuntime) *cobra.Command {
	return &cobra.Command{
		Use:          "unset <key>",
		Aliases:      []string{"rm", "remove"},
		Short:        "Remove a setting",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := localConfigContext(cmd, runtime)
			cfg, err := loadConfigForConfigCommand(runtime)
			if err != nil {
				return writeCommandError(ctx, validationCLIError(err.Error()))
			}
			if err := unsetConfigValue(cfg, args[0]); err != nil {
				return writeCommandError(ctx, validationCLIError(err.Error()))
			}
			if err := config.SaveFile(runtime.ConfigPath, cfg); err != nil {
				return writeCommandError(ctx, validationCLIError(err.Error()))
			}
			runtime.Config = cfg
			return ctx.WriteResult("config.unset", configMutationData{Path: runtime.ConfigPath, Key: args[0]})
		},
	}
}

func localConfigContext(cmd *cobra.Command, runtime *RootRuntime) *CommandContext {
	opts := rootOptionsFromCommand(cmd, runtime)
	mode := opts.Output.Resolve(runtime.IsTTY, DetectAgentOutputMode(opts.Agent))
	sl, el := buildBaseLoggers(runtime.Stdout, runtime.Stderr, runtime.ColorMode)
	applyRenderMode(sl, mode)
	return &CommandContext{
		Workspace: "config",
		Mode:      mode,
		Stdout:    runtime.Stdout,
		Stderr:    runtime.Stderr,
		Now:       runtime.Now,
		RequestID: runtime.RequestID,
		stdoutLog: sl,
		stderrLog: el,
	}
}

func loadConfigForConfigCommand(runtime *RootRuntime) (*config.Config, error) {
	if runtime.Config != nil {
		return runtime.Config, nil
	}
	if runtime.ConfigPath == "" {
		return nil, errors.New("config path is unavailable")
	}
	return cliruntime.LoadDefaultConfig(runtime.ConfigPath)
}

func configEntries(cfg *config.Config) []configEntry {
	var entries []configEntry
	entries = append(entries, configEntry{
		Key:         "default_workspace",
		Value:       cfg.DefaultWorkspace,
		Description: slackConfigDescriptions["default_workspace"],
	})
	for _, name := range sortedWorkspaceNames(cfg) {
		workspace := cfg.Workspaces[name]
		prefix := "workspaces." + name + "."
		entries = append(entries,
			configEntry{Key: prefix + "default_channel", Value: workspace.DefaultChannel, Description: slackConfigDescriptions["workspaces.<profile>.default_channel"]},
			configEntry{Key: prefix + "attribution.enabled", Value: boolPtrString(attributionEnabled(workspace)), Description: slackConfigDescriptions["workspaces.<profile>.attribution.enabled"]},
			configEntry{Key: prefix + "attribution.label", Value: workspace.Attribution.Label, Description: slackConfigDescriptions["workspaces.<profile>.attribution.label"]},
			configEntry{Key: prefix + "attribution.emoji", Value: workspace.Attribution.Emoji, Description: slackConfigDescriptions["workspaces.<profile>.attribution.emoji"]},
			configEntry{Key: prefix + "attribution.message", Value: workspace.Attribution.Message, Description: slackConfigDescriptions["workspaces.<profile>.attribution.message"]},
		)
	}
	return entries
}

func sortedWorkspaceNames(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.Workspaces))
	for name := range cfg.Workspaces {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func boolPtrString(value *bool) string {
	if value == nil {
		return ""
	}
	return strconv.FormatBool(*value)
}

func attributionEnabled(workspace config.WorkspaceProfile) *bool {
	if workspace.Attribution.Enabled != nil {
		return workspace.Attribution.Enabled
	}
	return workspace.AgentAttribution
}

func firstNonEmptyConfigValue(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func getConfigValue(cfg *config.Config, key string) (string, error) {
	if key == "default_workspace" {
		return cfg.DefaultWorkspace, nil
	}
	workspaceName, field, err := parseWorkspaceConfigKey(key)
	if err != nil {
		return "", err
	}
	workspace, ok := cfg.Workspaces[workspaceName]
	if !ok {
		return "", fmt.Errorf("workspace %q not found", workspaceName)
	}
	switch field {
	case "default_channel":
		return workspace.DefaultChannel, nil
	case "agent_attribution":
		return boolPtrString(attributionEnabled(workspace)), nil
	case "agent_label":
		return firstNonEmptyConfigValue(workspace.Attribution.Label, workspace.AgentLabel), nil
	case "agent_emoji":
		return firstNonEmptyConfigValue(workspace.Attribution.Emoji, workspace.AgentEmoji), nil
	case "agent_message":
		return firstNonEmptyConfigValue(workspace.Attribution.Message, workspace.AgentMessage), nil
	case "attribution.enabled":
		return boolPtrString(attributionEnabled(workspace)), nil
	case "attribution.label":
		return workspace.Attribution.Label, nil
	case "attribution.emoji":
		return workspace.Attribution.Emoji, nil
	case "attribution.message":
		return workspace.Attribution.Message, nil
	default:
		if configAuthOwnedField(field) {
			return "", authOwnedConfigKey(field)
		}
		return "", unknownConfigKey(key)
	}
}

func setConfigValue(cfg *config.Config, key, value string) error {
	if key == "default_workspace" {
		if _, ok := cfg.Workspaces[value]; !ok {
			return fmt.Errorf("workspace %q not found", value)
		}
		cfg.DefaultWorkspace = value
		return nil
	}
	workspaceName, field, err := parseWorkspaceConfigKey(key)
	if err != nil {
		return err
	}
	workspace, ok := cfg.Workspaces[workspaceName]
	if !ok {
		return fmt.Errorf("workspace %q not found", workspaceName)
	}
	switch field {
	case "default_channel":
		workspace.DefaultChannel = value
	case "agent_attribution":
		fallthrough
	case "attribution.enabled":
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("%s must be true or false: %w", field, err)
		}
		workspace.Attribution.Enabled = &parsed
		workspace.AgentAttribution = nil
	case "agent_label":
		workspace.Attribution.Label = value
		workspace.AgentLabel = ""
	case "agent_emoji":
		workspace.Attribution.Emoji = value
		workspace.AgentEmoji = ""
	case "agent_message":
		workspace.Attribution.Message = value
		workspace.AgentMessage = ""
	case "attribution.label":
		workspace.Attribution.Label = value
	case "attribution.emoji":
		workspace.Attribution.Emoji = value
	case "attribution.message":
		workspace.Attribution.Message = value
	default:
		if configAuthOwnedField(field) {
			return authOwnedConfigKey(field)
		}
		return unknownConfigKey(key)
	}
	cfg.Workspaces[workspaceName] = workspace
	return cfg.Validate()
}

func unsetConfigValue(cfg *config.Config, key string) error {
	if key == "default_workspace" {
		return errors.New("default_workspace cannot be unset")
	}
	workspaceName, field, err := parseWorkspaceConfigKey(key)
	if err != nil {
		return err
	}
	workspace, ok := cfg.Workspaces[workspaceName]
	if !ok {
		return fmt.Errorf("workspace %q not found", workspaceName)
	}
	switch field {
	case "default_channel":
		workspace.DefaultChannel = ""
	case "agent_attribution":
		fallthrough
	case "attribution.enabled":
		workspace.AgentAttribution = nil
		workspace.Attribution.Enabled = nil
	case "agent_label":
		workspace.AgentLabel = ""
		workspace.Attribution.Label = ""
	case "agent_emoji":
		workspace.AgentEmoji = ""
		workspace.Attribution.Emoji = ""
	case "agent_message":
		workspace.AgentMessage = ""
		workspace.Attribution.Message = ""
	case "attribution.label":
		workspace.Attribution.Label = ""
	case "attribution.emoji":
		workspace.Attribution.Emoji = ""
	case "attribution.message":
		workspace.Attribution.Message = ""
	default:
		if configAuthOwnedField(field) {
			return authOwnedConfigKey(field)
		}
		return unknownConfigKey(key)
	}
	cfg.Workspaces[workspaceName] = workspace
	return cfg.Validate()
}

func parseWorkspaceConfigKey(key string) (string, string, error) {
	parts := strings.Split(key, ".")
	if len(parts) < 3 || parts[0] != "workspaces" {
		return "", "", unknownConfigKey(key)
	}
	workspace := parts[1]
	field := strings.Join(parts[2:], ".")
	if workspace == "" || field == "" {
		return "", "", unknownConfigKey(key)
	}
	return workspace, field, nil
}

func configAuthOwnedField(field string) bool {
	switch field {
	case "team_id", "team_name", "token_type", "token", "token_ref":
		return true
	default:
		return false
	}
}

func authOwnedConfigKey(field string) error {
	return fmt.Errorf("%s is auth-owned; auth settings are managed by slick auth", field)
}

func unknownConfigKey(key string) error {
	return fmt.Errorf("unknown config key %s - run 'slick config list' to see all keys", key)
}

func slackConfigKeyCompletions(cfg *config.Config) []string {
	if cfg == nil || len(cfg.Workspaces) == 0 {
		completions := make([]string, 0, len(slackConfigKeys))
		for _, key := range slackConfigKeys {
			completions = append(completions, key+"\t"+slackConfigDescriptions[key])
		}
		return completions
	}
	completions := []string{"default_workspace\t" + slackConfigDescriptions["default_workspace"]}
	for _, profile := range sortedWorkspaceNames(cfg) {
		prefix := "workspaces." + profile + "."
		completions = append(completions,
			prefix+"default_channel\t"+slackConfigDescriptions["workspaces.<profile>.default_channel"],
			prefix+"attribution.enabled\t"+slackConfigDescriptions["workspaces.<profile>.attribution.enabled"],
			prefix+"attribution.label\t"+slackConfigDescriptions["workspaces.<profile>.attribution.label"],
			prefix+"attribution.emoji\t"+slackConfigDescriptions["workspaces.<profile>.attribution.emoji"],
			prefix+"attribution.message\t"+slackConfigDescriptions["workspaces.<profile>.attribution.message"],
		)
	}
	return completions
}

func slackConfigValueCompletions(key string, cfg *config.Config) []string {
	switch {
	case key == "default_workspace":
		return completionWorkspaceNames(cfg)
	case strings.HasSuffix(key, ".attribution.enabled"):
		return []string{"true", "false"}
	case strings.HasSuffix(key, ".attribution.emoji"):
		return commonEmojiCompletions()
	default:
		return nil
	}
}

func extendConfigInitFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	clib.Extend(f.Lookup("profile"), clib.FlagExtra{Group: "Profile", Placeholder: "NAME", Terse: "profile name"})
	clib.Extend(f.Lookup("default-channel"), clib.FlagExtra{Group: "Profile", Placeholder: "C7N2Q8L4P", Terse: "default channel"})
	clib.Extend(f.Lookup("attribution-enabled"), clib.FlagExtra{Group: "Attribution", Terse: "attribution"})
	clib.Extend(f.Lookup("attribution-label"), clib.FlagExtra{Group: "Attribution", Placeholder: "LABEL", Terse: "attribution label"})
	clib.Extend(f.Lookup("attribution-emoji"), clib.FlagExtra{Group: "Attribution", Placeholder: ":robot_face:", Terse: "attribution emoji"})
	clib.Extend(f.Lookup("attribution-message"), clib.FlagExtra{Group: "Attribution", Placeholder: "TEXT", Terse: "attribution message"})
	clib.Extend(f.Lookup("force"), clib.FlagExtra{Group: "Safety", Terse: "overwrite config"})
}
