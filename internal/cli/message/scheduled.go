package message

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gechr/clog"
	"github.com/matcra587/slack-cli/internal/cli/cliutil"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	cliscope "github.com/matcra587/slack-cli/internal/cli/scope"
	slackclient "github.com/matcra587/slack-cli/internal/cli/slackclient"
	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

type AttributionData struct {
	Enabled bool   `json:"enabled"`
	Label   string `json:"label"`
}

type ScheduledSendData struct {
	Channel            string          `json:"channel"`
	ScheduledMessageID string          `json:"scheduled_message_id"`
	PostAt             int64           `json:"post_at"`
	PostAtISO          string          `json:"post_at_iso"`
	Text               string          `json:"text"`
	Attribution        AttributionData `json:"attribution"`
	DryRun             bool            `json:"dry_run,omitempty"`
}

var _ clioutput.PlainRenderer = ScheduledSendData{}

func (d ScheduledSendData) WritePlain(c *clioutput.CommandContext, command string, _ *clioutput.Pagination) error {
	event := c.ResultEventWithStyles(command, clioutput.EntityFieldStyle("channel", d.Channel)).
		Str("channel", d.Channel).
		Str("scheduled_message_id", d.ScheduledMessageID).
		Int64("post_at", d.PostAt).
		Str("post_at_iso", d.PostAtISO).
		Bool("dry_run", d.DryRun)
	if clog.IsVerbose() {
		event.Bool("attribution", d.Attribution.Enabled)
	}
	event.Msg("Message scheduled")
	return nil
}

type ScheduledListData struct {
	ScheduledMessages []clioutput.ScheduledMessage `json:"scheduled_messages"`
}

type ScheduledDeleteData struct {
	Channel            string `json:"channel"`
	ScheduledMessageID string `json:"scheduled_message_id"`
	Deleted            bool   `json:"deleted"`
	DryRun             bool   `json:"dry_run,omitempty"`
}

var _ clioutput.PlainRenderer = ScheduledDeleteData{}

func (d ScheduledDeleteData) WritePlain(c *clioutput.CommandContext, command string, _ *clioutput.Pagination) error {
	c.ResultEventWithStyles(command, clioutput.EntityFieldStyle("channel", d.Channel)).
		Str("channel", d.Channel).
		Str("scheduled_message_id", d.ScheduledMessageID).
		Bool("deleted", d.Deleted).
		Bool("dry_run", d.DryRun).
		Msg(clioutput.ActionLabel(command))
	return nil
}

type scheduledListOptions struct {
	Channel string
	Cursor  string
	Limit   int
}

func newScheduledCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	scheduledCmd := &cobra.Command{
		Use:          "scheduled",
		Short:        "Manage scheduled Slack messages",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	var listOpts scheduledListOptions
	listCmd := &cobra.Command{
		Use:          "list",
		Short:        "List scheduled Slack messages",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runScheduledList(cmd, runtime, listOpts)
		},
	}
	listCmd.Flags().StringVarP(&listOpts.Channel, "channel", "c", "", "Channel ID, name, or alias")
	listCmd.Flags().StringVarP(&listOpts.Cursor, "cursor", "C", "", "Pagination cursor")
	listCmd.Flags().IntVarP(&listOpts.Limit, "limit", "L", 0, "Maximum scheduled messages to return")
	scheduledCmd.AddCommand(listCmd)

	var channel string
	var scheduledID string
	deleteCmd := &cobra.Command{
		Use:          "delete",
		Short:        "Delete a scheduled Slack message",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runScheduledDelete(cmd, runtime, channel, scheduledID, cliruntime.DryRun(cmd))
		},
	}
	deleteCmd.Flags().StringVarP(&channel, "channel", "c", "", "Channel ID, name, or alias")
	deleteCmd.Flags().StringVar(&scheduledID, "scheduled-id", "", "Scheduled message ID")
	scheduledCmd.AddCommand(deleteCmd)

	return scheduledCmd
}

func runScheduledMessageSend(cmd *cobra.Command, runtime *cliruntime.RootRuntime, src Source, dryRun bool, scheduleWhen string) error {
	ctx, profile, attribution, err := cliruntime.CommandContext(cmd, runtime)
	if err != nil {
		return writeRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
	}
	if runtime.Config == nil {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("config is required"))
	}

	channel, _ := cmd.Flags().GetString("channel")
	users, _ := cmd.Flags().GetStringArray("user")
	target, err := resolveExplicitMessageSendTarget(profile, channel, users)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
	}

	content, err := ReadMessageSource(runtime.Stdin, src)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
	}
	blocks, err := ComposeBlocks(content, src.Blocks, attribution)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
	}
	postAt, err := parseScheduleWhen(scheduleWhen, ctx.Now())
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
	}

	data := ScheduledSendData{
		Channel:            target.previewChannel(),
		ScheduledMessageID: "Q-dry-run",
		PostAt:             postAt.Unix(),
		PostAtISO:          postAt.UTC().Format(time.RFC3339),
		Text:               strings.TrimSpace(content),
		Attribution:        AttributionData{Enabled: attribution.Enabled, Label: attribution.Label},
		DryRun:             dryRun,
	}
	if dryRun {
		return ctx.WriteResult("message.send", data)
	}

	client, err := slackclient.Client(cmd, profile, runtime)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.AuthCLIError(err.Error()))
	}
	if len(target.Users) > 0 {
		target.Channel, err = openMessageUserChannel(cmd.Context(), client, target.Users)
		if err != nil {
			return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), fmt.Errorf("open scheduled message target: %w", err), ""))
		}
	} else if err := cliscope.Require(cmd.Context(), client, cliscope.AllOf("chat:write")); err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), fmt.Errorf("check scheduled message scopes: %w", err), ""))
	}
	respChannel, scheduledID, err := client.ScheduleMessageContext(cmd.Context(), target.Channel, strconv.FormatInt(postAt.Unix(), 10), MessageOptions(content, blocks, attribution)...)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), fmt.Errorf("schedule message: %w", err), ""))
	}
	data.Channel = cliutil.FirstNonEmpty(respChannel, target.Channel)
	data.ScheduledMessageID = scheduledID
	return ctx.WriteResult("message.send", data)
}

func runScheduledList(cmd *cobra.Command, runtime *cliruntime.RootRuntime, opts scheduledListOptions) error {
	ctx, profile, _, err := cliruntime.CommandContext(cmd, runtime)
	if err != nil {
		return writeRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
	}

	channel := resolveAlias(profile, strings.TrimSpace(opts.Channel))
	client, err := slackclient.Client(cmd, profile, runtime)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.AuthCLIError(err.Error()))
	}
	if err := cliscope.Require(cmd.Context(), client, cliscope.AllOf("chat:write")); err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), fmt.Errorf("check scheduled message scopes: %w", err), ""))
	}
	messages, nextCursor, err := client.GetScheduledMessagesContext(cmd.Context(), &slackgo.GetScheduledMessagesParameters{
		Channel: channel,
		Cursor:  opts.Cursor,
		Limit:   opts.Limit,
	})
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), fmt.Errorf("list scheduled messages: %w", err), ""))
	}

	rows := enrichScheduledRows(cmd, client, scheduledRowsFromSlack(messages))
	pagination := &clioutput.Pagination{
		Cursor:        cliutil.StringPtr(opts.Cursor),
		NextCursor:    cliutil.StringPtr(nextCursor),
		HasMore:       nextCursor != "",
		MaxItems:      cliutil.IntPtr(opts.Limit),
		ItemsReturned: cliutil.IntPtr(len(rows)),
	}
	data := ScheduledListData{ScheduledMessages: rows}
	return clioutput.WriteList(ctx, "message.scheduled.list", data, rows, pagination, writeScheduledListPlain)
}

func runScheduledDelete(cmd *cobra.Command, runtime *cliruntime.RootRuntime, channel, scheduledID string, dryRun bool) error {
	ctx, profile, _, err := cliruntime.CommandContext(cmd, runtime)
	if err != nil {
		return writeRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
	}
	channel = resolveAlias(profile, strings.TrimSpace(channel))
	if channel == "" {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("channel is required"))
	}
	scheduledID = strings.TrimSpace(scheduledID)
	if scheduledID == "" {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("scheduled-id is required"))
	}

	data := ScheduledDeleteData{
		Channel:            channel,
		ScheduledMessageID: scheduledID,
		Deleted:            true,
		DryRun:             dryRun,
	}
	if dryRun {
		return ctx.WriteResult("message.scheduled.delete", data)
	}

	client, err := slackclient.Client(cmd, profile, runtime)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.AuthCLIError(err.Error()))
	}
	if err := cliscope.Require(cmd.Context(), client, cliscope.AllOf("chat:write")); err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), fmt.Errorf("check scheduled message scopes: %w", err), ""))
	}
	ok, err := client.DeleteScheduledMessageContext(cmd.Context(), &slackgo.DeleteScheduledMessageParameters{
		Channel:            channel,
		ScheduledMessageID: scheduledID,
		AsUser:             true,
	})
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), fmt.Errorf("delete scheduled message: %w", err), ""))
	}
	data.Deleted = ok
	return ctx.WriteResult("message.scheduled.delete", data)
}

func scheduledRowsFromSlack(messages []slackgo.ScheduledMessage) []clioutput.ScheduledMessage {
	rows := make([]clioutput.ScheduledMessage, 0, len(messages))
	for _, message := range messages {
		rows = append(rows, clioutput.ScheduledMessage{
			ID:          message.ID,
			Channel:     message.Channel,
			PostAt:      int64(message.PostAt),
			PostAtISO:   time.Unix(int64(message.PostAt), 0).UTC().Format(time.RFC3339),
			TextPreview: clioutput.TextPreview(message.Text, 200),
		})
	}
	return rows
}

type scheduledChannelMeta struct {
	name string
	typ  string
	user string
	isDM *bool
}

func enrichScheduledRows(cmd *cobra.Command, client *slackgo.Client, rows []clioutput.ScheduledMessage) []clioutput.ScheduledMessage {
	if len(rows) == 0 {
		return rows
	}
	cache := make(map[string]scheduledChannelMeta)
	for i := range rows {
		channelID := rows[i].Channel
		if channelID == "" {
			continue
		}
		meta, ok := cache[channelID]
		if !ok {
			meta = resolveScheduledChannelMeta(cmd, client, channelID)
			cache[channelID] = meta
		}
		rows[i].ChannelName = meta.name
		rows[i].ChannelType = meta.typ
		rows[i].ChannelUser = cliutil.StringPtr(meta.user)
		rows[i].IsDM = meta.isDM
	}
	return rows
}

func resolveScheduledChannelMeta(cmd *cobra.Command, client *slackgo.Client, channelID string) scheduledChannelMeta {
	meta := scheduledChannelMeta{}
	if strings.HasPrefix(channelID, "D") {
		meta.typ = "im"
		isDM := true
		meta.isDM = &isDM
	}

	info, err := client.GetConversationInfoContext(cmd.Context(), &slackgo.GetConversationInfoInput{
		ChannelID: channelID,
	})
	if err != nil {
		return meta
	}

	meta.name = info.Name
	meta.typ = scheduledChannelType(*info)
	meta.user = info.User
	isDM := info.IsIM || info.IsMpIM
	meta.isDM = &isDM
	if info.IsIM && info.User != "" {
		meta.name = cliutil.FirstNonEmpty(resolveScheduledUserName(cmd, client, info.User), info.User)
	}
	return meta
}

func resolveScheduledUserName(cmd *cobra.Command, client *slackgo.Client, userID string) string {
	user, err := client.GetUserInfoContext(cmd.Context(), userID)
	if err != nil {
		return ""
	}
	return cliutil.FirstNonEmpty(user.Profile.DisplayName, user.Profile.RealName, user.RealName, user.Name)
}

func scheduledChannelType(channel slackgo.Channel) string {
	if channel.IsIM {
		return "im"
	}
	if channel.IsMpIM {
		return "mpim"
	}
	if channel.IsPrivate {
		return "private_channel"
	}
	return "channel"
}

func writeScheduledListPlain(c *clioutput.CommandContext, command string, rows []clioutput.ScheduledMessage, pagination *clioutput.Pagination) error {
	return c.WriteScheduledMessages(command, rows, pagination)
}
