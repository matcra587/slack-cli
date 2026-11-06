package reaction

import (
	"strings"
	"time"

	"github.com/matcra587/slack-cli/api"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	cliscope "github.com/matcra587/slack-cli/internal/cli/scope"
	"github.com/matcra587/slack-cli/internal/cli/slackclient"
	"github.com/matcra587/slack-cli/internal/cli/slackmeta"
	"github.com/spf13/cobra"
)

func newReactionWatchCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	var emoji, user string
	var interval time.Duration
	cmd := &cobra.Command{
		Use: "watch", Short: "Wait for an emoji reaction on a message", Args: cobra.NoArgs,
		Long:    "Wait until a message has the requested emoji reaction. Existing matches succeed immediately. Use --timeout to bound the wait. A user filter succeeds only when Slack includes that user in the reaction response.",
		Example: "  slick react watch --channel C123 --timestamp 1746284582.123456 --emoji :+1: --timeout 10m --output=json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, profile, _, err := cliruntime.CommandContext(cmd, runtime)
			if err != nil {
				return cliruntime.WriteRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
			}
			target, err := reactionTargetFromFlags(cmd, profile)
			if err != nil {
				return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
			}
			if len(normalizeEmojiList([]string{emoji})) != 1 || strings.ContainsAny(normalizeEmoji(emoji), ", \t\r\n") {
				return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("one emoji is required"))
			}
			if interval < time.Second {
				return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("watch interval must be at least 1s"))
			}
			client, err := slackclient.Client(cmd, profile, runtime)
			if err != nil {
				return clioutput.WriteCommandError(ctx, slackclient.CLIError(cmd.Context(), err))
			}
			if err := cliscope.Require(cmd.Context(), client, cliscope.AllOf("reactions:read")); err != nil {
				return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err, ""))
			}
			match, err := api.WatchReaction(cmd.Context(), client, api.ReactionWatchOptions{
				Channel: target.Channel, Timestamp: target.TS, Emoji: emoji, User: user, Interval: interval,
			})
			if err != nil {
				return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err, ""))
			}
			target.ChannelRef = slackmeta.ResolveConversation(cmd.Context(), client, profile.Name, target.Channel)
			return ctx.WriteResult("react.watch", Data{
				Target: target, Reactions: []clioutput.ReactionSummary{{Name: match.Emoji, Count: match.Count, Users: match.Users}},
			})
		},
	}
	cmd.Flags().StringP("channel", "c", "", "Channel ID or configured alias")
	cmd.Flags().StringP("timestamp", "t", "", "Message timestamp")
	cmd.Flags().StringVarP(&emoji, "emoji", "e", "", "Emoji to wait for, such as :+1:")
	cmd.Flags().StringVarP(&user, "user", "U", "", "Require this reacting Slack user ID")
	cmd.Flags().DurationVarP(&interval, "interval", "i", 5*time.Second, "Polling interval, at least 1s")
	return cmd
}
