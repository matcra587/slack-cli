package message

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/gechr/clog"
	"github.com/gechr/x/human"
	xstrings "github.com/gechr/x/strings"
	"github.com/matcra587/slack-cli/internal/agent"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	cliscope "github.com/matcra587/slack-cli/internal/cli/scope"
	slackclient "github.com/matcra587/slack-cli/internal/cli/slackclient"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/pkg/blockkit"
	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

// SendData is the result type for message send and edit operations.
type SendData struct {
	Message     clioutput.CliMessage `json:"message"`
	Permalink   *string              `json:"permalink,omitempty"`
	DryRun      bool                 `json:"dry_run,omitempty"`
	Attribution bool                 `json:"attribution"`
}

var _ clioutput.PlainRenderer = SendData{}

func (d SendData) WritePlain(c *clioutput.CommandContext, command string, _ *clioutput.Pagination) error {
	channel := ""
	if d.Message.Channel != nil {
		channel = *d.Message.Channel
	}
	event := c.ResultEventWithStyles(command, clioutput.EntityFieldStyle("channel", channel))
	event = clioutput.AddSlackTimestampFields(event, d.Message.TS, c.Now()).
		Bool("dry_run", d.DryRun).
		When(clog.IsVerbose(), func(e *clog.Event) {
			e.Bool("attribution", d.Attribution)
			if d.Message.ThreadTS != nil {
				e.Str("thread_ts", *d.Message.ThreadTS)
			}
			if d.Permalink != nil {
				e.Str("permalink", *d.Permalink)
			}
		})
	if d.Message.Channel != nil {
		event = event.Str("channel", *d.Message.Channel)
	}
	event.Send()
	return nil
}

// DeleteData is the result type for message delete operations.
type DeleteData struct {
	Channel   string `json:"channel"`
	Timestamp string `json:"timestamp"`
	Deleted   bool   `json:"deleted"`
	DryRun    bool   `json:"dry_run,omitempty"`
}

var _ clioutput.PlainRenderer = DeleteData{}

func (d DeleteData) WritePlain(c *clioutput.CommandContext, command string, _ *clioutput.Pagination) error {
	event := c.ResultEventWithStyles(command, clioutput.EntityFieldStyle("channel", d.Channel)).
		Str("channel", d.Channel)
	event = clioutput.AddSlackTimestampFields(event, d.Timestamp, c.Now()).
		Bool("deleted", d.Deleted).
		Bool("dry_run", d.DryRun)
	event.Send()
	return nil
}

// Source describes where the message body comes from.
type Source struct {
	Message string
	File    string
	Blocks  bool
}

type sendTarget struct {
	Channel string
	Users   []string
}

func (t sendTarget) previewChannel() string {
	if t.Channel != "" {
		return t.Channel
	}
	return strings.Join(t.Users, ",")
}

// NewCommand returns the "message" cobra command tree.
func NewCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	messageCmd := &cobra.Command{
		Use:          "message",
		Short:        "Manage Slack messages",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	var src Source
	var filename string
	var dryRun bool
	sendCmd := &cobra.Command{
		Use:   "send",
		Short: "Send a Slack message",
		Args:  cobra.NoArgs,
		Example: `  # Send a message to a channel
  $ slick message send --channel <channel-id-or-alias> --message <markdown> --json

  # Send a message from stdin
  $ printf '%s\n' "$body" | slick message send --channel <channel-id-or-alias> --file - --json

  # Send a direct message
  $ slick message send --user <user-id-or-email> --message <markdown> --json`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_ = filename
			return runMessageSend(cmd, runtime, src, dryRun)
		},
	}
	sendCmd.Flags().StringVarP(&src.Message, "message", "m", "", "Message body")
	sendCmd.Flags().StringVarP(&src.File, "file", "f", "", "Read message body from file or - for stdin")
	sendCmd.Flags().StringP("channel", "c", "", "Channel ID, name, or alias")
	sendCmd.Flags().StringArrayP("user", "u", nil, "User ID or alias for DM target; repeat or comma-separate for group DMs")
	sendCmd.Flags().StringVarP(&filename, "filename", "N", "", "Filename metadata for stdin sources")
	sendCmd.Flags().BoolVarP(&src.Blocks, "blocks", "b", false, "Treat message source as raw Block Kit JSON")
	sendCmd.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "Preview without sending")
	sendCmd.MarkFlagsMutuallyExclusive("channel", "user")
	sendCmd.PreRunE = func(cmd *cobra.Command, _ []string) error {
		if err := cmd.ValidateFlagGroups(); err != nil {
			return writeRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
		}
		return nil
	}
	messageCmd.AddCommand(sendCmd)

	var editSrc Source
	var editDryRun bool
	editCmd := &cobra.Command{
		Use:          "edit",
		Short:        "Edit an owned Slack message",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMessageEdit(cmd, runtime, editSrc, editDryRun)
		},
	}
	editCmd.Flags().StringP("channel", "c", "", "Channel ID, name, or alias")
	editCmd.Flags().StringP("timestamp", "t", "", "Message timestamp")
	editCmd.Flags().StringVarP(&editSrc.Message, "message", "m", "", "Message body")
	editCmd.Flags().StringVarP(&editSrc.File, "file", "f", "", "Read message body from file or - for stdin")
	editCmd.Flags().BoolVarP(&editSrc.Blocks, "blocks", "b", false, "Treat message source as raw Block Kit JSON")
	editCmd.Flags().BoolVarP(&editDryRun, "dry-run", "n", false, "Preview without mutating")
	messageCmd.AddCommand(editCmd)

	var deleteDryRun bool
	var force bool
	deleteCmd := &cobra.Command{
		Use:          "delete",
		Short:        "Delete an owned Slack message",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMessageDelete(cmd, runtime, deleteDryRun, force)
		},
	}
	deleteCmd.Flags().StringP("channel", "c", "", "Channel ID, name, or alias")
	deleteCmd.Flags().StringP("timestamp", "t", "", "Message timestamp")
	deleteCmd.Flags().BoolVarP(&deleteDryRun, "dry-run", "n", false, "Preview without mutating")
	deleteCmd.Flags().BoolVarP(&force, "force", "F", false, "Confirm deletion")
	messageCmd.AddCommand(deleteCmd)

	return messageCmd
}

func runMessageSend(cmd *cobra.Command, runtime *cliruntime.RootRuntime, src Source, dryRun bool) error {
	ctx, profile, attribution, err := commandContext(cmd, runtime)
	if err != nil {
		return writeRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
	}
	if runtime.Config == nil {
		return writeCommandError(ctx, clioutput.ValidationCLIError("config is required"))
	}

	channel, _ := cmd.Flags().GetString("channel")
	users, _ := cmd.Flags().GetStringArray("user")
	target, err := resolveMessageSendTarget(profile, channel, users)
	if err != nil {
		return writeCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
	}

	content, err := ReadMessageSource(runtime.Stdin, src)
	if err != nil {
		return writeCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
	}
	blocks, err := ComposeBlocks(content, src.Blocks, attribution)
	if err != nil {
		return writeCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
	}

	result := SendData{Attribution: attribution.Enabled}
	if dryRun {
		result.Message = clioutput.CliMessage{Type: "message", TS: "dry-run", Channel: stringPtr(target.previewChannel()), Text: stringPtr(strings.TrimSpace(content))}
		result.DryRun = true
	} else {
		client, err := slackclient.Client(cmd, profile, runtime)
		if err != nil {
			return writeCommandError(ctx, clioutput.AuthCLIError(err.Error()))
		}
		if len(target.Users) > 0 {
			if err := cliscope.Require(cmd.Context(), client, messageUserTargetScopes(target.Users)); err != nil {
				return writeCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err))
			}
			target.Users, err = resolveMessageUserIDs(cmd.Context(), client, target.Users)
			if err != nil {
				return writeCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err))
			}
			opened, _, _, err := client.OpenConversationContext(cmd.Context(), &slackgo.OpenConversationParameters{
				Users:    target.Users,
				ReturnIM: true,
			})
			if err != nil {
				return writeCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err))
			}
			target.Channel = opened.ID
		} else if err := cliscope.Require(cmd.Context(), client, cliscope.AllOf("chat:write")); err != nil {
			return writeCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err))
		}
		channel, ts, err := client.PostMessageContext(cmd.Context(), target.Channel, MessageOptions(content, blocks, attribution)...)
		if err != nil {
			return writeCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err))
		}
		result.Message = clioutput.CliMessage{Type: "message", TS: ts, Channel: stringPtr(channel), Text: stringPtr(strings.TrimSpace(content))}
		result.Permalink = Permalink(cmd.Context(), client, channel, ts)
	}

	return ctx.WriteResult("message.send", result)
}

func runMessageEdit(cmd *cobra.Command, runtime *cliruntime.RootRuntime, src Source, dryRun bool) error {
	ctx, profile, attribution, err := commandContext(cmd, runtime)
	if err != nil {
		return writeRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
	}
	channel, _ := cmd.Flags().GetString("channel")
	if strings.TrimSpace(channel) == "" {
		return writeCommandError(ctx, clioutput.ValidationCLIError("channel is required"))
	}
	timestamp, _ := cmd.Flags().GetString("timestamp")
	if strings.TrimSpace(timestamp) == "" {
		return writeCommandError(ctx, clioutput.ValidationCLIError("timestamp is required"))
	}
	content, err := ReadMessageSource(runtime.Stdin, src)
	if err != nil {
		return writeCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
	}
	blocks, err := ComposeBlocks(content, src.Blocks, attribution)
	if err != nil {
		return writeCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
	}
	result := SendData{Attribution: attribution.Enabled}
	if dryRun {
		result.Message = clioutput.CliMessage{Type: "message", TS: timestamp, Channel: stringPtr(channel), Text: stringPtr(strings.TrimSpace(content))}
		result.DryRun = true
	} else {
		client, err := slackclient.Client(cmd, profile, runtime)
		if err != nil {
			return writeCommandError(ctx, clioutput.AuthCLIError(err.Error()))
		}
		if err := cliscope.Require(cmd.Context(), client, cliscope.AllOf("chat:write")); err != nil {
			return writeCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err))
		}
		respChannel, respTS, respText, err := client.UpdateMessageContext(cmd.Context(), channel, timestamp, MessageOptions(content, blocks, attribution)...)
		if err != nil {
			return writeCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err))
		}
		result.Message = clioutput.CliMessage{
			Type:    "message",
			TS:      firstNonEmpty(respTS, timestamp),
			Channel: stringPtr(firstNonEmpty(respChannel, channel)),
			Text:    stringPtr(firstNonEmpty(respText, strings.TrimSpace(content))),
		}
	}
	return ctx.WriteResult("message.edit", SendData{
		Message:     result.Message,
		DryRun:      result.DryRun,
		Attribution: attribution.Enabled,
	})
}

func runMessageDelete(cmd *cobra.Command, runtime *cliruntime.RootRuntime, dryRun, force bool) error {
	ctx, profile, _, err := commandContext(cmd, runtime)
	if err != nil {
		return writeRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
	}
	channel, _ := cmd.Flags().GetString("channel")
	if strings.TrimSpace(channel) == "" {
		return writeCommandError(ctx, clioutput.ValidationCLIError("channel is required"))
	}
	timestamp, _ := cmd.Flags().GetString("timestamp")
	if strings.TrimSpace(timestamp) == "" {
		return writeCommandError(ctx, clioutput.ValidationCLIError("timestamp is required"))
	}
	if !dryRun && !force {
		return writeCommandError(ctx, clioutput.ValidationCLIError("message delete requires --force unless --dry-run is used"))
	}
	if dryRun {
		return ctx.WriteResult("message.delete", DeleteData{Channel: channel, Timestamp: timestamp, Deleted: true, DryRun: true})
	}
	client, err := slackclient.Client(cmd, profile, runtime)
	if err != nil {
		return writeCommandError(ctx, clioutput.AuthCLIError(err.Error()))
	}
	if err := cliscope.Require(cmd.Context(), client, cliscope.AllOf("chat:write")); err != nil {
		return writeCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err))
	}
	respChannel, respTS, err := client.DeleteMessageContext(cmd.Context(), channel, timestamp)
	if err != nil {
		return writeCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err))
	}
	return ctx.WriteResult("message.delete", DeleteData{
		Channel:   firstNonEmpty(respChannel, channel),
		Timestamp: firstNonEmpty(respTS, timestamp),
		Deleted:   true,
	})
}

func resolveMessageSendTarget(profile config.WorkspaceProfile, channel string, users []string) (sendTarget, error) {
	channel = resolveAlias(profile, strings.TrimSpace(channel))
	resolvedUsers := resolveUserTargets(profile, users)
	switch {
	case channel == "" && len(resolvedUsers) == 0:
		channel = resolveAlias(profile, strings.TrimSpace(profile.DefaultChannel))
		if channel == "" {
			return sendTarget{}, errors.New("channel or user is required")
		}
		return sendTarget{Channel: channel}, nil
	case channel != "" && len(resolvedUsers) > 0:
		return sendTarget{}, errors.New("channel and user are mutually exclusive")
	case channel != "":
		return sendTarget{Channel: channel}, nil
	}
	return sendTarget{Channel: strings.Join(resolvedUsers, ","), Users: resolvedUsers}, nil
}

func resolveUserTargets(profile config.WorkspaceProfile, values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range xstrings.SplitCSV(value) {
			out = append(out, resolveAlias(profile, part))
		}
	}
	return out
}

func messageUserTargetScopes(users []string) cliscope.Requirement {
	if len(users) > 1 {
		if messageTargetsNeedEmailLookup(users) {
			return cliscope.AllOf("chat:write", "im:write", "mpim:write", "users:read.email")
		}
		return cliscope.AllOf("chat:write", "im:write", "mpim:write")
	}
	if messageTargetsNeedEmailLookup(users) {
		return cliscope.AllOf("chat:write", "im:write", "users:read.email")
	}
	return cliscope.AllOf("chat:write", "im:write")
}

func messageTargetsNeedEmailLookup(users []string) bool {
	for _, user := range users {
		if strings.Contains(user, "@") {
			return true
		}
	}
	return false
}

func resolveMessageUserIDs(ctx context.Context, client *slackgo.Client, users []string) ([]string, error) {
	out := make([]string, 0, len(users))
	for _, user := range users {
		if !strings.Contains(user, "@") {
			out = append(out, user)
			continue
		}
		resolved, err := client.GetUserByEmailContext(ctx, user)
		if err != nil {
			return nil, err
		}
		out = append(out, resolved.ID)
	}
	return out, nil
}

// ResolveAlias resolves a channel/user alias from the workspace profile.
func ResolveAlias(profile config.WorkspaceProfile, value string) string {
	return resolveAlias(profile, value)
}

func resolveAlias(profile config.WorkspaceProfile, value string) string {
	if value == "" || profile.Aliases == nil {
		return value
	}
	if resolved, ok := profile.Aliases[value]; ok {
		return resolved
	}
	return value
}

// ReadMessageSource reads the message content from the given source.
func ReadMessageSource(stdin io.Reader, src Source) (string, error) {
	sourceCount := 0
	if src.Message != "" {
		sourceCount++
	}
	if src.File != "" {
		sourceCount++
	}
	if sourceCount != 1 {
		return "", errors.New("exactly one message source is required")
	}
	if src.Message != "" {
		return src.Message, nil
	}
	if src.File == "-" {
		if stdin == nil {
			return "", errors.New("stdin is unavailable")
		}
		raw, err := io.ReadAll(stdin)
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
	raw, err := os.ReadFile(human.ExpandPath(src.File))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// ComposeBlocks converts message content into Slack Block Kit blocks.
func ComposeBlocks(content string, raw bool, attribution agent.Attribution) ([]slackgo.Block, error) {
	if raw {
		var parsed slackgo.Blocks
		if err := json.Unmarshal([]byte(content), &parsed); err != nil {
			return nil, err
		}
		if attribution.Enabled {
			parsed.BlockSet = append(parsed.BlockSet, blockkit.AttributionBlockWithMessage(attribution.Emoji, attribution.Message))
		}
		if err := blockkit.ValidateBlocks(parsed.BlockSet); err != nil {
			return nil, err
		}
		return parsed.BlockSet, nil
	}

	blocks, err := blockkit.FromMarkdown(content)
	if err != nil {
		return nil, err
	}
	if attribution.Enabled {
		blocks = append(blocks, blockkit.AttributionBlockWithMessage(attribution.Emoji, attribution.Message))
	}
	if err := blockkit.ValidateBlocks(blocks); err != nil {
		return nil, err
	}
	return blocks, nil
}

// MessageOptions builds the Slack message options for sending or updating.
func MessageOptions(content string, blocks []slackgo.Block, attribution ...agent.Attribution) []slackgo.MsgOption {
	_ = attribution
	options := []slackgo.MsgOption{slackgo.MsgOptionText(strings.TrimSpace(content), false)}
	if len(blocks) > 0 {
		options = append(options, slackgo.MsgOptionBlocks(blocks...))
	}
	return options
}

// Permalink fetches the permalink for a message, returning nil on failure.
func Permalink(ctx context.Context, client *slackgo.Client, channel, ts string) *string {
	if channel == "" || ts == "" {
		return nil
	}
	value, err := client.GetPermalinkContext(ctx, &slackgo.PermalinkParameters{Channel: channel, Ts: ts})
	if err != nil || value == "" {
		return nil
	}
	return stringPtr(value)
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func commandContext(cmd *cobra.Command, runtime *cliruntime.RootRuntime) (*clioutput.CommandContext, config.WorkspaceProfile, agent.Attribution, error) {
	return cliruntime.CommandContext(cmd, runtime)
}

func writeCommandError(ctx *clioutput.CommandContext, err clioutput.CLIError) error {
	return clioutput.WriteCommandError(ctx, err)
}

func writeRuntimeError(runtime *cliruntime.RootRuntime, err clioutput.CLIError) error {
	return cliruntime.WriteRuntimeError(runtime, err)
}
