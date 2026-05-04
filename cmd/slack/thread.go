package main

import (
	"context"
	"strings"

	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

func newThreadCommand(runtime *RootRuntime) *cobra.Command {
	threadCmd := &cobra.Command{
		Use:   "thread",
		Short: "Manage Slack threads",
	}

	var source messageSource
	var dryRun bool
	replyCmd := &cobra.Command{
		Use:          "reply",
		Short:        "Reply to a Slack thread",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runThreadReply(cmd, runtime, source, dryRun)
		},
	}
	replyCmd.Flags().String("channel", "", "Channel ID, name, or alias")
	replyCmd.Flags().String("parent", "", "Parent message timestamp")
	replyCmd.Flags().StringVar(&source.Message, "message", "", "Message body")
	replyCmd.Flags().StringVar(&source.File, "file", "", "Read message body from file or - for stdin")
	replyCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview without sending")
	threadCmd.AddCommand(replyCmd)

	return threadCmd
}

func runThreadReply(cmd *cobra.Command, runtime *RootRuntime, source messageSource, dryRun bool) error {
	ctx, profile, attribution, err := commandContext(cmd, runtime)
	if err != nil {
		return writeRuntimeError(runtime, validationCLIError(err.Error()))
	}

	channel, _ := cmd.Flags().GetString("channel")
	if strings.TrimSpace(channel) == "" {
		return writeCommandError(ctx, validationCLIError("channel is required"))
	}
	parent, _ := cmd.Flags().GetString("parent")
	if strings.TrimSpace(parent) == "" {
		return writeCommandError(ctx, validationCLIError("parent is required"))
	}

	content, err := readMessageSource(runtime.Stdin, source)
	if err != nil {
		return writeCommandError(ctx, validationCLIError(err.Error()))
	}
	blocks, err := composeBlocks(content, isRawMode(cmd), attribution)
	if err != nil {
		return writeCommandError(ctx, validationCLIError(err.Error()))
	}

	client, err := slackClient(cmd, profile, runtime)
	if err != nil {
		return writeCommandError(ctx, authCLIError(err.Error()))
	}
	result := sendCommandData{Attribution: attribution.Enabled}
	if dryRun {
		result.Message = cliMessage{Type: "message", TS: "dry-run", Channel: stringPtr(channel), Text: stringPtr(strings.TrimSpace(content)), ThreadTS: stringPtr(parent)}
		result.DryRun = true
	} else {
		options := append(messageOptions(content, blocks, attribution), slackgo.MsgOptionTS(parent))
		respChannel, ts, err := client.PostMessageContext(context.Background(), channel, options...)
		if err != nil {
			return writeCommandError(ctx, cliErrorFromSlack(err))
		}
		result.Message = cliMessage{Type: "message", TS: ts, Channel: stringPtr(respChannel), Text: stringPtr(strings.TrimSpace(content)), ThreadTS: stringPtr(parent)}
		result.Permalink = permalink(context.Background(), client, respChannel, ts)
	}

	return writeSendResult(ctx, "thread.reply", result)
}
