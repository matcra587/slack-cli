package reaction

import (
	"strings"

	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	cliscope "github.com/matcra587/slack-cli/internal/cli/scope"
	slackclient "github.com/matcra587/slack-cli/internal/cli/slackclient"
	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

// Target identifies the message a reaction operates on.
type Target struct {
	Channel   string `json:"channel"`
	Timestamp string `json:"timestamp"`
}

// Result is the outcome of a single reaction add/remove operation.
type Result struct {
	Channel   string `json:"channel"`
	Timestamp string `json:"timestamp"`
	Emoji     string `json:"emoji,omitempty"`
	Removed   bool   `json:"removed,omitempty"`
	DryRun    bool   `json:"dry_run,omitempty"`
}

// Data is the result type for all react subcommands.
type Data struct {
	Reaction  *Result                        `json:"reaction,omitempty"`
	Reactions []clioutput.CliReactionSummary `json:"reactions,omitempty"`
	Target    Target                         `json:"target"`
}

var _ clioutput.PlainRenderer = Data{}

func (d Data) WritePlain(c *clioutput.CommandContext, command string, _ *clioutput.Pagination) error {
	if d.Reaction != nil {
		event := c.ResultEventWithStyles(command, clioutput.EntityFieldStyle("channel", d.Reaction.Channel)).
			Str("channel", d.Reaction.Channel)
		event = clioutput.AddSlackTimestampFields(event, d.Reaction.Timestamp, c.Now()).
			Str("emoji", d.Reaction.Emoji).
			Bool("removed", d.Reaction.Removed).
			Bool("dry_run", d.Reaction.DryRun)
		event.Send()
		return nil
	}
	if len(d.Reactions) > 0 {
		return c.WriteReactionTable(d.Reactions)
	}
	event := c.ResultEventWithStyles(command, clioutput.EntityFieldStyle("channel", d.Target.Channel)).
		Str("channel", d.Target.Channel)
	clioutput.AddSlackTimestampFields(event, d.Target.Timestamp, c.Now()).
		Send()
	return nil
}

// NewCommand returns the "react" cobra command tree.
func NewCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	reactCmd := &cobra.Command{
		Use:   "react",
		Short: "Manage Slack reactions",
	}
	reactCmd.AddCommand(newReactionMutationCommand(runtime, "add"))
	reactCmd.AddCommand(newReactionMutationCommand(runtime, "remove"))
	reactCmd.AddCommand(newReactionListCommand(runtime))
	return reactCmd
}

func newReactionMutationCommand(runtime *cliruntime.RootRuntime, action string) *cobra.Command {
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

func newReactionListCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
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

func runReactionMutation(cmd *cobra.Command, runtime *cliruntime.RootRuntime, action string, dryRun bool) error {
	ctx, profile, _, err := cliruntime.CommandContext(cmd, runtime)
	if err != nil {
		return cliruntime.WriteRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
	}

	target, err := reactionTargetFromFlags(cmd)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
	}
	emoji, _ := cmd.Flags().GetString("emoji")
	emoji = normalizeEmoji(emoji)
	if emoji == "" {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("emoji is required"))
	}

	result := Result{Channel: target.Channel, Timestamp: target.Timestamp, Emoji: emoji}
	if dryRun {
		result.DryRun = true
		result.Removed = action == "remove"
		return ctx.WriteResult("react."+action, Data{Reaction: &result, Target: target})
	}
	client, err := slackclient.Client(cmd, profile, runtime)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.AuthCLIError(err.Error()))
	}
	if err := cliscope.Require(cmd.Context(), client, cliscope.AllOf("reactions:write")); err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err))
	}
	switch action {
	case "remove":
		result.Removed = true
		err = client.RemoveReactionContext(cmd.Context(), emoji, slackgo.NewRefToMessage(target.Channel, target.Timestamp))
	default:
		err = client.AddReactionContext(cmd.Context(), emoji, slackgo.NewRefToMessage(target.Channel, target.Timestamp))
	}
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err))
	}
	return ctx.WriteResult("react."+action, Data{Reaction: &result, Target: target})
}

func runReactionList(cmd *cobra.Command, runtime *cliruntime.RootRuntime) error {
	ctx, profile, _, err := cliruntime.CommandContext(cmd, runtime)
	if err != nil {
		return cliruntime.WriteRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
	}
	target, err := reactionTargetFromFlags(cmd)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
	}

	client, err := slackclient.Client(cmd, profile, runtime)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.AuthCLIError(err.Error()))
	}
	if err := cliscope.Require(cmd.Context(), client, cliscope.AllOf("reactions:read")); err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err))
	}
	item, err := client.GetReactionsContext(cmd.Context(), slackgo.NewRefToMessage(target.Channel, target.Timestamp), slackgo.GetReactionsParameters{})
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err))
	}
	return ctx.WriteResult("react.list", Data{Reactions: clioutput.CliReactionsFromSlack(item.Reactions), Target: target})
}

func reactionTargetFromFlags(cmd *cobra.Command) (Target, error) {
	channel, _ := cmd.Flags().GetString("channel")
	if strings.TrimSpace(channel) == "" {
		return Target{}, errString("channel is required")
	}
	timestamp, _ := cmd.Flags().GetString("timestamp")
	if strings.TrimSpace(timestamp) == "" {
		return Target{}, errString("timestamp is required")
	}
	return Target{Channel: channel, Timestamp: timestamp}, nil
}

func normalizeEmoji(value string) string {
	return strings.Trim(strings.TrimSpace(value), ":")
}

type errString string

func (e errString) Error() string { return string(e) }
