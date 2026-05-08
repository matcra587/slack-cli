package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	cobracli "github.com/gechr/clib/cli/cobra"
	"github.com/gechr/clib/complete"
	"github.com/matcra587/slack-cli/internal/agenthelp"
	slackcache "github.com/matcra587/slack-cli/internal/cache"
	"github.com/matcra587/slack-cli/internal/config"
	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const completionTimeout = 5 * time.Second

func setupClibCompletion(root *cobra.Command, runtime *RootRuntime) {
	completion := cobracli.NewCompletion(root)
	root.RunE = func(cmd *cobra.Command, args []string) error {
		handled, err := completion.Handle(
			slackCompletionGenerator(root),
			slackCompletionHandlerForRuntime(runtime),
			cobracli.WithArgs(args),
		)
		if err != nil || handled {
			return err
		}
		return cmd.Help()
	}
}

func addClibCompletionCommand(root *cobra.Command) {
	root.AddCommand(cobracli.CompletionCommand(root, func() *complete.Generator {
		return slackCompletionGenerator(root)
	}))
}

func slackCompletionGenerator(root *cobra.Command) *complete.Generator {
	gen := complete.NewGenerator("slick", complete.WithOrder(complete.OrderKeep)).
		FromFlags(cobracli.FlagMeta(root))
	gen.Subs = cobracli.Subcommands(root)
	return gen
}

func slackCompletionHandlerForRuntime(runtime *RootRuntime) complete.Handler {
	cfg := runtime.Config
	if runtime.ConfigLoadError != nil {
		cfg = nil
	}
	token := slackCompletionToken(runtime, cfg)
	return slackCompletionHandler(token, cfg, runtime)
}

func slackCompletionToken(runtime *RootRuntime, cfg *config.Config) string {
	if cfg == nil {
		if token, ok := runtimeEnvToken(""); ok {
			return token
		}
		return ""
	}
	profile, err := cfg.ResolveWorkspace("")
	if err != nil {
		return ""
	}
	if token, ok := runtimeEnvToken(profile.Name); ok {
		return token
	}
	resolver := runtime.TokenResolver
	if resolver == nil {
		resolver = CredentialTokenResolver{
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

func slackCompletionHandler(token string, cfg *config.Config, runtime *RootRuntime) complete.Handler {
	profileName := slackCompletionProfileName(cfg)
	return func(shell, kind string, args []string) {
		switch kind {
		case "workspace":
			for _, name := range completionWorkspaceNames(cfg) {
				printCompletion(shell, name, "")
			}
			return
		case "config_key":
			for _, candidate := range slackConfigKeyCompletions(cfg) {
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
				completeSlackChannels(shell, slackCompletionClient(token, runtime))
				return
			}
			for _, value := range slackConfigValueCompletions(args[0], cfg) {
				printCompletion(shell, value, "")
			}
			return
		case "guide_workflow":
			for _, workflow := range agenthelp.WorkflowNames() {
				printCompletion(shell, workflow, "")
			}
			return
		case "cache_resource":
			for _, resource := range cacheResources {
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
		client := slackCompletionClient(token, runtime)
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

func slackCompletionProfileName(cfg *config.Config) string {
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
		printCompletion(shell, channel.ID, completionChannelDescription(channel))
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
	var payload cachedUsersPayload
	if !readCompletionCache(profile, "users", &payload) {
		return false
	}
	printed := false
	for _, user := range payload.Users {
		if !completionUserActive(user) {
			continue
		}
		printCompletion(shell, user.ID, user.Name)
		printed = true
	}
	return printed
}

func completeCachedChannels(shell, profile string) bool {
	var payload cachedChannelsPayload
	if !readCompletionCache(profile, "channels", &payload) {
		return false
	}
	for _, channel := range payload.Channels {
		printCompletion(shell, channel.ID, completionChannelDescriptionFromCLI(channel))
	}
	return true
}

func readCompletionCache(profile, resource string, target any) bool {
	entry, ok, stale, err := slackcache.Read(profile, resource, time.Duration(slackMetadataCacheTTLMinutes)*time.Minute)
	if err != nil || !ok || stale {
		return false
	}
	return json.Unmarshal(entry.Data, target) == nil
}

func completionUserActive(user cliUser) bool {
	return user.Deleted == nil || !*user.Deleted
}

func slackCompletionClient(token string, runtime *RootRuntime) *slackgo.Client {
	return newSlackClient(context.Background(), nil, runtime, token)
}

func completionWorkspaceNames(cfg *config.Config) []string {
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

func completionChannelDescription(channel slackgo.Channel) string {
	if channel.Name != "" {
		return channel.Name
	}
	if channel.User != "" {
		return channel.User
	}
	return ""
}

func completionChannelDescriptionFromCLI(channel cliChannel) string {
	if channel.Name != "" {
		return channel.Name
	}
	return ptrString(channel.User)
}

func printCompletion(shell, value, desc string) {
	if value == "" {
		return
	}
	line := value
	if shell == "fish" && desc != "" {
		line += "\t" + desc
	}
	_, _ = io.WriteString(os.Stdout, line+"\n")
}

func extendSlackCompletionMetadata(root *cobra.Command) {
	extendRootCompletionFlags(root)
	extendMessageCompletionFlags(root)
	extendHistoryCompletionFlags(root)
	extendReplyCompletionFlags(root)
	extendReactCompletionFlags(root)
	extendLookupCompletionFlags(root)
	extendFileCompletionFlags(root)
	extendStatusCompletionFlags(root)
	extendCacheCompletionMetadata(root)
	extendManifestCompletionFlags(root)
	extendConfigCompletionArgs(root)
	extendAgentCompletionArgs(root)
	extendAuthCompletionMetadata(root)
	extendWorkspaceCompletionMetadata(root)
}

func extendRootCompletionFlags(root *cobra.Command) {
	pf := root.PersistentFlags()
	cobracli.Extend(pf.Lookup("workspace"), cobracli.FlagExtra{Complete: "predictor=workspace", Placeholder: "PROFILE", Terse: "workspace"})
	cobracli.Extend(pf.Lookup("json"), cobracli.FlagExtra{Group: "Output", Terse: "JSON output"})
	cobracli.Extend(pf.Lookup("plain"), cobracli.FlagExtra{Group: "Output", Terse: "plain output"})
	cobracli.Extend(pf.Lookup("compact"), cobracli.FlagExtra{Group: "Output", Terse: "compact JSON"})
	cobracli.Extend(pf.Lookup("raw"), cobracli.FlagExtra{Group: "Output", Terse: "raw output"})
	cobracli.Extend(pf.Lookup("agent"), cobracli.FlagExtra{Group: "Agent", Terse: "agent mode"})
	cobracli.Extend(pf.Lookup("no-agent-attribution"), cobracli.FlagExtra{Group: "Agent", Terse: "disable attribution"})
	cobracli.Extend(pf.Lookup("agent-label"), cobracli.FlagExtra{Group: "Agent", Placeholder: "LABEL", Terse: "agent label"})
	cobracli.Extend(pf.Lookup("agent-emoji"), cobracli.FlagExtra{Group: "Agent", Placeholder: ":robot_face:", Complete: "values=" + strings.Join(commonEmojiCompletions(), " "), Terse: "agent emoji"})
	cobracli.Extend(pf.Lookup("agent-message"), cobracli.FlagExtra{Group: "Agent", Placeholder: "TEXT", Terse: "agent message"})
	cobracli.Extend(pf.Lookup("no-throttle"), cobracli.FlagExtra{Group: "Network", Terse: "disable throttle"})
}

func extendMessageCompletionFlags(root *cobra.Command) {
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

func extendHistoryCompletionFlags(root *cobra.Command) {
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

func extendReplyCompletionFlags(root *cobra.Command) {
	cmd := commandPath(root, "reply")
	extendChannelFlag(cmd, "channel")
	extendTimestampFlag(cmd, "parent")
	extendSlackMessageInputFlags(cmd)
	extendFileHint(cmd, "file")
	extendDryRunFlag(cmd)
}

func extendReactCompletionFlags(root *cobra.Command) {
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

func extendLookupCompletionFlags(root *cobra.Command) {
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

func extendFileCompletionFlags(root *cobra.Command) {
	cmd := commandPath(root, "file", "upload")
	extendChannelFlag(cmd, "channel")
	extendFileHint(cmd, "file")
	extendFileNameHint(cmd)
	cobracli.Extend(flag(cmd, "title"), cobracli.FlagExtra{Placeholder: "TITLE", Terse: "file title"})
	cobracli.Extend(flag(cmd, "message"), cobracli.FlagExtra{Placeholder: "TEXT", Terse: "message"})
	extendTimestampFlag(cmd, "thread")
	extendDryRunFlag(cmd)
}

func extendStatusCompletionFlags(root *cobra.Command) {
	set := commandPath(root, "status", "set")
	cobracli.Extend(flag(set, "text"), cobracli.FlagExtra{Placeholder: "TEXT", Terse: "status text"})
	extendEmojiFlag(set)
	cobracli.Extend(flag(set, "expires-in"), cobracli.FlagExtra{Placeholder: "DURATION", Terse: "expires in"})
	cobracli.Extend(flag(set, "until"), cobracli.FlagExtra{Placeholder: "TIME", Terse: "until"})
	extendDryRunFlag(set)

	clear := commandPath(root, "status", "clear")
	extendDryRunFlag(clear)
}

func extendManifestCompletionFlags(root *cobra.Command) {
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

func extendConfigCompletionArgs(root *cobra.Command) {
	for _, path := range [][]string{{"config", "get"}, {"config", "unset"}} {
		if cmd := commandPath(root, path...); cmd != nil {
			cmd.Annotations = mergeClibAnnotation(cmd.Annotations, "dynamic-args='config_key'")
		}
	}
	if cmd := commandPath(root, "config", "set"); cmd != nil {
		cmd.Annotations = mergeClibAnnotation(cmd.Annotations, "dynamic-args='config_key, config_value'")
	}
}

func extendAgentCompletionArgs(root *cobra.Command) {
	if cmd := commandPath(root, "agent", "guide"); cmd != nil {
		cmd.Annotations = mergeClibAnnotation(cmd.Annotations, "dynamic-args='guide_workflow'")
	}
}

func extendCacheCompletionMetadata(root *cobra.Command) {
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

func extendAuthCompletionMetadata(root *cobra.Command) {
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

func extendWorkspaceCompletionMetadata(*cobra.Command) {}

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
		Complete:    "values=" + strings.Join(commonEmojiCompletions(), " "),
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

func commonEmojiCompletions() []string {
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
