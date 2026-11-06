package history

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/matcra587/slack-cli/internal/cli/cliutil"
	climessage "github.com/matcra587/slack-cli/internal/cli/message"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	cliscope "github.com/matcra587/slack-cli/internal/cli/scope"
	slackclient "github.com/matcra587/slack-cli/internal/cli/slackclient"
	"github.com/matcra587/slack-cli/internal/cli/slackmeta"
	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

// Data is the result type for history list operations.
type Data struct {
	Messages   []clioutput.Message            `json:"messages"`
	ChannelRef clioutput.SlackConversationRef `json:"-"`
	Pagination *clioutput.Pagination          `json:"pagination,omitempty"`
}

func (d Data) WithPagination(pagination *clioutput.Pagination) any {
	d.Pagination = pagination
	return d
}

var (
	_ clioutput.PlainRenderer  = Data{}
	_ clioutput.ResultEnricher = Data{}
)

func (d Data) EnrichResult(c *clioutput.CommandContext) any {
	for i := range d.Messages {
		c.EnrichMessageConversation(&d.Messages[i], d.ChannelRef)
		for j := range d.Messages[i].Replies {
			c.EnrichMessageConversation(&d.Messages[i].Replies[j], d.ChannelRef)
		}
	}
	return d
}

func (d Data) WritePlain(c *clioutput.CommandContext, command string, pagination *clioutput.Pagination) error {
	return c.WriteMessages(command, d.Messages, pagination)
}

// ListOptions configures a history list request.
type ListOptions struct {
	MaxItems       int
	Since          string
	Until          string
	User           string
	Thread         string
	Cursor         string
	IncludeReplies bool
	ReplyLimit     int
}

type result struct {
	Messages   []clioutput.Message
	Pagination *clioutput.Pagination
}

// NewCommand returns the "history" cobra command tree.
func NewCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	historyCmd := &cobra.Command{
		Use:   "history",
		Short: "Read Slack message history",
	}

	var maxItems int
	var since string
	var until string
	var user string
	var thread string
	var cursor string
	var includeReplies bool
	var replyLimit int
	listCmd := &cobra.Command{
		Use:          "list",
		Short:        "List channel or thread history",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_ = includeReplies
			_ = replyLimit
			return runHistoryList(cmd, runtime, ListOptions{
				MaxItems:       maxItems,
				Since:          since,
				Until:          until,
				User:           user,
				Thread:         thread,
				Cursor:         cursor,
				IncludeReplies: includeReplies,
				ReplyLimit:     replyLimit,
			})
		},
	}
	listCmd.Flags().StringP("channel", "c", "", "Channel ID or configured alias")
	listCmd.Flags().IntVarP(&maxItems, "max-items", "M", 0, "Maximum messages on one page (1-999; 1-1000 with --thread; 0 uses Slack's default)")
	listCmd.Flags().StringVarP(&since, "since", "s", "", "Oldest Slack timestamp")
	listCmd.Flags().StringVarP(&until, "until", "u", "", "Latest Slack timestamp")
	listCmd.Flags().StringVarP(&user, "user", "U", "", "Filter by user ID")
	listCmd.Flags().StringVarP(&thread, "thread", "t", "", "Read replies for parent timestamp")
	listCmd.Flags().StringVarP(&cursor, "cursor", "C", "", "Pagination cursor")
	listCmd.Flags().BoolVarP(&includeReplies, "include-replies", "R", false, "Include bounded thread replies")
	listCmd.Flags().IntVarP(&replyLimit, "reply-limit", "L", 0, "Maximum replies per parent with --include-replies (1-999; 0 uses Slack's page default)")
	historyCmd.AddCommand(listCmd)

	return historyCmd
}

func runHistoryList(cmd *cobra.Command, runtime *cliruntime.RootRuntime, opts ListOptions) error {
	ctx, profile, _, err := cliruntime.CommandContext(cmd, runtime)
	if err != nil {
		return cliruntime.WriteRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
	}

	channel, _ := cmd.Flags().GetString("channel")
	channel = strings.TrimSpace(climessage.ResolveAlias(profile, strings.TrimSpace(channel)))
	if strings.TrimSpace(channel) == "" {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("channel is required"))
	}
	opts.Since = strings.TrimSpace(opts.Since)
	opts.Until = strings.TrimSpace(opts.Until)
	opts.User = strings.TrimSpace(opts.User)
	opts.Thread = strings.TrimSpace(opts.Thread)
	maxPage := 999
	if opts.Thread != "" {
		maxPage = 1000
	}
	if opts.MaxItems < 0 || opts.MaxItems > maxPage {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(fmt.Sprintf("max-items must be 0 (Slack's default) or between 1 and %d", maxPage)))
	}
	if opts.ReplyLimit < 0 || opts.ReplyLimit > 999 {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("reply-limit must be 0 (Slack's default) or between 1 and 999"))
	}
	if opts.Thread != "" && opts.IncludeReplies {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("include-replies cannot be combined with thread; thread already reads replies"))
	}
	if cmd.Flags().Changed("reply-limit") && !opts.IncludeReplies {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("reply-limit requires include-replies"))
	}

	client, err := slackclient.Client(cmd, profile, runtime)
	if err != nil {
		return clioutput.WriteCommandError(ctx, slackclient.CLIError(cmd.Context(), err))
	}
	if err := cliscope.Require(cmd.Context(), client, cliscope.AnyOf("channels:history", "groups:history", "im:history", "mpim:history")); err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err, ""))
	}
	var res result
	if opts.Thread != "" {
		res, err = threadHistory(cmd.Context(), client, channel, opts)
	} else {
		res, err = channelHistory(cmd.Context(), client, channel, opts)
	}
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err, ""))
	}

	channelRef := slackmeta.ResolveConversation(cmd.Context(), client, profile.Name, channel)
	return ctx.WriteResultWithPagination("history.list", Data{Messages: res.Messages, ChannelRef: channelRef}, res.Pagination)
}

func channelHistory(ctx context.Context, client *slackgo.Client, channel string, opts ListOptions) (result, error) {
	resp, err := client.GetConversationHistoryContext(ctx, &slackgo.GetConversationHistoryParameters{
		ChannelID: channel,
		Cursor:    opts.Cursor,
		Limit:     opts.MaxItems,
		Oldest:    opts.Since,
		Latest:    opts.Until,
	})
	if err != nil {
		return result{}, err
	}
	if !resp.Ok {
		return result{}, errors.New("slack history response did not report success")
	}
	page, err := historyPage(resp.Messages, resp.HasMore, resp.ResponseMetaData.NextCursor, channel, opts)
	if err != nil {
		return result{}, err
	}
	messages := page.Messages
	for i := range messages {
		messages[i].Permalink = permalink(ctx, client, channel, messages[i].TS)
	}
	if opts.IncludeReplies {
		for i := range messages {
			if messages[i].ReplyCount == nil || *messages[i].ReplyCount == 0 {
				continue
			}
			replyLimitVal := 0
			if opts.ReplyLimit > 0 {
				replyLimitVal = opts.ReplyLimit + 1
			}
			thread, err := threadHistory(ctx, client, channel, ListOptions{Thread: messages[i].TS, MaxItems: replyLimitVal})
			if err != nil {
				return result{}, err
			}
			messages[i].Replies = repliesOnly(messages[i].TS, thread.Messages)
			thread.Pagination.ItemsReturned = new(len(messages[i].Replies))
			thread.Pagination.MaxItems = cliutil.IntPtr(opts.ReplyLimit)
			messages[i].RepliesPagination = thread.Pagination
		}
	}
	return page, nil
}

func threadHistory(ctx context.Context, client *slackgo.Client, channel string, opts ListOptions) (result, error) {
	messages, hasMore, nextCursor, err := client.GetConversationRepliesContext(ctx, &slackgo.GetConversationRepliesParameters{
		ChannelID: channel,
		Timestamp: opts.Thread,
		Cursor:    opts.Cursor,
		Limit:     opts.MaxItems,
		Oldest:    opts.Since,
		Latest:    opts.Until,
	})
	if err != nil {
		return result{}, err
	}
	return historyPage(messages, hasMore, nextCursor, channel, opts)
}

func historyPage(raw []slackgo.Message, hasMore bool, nextCursor, channel string, opts ListOptions) (result, error) {
	if raw == nil {
		return result{}, errors.New("slack history response is missing its messages array")
	}
	messages := filterMessagesByUser(cliMessagesFromSlack(raw, channel), opts.User)
	pagination := &clioutput.Pagination{
		Cursor: cliutil.StringPtr(opts.Cursor), NextCursor: cliutil.StringPtr(nextCursor),
		HasMore: hasMore || nextCursor != "", MaxItems: cliutil.IntPtr(opts.MaxItems),
		ItemsReturned: new(len(messages)),
	}
	if hasMore && nextCursor == "" {
		if len(raw) == 0 || raw[len(raw)-1].Timestamp == "" {
			return result{}, errors.New("slack history response has more messages without a continuation boundary")
		}
		boundary := raw[len(raw)-1].Timestamp
		if opts.Thread != "" {
			if boundary == opts.Since {
				return result{}, errors.New("slack thread history did not advance its time boundary")
			}
			pagination.NextSince = new(boundary)
		} else {
			if boundary == opts.Until {
				return result{}, errors.New("slack channel history did not advance its time boundary")
			}
			pagination.NextUntil = new(boundary)
		}
	}
	return result{Messages: messages, Pagination: pagination}, nil
}

func cliMessagesFromSlack(messages []slackgo.Message, channel string) []clioutput.Message {
	out := make([]clioutput.Message, 0, len(messages))
	for _, message := range messages {
		out = append(out, clioutput.MessageFromSlack(message, channel))
	}
	return out
}

func filterMessagesByUser(messages []clioutput.Message, user string) []clioutput.Message {
	if user == "" {
		return messages
	}
	out := make([]clioutput.Message, 0, len(messages))
	for _, message := range messages {
		if message.User != nil && *message.User == user {
			out = append(out, message)
		}
	}
	return out
}

func repliesOnly(parent string, messages []clioutput.Message) []clioutput.Message {
	out := make([]clioutput.Message, 0, len(messages))
	for _, message := range messages {
		if message.TS == parent {
			continue
		}
		out = append(out, message)
	}
	return out
}

func permalink(ctx context.Context, client *slackgo.Client, channel, ts string) *string {
	if channel == "" || ts == "" {
		return nil
	}
	value, err := client.GetPermalinkContext(ctx, &slackgo.PermalinkParameters{Channel: channel, Ts: ts})
	if err != nil || value == "" {
		return nil
	}
	return cliutil.StringPtr(value)
}
