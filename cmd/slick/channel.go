package main

import (
	"strings"

	xstrings "github.com/gechr/x/strings"
	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

type channelListData struct {
	Channels []cliChannel `json:"channels"`
}

var _ PlainRenderer = channelListData{}

func (d channelListData) WritePlain(c *CommandContext, command string, pagination *Pagination) error {
	return c.WriteChannels(command, d.Channels, pagination)
}

type channelInfoData struct {
	Channel cliChannel `json:"channel"`
}

var _ PlainRenderer = channelInfoData{}

func (d channelInfoData) WritePlain(c *CommandContext, command string, _ *Pagination) error {
	ch := d.Channel
	event := c.ResultEventWithStyles(command, entityFieldStyle("channel", ch.ID)).
		Str("channel", ch.ID).
		Str("name", ch.Name).
		Str("type", ch.Type)
	event = addBoolField(event, "is_member", ch.IsMember)
	event = addBoolField(event, "is_im", ch.IsIM)
	event = addBoolField(event, "is_archived", ch.IsArchived)
	if ch.User != nil {
		event = event.Str("user", *ch.User)
	}
	if ch.Topic != nil {
		event = event.Str("topic", *ch.Topic)
	}
	event = addIntField(event, "num_members", ch.NumMembers)
	event.Send()
	return nil
}

func newLookupCommand(runtime *RootRuntime) *cobra.Command {
	lookupCmd := &cobra.Command{
		Use:   "lookup",
		Short: "Look up Slack channels and users",
	}
	lookupCmd.AddCommand(newLookupChannelCommand(runtime))
	lookupCmd.AddCommand(newLookupUserCommand(runtime))
	lookupCmd.AddCommand(newLookupMessagesCommand(runtime))
	return lookupCmd
}

func newLookupChannelCommand(runtime *RootRuntime) *cobra.Command {
	var maxItems int
	var cursor string
	var filter string
	var types string
	channelCmd := &cobra.Command{
		Use:          "channel",
		Short:        "Look up Slack channels and conversations",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			channel, _ := cmd.Flags().GetString("channel")
			if strings.TrimSpace(channel) != "" {
				return runChannelInfoValue(cmd, runtime, "lookup.channel", channel)
			}
			return runChannelListWithTypes(cmd, runtime, "lookup.channel", maxItems, cursor, filter, parseConversationTypes(types))
		},
	}
	channelCmd.Flags().StringP("channel", "c", "", "Channel or conversation ID, name, or alias")
	channelCmd.Flags().IntVarP(&maxItems, "max-items", "M", 0, "Maximum conversations to return")
	channelCmd.Flags().StringVarP(&cursor, "cursor", "C", "", "Pagination cursor")
	channelCmd.Flags().StringVarP(&filter, "filter", "f", "", "Filter by ID or name")
	channelCmd.Flags().StringVarP(&types, "types", "t", "public_channel,private_channel", "Conversation types: public_channel, private_channel, im, mpim, dm, or all")
	return channelCmd
}

func runChannelListWithTypes(cmd *cobra.Command, runtime *RootRuntime, command string, maxItems int, cursor, filter string, types []string) error {
	ctx, profile, _, err := commandContext(cmd, runtime)
	if err != nil {
		return writeRuntimeError(runtime, validationCLIError(err.Error()))
	}
	client, err := slackClient(cmd, profile, runtime)
	if err != nil {
		return writeCommandError(ctx, authCLIError(err.Error()))
	}
	if err := requireSlackScopes(cmd.Context(), client, conversationReadScopeRequirement(types)); err != nil {
		return writeCommandError(ctx, cliErrorFromSlack(cmd.Context(), err))
	}

	channels, nextCursor, err := client.GetConversationsContext(cmd.Context(), &slackgo.GetConversationsParameters{
		Types:  types,
		Limit:  maxItems,
		Cursor: cursor,
	})
	if err != nil {
		return writeCommandError(ctx, cliErrorFromSlack(cmd.Context(), err))
	}
	result := filterChannels(cliChannelsFromSlack(channels), filter)
	return ctx.WriteResultWithPagination(command, channelListData(result), &Pagination{
		Cursor:        stringPtr(cursor),
		NextCursor:    stringPtr(nextCursor),
		HasMore:       nextCursor != "",
		MaxItems:      intPtr(maxItems),
		ItemsReturned: intPtr(len(result.Channels)),
	})
}

func runChannelInfoValue(cmd *cobra.Command, runtime *RootRuntime, command, channel string) error {
	ctx, profile, _, err := commandContext(cmd, runtime)
	if err != nil {
		return writeRuntimeError(runtime, validationCLIError(err.Error()))
	}
	if strings.TrimSpace(channel) == "" {
		return writeCommandError(ctx, validationCLIError("channel is required"))
	}
	channel = resolveAlias(profile, strings.TrimSpace(channel))
	client, err := slackClient(cmd, profile, runtime)
	if err != nil {
		return writeCommandError(ctx, authCLIError(err.Error()))
	}
	if err := requireSlackScopes(cmd.Context(), client, anyScope("channels:read", "groups:read", "im:read", "mpim:read")); err != nil {
		return writeCommandError(ctx, cliErrorFromSlack(cmd.Context(), err))
	}
	result, err := client.GetConversationInfoContext(cmd.Context(), &slackgo.GetConversationInfoInput{
		ChannelID:         channel,
		IncludeNumMembers: true,
	})
	if err != nil {
		return writeCommandError(ctx, cliErrorFromSlack(cmd.Context(), err))
	}
	return ctx.WriteResult(command, channelInfoData{Channel: cliChannelFromSlack(*result)})
}

type channelListResult struct {
	Channels []cliChannel
}

func cliChannelsFromSlack(channels []slackgo.Channel) channelListResult {
	out := make([]cliChannel, 0, len(channels))
	for i := range channels {
		channel := channels[i]
		out = append(out, cliChannelFromSlack(channel))
	}
	return channelListResult{Channels: out}
}

func filterChannels(result channelListResult, filter string) channelListResult {
	if filter == "" {
		return result
	}
	filter = strings.ToLower(filter)
	out := make([]cliChannel, 0, len(result.Channels))
	for _, channel := range result.Channels {
		if strings.Contains(strings.ToLower(channel.ID), filter) || strings.Contains(strings.ToLower(channel.Name), filter) {
			out = append(out, channel)
		}
	}
	return channelListResult{Channels: out}
}

func parseConversationTypes(value string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{"public_channel", "private_channel"}
	}
	seen := map[string]bool{}
	var out []string
	for _, part := range xstrings.SplitCSV(value) {
		normalized := normalizeConversationType(part)
		if normalized == "" {
			continue
		}
		if normalized == "all" {
			return []string{"public_channel", "private_channel", "im", "mpim"}
		}
		if !seen[normalized] {
			seen[normalized] = true
			out = append(out, normalized)
		}
	}
	if len(out) == 0 {
		return []string{"public_channel", "private_channel"}
	}
	return out
}

func normalizeConversationType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "all":
		return "all"
	case "channel", "channels", "public", "public_channel":
		return "public_channel"
	case "private", "private_channel", "group", "groups":
		return "private_channel"
	case "dm", "dms", "im":
		return "im"
	case "group_dm", "group-dm", "mpim":
		return "mpim"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func conversationReadScopeRequirement(types []string) scopeRequirement {
	scopes := make([]string, 0, len(types))
	for _, typ := range types {
		switch typ {
		case "public_channel":
			scopes = append(scopes, "channels:read")
		case "private_channel":
			scopes = append(scopes, "groups:read")
		case "im":
			scopes = append(scopes, "im:read")
		case "mpim":
			scopes = append(scopes, "mpim:read")
		}
	}
	if len(scopes) == 0 {
		return allScopes("channels:read", "groups:read")
	}
	return allScopes(scopes...)
}
