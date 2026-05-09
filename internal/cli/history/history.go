package history

import (
	"context"
	"strings"

	"github.com/matcra587/slack-cli/internal/cli/cliutil"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	cliscope "github.com/matcra587/slack-cli/internal/cli/scope"
	slackclient "github.com/matcra587/slack-cli/internal/cli/slackclient"
	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

// Data is the result type for history list operations.
type Data struct {
	Messages []clioutput.CliMessage `json:"messages"`
}

var _ clioutput.PlainRenderer = Data{}

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
	Messages   []clioutput.CliMessage
	NextCursor *string
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
	listCmd.Flags().StringP("channel", "c", "", "Channel ID, name, or alias")
	listCmd.Flags().IntVarP(&maxItems, "max-items", "M", 0, "Maximum messages to return")
	listCmd.Flags().StringVarP(&since, "since", "s", "", "Oldest Slack timestamp")
	listCmd.Flags().StringVarP(&until, "until", "u", "", "Latest Slack timestamp")
	listCmd.Flags().StringVarP(&user, "user", "U", "", "Filter by user ID")
	listCmd.Flags().StringVarP(&thread, "thread", "t", "", "Read replies for parent timestamp")
	listCmd.Flags().StringVarP(&cursor, "cursor", "C", "", "Pagination cursor")
	listCmd.Flags().BoolVarP(&includeReplies, "include-replies", "R", false, "Include bounded thread replies")
	listCmd.Flags().IntVarP(&replyLimit, "reply-limit", "L", 0, "Maximum replies per parent")
	historyCmd.AddCommand(listCmd)

	return historyCmd
}

func runHistoryList(cmd *cobra.Command, runtime *cliruntime.RootRuntime, opts ListOptions) error {
	ctx, profile, _, err := cliruntime.CommandContext(cmd, runtime)
	if err != nil {
		return cliruntime.WriteRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
	}

	channel, _ := cmd.Flags().GetString("channel")
	if strings.TrimSpace(channel) == "" {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("channel is required"))
	}

	client, err := slackclient.Client(cmd, profile, runtime)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.AuthCLIError(err.Error()))
	}
	if err := cliscope.Require(cmd.Context(), client, cliscope.AnyOf("channels:history", "groups:history", "im:history", "mpim:history")); err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err))
	}
	var res result
	if opts.Thread != "" {
		res, err = threadHistory(cmd.Context(), client, channel, opts.Thread, opts.MaxItems, opts.Cursor)
	} else {
		res, err = channelHistory(cmd.Context(), client, channel, opts)
	}
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err))
	}

	return ctx.WriteResultWithPagination("history.list", Data{Messages: res.Messages}, &clioutput.Pagination{
		Cursor:        cliutil.StringPtr(opts.Cursor),
		NextCursor:    res.NextCursor,
		HasMore:       res.NextCursor != nil,
		MaxItems:      cliutil.IntPtr(opts.MaxItems),
		ItemsReturned: cliutil.IntPtr(len(res.Messages)),
	})
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
	messages := cliMessagesFromSlack(resp.Messages, channel)
	messages = filterMessagesByUser(messages, opts.User)
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
			thread, err := threadHistory(ctx, client, channel, messages[i].TS, replyLimitVal, "")
			if err != nil {
				return result{}, err
			}
			messages[i].Replies = repliesOnly(messages[i].TS, thread.Messages)
		}
	}
	return result{Messages: messages, NextCursor: cliutil.StringPtr(resp.ResponseMetaData.NextCursor)}, nil
}

func threadHistory(ctx context.Context, client *slackgo.Client, channel, threadTS string, maxItems int, cursor string) (result, error) {
	messages, _, nextCursor, err := client.GetConversationRepliesContext(ctx, &slackgo.GetConversationRepliesParameters{
		ChannelID: channel,
		Timestamp: threadTS,
		Cursor:    cursor,
		Limit:     maxItems,
	})
	if err != nil {
		return result{}, err
	}
	return result{Messages: cliMessagesFromSlack(messages, channel), NextCursor: cliutil.StringPtr(nextCursor)}, nil
}

func cliMessagesFromSlack(messages []slackgo.Message, channel string) []clioutput.CliMessage {
	out := make([]clioutput.CliMessage, 0, len(messages))
	for _, message := range messages {
		out = append(out, clioutput.CliMessageFromSlack(message, channel))
	}
	return out
}

func filterMessagesByUser(messages []clioutput.CliMessage, user string) []clioutput.CliMessage {
	if user == "" {
		return messages
	}
	out := make([]clioutput.CliMessage, 0, len(messages))
	for _, message := range messages {
		if message.User != nil && *message.User == user {
			out = append(out, message)
		}
	}
	return out
}

func repliesOnly(parent string, messages []clioutput.CliMessage) []clioutput.CliMessage {
	out := make([]clioutput.CliMessage, 0, len(messages))
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
