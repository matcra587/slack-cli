package channel

import (
	"strings"

	xstrings "github.com/gechr/x/strings"
	"github.com/matcra587/slack-cli/internal/cli/cliutil"
	climessage "github.com/matcra587/slack-cli/internal/cli/message"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	cliscope "github.com/matcra587/slack-cli/internal/cli/scope"
	clisearch "github.com/matcra587/slack-cli/internal/cli/search"
	slackclient "github.com/matcra587/slack-cli/internal/cli/slackclient"
	cliuser "github.com/matcra587/slack-cli/internal/cli/user"
	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

// ListData is the result type for channel list operations.
type ListData struct {
	Channels []clioutput.CliChannel `json:"channels"`
}

var _ clioutput.PlainRenderer = ListData{}

func (d ListData) WritePlain(c *clioutput.CommandContext, command string, pagination *clioutput.Pagination) error {
	return c.WriteChannels(command, d.Channels, pagination)
}

// InfoData is the result type for channel info operations.
type InfoData struct {
	Channel clioutput.CliChannel `json:"channel"`
}

var _ clioutput.PlainRenderer = InfoData{}

func (d InfoData) WritePlain(c *clioutput.CommandContext, command string, _ *clioutput.Pagination) error {
	ch := d.Channel
	event := c.ResultEventWithStyles(command, clioutput.EntityFieldStyle("channel", ch.ID)).
		Str("channel", ch.ID).
		Str("name", ch.Name).
		Str("type", ch.Type)
	event = clioutput.AddBoolField(event, "is_member", ch.IsMember)
	event = clioutput.AddBoolField(event, "is_im", ch.IsIM)
	event = clioutput.AddBoolField(event, "is_archived", ch.IsArchived)
	if ch.User != nil {
		event = event.Str("user", *ch.User)
	}
	if ch.Topic != nil {
		event = event.Str("topic", *ch.Topic)
	}
	event = clioutput.AddIntField(event, "num_members", ch.NumMembers)
	event.Msg(clioutput.ActionLabel(command))
	return nil
}

// NewCommand returns the "lookup" parent command wiring channel, user, and messages subcommands.
func NewCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	lookupCmd := &cobra.Command{
		Use:   "lookup",
		Short: "Look up Slack channels and users",
	}
	lookupCmd.AddCommand(newLookupChannelCommand(runtime))
	lookupCmd.AddCommand(cliuser.NewLookupUserCommand(runtime))
	lookupCmd.AddCommand(clisearch.NewLookupMessagesCommand(runtime))
	return lookupCmd
}

func newLookupChannelCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
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

func runChannelListWithTypes(cmd *cobra.Command, runtime *cliruntime.RootRuntime, command string, maxItems int, cursor, filter string, types []string) error {
	ctx, profile, _, err := cliruntime.CommandContext(cmd, runtime)
	if err != nil {
		return cliruntime.WriteRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
	}
	client, err := slackclient.Client(cmd, profile, runtime)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.AuthCLIError(err.Error()))
	}
	if err := cliscope.Require(cmd.Context(), client, conversationReadScopeRequirement(types)); err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err))
	}

	channels, nextCursor, err := client.GetConversationsContext(cmd.Context(), &slackgo.GetConversationsParameters{
		Types:  types,
		Limit:  maxItems,
		Cursor: cursor,
	})
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err))
	}
	result := ListData{Channels: filterChannels(CliChannelsFromSlack(channels), filter)}
	return ctx.WriteResultWithPagination(command, result, &clioutput.Pagination{
		Cursor:        cliutil.StringPtr(cursor),
		NextCursor:    cliutil.StringPtr(nextCursor),
		HasMore:       nextCursor != "",
		MaxItems:      cliutil.IntPtr(maxItems),
		ItemsReturned: cliutil.IntPtr(len(result.Channels)),
	})
}

func runChannelInfoValue(cmd *cobra.Command, runtime *cliruntime.RootRuntime, command, channel string) error {
	ctx, profile, _, err := cliruntime.CommandContext(cmd, runtime)
	if err != nil {
		return cliruntime.WriteRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
	}
	if strings.TrimSpace(channel) == "" {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("channel is required"))
	}
	channel = climessage.ResolveAlias(profile, strings.TrimSpace(channel))
	client, err := slackclient.Client(cmd, profile, runtime)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.AuthCLIError(err.Error()))
	}
	if err := cliscope.Require(cmd.Context(), client, cliscope.AnyOf("channels:read", "groups:read", "im:read", "mpim:read")); err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err))
	}
	result, err := client.GetConversationInfoContext(cmd.Context(), &slackgo.GetConversationInfoInput{
		ChannelID:         channel,
		IncludeNumMembers: true,
	})
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err))
	}
	return ctx.WriteResult(command, InfoData{Channel: clioutput.CliChannelFromSlack(*result)})
}

// CliChannelsFromSlack converts a slice of slack-go Channels to CliChannel DTOs.
func CliChannelsFromSlack(channels []slackgo.Channel) []clioutput.CliChannel {
	out := make([]clioutput.CliChannel, 0, len(channels))
	for i := range channels {
		out = append(out, clioutput.CliChannelFromSlack(channels[i]))
	}
	return out
}

func filterChannels(channels []clioutput.CliChannel, filter string) []clioutput.CliChannel {
	if filter == "" {
		return channels
	}
	out := make([]clioutput.CliChannel, 0, len(channels))
	for _, channel := range channels {
		if cliutil.ContainsAnyFold(filter, channel.ID, channel.Name) {
			out = append(out, channel)
		}
	}
	return out
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

// ConversationReadScopeRequirement builds a scope requirement for the given conversation types.
func ConversationReadScopeRequirement(types []string) cliscope.Requirement {
	return conversationReadScopeRequirement(types)
}

func conversationReadScopeRequirement(types []string) cliscope.Requirement {
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
		return cliscope.AllOf("channels:read", "groups:read")
	}
	return cliscope.AllOf(scopes...)
}
