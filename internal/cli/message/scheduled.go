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
	"github.com/matcra587/slack-cli/internal/cli/slackmeta"
	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

type AttributionData struct {
	Enabled bool   `json:"enabled"`
	Label   string `json:"label"`
}

type ScheduledSendData struct {
	Channel                           string                         `json:"channel"`
	clioutput.SlackConversationFields                                // channel_name, channel_hr, channel_url
	ScheduledMessageID                string                         `json:"scheduled_message_id"`
	PostAt                            int64                          `json:"post_at"`
	PostAtISO                         string                         `json:"post_at_iso"`
	Text                              string                         `json:"text"`
	Attribution                       AttributionData                `json:"attribution"`
	DryRun                            bool                           `json:"dry_run,omitempty"`
	ChannelRef                        clioutput.SlackConversationRef `json:"-"`
}

var (
	_ clioutput.PlainRenderer  = ScheduledSendData{}
	_ clioutput.ResultEnricher = ScheduledSendData{}
)

func (d ScheduledSendData) EnrichResult(c *clioutput.CommandContext) any {
	ref := d.ChannelRef
	if ref.ID == "" {
		ref.ID = d.Channel
	}
	d.SlackConversationFields = c.SlackConversationFields(ref)
	return d
}

func (d ScheduledSendData) WritePlain(c *clioutput.CommandContext, command string, _ *clioutput.Pagination) error {
	ref := d.ChannelRef
	if ref.ID == "" {
		ref.ID = d.Channel
	}
	event := c.ResultEventWithStyles(command, clioutput.EntityFieldStyle("channel", ref.ID))
	event = clioutput.AddSlackConversationField(event, c, "channel", ref).
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

var _ clioutput.ResultEnricher = ScheduledListData{}

func (d ScheduledListData) EnrichResult(c *clioutput.CommandContext) any {
	for i := range d.ScheduledMessages {
		c.EnrichScheduledConversation(&d.ScheduledMessages[i], clioutput.SlackConversationRefFromScheduled(d.ScheduledMessages[i]))
	}
	return d
}

type ScheduledDeleteData struct {
	Channel                           string                         `json:"channel"`
	clioutput.SlackConversationFields                                // channel_name, channel_hr, channel_url
	ScheduledMessageID                string                         `json:"scheduled_message_id"`
	Deleted                           bool                           `json:"deleted"`
	DryRun                            bool                           `json:"dry_run,omitempty"`
	ChannelRef                        clioutput.SlackConversationRef `json:"-"`
}

var (
	_ clioutput.PlainRenderer  = ScheduledDeleteData{}
	_ clioutput.ResultEnricher = ScheduledDeleteData{}
)

func (d ScheduledDeleteData) EnrichResult(c *clioutput.CommandContext) any {
	ref := d.ChannelRef
	if ref.ID == "" {
		ref.ID = d.Channel
	}
	d.SlackConversationFields = c.SlackConversationFields(ref)
	return d
}

func (d ScheduledDeleteData) WritePlain(c *clioutput.CommandContext, command string, _ *clioutput.Pagination) error {
	ref := d.ChannelRef
	if ref.ID == "" {
		ref.ID = d.Channel
	}
	event := c.ResultEventWithStyles(command, clioutput.EntityFieldStyle("channel", ref.ID))
	event = clioutput.AddSlackConversationField(event, c, "channel", ref)
	event.
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
	data.ChannelRef = slackmeta.ResolveConversation(cmd.Context(), nil, profile.Name, data.Channel)
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
	data.ChannelRef = slackmeta.ResolveConversation(cmd.Context(), client, profile.Name, data.Channel)
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

	rows := enrichScheduledRows(cmd, client, profile.Name, scheduledRowsFromSlack(messages))
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
	data.ChannelRef = slackmeta.ResolveConversation(cmd.Context(), nil, profile.Name, data.Channel)
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
	data.ChannelRef = slackmeta.ResolveConversation(cmd.Context(), client, profile.Name, data.Channel)
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

func enrichScheduledRows(cmd *cobra.Command, client *slackgo.Client, profile string, rows []clioutput.ScheduledMessage) []clioutput.ScheduledMessage {
	if len(rows) == 0 {
		return rows
	}
	cache := make(map[string]clioutput.SlackConversationRef)
	for i := range rows {
		channelID := rows[i].Channel
		if channelID == "" {
			continue
		}
		ref, ok := cache[channelID]
		if !ok {
			ref = slackmeta.ResolveConversation(cmd.Context(), client, profile, channelID)
			cache[channelID] = ref
		}
		rows[i].ChannelName = ref.Name
		rows[i].ChannelType = ref.Type
		if ref.User != "" {
			rows[i].ChannelUser = cliutil.StringPtr(ref.User)
		}
		rows[i].IsDM = ref.IsDM
	}
	return rows
}

func writeScheduledListPlain(c *clioutput.CommandContext, command string, rows []clioutput.ScheduledMessage, pagination *clioutput.Pagination) error {
	return c.WriteScheduledMessages(command, rows, pagination)
}
