package main

import (
	"strings"

	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

type reactionTarget struct {
	Channel   string `json:"channel"`
	Timestamp string `json:"timestamp"`
}

type reactionResult struct {
	Channel   string `json:"channel"`
	Timestamp string `json:"timestamp"`
	Emoji     string `json:"emoji,omitempty"`
	Removed   bool   `json:"removed,omitempty"`
	DryRun    bool   `json:"dry_run,omitempty"`
}

type reactionCommandData struct {
	Reaction  *reactionResult      `json:"reaction,omitempty"`
	Reactions []cliReactionSummary `json:"reactions,omitempty"`
	Target    reactionTarget       `json:"target"`
}

var _ PlainRenderer = reactionCommandData{}

func (d reactionCommandData) WritePlain(c *CommandContext, command string, _ *Pagination) error {
	if d.Reaction != nil {
		event := c.ResultEventWithStyles(command, entityFieldStyle("channel", d.Reaction.Channel)).
			Str("channel", d.Reaction.Channel)
		event = addSlackTimestampFields(event, d.Reaction.Timestamp, c.Now()).
			Str("emoji", d.Reaction.Emoji).
			Bool("removed", d.Reaction.Removed).
			Bool("dry_run", d.Reaction.DryRun)
		event.Send()
		return nil
	}
	if len(d.Reactions) > 0 {
		return c.WriteReactionTable(d.Reactions)
	}
	if len(d.Reactions) == 0 {
		event := c.ResultEventWithStyles(command, entityFieldStyle("channel", d.Target.Channel)).
			Str("channel", d.Target.Channel)
		addSlackTimestampFields(event, d.Target.Timestamp, c.Now()).
			Send()
		return nil
	}
	for _, reaction := range d.Reactions {
		event := c.ResultEventWithStyles(command, entityFieldStyle("channel", d.Target.Channel)).
			Str("channel", d.Target.Channel)
		event = addSlackTimestampFields(event, d.Target.Timestamp, c.Now()).
			Str("emoji", reaction.Name).
			Int("count", reaction.Count).
			Strs("users", reaction.Users)
		event.Send()
	}
	return nil
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
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runReactionMutation(cmd, runtime, action, dryRun)
		},
	}
	cmd.Flags().StringP("channel", "c", "", "Channel ID, name, or alias")
	cmd.Flags().StringP("timestamp", "t", "", "Message timestamp")
	cmd.Flags().StringP("emoji", "e", "", "Emoji name")
	cmd.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "Preview without mutating")
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
	cmd.Flags().StringP("channel", "c", "", "Channel ID, name, or alias")
	cmd.Flags().StringP("timestamp", "t", "", "Message timestamp")
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
		return writeCommandError(ctx, cliErrorFromSlack(cmd.Context(), err))
	}
	switch action {
	case "remove":
		result.Removed = true
		err = client.RemoveReactionContext(cmd.Context(), emoji, slackgo.NewRefToMessage(target.Channel, target.Timestamp))
	default:
		err = client.AddReactionContext(cmd.Context(), emoji, slackgo.NewRefToMessage(target.Channel, target.Timestamp))
	}
	if err != nil {
		return writeCommandError(ctx, cliErrorFromSlack(cmd.Context(), err))
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
		return writeCommandError(ctx, cliErrorFromSlack(cmd.Context(), err))
	}
	item, err := client.GetReactionsContext(cmd.Context(), slackgo.NewRefToMessage(target.Channel, target.Timestamp), slackgo.GetReactionsParameters{})
	if err != nil {
		return writeCommandError(ctx, cliErrorFromSlack(cmd.Context(), err))
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
