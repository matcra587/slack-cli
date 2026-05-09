package main

import (
	"context"
	"strings"

	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

type historyCommandData struct {
	Messages []cliMessage `json:"messages"`
}

var _ PlainRenderer = historyCommandData{}

func (d historyCommandData) WritePlain(c *CommandContext, command string, pagination *Pagination) error {
	return c.WriteMessages(command, d.Messages, pagination)
}

func newHistoryCommand(runtime *RootRuntime) *cobra.Command {
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
			return runHistoryList(cmd, runtime, historyListOptions{
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

type historyListOptions struct {
	MaxItems       int
	Since          string
	Until          string
	User           string
	Thread         string
	Cursor         string
	IncludeReplies bool
	ReplyLimit     int
}

func runHistoryList(cmd *cobra.Command, runtime *RootRuntime, opts historyListOptions) error {
	ctx, profile, _, err := commandContext(cmd, runtime)
	if err != nil {
		return writeRuntimeError(runtime, validationCLIError(err.Error()))
	}

	channel, _ := cmd.Flags().GetString("channel")
	if strings.TrimSpace(channel) == "" {
		return writeCommandError(ctx, validationCLIError("channel is required"))
	}

	client, err := slackClient(cmd, profile, runtime)
	if err != nil {
		return writeCommandError(ctx, authCLIError(err.Error()))
	}
	if err := requireSlackScopes(cmd.Context(), client, anyScope("channels:history", "groups:history", "im:history", "mpim:history")); err != nil {
		return writeCommandError(ctx, cliErrorFromSlack(cmd.Context(), err))
	}
	var result historyResult
	if opts.Thread != "" {
		result, err = threadHistory(cmd.Context(), client, channel, opts.Thread, opts.MaxItems, opts.Cursor)
	} else {
		result, err = channelHistory(cmd.Context(), client, channel, opts)
	}
	if err != nil {
		return writeCommandError(ctx, cliErrorFromSlack(cmd.Context(), err))
	}

	return ctx.WriteResultWithPagination("history.list", historyCommandData{Messages: result.Messages}, &Pagination{
		Cursor:        stringPtr(opts.Cursor),
		NextCursor:    result.NextCursor,
		HasMore:       result.NextCursor != nil,
		MaxItems:      intPtr(opts.MaxItems),
		ItemsReturned: intPtr(len(result.Messages)),
	})
}

type historyResult struct {
	Messages   []cliMessage
	NextCursor *string
}

func channelHistory(ctx context.Context, client *slackgo.Client, channel string, opts historyListOptions) (historyResult, error) {
	resp, err := client.GetConversationHistoryContext(ctx, &slackgo.GetConversationHistoryParameters{
		ChannelID: channel,
		Cursor:    opts.Cursor,
		Limit:     opts.MaxItems,
		Oldest:    opts.Since,
		Latest:    opts.Until,
	})
	if err != nil {
		return historyResult{}, err
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
			replyLimit := 0
			if opts.ReplyLimit > 0 {
				replyLimit = opts.ReplyLimit + 1
			}
			thread, err := threadHistory(ctx, client, channel, messages[i].TS, replyLimit, "")
			if err != nil {
				return historyResult{}, err
			}
			messages[i].Replies = repliesOnly(messages[i].TS, thread.Messages)
		}
	}
	return historyResult{Messages: messages, NextCursor: stringPtr(resp.ResponseMetaData.NextCursor)}, nil
}

func threadHistory(ctx context.Context, client *slackgo.Client, channel, threadTS string, maxItems int, cursor string) (historyResult, error) {
	messages, _, nextCursor, err := client.GetConversationRepliesContext(ctx, &slackgo.GetConversationRepliesParameters{
		ChannelID: channel,
		Timestamp: threadTS,
		Cursor:    cursor,
		Limit:     maxItems,
	})
	if err != nil {
		return historyResult{}, err
	}
	return historyResult{Messages: cliMessagesFromSlack(messages, channel), NextCursor: stringPtr(nextCursor)}, nil
}

func cliMessagesFromSlack(messages []slackgo.Message, channel string) []cliMessage {
	out := make([]cliMessage, 0, len(messages))
	for _, message := range messages {
		out = append(out, cliMessageFromSlack(message, channel))
	}
	return out
}

func filterMessagesByUser(messages []cliMessage, user string) []cliMessage {
	if user == "" {
		return messages
	}
	out := make([]cliMessage, 0, len(messages))
	for _, message := range messages {
		if message.User != nil && *message.User == user {
			out = append(out, message)
		}
	}
	return out
}

func repliesOnly(parent string, messages []cliMessage) []cliMessage {
	out := make([]cliMessage, 0, len(messages))
	for _, message := range messages {
		if message.TS == parent {
			continue
		}
		out = append(out, message)
	}
	return out
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func intPtr(value int) *int {
	if value <= 0 {
		return nil
	}
	return &value
}
