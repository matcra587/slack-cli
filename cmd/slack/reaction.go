package main

import (
	"context"
	"strings"

	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

type reactionCommandData struct {
	Reaction  *reactionResult      `json:"reaction,omitempty"`
	Reactions []cliReactionSummary `json:"reactions,omitempty"`
	Target    reactionTarget       `json:"target"`
}

func newReactCommand(runtime *RootRuntime) *cobra.Command {
	reactCmd := &cobra.Command{
		Use:   "react",
		Short: "Manage Slack reactions",
	}

	reactCmd.AddCommand(newReactionMutationCommand(runtime, "add"))
	reactCmd.AddCommand(newReactionMutationCommand(runtime, "remove"))
	reactCmd.AddCommand(newReactionListCommand(runtime))
	return reactCmd
}

func newReactionMutationCommand(runtime *RootRuntime, action string) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:          action,
		Short:        action + " a Slack reaction",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runReactionMutation(cmd, runtime, action, dryRun)
		},
	}
	cmd.Flags().String("channel", "", "Channel ID, name, or alias")
	cmd.Flags().String("timestamp", "", "Message timestamp")
	cmd.Flags().String("emoji", "", "Emoji name")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview without mutating")
	return cmd
}

func newReactionListCommand(runtime *RootRuntime) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "list",
		Short:        "List Slack reactions",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runReactionList(cmd, runtime)
		},
	}
	cmd.Flags().String("channel", "", "Channel ID, name, or alias")
	cmd.Flags().String("timestamp", "", "Message timestamp")
	return cmd
}

func runReactionMutation(cmd *cobra.Command, runtime *RootRuntime, action string, dryRun bool) error {
	ctx, profile, _, err := commandContext(cmd, runtime)
	if err != nil {
		return writeRuntimeError(runtime, validationCLIError(err.Error()))
	}

	target, err := reactionTargetFromFlags(cmd)
	if err != nil {
		return writeCommandError(ctx, validationCLIError(err.Error()))
	}
	emoji, _ := cmd.Flags().GetString("emoji")
	emoji = normalizeEmoji(emoji)
	if emoji == "" {
		return writeCommandError(ctx, validationCLIError("emoji is required"))
	}

	result := reactionResult{Channel: target.Channel, Timestamp: target.Timestamp, Emoji: emoji}
	if dryRun {
		result.DryRun = true
		result.Removed = action == "remove"
		return ctx.WriteResult("react."+action, reactionCommandData{Reaction: &result, Target: target})
	}
	client, err := slackClient(cmd, profile, runtime)
	if err != nil {
		return writeCommandError(ctx, authCLIError(err.Error()))
	}
	if err := requireSlackScopes(cmd.Context(), client, allScopes("reactions:write")); err != nil {
		return writeCommandError(ctx, cliErrorFromSlack(err))
	}
	switch action {
	case "remove":
		result.Removed = true
		err = client.RemoveReactionContext(context.Background(), emoji, slackgo.NewRefToMessage(target.Channel, target.Timestamp))
	default:
		err = client.AddReactionContext(context.Background(), emoji, slackgo.NewRefToMessage(target.Channel, target.Timestamp))
	}
	if err != nil {
		return writeCommandError(ctx, cliErrorFromSlack(err))
	}
	return ctx.WriteResult("react."+action, reactionCommandData{Reaction: &result, Target: target})
}

func runReactionList(cmd *cobra.Command, runtime *RootRuntime) error {
	ctx, profile, _, err := commandContext(cmd, runtime)
	if err != nil {
		return writeRuntimeError(runtime, validationCLIError(err.Error()))
	}
	target, err := reactionTargetFromFlags(cmd)
	if err != nil {
		return writeCommandError(ctx, validationCLIError(err.Error()))
	}

	client, err := slackClient(cmd, profile, runtime)
	if err != nil {
		return writeCommandError(ctx, authCLIError(err.Error()))
	}
	if err := requireSlackScopes(cmd.Context(), client, allScopes("reactions:read")); err != nil {
		return writeCommandError(ctx, cliErrorFromSlack(err))
	}
	item, err := client.GetReactionsContext(context.Background(), slackgo.NewRefToMessage(target.Channel, target.Timestamp), slackgo.GetReactionsParameters{})
	if err != nil {
		return writeCommandError(ctx, cliErrorFromSlack(err))
	}
	return ctx.WriteResult("react.list", reactionCommandData{Reactions: cliReactionsFromSlack(item.Reactions), Target: target})
}

func reactionTargetFromFlags(cmd *cobra.Command) (reactionTarget, error) {
	channel, _ := cmd.Flags().GetString("channel")
	if strings.TrimSpace(channel) == "" {
		return reactionTarget{}, errString("channel is required")
	}
	timestamp, _ := cmd.Flags().GetString("timestamp")
	if strings.TrimSpace(timestamp) == "" {
		return reactionTarget{}, errString("timestamp is required")
	}
	return reactionTarget{Channel: channel, Timestamp: timestamp}, nil
}

func normalizeEmoji(value string) string {
	return strings.Trim(strings.TrimSpace(value), ":")
}

type errString string

func (e errString) Error() string {
	return string(e)
}
