package main

import (
	"strings"

	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

func newReplyCommand(runtime *RootRuntime) *cobra.Command {
	var source messageSource
	var dryRun bool
	replyCmd := &cobra.Command{
		Use:   "reply",
		Short: "Reply to a Slack thread",
		Example: `  # Reply to a thread with a message
  $ slick reply --channel <channel-id> --parent <parent-message-ts> --message <markdown> --json

  # Reply to a thread from stdin
  $ printf '%s\n' "$reply" | slick reply --channel <channel-id> --parent <parent-message-ts> --file - --json`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runThreadReply(cmd, runtime, source, dryRun)
		},
	}
	replyCmd.Flags().StringP("channel", "c", "", "Channel ID, name, or alias")
	replyCmd.Flags().StringP("parent", "p", "", "Parent message timestamp")
	replyCmd.Flags().StringVarP(&source.Message, "message", "m", "", "Message body")
	replyCmd.Flags().StringVarP(&source.File, "file", "f", "", "Read message body from file or - for stdin")
	replyCmd.Flags().BoolVarP(&source.Blocks, "blocks", "b", false, "Treat message source as raw Block Kit JSON")
	replyCmd.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "Preview without sending")
	return replyCmd
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
	blocks, err := composeBlocks(content, source.Blocks, attribution)
	if err != nil {
		return writeCommandError(ctx, validationCLIError(err.Error()))
	}

	result := sendCommandData{Attribution: attribution.Enabled}
	if dryRun {
		result.Message = cliMessage{Type: "message", TS: "dry-run", Channel: stringPtr(channel), Text: stringPtr(strings.TrimSpace(content)), ThreadTS: stringPtr(parent)}
		result.DryRun = true
	} else {
		client, err := slackClient(cmd, profile, runtime)
		if err != nil {
			return writeCommandError(ctx, authCLIError(err.Error()))
		}
		if err := requireSlackScopes(cmd.Context(), client, allScopes("chat:write")); err != nil {
			return writeCommandError(ctx, cliErrorFromSlack(cmd.Context(), err))
		}
		options := append(messageOptions(content, blocks, attribution), slackgo.MsgOptionTS(parent))
		respChannel, ts, err := client.PostMessageContext(cmd.Context(), channel, options...)
		if err != nil {
			return writeCommandError(ctx, cliErrorFromSlack(cmd.Context(), err))
		}
		result.Message = cliMessage{Type: "message", TS: ts, Channel: stringPtr(respChannel), Text: stringPtr(strings.TrimSpace(content)), ThreadTS: stringPtr(parent)}
		result.Permalink = permalink(cmd.Context(), client, respChannel, ts)
	}

	return writeSendResult(ctx, "reply", result)
}
