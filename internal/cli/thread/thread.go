package thread

import (
	"strings"

	"github.com/matcra587/slack-cli/internal/cli/cliutil"
	climessage "github.com/matcra587/slack-cli/internal/cli/message"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	cliscope "github.com/matcra587/slack-cli/internal/cli/scope"
	slackclient "github.com/matcra587/slack-cli/internal/cli/slackclient"
	"github.com/matcra587/slack-cli/internal/cli/slackmeta"
	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

// NewCommand returns the "reply" cobra command.
func NewCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	var src climessage.Source
	replyCmd := &cobra.Command{
		Use:   "reply",
		Short: "Reply to a Slack thread",
		Args:  cobra.NoArgs,
		Example: `  # Reply to a thread with a message
  $ slick reply --channel <channel-id> --parent <parent-message-ts> --message <markdown> --output=json

  # Reply to a thread from stdin
  $ printf '%s\n' "$reply" | slick reply --channel <channel-id> --parent <parent-message-ts> --file - --output=json`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runThreadReply(cmd, runtime, src, cliruntime.DryRun(cmd))
		},
	}
	replyCmd.Flags().StringP("channel", "c", "", "Channel ID or configured alias")
	replyCmd.Flags().StringP("parent", "p", "", "Parent message timestamp")
	replyCmd.Flags().StringVarP(&src.Message, "message", "m", "", "Message body")
	replyCmd.Flags().StringVarP(&src.File, "file", "f", "", "Read message body from file or - for stdin")
	replyCmd.Flags().BoolVarP(&src.Blocks, "blocks", "b", false, "Treat message source as raw Block Kit JSON")
	cliruntime.RegisterAttributionFlags(replyCmd)
	return replyCmd
}

func runThreadReply(cmd *cobra.Command, runtime *cliruntime.RootRuntime, src climessage.Source, dryRun bool) error {
	ctx, profile, attribution, err := cliruntime.CommandContext(cmd, runtime)
	if err != nil {
		return cliruntime.WriteRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
	}

	channel, _ := cmd.Flags().GetString("channel")
	channel = strings.TrimSpace(climessage.ResolveAlias(profile, strings.TrimSpace(channel)))
	if strings.TrimSpace(channel) == "" {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("channel is required"))
	}
	parent, _ := cmd.Flags().GetString("parent")
	parent = strings.TrimSpace(parent)
	if parent == "" {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("parent is required"))
	}

	content, err := climessage.ReadMessageSource(runtime.Stdin, src)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
	}
	blocks, err := climessage.ComposeBlocks(content, src.Blocks, attribution)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
	}
	if src.Blocks && len(blocks) == 0 {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("new messages require at least one block"))
	}

	result := climessage.SendData{Attribution: attribution.Enabled}
	if dryRun {
		result.Message = clioutput.Message{
			Type:     "message",
			TS:       "dry-run",
			Channel:  cliutil.StringPtr(channel),
			Text:     cliutil.StringPtr(strings.TrimSpace(content)),
			ThreadTS: cliutil.StringPtr(parent),
		}
		if src.Blocks {
			result.Message.Text = nil
			result.Message.Blocks = &slackgo.Blocks{BlockSet: blocks}
		}
		result.DryRun = true
		result.ChannelRef = slackmeta.ResolveConversation(cmd.Context(), nil, profile.Name, channel)
	} else {
		client, err := slackclient.Client(cmd, profile, runtime)
		if err != nil {
			return clioutput.WriteCommandError(ctx, slackclient.CLIError(cmd.Context(), err))
		}
		if err := cliscope.Require(cmd.Context(), client, cliscope.AllOf("chat:write")); err != nil {
			return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err, ""))
		}
		options := append(climessage.MessageOptions(content, blocks, src.Blocks), slackgo.MsgOptionTS(parent))
		respChannel, ts, sent, err := client.PostMessageWithResponseContext(cmd.Context(), channel, options...)
		if err != nil {
			return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err, ""))
		}
		sent.Type = cliutil.FirstNonEmpty(sent.Type, "message")
		sent.Timestamp = cliutil.FirstNonEmpty(sent.Timestamp, ts)
		result.Message = clioutput.MessageFromSlack(sent, respChannel)
		result.Permalink = climessage.Permalink(cmd.Context(), client, respChannel, ts)
		result.ChannelRef = slackmeta.ResolveConversation(cmd.Context(), client, profile.Name, respChannel)
	}

	return ctx.WriteResult("reply", result)
}
