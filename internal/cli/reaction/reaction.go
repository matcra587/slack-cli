package reaction

import (
	"strings"

	"github.com/gechr/clog"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	cliscope "github.com/matcra587/slack-cli/internal/cli/scope"
	slackclient "github.com/matcra587/slack-cli/internal/cli/slackclient"
	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

// isDMChannel reports whether the channel id refers to a direct message
// (D-prefix). DM channel ids are opaque to humans, so we hide them in
// plain-mode output unless --debug is set.
func isDMChannel(id string) bool { return strings.HasPrefix(id, "D") }

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

// Data is the result type for all react subcommands. Mutations holds the
// per-emoji outcomes for `react add`/`react remove` (length 1 for the
// single-emoji case, length N for ordered multi-emoji); Reactions holds
// the existing-reaction summary for `react list`.
type Data struct {
	Mutations []Result                       `json:"mutations,omitempty"`
	Reactions []clioutput.CliReactionSummary `json:"reactions,omitempty"`
	Target    Target                         `json:"target"`
}

var _ clioutput.PlainRenderer = Data{}

func (d Data) WritePlain(c *clioutput.CommandContext, command string, _ *clioutput.Pagination) error {
	label := clioutput.ActionLabel(command)
	showChannel := func(id string) bool { return clog.IsVerbose() || !isDMChannel(id) }
	if len(d.Mutations) > 0 {
		for _, mutation := range d.Mutations {
			event := c.ResultEventWithStyles(command, clioutput.EntityFieldStyle("channel", mutation.Channel))
			if showChannel(mutation.Channel) {
				event = event.Str("channel", mutation.Channel)
			}
			clioutput.AddSlackTimestampFields(event, mutation.Timestamp, c.Now()).
				Str("emoji", mutation.Emoji).
				Bool("removed", mutation.Removed).
				Bool("dry_run", mutation.DryRun).
				Msg(label)
		}
		return nil
	}
	if len(d.Reactions) > 0 {
		return c.WriteReactionTable(d.Reactions)
	}
	event := c.ResultEventWithStyles(command, clioutput.EntityFieldStyle("channel", d.Target.Channel))
	if showChannel(d.Target.Channel) {
		event = event.Str("channel", d.Target.Channel)
	}
	clioutput.AddSlackTimestampFields(event, d.Target.Timestamp, c.Now()).
		Msg(label)
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
	cmd.Flags().StringSliceP("emoji", "e", nil, "Emoji name; repeat or comma-separate to apply multiple in order")
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
	rawEmojis, _ := cmd.Flags().GetStringSlice("emoji")
	emojis := normalizeEmojiList(rawEmojis)
	if len(emojis) == 0 {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("emoji is required"))
	}

	if dryRun {
		mutations := make([]Result, 0, len(emojis))
		for _, emoji := range emojis {
			mutations = append(mutations, Result{
				Channel:   target.Channel,
				Timestamp: target.Timestamp,
				Emoji:     emoji,
				Removed:   action == "remove",
				DryRun:    true,
			})
		}
		return ctx.WriteResult("react."+action, Data{Mutations: mutations, Target: target})
	}

	client, err := slackclient.Client(cmd, profile, runtime)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.AuthCLIError(err.Error()))
	}
	if err := cliscope.Require(cmd.Context(), client, cliscope.AllOf("reactions:write")); err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err))
	}
	mutations := make([]Result, 0, len(emojis))
	for _, emoji := range emojis {
		ref := slackgo.NewRefToMessage(target.Channel, target.Timestamp)
		var apiErr error
		if action == "remove" {
			apiErr = client.RemoveReactionContext(cmd.Context(), emoji, ref)
		} else {
			apiErr = client.AddReactionContext(cmd.Context(), emoji, ref)
		}
		if apiErr != nil {
			return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), apiErr))
		}
		mutations = append(mutations, Result{
			Channel:   target.Channel,
			Timestamp: target.Timestamp,
			Emoji:     emoji,
			Removed:   action == "remove",
		})
	}
	return ctx.WriteResult("react."+action, Data{Mutations: mutations, Target: target})
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

// normalizeEmojiList trims, strips wrapping colons, and drops empty entries
// while preserving the input order. Repeated entries are kept (Slack rejects
// duplicate reactions, but the CLI surfaces that as an error rather than
// silently deduping).
func normalizeEmojiList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		normalized := normalizeEmoji(value)
		if normalized == "" {
			continue
		}
		out = append(out, normalized)
	}
	return out
}

type errString string

func (e errString) Error() string { return string(e) }
