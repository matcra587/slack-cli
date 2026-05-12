// Package completion wires shell completions for the `slick` cobra command
// tree, including dynamic Slack-resource handlers for channels, users, and
// configuration keys.
package completion

import (
	"context"
	"os"
	"sort"
	"strings"
	"time"

	cobracli "github.com/gechr/clib/cli/cobra"
	"github.com/gechr/clib/complete"
	"github.com/matcra587/slack-cli/internal/agenthelp"
	clicache "github.com/matcra587/slack-cli/internal/cli/cache"
	cliconfig "github.com/matcra587/slack-cli/internal/cli/config"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	slackclient "github.com/matcra587/slack-cli/internal/cli/slackclient"
	clitoken "github.com/matcra587/slack-cli/internal/cli/token"
	"github.com/matcra587/slack-cli/internal/config"
	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const completionTimeout = 5 * time.Second

// Setup registers the clib completion handler on the root command.
func Setup(root *cobra.Command, runtime *cliruntime.RootRuntime) {
	completion := cobracli.NewCompletion(root)
	root.RunE = func(cmd *cobra.Command, args []string) error {
		handled, err := completion.Handle(
			Generator(root),
			HandlerForRuntime(runtime),
			cobracli.WithArgs(args),
		)
		if err != nil || handled {
			return err
		}
		return cmd.Help()
	}
}

// AddCommand registers the `slick completion` subcommand on the root.
func AddCommand(root *cobra.Command) {
	root.AddCommand(cobracli.CompletionCommand(root, func() *complete.Generator {
		return Generator(root)
	}))
}

// Generator returns a clib completion generator for the supplied root command.
func Generator(root *cobra.Command) *complete.Generator {
	gen := complete.NewGenerator("slick", complete.WithOrder(complete.OrderKeep)).
		FromFlags(cobracli.FlagMeta(root))
	gen.Subs = cobracli.Subcommands(root)
	return gen
}

// HandlerForRuntime returns a dynamic-completion handler bound to the runtime.
func HandlerForRuntime(runtime *cliruntime.RootRuntime) complete.Handler {
	cfg := runtime.Config
	if runtime.ConfigLoadError != nil {
		cfg = nil
	}
	token := tokenFor(runtime, cfg)
	return Handler(token, cfg, runtime)
}

func tokenFor(runtime *cliruntime.RootRuntime, cfg *config.Config) string {
	if cfg == nil {
		if token, ok := clitoken.RuntimeEnvToken(""); ok {
			return token
		}
		return ""
	}
	profile, err := cfg.ResolveWorkspace("")
	if err != nil {
		return ""
	}
	if token, ok := clitoken.RuntimeEnvToken(profile.Name); ok {
		return token
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
	token, err := resolver.ResolveToken(context.Background(), profile)
	if err != nil {
		return ""
	}
	return token
}

// Handler returns a dynamic completion handler that uses the supplied token,
// config, and runtime to resolve channel, user, workspace, and config keys.
func Handler(token string, cfg *config.Config, runtime *cliruntime.RootRuntime) complete.Handler {
	profileName := profileNameFromConfig(cfg)
	return func(shell, kind string, args []string) {
		switch kind {
		case "workspace":
			for _, name := range cliconfig.WorkspaceNames(cfg) {
				printCompletion(shell, name, "")
			}
			return
		case "config_key":
			for _, candidate := range cliconfig.KeyCompletions(cfg) {
				value, desc, _ := strings.Cut(candidate, "\t")
				printCompletion(shell, value, desc)
			}
			return
		case "config_value":
			if len(args) == 0 {
				return
			}
			if strings.HasSuffix(args[0], ".default_channel") {
				if completeCachedChannels(shell, profileName) {
					return
				}
				if token == "" {
					return
				}
				completeSlackChannels(shell, completionClient(token, runtime))
				return
			}
			for _, value := range cliconfig.ValueCompletions(args[0], cfg) {
				printCompletion(shell, value, "")
			}
			return
		case "guide_workflow":
			for _, workflow := range agenthelp.WorkflowNames() {
				printCompletion(shell, workflow, "")
			}
			return
		case "cache_resource":
			for _, resource := range clicache.Resources {
				printCompletion(shell, resource, "")
			}
			return
		case "channel":
			if completeCachedChannels(shell, profileName) {
				return
			}
		case "user":
			if completeCachedUsers(shell, profileName) {
				return
			}
		}

		if token == "" {
			return
		}
		client := completionClient(token, runtime)
		ctx, cancel := context.WithTimeout(context.Background(), completionTimeout)
		defer cancel()

		switch kind {
		case "channel":
			completeSlackChannels(shell, client)
		case "user":
			completeSlackUsers(ctx, shell, client)
		}
	}
}

func profileNameFromConfig(cfg *config.Config) string {
	if cfg == nil {
		return "default"
	}
	profile, err := cfg.ResolveWorkspace("")
	if err != nil {
		return "default"
	}
	return profile.Name
}

func completeSlackChannels(shell string, client *slackgo.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), completionTimeout)
	defer cancel()
	channels, _, err := client.GetConversationsContext(ctx, &slackgo.GetConversationsParameters{
		Types: []string{"public_channel", "private_channel", "im", "mpim"},
		Limit: 100,
	})
	if err != nil {
		return
	}
	for _, channel := range channels {
		printCompletion(shell, channel.ID, channelDescription(channel))
	}
}

func completeSlackUsers(ctx context.Context, shell string, client *slackgo.Client) {
	users, err := client.GetUsersContext(ctx, slackgo.GetUsersOptionLimit(100))
	if err != nil {
		return
	}
	for _, user := range users {
		if user.Deleted {
			continue
		}
		printCompletion(shell, user.ID, user.Name)
	}
}

func completeCachedUsers(shell, profile string) bool {
	users, ok := clicache.LoadCachedUsers(profile)
	if !ok {
		return false
	}
	printed := false
	for _, user := range users {
		if !cachedUserActive(user) {
			continue
		}
		printCompletion(shell, user.ID, user.Name)
		printed = true
	}
	return printed
}

func completeCachedChannels(shell, profile string) bool {
	channels, ok := clicache.LoadCachedChannels(profile)
	if !ok {
		return false
	}
	for _, channel := range channels {
		printCompletion(shell, channel.ID, cachedChannelDescription(channel))
	}
	return true
}

func cachedUserActive(user clioutput.User) bool {
	return user.Deleted == nil || !*user.Deleted
}

func completionClient(token string, runtime *cliruntime.RootRuntime) *slackgo.Client {
	return slackclient.New(context.Background(), nil, runtime, token)
}

// WorkspaceNames returns the sorted workspace profile names for the supplied
// configuration. Exported for use by tests and external callers (e.g. main.go's
// schema documentation).
func WorkspaceNames(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}
	names := make([]string, 0, len(cfg.Workspaces))
	for name := range cfg.Workspaces {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func channelDescription(channel slackgo.Channel) string {
	if channel.Name != "" {
		return channel.Name
	}
	if channel.User != "" {
		return channel.User
	}
	return ""
}

func cachedChannelDescription(channel clioutput.Channel) string {
	if channel.Name != "" {
		return channel.Name
	}
	if channel.User != nil {
		return *channel.User
	}
	return ""
}

func printCompletion(shell, value, desc string) {
	if value == "" {
		return
	}
	line := value
	if shell == "fish" && desc != "" {
		line += "\t" + desc
	}
	_, _ = os.Stdout.WriteString(line + "\n")
}

// ExtendSlackMetadata attaches clib completion metadata to the per-command
// flags after all subcommands have been added to the root.
func ExtendSlackMetadata(root *cobra.Command) {
	extendRootFlags(root)
	extendMessageFlags(root)
	extendHistoryFlags(root)
	extendReplyFlags(root)
	extendReactFlags(root)
	extendLookupFlags(root)
	extendFileFlags(root)
	extendStatusFlags(root)
	extendCacheMetadata(root)
	extendManifestFlags(root)
	extendConfigArgs(root)
	extendAgentArgs(root)
	extendAuthMetadata(root)
}

func extendRootFlags(root *cobra.Command) {
	pf := root.PersistentFlags()
	cobracli.Extend(pf.Lookup("workspace"), cobracli.FlagExtra{Complete: "predictor=workspace", Placeholder: "PROFILE", Terse: "workspace"})
	cobracli.Extend(pf.Lookup("output"), cobracli.FlagExtra{
		Group:       "Output",
		Placeholder: "MODE",
		Enum:        clioutput.ValidOutputModes(),
		EnumTerse: []string{
			"auto-detect (human on TTY, JSON otherwise)",
			"human-readable clog event",
			"JSON envelope (meta + data + errors)",
			"JSON data only (no envelope)",
		},
		EnumDefault: clioutput.OutputAuto,
		Terse:       "output format",
	})
	cobracli.Extend(pf.Lookup("color"), cobracli.FlagExtra{
		Group:       "Output",
		Placeholder: "MODE",
		Enum:        []string{"auto", "always", "never"},
		EnumTerse: []string{
			"color when stdout is a TTY",
			"force color on (ANSI escapes always emitted)",
			"never emit color escapes",
		},
		EnumDefault: "auto",
		Terse:       "color mode",
	})
	cobracli.Extend(pf.Lookup("agent"), cobracli.FlagExtra{Group: "Agent", Terse: "agent mode"})
	cobracli.Extend(pf.Lookup("no-agent-attribution"), cobracli.FlagExtra{Group: "Agent", Terse: "disable attribution"})
	cobracli.Extend(pf.Lookup("agent-label"), cobracli.FlagExtra{Group: "Agent", Placeholder: "LABEL", Terse: "agent label"})
	cobracli.Extend(pf.Lookup("agent-emoji"), cobracli.FlagExtra{Group: "Agent", Placeholder: ":robot_face:", Complete: "values=" + strings.Join(cliconfig.CommonEmojis(), " "), Terse: "agent emoji"})
	cobracli.Extend(pf.Lookup("agent-message"), cobracli.FlagExtra{Group: "Agent", Placeholder: "TEXT", Terse: "agent message"})
	cobracli.Extend(pf.Lookup("no-throttle"), cobracli.FlagExtra{Group: "Network", Terse: "disable throttle"})
}

func extendMessageFlags(root *cobra.Command) {
	send := commandPath(root, "message", "send")
	extendSlackMessageInputFlags(send)
	extendSlackTargetFlags(send)
	extendFileHint(send, "file")
	extendFileNameHint(send)
	extendDryRunFlag(send)

	edit := commandPath(root, "message", "edit")
	extendSlackTargetFlags(edit)
	extendTimestampFlag(edit, "timestamp")
	extendSlackMessageInputFlags(edit)
	extendFileHint(edit, "file")
	extendDryRunFlag(edit)

	deleteCmd := commandPath(root, "message", "delete")
	extendSlackTargetFlags(deleteCmd)
	extendTimestampFlag(deleteCmd, "timestamp")
	extendDryRunFlag(deleteCmd)
	extendForceFlag(deleteCmd)
}

func extendHistoryFlags(root *cobra.Command) {
	cmd := commandPath(root, "history", "list")
	extendChannelFlag(cmd, "channel")
	extendUserFlag(cmd, "user")
	extendTimestampFlag(cmd, "since")
	extendTimestampFlag(cmd, "until")
	extendTimestampFlag(cmd, "thread")
	extendMaxItemsFlag(cmd)
	extendCursorFlag(cmd)
	cobracli.Extend(flag(cmd, "reply-limit"), cobracli.FlagExtra{Placeholder: "N", Terse: "reply limit"})
}

func extendReplyFlags(root *cobra.Command) {
	cmd := commandPath(root, "reply")
	extendChannelFlag(cmd, "channel")
	extendTimestampFlag(cmd, "parent")
	extendSlackMessageInputFlags(cmd)
	extendFileHint(cmd, "file")
	extendDryRunFlag(cmd)
}

func extendReactFlags(root *cobra.Command) {
	for _, action := range []string{"add", "remove"} {
		cmd := commandPath(root, "react", action)
		extendChannelFlag(cmd, "channel")
		extendTimestampFlag(cmd, "timestamp")
		extendEmojiFlag(cmd)
		extendDryRunFlag(cmd)
	}
	list := commandPath(root, "react", "list")
	extendChannelFlag(list, "channel")
	extendTimestampFlag(list, "timestamp")
}

func extendLookupFlags(root *cobra.Command) {
	channel := commandPath(root, "lookup", "channel")
	extendChannelFlag(channel, "channel")
	extendMaxItemsFlag(channel)
	extendCursorFlag(channel)
	cobracli.Extend(flag(channel, "filter"), cobracli.FlagExtra{Placeholder: "TEXT", Terse: "filter"})
	cobracli.Extend(flag(channel, "types"), cobracli.FlagExtra{
		Complete:    "values=public_channel private_channel im mpim dm all,comma",
		Enum:        []string{"public_channel", "private_channel", "im", "mpim", "dm", "all"},
		EnumDefault: "public_channel,private_channel",
		Placeholder: "TYPE",
		Terse:       "conversation types",
	})

	user := commandPath(root, "lookup", "user")
	extendUserFlag(user, "user")
	extendMaxItemsFlag(user)
	extendCursorFlag(user)
	cobracli.Extend(flag(user, "filter"), cobracli.FlagExtra{Placeholder: "TEXT", Terse: "filter"})
	cobracli.Extend(flag(user, "include-deleted"), cobracli.FlagExtra{Terse: "include deleted users"})

	messages := commandPath(root, "lookup", "messages")
	cobracli.Extend(flag(messages, "query"), cobracli.FlagExtra{Placeholder: "QUERY", Terse: "search query"})
	extendMaxItemsFlag(messages)
	extendCursorFlag(messages)
}

func extendFileFlags(root *cobra.Command) {
	cmd := commandPath(root, "file", "upload")
	extendChannelFlag(cmd, "channel")
	extendFileHint(cmd, "file")
	extendFileNameHint(cmd)
	cobracli.Extend(flag(cmd, "title"), cobracli.FlagExtra{Placeholder: "TITLE", Terse: "file title"})
	cobracli.Extend(flag(cmd, "message"), cobracli.FlagExtra{Placeholder: "TEXT", Terse: "message"})
	extendTimestampFlag(cmd, "thread")
	extendDryRunFlag(cmd)
}

func extendStatusFlags(root *cobra.Command) {
	set := commandPath(root, "status", "set")
	cobracli.Extend(flag(set, "text"), cobracli.FlagExtra{Placeholder: "TEXT", Terse: "status text"})
	extendEmojiFlag(set)
	cobracli.Extend(flag(set, "expires-in"), cobracli.FlagExtra{Placeholder: "DURATION", Terse: "expires in"})
	cobracli.Extend(flag(set, "until"), cobracli.FlagExtra{Placeholder: "TIME", Terse: "until"})
	extendDryRunFlag(set)

	clear := commandPath(root, "status", "clear")
	extendDryRunFlag(clear)
}

func extendManifestFlags(root *cobra.Command) {
	cmd := commandPath(root, "manifest", "template")
	cobracli.Extend(flag(cmd, "name"), cobracli.FlagExtra{Placeholder: "name", Terse: "app name"})
	cobracli.Extend(flag(cmd, "description"), cobracli.FlagExtra{Placeholder: "text", Terse: "description"})
	cobracli.Extend(flag(cmd, "long-description"), cobracli.FlagExtra{Placeholder: "text", Terse: "long description"})
	cobracli.Extend(flag(cmd, "preset"), cobracli.FlagExtra{
		Enum:        []string{"readonly", "messaging", "files", "search", "full"},
		EnumDefault: "messaging",
		Placeholder: "preset",
		Terse:       "scope preset",
	})
	cobracli.Extend(flag(cmd, "type"), cobracli.FlagExtra{
		Enum:        []string{"user", "bot", "both"},
		EnumDefault: "user",
		Placeholder: "type",
		Terse:       "auth type",
	})
	cobracli.Extend(flag(cmd, "format"), cobracli.FlagExtra{
		Enum:        []string{"json", "yaml"},
		EnumDefault: "json",
		Placeholder: "format",
		Terse:       "format",
	})
	cobracli.Extend(flag(cmd, "redirect-url"), cobracli.FlagExtra{Hint: complete.HintURL, Placeholder: "url", Terse: "redirect URL"})
	cobracli.Extend(flag(cmd, "callback-port"), cobracli.FlagExtra{Placeholder: "port", Terse: "callback port"})
	cobracli.Extend(flag(cmd, "background-color"), cobracli.FlagExtra{Placeholder: "#RRGGBB", Terse: "background"})
}

func extendConfigArgs(root *cobra.Command) {
	for _, path := range [][]string{{"config", "get"}, {"config", "unset"}} {
		if cmd := commandPath(root, path...); cmd != nil {
			cmd.Annotations = mergeClibAnnotation(cmd.Annotations, "dynamic-args='config_key'")
		}
	}
	if cmd := commandPath(root, "config", "set"); cmd != nil {
		cmd.Annotations = mergeClibAnnotation(cmd.Annotations, "dynamic-args='config_key, config_value'")
	}
}

func extendAgentArgs(root *cobra.Command) {
	if cmd := commandPath(root, "agent", "guide"); cmd != nil {
		cmd.Annotations = mergeClibAnnotation(cmd.Annotations, "dynamic-args='guide_workflow'")
	}
}

func extendCacheMetadata(root *cobra.Command) {
	for _, path := range [][]string{{"cache", "users"}, {"cache", "channels"}} {
		cmd := commandPath(root, path...)
		cobracli.Extend(flag(cmd, "refresh"), cobracli.FlagExtra{Terse: "refresh"})
		cobracli.Extend(flag(cmd, "ttl-minutes"), cobracli.FlagExtra{Placeholder: "MINUTES", Terse: "TTL"})
		cobracli.Extend(flag(cmd, "page-size"), cobracli.FlagExtra{Placeholder: "N", Terse: "page size"})
		cobracli.Extend(flag(cmd, "max-pages"), cobracli.FlagExtra{Placeholder: "N", Terse: "max pages"})
	}
	if cmd := commandPath(root, "cache", "clear"); cmd != nil {
		cmd.Annotations = mergeClibAnnotation(cmd.Annotations, "dynamic-args='cache_resource'")
	}
}

func extendAuthMetadata(root *cobra.Command) {
	login := commandPath(root, "auth", "login")
	cobracli.Extend(flag(login, "workspace-name"), cobracli.FlagExtra{Complete: "predictor=workspace", Placeholder: "PROFILE", Terse: "profile"})
	cobracli.Extend(flag(login, "token-file"), cobracli.FlagExtra{Hint: complete.HintFile, Placeholder: "FILE", Terse: "token file"})
	cobracli.Extend(flag(login, "token-env"), cobracli.FlagExtra{Placeholder: "VAR", Terse: "token env"})
	cobracli.Extend(flag(login, "team-id"), cobracli.FlagExtra{Placeholder: "T1234567890", Terse: "workspace ID"})
	cobracli.Extend(flag(login, "team-name"), cobracli.FlagExtra{Placeholder: "NAME", Terse: "workspace name"})
	methodExtra := cobracli.FlagExtra{Enum: []string{"oauth", "token"}, EnumDefault: "token", Placeholder: "METHOD", Terse: "auth method"}
	cobracli.Extend(flag(login, "method"), methodExtra)
	cobracli.Extend(flag(login, "oauth-client-id"), cobracli.FlagExtra{Placeholder: "CLIENT_ID", Terse: "client ID"})
	cobracli.Extend(flag(login, "oauth-redirect-url"), cobracli.FlagExtra{Hint: complete.HintURL, Placeholder: "URL", Terse: "redirect URL"})
	cobracli.Extend(flag(login, "oauth-callback-port"), cobracli.FlagExtra{Placeholder: "PORT", Terse: "callback port"})
	extendForceFlag(login)

	for _, path := range [][]string{{"auth", "switch"}, {"auth", "logout"}} {
		if cmd := commandPath(root, path...); cmd != nil {
			cmd.Annotations = mergeClibAnnotation(cmd.Annotations, "dynamic-args='workspace'")
		}
	}
}

func extendSlackTargetFlags(cmd *cobra.Command) {
	extendChannelFlag(cmd, "channel")
	extendUserFlag(cmd, "user")
}

func extendSlackMessageInputFlags(cmd *cobra.Command) {
	cobracli.Extend(flag(cmd, "message"), cobracli.FlagExtra{Placeholder: "TEXT", Terse: "message"})
}

func extendChannelFlag(cmd *cobra.Command, name string) {
	cobracli.Extend(flag(cmd, name), cobracli.FlagExtra{Complete: "predictor=channel", Placeholder: "CHANNEL", Terse: "channel"})
}

func extendUserFlag(cmd *cobra.Command, name string) {
	cobracli.Extend(flag(cmd, name), cobracli.FlagExtra{Complete: "predictor=user", Placeholder: "USER", Terse: "user"})
}

func extendTimestampFlag(cmd *cobra.Command, name string) {
	cobracli.Extend(flag(cmd, name), cobracli.FlagExtra{Placeholder: "TS", Terse: name})
}

func extendFileHint(cmd *cobra.Command, name string) {
	cobracli.Extend(flag(cmd, name), cobracli.FlagExtra{Hint: complete.HintFile, Placeholder: "FILE", Terse: "file"})
}

func extendFileNameHint(cmd *cobra.Command) {
	cobracli.Extend(flag(cmd, "filename"), cobracli.FlagExtra{Placeholder: "NAME", Terse: "filename"})
}

func extendDryRunFlag(cmd *cobra.Command) {
	cobracli.Extend(flag(cmd, "dry-run"), cobracli.FlagExtra{Group: "Safety", Terse: "dry run"})
}

func extendForceFlag(cmd *cobra.Command) {
	cobracli.Extend(flag(cmd, "force"), cobracli.FlagExtra{Group: "Safety", Terse: "force"})
}

func extendMaxItemsFlag(cmd *cobra.Command) {
	cobracli.Extend(flag(cmd, "max-items"), cobracli.FlagExtra{Placeholder: "N", Terse: "max items"})
}

func extendCursorFlag(cmd *cobra.Command) {
	cobracli.Extend(flag(cmd, "cursor"), cobracli.FlagExtra{Placeholder: "CURSOR", Terse: "cursor"})
}

func extendEmojiFlag(cmd *cobra.Command) {
	cobracli.Extend(flag(cmd, "emoji"), cobracli.FlagExtra{
		Complete:    "values=" + strings.Join(cliconfig.CommonEmojis(), " "),
		Placeholder: ":thumbsup:",
		Terse:       "emoji",
	})
}

func commandPath(root *cobra.Command, path ...string) *cobra.Command {
	current := root
	for _, name := range path {
		current = directChild(current, name)
		if current == nil {
			return nil
		}
	}
	return current
}

func directChild(cmd *cobra.Command, name string) *cobra.Command {
	if cmd == nil {
		return nil
	}
	for _, child := range cmd.Commands() {
		if child.Name() == name {
			return child
		}
	}
	return nil
}

func flag(cmd *cobra.Command, name string) *pflag.Flag {
	if cmd == nil {
		return nil
	}
	return cmd.Flags().Lookup(name)
}

func mergeClibAnnotation(annotations map[string]string, value string) map[string]string {
	if annotations == nil {
		annotations = map[string]string{}
	}
	if existing := strings.TrimSpace(annotations["clib"]); existing != "" {
		annotations["clib"] = existing + "," + value
	} else {
		annotations["clib"] = value
	}
	return annotations
}
