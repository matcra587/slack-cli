package main

import (
	"context"
	"strings"

	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

type channelListData struct {
	Channels []cliChannel `json:"channels"`
}

type channelInfoData struct {
	Channel cliChannel `json:"channel"`
}

func newChannelCommand(runtime *RootRuntime) *cobra.Command {
	channelCmd := &cobra.Command{
		Use:   "channel",
		Short: "Discover Slack channels",
	}

	var maxItems int
	var cursor string
	var filter string
	listCmd := &cobra.Command{
		Use:          "list",
		Short:        "List Slack channels",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runChannelList(cmd, runtime, maxItems, cursor, filter)
		},
	}
	listCmd.Flags().IntVar(&maxItems, "max-items", 0, "Maximum channels to return")
	listCmd.Flags().StringVar(&cursor, "cursor", "", "Pagination cursor")
	listCmd.Flags().StringVar(&filter, "filter", "", "Filter by ID or name")

	infoCmd := &cobra.Command{
		Use:          "info",
		Short:        "Show Slack channel metadata",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runChannelInfo(cmd, runtime)
		},
	}
	infoCmd.Flags().String("channel", "", "Channel ID, name, or alias")

	channelCmd.AddCommand(listCmd)
	channelCmd.AddCommand(infoCmd)
	return channelCmd
}

func runChannelList(cmd *cobra.Command, runtime *RootRuntime, maxItems int, cursor string, filter string) error {
	ctx, profile, _, err := commandContext(cmd, runtime)
	if err != nil {
		return writeRuntimeError(runtime, validationCLIError(err.Error()))
	}
	client, err := slackClient(cmd, profile, runtime)
	if err != nil {
		return writeCommandError(ctx, authCLIError(err.Error()))
	}

	channels, nextCursor, err := client.GetConversationsContext(context.Background(), &slackgo.GetConversationsParameters{
		Types:  []string{"public_channel", "private_channel"},
		Limit:  maxItems,
		Cursor: cursor,
	})
	if err != nil {
		return writeCommandError(ctx, cliErrorFromSlack(err))
	}
	result := filterChannels(cliChannelsFromSlack(channels), filter)
	return ctx.WriteResultWithPagination("channel.list", channelListData(result), &Pagination{
		Cursor:        stringPtr(cursor),
		NextCursor:    stringPtr(nextCursor),
		HasMore:       nextCursor != "",
		MaxItems:      intPtr(maxItems),
		ItemsReturned: intPtr(len(result.Channels)),
	})
}

func runChannelInfo(cmd *cobra.Command, runtime *RootRuntime) error {
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
	result, err := client.GetConversationInfoContext(context.Background(), &slackgo.GetConversationInfoInput{
		ChannelID:         channel,
		IncludeNumMembers: true,
	})
	if err != nil {
		return writeCommandError(ctx, cliErrorFromSlack(err))
	}
	return ctx.WriteResult("channel.info", channelInfoData{Channel: cliChannelFromSlack(*result)})
}

type channelListResult struct {
	Channels []cliChannel
}

func cliChannelsFromSlack(channels []slackgo.Channel) channelListResult {
	out := make([]cliChannel, 0, len(channels))
	for _, channel := range channels {
		out = append(out, cliChannelFromSlack(channel))
	}
	return channelListResult{Channels: out}
}

func filterChannels(result channelListResult, filter string) channelListResult {
	if filter == "" {
		return result
	}
	filter = strings.ToLower(filter)
	var out []cliChannel
	for _, channel := range result.Channels {
		if strings.Contains(strings.ToLower(channel.ID), filter) || strings.Contains(strings.ToLower(channel.Name), filter) {
			out = append(out, channel)
		}
	}
	return channelListResult{Channels: out}
}
