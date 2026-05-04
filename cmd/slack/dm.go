package main

import (
	"context"
	"strings"

	"github.com/matcra587/slack-cli/internal/config"
	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

func newDMCommand(runtime *RootRuntime) *cobra.Command {
	dmCmd := &cobra.Command{
		Use:   "dm",
		Short: "Manage Slack direct messages",
	}

	var maxItems int
	var cursor string
	listCmd := &cobra.Command{
		Use:          "list",
		Short:        "List direct message conversations",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDMList(cmd, runtime, maxItems, cursor)
		},
	}
	listCmd.Flags().IntVar(&maxItems, "max-items", 0, "Maximum DMs to return")
	listCmd.Flags().StringVar(&cursor, "cursor", "", "Pagination cursor")
	dmCmd.AddCommand(listCmd)

	var source messageSource
	var dryRun bool
	sendCmd := &cobra.Command{
		Use:          "send",
		Short:        "Send a direct message",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDMSend(cmd, runtime, source, dryRun)
		},
	}
	sendCmd.Flags().String("user", "", "Slack user ID or alias")
	sendCmd.Flags().StringVar(&source.Message, "message", "", "Message body")
	sendCmd.Flags().StringVar(&source.File, "file", "", "Read message body from file or - for stdin")
	sendCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview without sending")
	dmCmd.AddCommand(sendCmd)

	return dmCmd
}

func runDMList(cmd *cobra.Command, runtime *RootRuntime, maxItems int, cursor string) error {
	ctx, profile, _, err := commandContext(cmd, runtime)
	if err != nil {
		return writeRuntimeError(runtime, validationCLIError(err.Error()))
	}
	client, err := slackClient(cmd, profile, runtime)
	if err != nil {
		return writeCommandError(ctx, authCLIError(err.Error()))
	}

	channels, nextCursor, err := client.GetConversationsContext(context.Background(), &slackgo.GetConversationsParameters{
		Types:  []string{"im"},
		Limit:  maxItems,
		Cursor: cursor,
	})
	if err != nil {
		return writeCommandError(ctx, cliErrorFromSlack(err))
	}
	result := cliChannelsFromSlack(channels)
	return ctx.WriteResultWithPagination("dm.list", channelListData(result), &Pagination{
		Cursor:        stringPtr(cursor),
		NextCursor:    stringPtr(nextCursor),
		HasMore:       nextCursor != "",
		MaxItems:      intPtr(maxItems),
		ItemsReturned: intPtr(len(result.Channels)),
	})
}

func runDMSend(cmd *cobra.Command, runtime *RootRuntime, source messageSource, dryRun bool) error {
	ctx, profile, attribution, err := commandContext(cmd, runtime)
	if err != nil {
		return writeRuntimeError(runtime, validationCLIError(err.Error()))
	}
	if profile.TokenType == config.TokenTypeBot {
		return writeCommandError(ctx, validationCLIError("bot tokens cannot initiate arbitrary DM conversations"))
	}

	user, _ := cmd.Flags().GetString("user")
	if strings.TrimSpace(user) == "" {
		return writeCommandError(ctx, validationCLIError("user is required"))
	}

	content, err := readMessageSource(runtime.Stdin, source)
	if err != nil {
		return writeCommandError(ctx, validationCLIError(err.Error()))
	}
	blocks, err := composeBlocks(content, isRawMode(cmd), attribution)
	if err != nil {
		return writeCommandError(ctx, validationCLIError(err.Error()))
	}

	if dryRun {
		return writeSendResult(ctx, "dm.send", sendCommandData{
			Message:     cliMessage{Type: "message", TS: "dry-run", Channel: stringPtr(user), Text: stringPtr(strings.TrimSpace(content))},
			DryRun:      true,
			Attribution: attribution.Enabled,
		})
	}

	client, err := slackClient(cmd, profile, runtime)
	if err != nil {
		return writeCommandError(ctx, authCLIError(err.Error()))
	}

	opened, _, _, err := client.OpenConversationContext(context.Background(), &slackgo.OpenConversationParameters{
		Users:    []string{user},
		ReturnIM: true,
	})
	if err != nil {
		return writeCommandError(ctx, cliErrorFromSlack(err))
	}

	channel, ts, err := client.PostMessageContext(context.Background(), opened.ID, messageOptions(content, blocks, attribution)...)
	if err != nil {
		return writeCommandError(ctx, cliErrorFromSlack(err))
	}
	result := sendCommandData{Attribution: attribution.Enabled}
	result.Message = cliMessage{Type: "message", TS: ts, Channel: stringPtr(channel), Text: stringPtr(strings.TrimSpace(content))}
	result.Permalink = permalink(context.Background(), client, channel, ts)

	return writeSendResult(ctx, "dm.send", result)
}
