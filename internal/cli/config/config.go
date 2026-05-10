// Package config implements the `slick config` cobra command tree, which
// manages the local TOML configuration profile and preferences.
package config

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"charm.land/huh/v2"
	clib "github.com/gechr/clib/cli/cobra"
	clibtheme "github.com/gechr/clib/theme"
	"github.com/gechr/clog"
	xfs "github.com/gechr/x/fs"
	"github.com/gechr/x/human"
	clitheme "github.com/matcra587/slack-cli/internal/cli/clitheme"
	"github.com/matcra587/slack-cli/internal/cli/cliutil"
	clioauth "github.com/matcra587/slack-cli/internal/cli/oauth"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// InitOptions captures CLI-flag-driven inputs for `slick config init`.
type InitOptions struct {
	Profile          string
	DefaultChannel   string
	AgentAttribution bool
	AgentLabel       string
	AgentEmoji       string
	AgentMessage     string
	Force            bool
}

// InitData is the result returned by `slick config init`.
type InitData struct {
	Path      string `json:"path"`
	Profile   string `json:"profile"`
	Workspace string `json:"workspace"`
	Written   bool   `json:"written"`
}

var _ clioutput.PlainRenderer = InitData{}

func (d InitData) WritePlain(c *clioutput.CommandContext, command string, _ *clioutput.Pagination) error {
	clioutput.ApplyFieldStyles(c.StdoutLogger(), c.Theme,
		clioutput.HashedFieldStyle("profile", "workspace:"+d.Profile),
		clioutput.EntityFieldStyle("workspace", d.Workspace),
	)
	c.ResultEvent(command).
		Link("path", d.Path, clioutput.HyperlinkText(human.ContractHome(d.Path))).
		Str("profile", d.Profile).
		Str("workspace", d.Workspace).
		Bool("written", d.Written).
		Msg(clioutput.ActionLabel(command))
	return nil
}

// PathData is the result returned by `slick config path`.
type PathData struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
}

var _ clioutput.PlainRenderer = PathData{}

func (d PathData) WritePlain(c *clioutput.CommandContext, command string, _ *clioutput.Pagination) error {
	c.ResultEvent(command).
		Link("path", d.Path, clioutput.HyperlinkText(human.ContractHome(d.Path))).
		Bool("exists", d.Exists).
		Msg(clioutput.ActionLabel(command))
	return nil
}

// Entry is a single key/value/description triple in the listed configuration.
type Entry struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

// ListData is the result returned by `slick config list`.
type ListData struct {
	Path             string  `json:"path"`
	DefaultWorkspace string  `json:"default_workspace"`
	Settings         []Entry `json:"settings"`
}

var _ clioutput.PlainRenderer = ListData{}

func (d ListData) WritePlain(c *clioutput.CommandContext, command string, _ *clioutput.Pagination) error {
	if len(d.Settings) == 0 || clog.IsVerbose() {
		clioutput.ApplyFieldStyles(c.StdoutLogger(), c.Theme,
			clioutput.HashedFieldStyle("default_workspace", "workspace:"+d.DefaultWorkspace),
		)
		c.ResultEvent(command).
			Link("path", d.Path, clioutput.HyperlinkText(human.ContractHome(d.Path))).
			Str("default_workspace", d.DefaultWorkspace).
			Int("settings", len(d.Settings)).
			Msg(clioutput.ActionLabel(command))
	}
	if len(d.Settings) == 0 {
		return nil
	}
	rows := make([]clioutput.ConfigEntry, 0, len(d.Settings))
	for _, setting := range d.Settings {
		rows = append(rows, clioutput.ConfigEntry{
			Key:         setting.Key,
			Value:       setting.Value,
			Description: setting.Description,
		})
	}
	return c.WriteConfigEntriesTable(rows)
}

// GetData is the result returned by `slick config get`.
type GetData struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

var _ clioutput.PlainRenderer = GetData{}

func (d GetData) WritePlain(c *clioutput.CommandContext, command string, _ *clioutput.Pagination) error {
	clioutput.ApplyConfigValueStyle(c.StdoutLogger(), c.Theme, "value", d.Value)
	value := d.Value
	if value == "" {
		value = "(unset)"
	}
	c.ResultEvent(command).
		Str("key", d.Key).
		Str("value", value).
		Msg(clioutput.ActionLabel(command))
	return nil
}

// MutationData is the result returned by `slick config set`/`unset`.
type MutationData struct {
	Path  string `json:"path"`
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
}

var _ clioutput.PlainRenderer = MutationData{}

func (d MutationData) WritePlain(c *clioutput.CommandContext, command string, _ *clioutput.Pagination) error {
	// Style the value field for the set case; omit the field entirely when
	// unsetting since "value=" reads as noise when there is no value to
	// display. (The set/unset distinction is also encoded in the command
	// label and the JSON envelope's meta.command.)
	clioutput.ApplyConfigValueStyle(c.StdoutLogger(), c.Theme, "value", d.Value)
	event := c.ResultEvent(command).
		Link("path", d.Path, clioutput.HyperlinkText(human.ContractHome(d.Path))).
		Str("key", d.Key)
	if d.Value != "" {
		event = event.Str("value", d.Value)
	}
	event.Msg(clioutput.ActionLabel(command))
	return nil
}

// Keys lists all configurable preference keys (workspace placeholder uses
// `<profile>` as a templating sentinel).
var Keys = []string{
	"default_workspace",
	"workspaces.<profile>.default_channel",
	"workspaces.<profile>.attribution.enabled",
	"workspaces.<profile>.attribution.label",
	"workspaces.<profile>.attribution.emoji",
	"workspaces.<profile>.attribution.message",
}

// Descriptions returns a one-line description for each configurable key.
var Descriptions = map[string]string{
	"default_workspace":                        "Default workspace profile name",
	"workspaces.<profile>.default_channel":     "Fallback message channel ID or alias",
	"workspaces.<profile>.attribution.enabled": "Add visible attribution by default",
	"workspaces.<profile>.attribution.label":   "Attribution label override",
	"workspaces.<profile>.attribution.emoji":   "Attribution emoji override",
	"workspaces.<profile>.attribution.message": "Attribution message override",
}

// NewCommand returns the `slick config` parent command.
func NewCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "config",
		Short:        "Manage slack configuration",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newInitCommand(runtime))
	cmd.AddCommand(newPathCommand(runtime))
	cmd.AddCommand(newListCommand(runtime))
	cmd.AddCommand(newGetCommand(runtime))
	cmd.AddCommand(newSetCommand(runtime))
	cmd.AddCommand(newUnsetCommand(runtime))
	return cmd
}

func newInitCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	opts := InitOptions{
		Profile:          "default",
		AgentAttribution: true,
	}
	cmd := &cobra.Command{
		Use:          "init",
		Short:        "First-run config setup",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInit(cmd, runtime, opts)
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
	extendInitFlags(cmd)
	return cmd
}

func runInit(cmd *cobra.Command, runtime *cliruntime.RootRuntime, opts InitOptions) error {
	ctx := cliruntime.LocalContext(cmd, runtime, "config")
	path := runtime.ConfigPath
	if strings.TrimSpace(path) == "" {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("config path is unavailable"))
	}
	exists, err := xfs.Exists(path)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
	}
	if exists && !opts.Force {
		if !runtime.IsTTY {
			return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("config already exists; rerun with --force to overwrite"))
		}
		overwrite, err := confirmOverwrite(runtime, path)
		if err != nil {
			return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
		}
		if !overwrite {
			return ctx.WriteResult("config.init", InitData{Path: path, Profile: opts.Profile, Workspace: opts.Profile, Written: false})
		}
	}

	if runtime.IsTTY && shouldPromptInit(cmd) {
		if err := runInitForm(runtime, &opts); err != nil {
			return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
		}
	}
	profile, err := profileFromInitOptions(opts)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
	}
	cfg := &config.Config{
		SchemaVersion:    config.SchemaVersion,
		DefaultWorkspace: profile.Name,
		Workspaces: map[string]config.WorkspaceProfile{
			profile.Name: profile,
		},
	}
	if err := config.SaveFile(path, cfg); err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
	}
	runtime.Config = cfg
	return ctx.WriteResult("config.init", InitData{Path: path, Profile: profile.Name, Workspace: profile.Name, Written: true})
}

func shouldPromptInit(cmd *cobra.Command) bool {
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

func confirmOverwrite(runtime *cliruntime.RootRuntime, path string) (bool, error) {
	overwrite := false
	confirm := huh.NewConfirm().
		Title("Overwrite existing config?").
		Description(human.ContractHome(path)).
		Affirmative("Overwrite").
		Negative("Keep existing").
		Value(&overwrite)
	if err := runForm(runtime, huh.NewForm(huh.NewGroup(confirm))); err != nil {
		return false, err
	}
	return overwrite, nil
}

func runInitForm(runtime *cliruntime.RootRuntime, opts *InitOptions) error {
	help := initFieldHelp()
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Profile name").
				Description(help["profile"]).
				Placeholder("default").
				Value(&opts.Profile).
				Validate(clioauth.RequiredField("profile name")),
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
	return runForm(runtime, form)
}

func runForm(runtime *cliruntime.RootRuntime, form *huh.Form) error {
	form = form.
		WithTheme(clitheme.LoginHuhTheme(clibtheme.Default())).
		WithInput(runtime.Stdin).
		WithOutput(runtime.Stderr)
	if !clioauth.UsesTerminalFiles(runtime) {
		form.WithAccessible(true)
	}
	return form.Run()
}

func initFieldHelp() map[string]string {
	return map[string]string{
		"profile":           "Local Slack CLI profile. Select it later with --workspace; use default if you only need one profile.",
		"default_channel":   "Optional fallback destination for slick message send when no --channel or --user target is provided. Use a channel ID or configured alias.",
		"agent_attribution": "Default on. Sent messages include a visible Slack context block; detected agent and CI runs add agent-mode wording.",
		"agent_emoji":       "Emoji shown in the attribution context block.",
		"agent_message":     "Text shown in the attribution context block.",
	}
}

func profileFromInitOptions(opts InitOptions) (config.WorkspaceProfile, error) {
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

func newPathCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	return &cobra.Command{
		Use:          "path",
		Short:        "Print the config file path",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cliruntime.LocalContext(cmd, runtime, "config")
			exists, err := xfs.Exists(runtime.ConfigPath)
			return ctx.WriteResult("config.path", PathData{Path: runtime.ConfigPath, Exists: err == nil && exists})
		},
	}
}

func newListCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	return &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "Show current settings",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cliruntime.LocalContext(cmd, runtime, "config")
			cfg, err := loadConfig(runtime)
			if err != nil {
				return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
			}
			return ctx.WriteResult("config.list", ListData{
				Path:             runtime.ConfigPath,
				DefaultWorkspace: cfg.DefaultWorkspace,
				Settings:         entries(cfg),
			})
		},
	}
}

func newGetCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	return &cobra.Command{
		Use:          "get <key>",
		Short:        "Show a configuration value",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cliruntime.LocalContext(cmd, runtime, "config")
			cfg, err := loadConfig(runtime)
			if err != nil {
				return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
			}
			value, err := getValue(cfg, args[0])
			if err != nil {
				return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
			}
			return ctx.WriteResult("config.get", GetData{Key: args[0], Value: value})
		},
	}
}

func newSetCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	return &cobra.Command{
		Use:          "set <key> <value>",
		Short:        "Add or update a setting",
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cliruntime.LocalContext(cmd, runtime, "config")
			cfg, err := loadConfig(runtime)
			if err != nil {
				return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
			}
			if err := setValue(cfg, args[0], args[1]); err != nil {
				return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
			}
			if err := config.SaveFile(runtime.ConfigPath, cfg); err != nil {
				return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
			}
			runtime.Config = cfg
			return ctx.WriteResult("config.set", MutationData{Path: runtime.ConfigPath, Key: args[0], Value: args[1]})
		},
	}
}

func newUnsetCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	return &cobra.Command{
		Use:          "unset <key>",
		Aliases:      []string{"rm", "remove"},
		Short:        "Remove a setting",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cliruntime.LocalContext(cmd, runtime, "config")
			cfg, err := loadConfig(runtime)
			if err != nil {
				return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
			}
			if err := unsetValue(cfg, args[0]); err != nil {
				return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
			}
			if err := config.SaveFile(runtime.ConfigPath, cfg); err != nil {
				return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
			}
			runtime.Config = cfg
			return ctx.WriteResult("config.unset", MutationData{Path: runtime.ConfigPath, Key: args[0]})
		},
	}
}

func loadConfig(runtime *cliruntime.RootRuntime) (*config.Config, error) {
	if runtime.Config != nil {
		return runtime.Config, nil
	}
	if runtime.ConfigPath == "" {
		return nil, errors.New("config path is unavailable")
	}
	return cliruntime.LoadDefaultConfig(runtime.ConfigPath)
}

func entries(cfg *config.Config) []Entry {
	var out []Entry
	out = append(out, Entry{
		Key:         "default_workspace",
		Value:       cfg.DefaultWorkspace,
		Description: Descriptions["default_workspace"],
	})
	for _, name := range sortedWorkspaceNames(cfg) {
		workspace := cfg.Workspaces[name]
		prefix := "workspaces." + name + "."
		out = append(out,
			Entry{Key: prefix + "default_channel", Value: workspace.DefaultChannel, Description: Descriptions["workspaces.<profile>.default_channel"]},
			Entry{Key: prefix + "attribution.enabled", Value: boolPtrString(attributionEnabled(workspace)), Description: Descriptions["workspaces.<profile>.attribution.enabled"]},
			Entry{Key: prefix + "attribution.label", Value: workspace.Attribution.Label, Description: Descriptions["workspaces.<profile>.attribution.label"]},
			Entry{Key: prefix + "attribution.emoji", Value: workspace.Attribution.Emoji, Description: Descriptions["workspaces.<profile>.attribution.emoji"]},
			Entry{Key: prefix + "attribution.message", Value: workspace.Attribution.Message, Description: Descriptions["workspaces.<profile>.attribution.message"]},
		)
	}
	return out
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

func getValue(cfg *config.Config, key string) (string, error) {
	if key == "default_workspace" {
		return cfg.DefaultWorkspace, nil
	}
	workspaceName, field, err := parseWorkspaceKey(key)
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
		return cliutil.FirstNonEmpty(workspace.Attribution.Label, workspace.AgentLabel), nil
	case "agent_emoji":
		return cliutil.FirstNonEmpty(workspace.Attribution.Emoji, workspace.AgentEmoji), nil
	case "agent_message":
		return cliutil.FirstNonEmpty(workspace.Attribution.Message, workspace.AgentMessage), nil
	case "attribution.enabled":
		return boolPtrString(attributionEnabled(workspace)), nil
	case "attribution.label":
		return workspace.Attribution.Label, nil
	case "attribution.emoji":
		return workspace.Attribution.Emoji, nil
	case "attribution.message":
		return workspace.Attribution.Message, nil
	default:
		if authOwnedField(field) {
			return "", authOwnedKey(field)
		}
		return "", unknownKey(key)
	}
}

func setValue(cfg *config.Config, key, value string) error {
	if key == "default_workspace" {
		if _, ok := cfg.Workspaces[value]; !ok {
			return fmt.Errorf("workspace %q not found", value)
		}
		cfg.DefaultWorkspace = value
		return nil
	}
	workspaceName, field, err := parseWorkspaceKey(key)
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
		if authOwnedField(field) {
			return authOwnedKey(field)
		}
		return unknownKey(key)
	}
	cfg.Workspaces[workspaceName] = workspace
	return cfg.Validate()
}

func unsetValue(cfg *config.Config, key string) error {
	if key == "default_workspace" {
		return errors.New("default_workspace cannot be unset")
	}
	workspaceName, field, err := parseWorkspaceKey(key)
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
		if authOwnedField(field) {
			return authOwnedKey(field)
		}
		return unknownKey(key)
	}
	cfg.Workspaces[workspaceName] = workspace
	return cfg.Validate()
}

func parseWorkspaceKey(key string) (string, string, error) {
	parts := strings.Split(key, ".")
	if len(parts) < 3 || parts[0] != "workspaces" {
		return "", "", unknownKey(key)
	}
	workspace := parts[1]
	field := strings.Join(parts[2:], ".")
	if workspace == "" || field == "" {
		return "", "", unknownKey(key)
	}
	return workspace, field, nil
}

func authOwnedField(field string) bool {
	switch field {
	case "team_id", "team_name", "token_type", "token", "token_ref":
		return true
	default:
		return false
	}
}

func authOwnedKey(field string) error {
	return fmt.Errorf("%s is auth-owned; auth settings are managed by slick auth", field)
}

func unknownKey(key string) error {
	return fmt.Errorf("unknown config key %s - run 'slick config list' to see all keys", key)
}

// KeyCompletions returns shell completions for `slick config get/set/unset`
// keys, expanding the workspace placeholder for the given config.
func KeyCompletions(cfg *config.Config) []string {
	if cfg == nil || len(cfg.Workspaces) == 0 {
		completions := make([]string, 0, len(Keys))
		for _, key := range Keys {
			completions = append(completions, key+"\t"+Descriptions[key])
		}
		return completions
	}
	completions := []string{"default_workspace\t" + Descriptions["default_workspace"]}
	for _, profile := range sortedWorkspaceNames(cfg) {
		prefix := "workspaces." + profile + "."
		completions = append(completions,
			prefix+"default_channel\t"+Descriptions["workspaces.<profile>.default_channel"],
			prefix+"attribution.enabled\t"+Descriptions["workspaces.<profile>.attribution.enabled"],
			prefix+"attribution.label\t"+Descriptions["workspaces.<profile>.attribution.label"],
			prefix+"attribution.emoji\t"+Descriptions["workspaces.<profile>.attribution.emoji"],
			prefix+"attribution.message\t"+Descriptions["workspaces.<profile>.attribution.message"],
		)
	}
	return completions
}

// ValueCompletions returns shell completions for the value of a config key.
// For workspace-scoped boolean and emoji keys, common candidates are returned;
// the channel completion is delegated to the calling completion handler.
func ValueCompletions(key string, cfg *config.Config) []string {
	switch {
	case key == "default_workspace":
		return WorkspaceNames(cfg)
	case strings.HasSuffix(key, ".attribution.enabled"):
		return []string{"true", "false"}
	case strings.HasSuffix(key, ".attribution.emoji"):
		return CommonEmojis()
	default:
		return nil
	}
}

// WorkspaceNames returns the sorted workspace profile names for a config.
func WorkspaceNames(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}
	names := make([]string, 0, len(cfg.Workspaces))
	for name := range cfg.Workspaces {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// CommonEmojis returns the suggested emoji name list used for shell completions
// of attribution and reaction emoji flags.
func CommonEmojis() []string {
	return []string{
		"thumbsup",
		"eyes",
		"white_check_mark",
		"rocket",
		"heart",
		"fire",
		"robot_face",
		"gear",
		"wrench",
		"clock1",
		"moose",
	}
}

func extendInitFlags(cmd *cobra.Command) {
	f := cmd.Flags()
	clib.Extend(f.Lookup("profile"), clib.FlagExtra{Group: "Profile", Placeholder: "NAME", Terse: "profile name"})
	clib.Extend(f.Lookup("default-channel"), clib.FlagExtra{Group: "Profile", Placeholder: "C7N2Q8L4P", Terse: "default channel"})
	clib.Extend(f.Lookup("attribution-enabled"), clib.FlagExtra{Group: "Attribution", Terse: "attribution"})
	clib.Extend(f.Lookup("attribution-label"), clib.FlagExtra{Group: "Attribution", Placeholder: "LABEL", Terse: "attribution label"})
	clib.Extend(f.Lookup("attribution-emoji"), clib.FlagExtra{Group: "Attribution", Placeholder: ":robot_face:", Terse: "attribution emoji"})
	clib.Extend(f.Lookup("attribution-message"), clib.FlagExtra{Group: "Attribution", Placeholder: "TEXT", Terse: "attribution message"})
	clib.Extend(f.Lookup("force"), clib.FlagExtra{Group: "Safety", Terse: "overwrite config"})
}
