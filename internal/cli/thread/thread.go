package thread

import (
	"strings"

	"github.com/matcra587/slack-cli/internal/cli/cliutil"
	climessage "github.com/matcra587/slack-cli/internal/cli/message"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	cliscope "github.com/matcra587/slack-cli/internal/cli/scope"
	slackclient "github.com/matcra587/slack-cli/internal/cli/slackclient"
	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

// NewCommand returns the "reply" cobra command.
func NewCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	var src climessage.Source
	var dryRun bool
	replyCmd := &cobra.Command{
		Use:   "reply",
		Short: "Reply to a Slack thread",
		Args:  cobra.NoArgs,
		Example: `  # Reply to a thread with a message
  $ slick reply --channel <channel-id> --parent <parent-message-ts> --message <markdown> --json

  # Reply to a thread from stdin
  $ printf '%s\n' "$reply" | slick reply --channel <channel-id> --parent <parent-message-ts> --file - --json`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runThreadReply(cmd, runtime, src, dryRun)
		},
	}
	replyCmd.Flags().StringP("channel", "c", "", "Channel ID, name, or alias")
	replyCmd.Flags().StringP("parent", "p", "", "Parent message timestamp")
	replyCmd.Flags().StringVarP(&src.Message, "message", "m", "", "Message body")
	replyCmd.Flags().StringVarP(&src.File, "file", "f", "", "Read message body from file or - for stdin")
	replyCmd.Flags().BoolVarP(&src.Blocks, "blocks", "b", false, "Treat message source as raw Block Kit JSON")
	replyCmd.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "Preview without sending")
	return replyCmd
}

func runThreadReply(cmd *cobra.Command, runtime *cliruntime.RootRuntime, src climessage.Source, dryRun bool) error {
	ctx, profile, attribution, err := cliruntime.CommandContext(cmd, runtime)
	if err != nil {
		return cliruntime.WriteRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
	}

	channel, _ := cmd.Flags().GetString("channel")
	if strings.TrimSpace(channel) == "" {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("channel is required"))
	}
	parent, _ := cmd.Flags().GetString("parent")
	if strings.TrimSpace(parent) == "" {
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

	result := climessage.SendData{Attribution: attribution.Enabled}
	if dryRun {
		result.Message = clioutput.Message{
			Type:     "message",
			TS:       "dry-run",
			Channel:  cliutil.StringPtr(channel),
			Text:     cliutil.StringPtr(strings.TrimSpace(content)),
			ThreadTS: cliutil.StringPtr(parent),
		}
		result.DryRun = true
	} else {
		client, err := slackclient.Client(cmd, profile, runtime)
		if err != nil {
			return clioutput.WriteCommandError(ctx, clioutput.AuthCLIError(err.Error()))
		}
		if err := cliscope.Require(cmd.Context(), client, cliscope.AllOf("chat:write")); err != nil {
			return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err))
		}
		options := append(climessage.MessageOptions(content, blocks, attribution), slackgo.MsgOptionTS(parent))
		respChannel, ts, err := client.PostMessageContext(cmd.Context(), channel, options...)
		if err != nil {
			return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err))
		}
		result.Message = clioutput.Message{
			Type:     "message",
			TS:       ts,
			Channel:  cliutil.StringPtr(respChannel),
			Text:     cliutil.StringPtr(strings.TrimSpace(content)),
			ThreadTS: cliutil.StringPtr(parent),
		}
		result.Permalink = climessage.Permalink(cmd.Context(), client, respChannel, ts)
	}

	return ctx.WriteResult("reply", result)
}
