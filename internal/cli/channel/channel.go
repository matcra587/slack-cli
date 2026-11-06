package channel

import (
	"errors"
	"strings"

	xslices "github.com/gechr/x/slices"
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
	Channels   []clioutput.Channel   `json:"channels"`
	Pagination *clioutput.Pagination `json:"pagination,omitempty"`
}

func (d ListData) WithPagination(pagination *clioutput.Pagination) any {
	d.Pagination = pagination
	return d
}

var (
	_ clioutput.PlainRenderer  = ListData{}
	_ clioutput.ResultEnricher = ListData{}
)

func (d ListData) EnrichResult(c *clioutput.CommandContext) any {
	for i := range d.Channels {
		c.EnrichChannelConversation(&d.Channels[i])
	}
	return d
}

func (d ListData) WritePlain(c *clioutput.CommandContext, command string, pagination *clioutput.Pagination) error {
	return c.WriteChannels(command, d.Channels, pagination)
}

// InfoData is the result type for channel info operations.
type InfoData struct {
	Channel clioutput.Channel `json:"channel"`
}

var (
	_ clioutput.PlainRenderer  = InfoData{}
	_ clioutput.ResultEnricher = InfoData{}
)

func (d InfoData) EnrichResult(c *clioutput.CommandContext) any {
	c.EnrichChannelConversation(&d.Channel)
	return d
}

func (d InfoData) WritePlain(c *clioutput.CommandContext, command string, _ *clioutput.Pagination) error {
	ch := d.Channel
	styles := []clioutput.FieldStyle{
		clioutput.EntityFieldStyle("channel", ch.ID),
		clioutput.HashedFieldStyle("type", "channel_type:"+ch.Type),
	}
	if ch.User != nil {
		styles = append(styles, clioutput.EntityFieldStyle("user", *ch.User))
	}
	event := c.ResultEventWithStyles(command, styles...)
	ref := clioutput.SlackConversationRefFromChannel(ch)
	if link := c.SlackConversationURL(ref); link != "" {
		event = event.Link("channel", link, c.HyperlinkText(ch.ID))
	} else {
		event = event.Str("channel", ch.ID)
	}
	if ch.User != nil {
		event = event.Str("user", *ch.User)
	}
	event = event.
		Str("name", ch.Name).
		Str("type", ch.Type)
	if ch.IsMember != nil {
		clioutput.ApplyDimWhen(c.StdoutLogger(), c.Theme, "is_member", *ch.IsMember)
	}
	event = clioutput.AddBoolField(event, "is_member", ch.IsMember)
	event = clioutput.AddBoolField(event, "is_im", ch.IsIM)
	if ch.IsArchived != nil {
		clioutput.ApplyBoolStateStyle(c.StdoutLogger(), c.Theme, "is_archived", *ch.IsArchived)
	}
	event = clioutput.AddBoolField(event, "is_archived", ch.IsArchived)
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
			return runChannelListWithTypes(cmd, runtime, "lookup.channel", maxItems, cursor, filter, types)
		},
	}
	channelCmd.Flags().StringP("channel", "c", "", "Channel or conversation ID, or configured alias")
	channelCmd.Flags().IntVarP(&maxItems, "max-items", "M", 0, "Maximum conversations in one list page (0-999; 0 uses the default page size)")
	channelCmd.Flags().StringVarP(&cursor, "cursor", "C", "", "List pagination cursor")
	channelCmd.Flags().StringVarP(&filter, "filter", "f", "", "Filter the list by ID or name")
	channelCmd.Flags().StringVarP(&types, "types", "t", "public_channel,private_channel", "Conversation types: public_channel, private_channel, im, mpim, dm, or all")
	return channelCmd
}

func runChannelListWithTypes(cmd *cobra.Command, runtime *cliruntime.RootRuntime, command string, maxItems int, cursor, filter, typeInput string) error {
	ctx, profile, _, err := cliruntime.CommandContext(cmd, runtime)
	if err != nil {
		return cliruntime.WriteRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
	}
	if maxItems < 0 || maxItems >= 1000 {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("--max-items must be between 0 and 999"))
	}
	types, err := parseConversationTypes(typeInput)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
	}
	client, err := slackclient.Client(cmd, profile, runtime)
	if err != nil {
		return clioutput.WriteCommandError(ctx, slackclient.CLIError(cmd.Context(), err))
	}
	if err := cliscope.Require(cmd.Context(), client, conversationReadScopeRequirement(types)); err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err, ""))
	}

	channels, nextCursor, err := client.GetConversationsContext(cmd.Context(), &slackgo.GetConversationsParameters{
		Types:  types,
		Limit:  maxItems,
		Cursor: cursor,
	})
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err, ""))
	}
	result := ListData{Channels: filterChannels(ChannelsFromSlack(channels), filter)}
	return ctx.WriteResultWithPagination(command, result, &clioutput.Pagination{
		Cursor:        cliutil.StringPtr(cursor),
		NextCursor:    cliutil.StringPtr(nextCursor),
		HasMore:       nextCursor != "",
		MaxItems:      cliutil.IntPtr(maxItems),
		ItemsReturned: new(len(result.Channels)),
	})
}

func runChannelInfoValue(cmd *cobra.Command, runtime *cliruntime.RootRuntime, command, channel string) error {
	ctx, profile, _, err := cliruntime.CommandContext(cmd, runtime)
	if err != nil {
		return cliruntime.WriteRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
	}
	for _, flag := range []string{"max-items", "cursor", "filter", "types"} {
		if cmd.Flags().Changed(flag) {
			return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("--"+flag+" is only supported when listing channels; omit --channel"))
		}
	}
	channel = strings.TrimSpace(climessage.ResolveAlias(profile, strings.TrimSpace(channel)))
	if channel == "" {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("channel is required"))
	}
	client, err := slackclient.Client(cmd, profile, runtime)
	if err != nil {
		return clioutput.WriteCommandError(ctx, slackclient.CLIError(cmd.Context(), err))
	}
	if err := cliscope.Require(cmd.Context(), client, cliscope.AnyOf("channels:read", "groups:read", "im:read", "mpim:read")); err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err, ""))
	}
	result, err := client.GetConversationInfoContext(cmd.Context(), &slackgo.GetConversationInfoInput{
		ChannelID:         channel,
		IncludeNumMembers: true,
	})
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err, ""))
	}
	return ctx.WriteResult(command, InfoData{Channel: clioutput.ChannelFromSlack(*result)})
}

// ChannelsFromSlack converts a slice of slack-go Channels to Channel DTOs.
func ChannelsFromSlack(channels []slackgo.Channel) []clioutput.Channel {
	out := make([]clioutput.Channel, 0, len(channels))
	for i := range channels {
		out = append(out, clioutput.ChannelFromSlack(channels[i]))
	}
	return out
}

func filterChannels(channels []clioutput.Channel, filter string) []clioutput.Channel {
	if filter == "" {
		return channels
	}
	out := make([]clioutput.Channel, 0, len(channels))
	for _, channel := range channels {
		if cliutil.ContainsAnyFold(filter, channel.ID, channel.Name) {
			out = append(out, channel)
		}
	}
	return out
}

func parseConversationTypes(value string) ([]string, error) {
	parts := xstrings.SplitCSV(value)
	out := make([]string, 0, len(parts))
	all := false
	for _, part := range parts {
		normalized := normalizeConversationType(part)
		switch normalized {
		case "":
			continue
		case "all":
			all = true
		case "public_channel", "private_channel", "im", "mpim":
			out = append(out, normalized)
		default:
			return nil, errors.New("--types contains an unsupported conversation type; use public_channel, private_channel, im, mpim, dm, or all")
		}
	}
	if all {
		return []string{"public_channel", "private_channel", "im", "mpim"}, nil
	}
	if len(out) == 0 {
		return []string{"public_channel", "private_channel"}, nil
	}
	return xslices.Unique(out), nil
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

// ConversationReadScopeRequirement returns the alternative scopes accepted by
// conversations.list. Slack filters the results by token access and requested types.
func ConversationReadScopeRequirement(types []string) cliscope.Requirement {
	return conversationReadScopeRequirement(types)
}

func conversationReadScopeRequirement(_ []string) cliscope.Requirement {
	// Types narrow results; they do not require granting every listed scope.
	return cliscope.AnyOf("channels:read", "groups:read", "im:read", "mpim:read")
}
