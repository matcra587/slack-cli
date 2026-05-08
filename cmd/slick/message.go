package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/gechr/x/human"
	xstrings "github.com/gechr/x/strings"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/pkg/blockkit"
	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

type sendCommandData struct {
	Message     cliMessage `json:"message"`
	Permalink   *string    `json:"permalink,omitempty"`
	DryRun      bool       `json:"dry_run,omitempty"`
	Attribution bool       `json:"attribution"`
}

type messageSource struct {
	Message string
	File    string
	Blocks  bool
}

func newMessageCommand(runtime *RootRuntime) *cobra.Command {
	messageCmd := &cobra.Command{
		Use:          "message",
		Short:        "Manage Slack messages",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	var source messageSource
	var filename string
	var dryRun bool
	sendCmd := &cobra.Command{
		Use:   "send",
		Short: "Send a Slack message",
		Example: `  # Send a message to a channel
  $ slick message send --channel <channel-id-or-alias> --message <markdown> --json

  # Send a message from stdin
  $ printf '%s\n' "$body" | slick message send --channel <channel-id-or-alias> --file - --json

  # Send a direct message
  $ slick message send --user <user-id-or-email> --message <markdown> --json`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_ = filename
			return runMessageSend(cmd, runtime, source, dryRun)
		},
	}
	sendCmd.Flags().StringVarP(&source.Message, "message", "m", "", "Message body")
	sendCmd.Flags().StringVarP(&source.File, "file", "f", "", "Read message body from file or - for stdin")
	sendCmd.Flags().StringP("channel", "c", "", "Channel ID, name, or alias")
	sendCmd.Flags().StringArrayP("user", "u", nil, "User ID or alias for DM target; repeat or comma-separate for group DMs")
	sendCmd.Flags().StringVarP(&filename, "filename", "N", "", "Filename metadata for stdin sources")
	sendCmd.Flags().BoolVarP(&source.Blocks, "blocks", "b", false, "Treat message source as raw Block Kit JSON")
	sendCmd.Flags().BoolVarP(&dryRun, "dry-run", "n", false, "Preview without sending")
	sendCmd.MarkFlagsMutuallyExclusive("channel", "user")
	sendCmd.PreRunE = func(cmd *cobra.Command, _ []string) error {
		if err := cmd.ValidateFlagGroups(); err != nil {
			return writeRuntimeError(runtime, validationCLIError(err.Error()))
		}
		return nil
	}
	messageCmd.AddCommand(sendCmd)

	var editSource messageSource
	var editDryRun bool
	editCmd := &cobra.Command{
		Use:          "edit",
		Short:        "Edit an owned Slack message",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMessageEdit(cmd, runtime, editSource, editDryRun)
		},
	}
	editCmd.Flags().StringP("channel", "c", "", "Channel ID, name, or alias")
	editCmd.Flags().StringP("timestamp", "t", "", "Message timestamp")
	editCmd.Flags().StringVarP(&editSource.Message, "message", "m", "", "Message body")
	editCmd.Flags().StringVarP(&editSource.File, "file", "f", "", "Read message body from file or - for stdin")
	editCmd.Flags().BoolVarP(&editSource.Blocks, "blocks", "b", false, "Treat message source as raw Block Kit JSON")
	editCmd.Flags().BoolVarP(&editDryRun, "dry-run", "n", false, "Preview without mutating")
	messageCmd.AddCommand(editCmd)

	var deleteDryRun bool
	var force bool
	deleteCmd := &cobra.Command{
		Use:          "delete",
		Short:        "Delete an owned Slack message",
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

func runMessageSend(cmd *cobra.Command, runtime *RootRuntime, source messageSource, dryRun bool) error {
	ctx, profile, attribution, err := commandContext(cmd, runtime)
	if err != nil {
		return writeRuntimeError(runtime, validationCLIError(err.Error()))
	}
	if runtime.Config == nil {
		return writeCommandError(ctx, validationCLIError("config is required"))
	}

	channel, _ := cmd.Flags().GetString("channel")
	users, _ := cmd.Flags().GetStringArray("user")
	target, err := resolveMessageSendTarget(profile, channel, users)
	if err != nil {
		return writeCommandError(ctx, validationCLIError(err.Error()))
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
		result.Message = cliMessage{Type: "message", TS: "dry-run", Channel: stringPtr(target.previewChannel()), Text: stringPtr(strings.TrimSpace(content))}
		result.DryRun = true
	} else {
		client, err := slackClient(cmd, profile, runtime)
		if err != nil {
			return writeCommandError(ctx, authCLIError(err.Error()))
		}
		if len(target.Users) > 0 {
			if err := requireSlackScopes(cmd.Context(), client, messageUserTargetScopes(target.Users)); err != nil {
				return writeCommandError(ctx, cliErrorFromSlack(cmd.Context(), err))
			}
			target.Users, err = resolveMessageUserIDs(cmd.Context(), client, target.Users)
			if err != nil {
				return writeCommandError(ctx, cliErrorFromSlack(cmd.Context(), err))
			}
			opened, _, _, err := client.OpenConversationContext(cmd.Context(), &slackgo.OpenConversationParameters{
				Users:    target.Users,
				ReturnIM: true,
			})
			if err != nil {
				return writeCommandError(ctx, cliErrorFromSlack(cmd.Context(), err))
			}
			target.Channel = opened.ID
		} else if err := requireSlackScopes(cmd.Context(), client, allScopes("chat:write")); err != nil {
			return writeCommandError(ctx, cliErrorFromSlack(cmd.Context(), err))
		}
		channel, ts, err := client.PostMessageContext(cmd.Context(), target.Channel, messageOptions(content, blocks, attribution)...)
		if err != nil {
			return writeCommandError(ctx, cliErrorFromSlack(cmd.Context(), err))
		}
		result.Message = cliMessage{Type: "message", TS: ts, Channel: stringPtr(channel), Text: stringPtr(strings.TrimSpace(content))}
		result.Permalink = permalink(cmd.Context(), client, channel, ts)
	}

	return writeSendResult(ctx, "message.send", result)
}

type messageSendTarget struct {
	Channel string
	Users   []string
}

func resolveMessageSendTarget(profile config.WorkspaceProfile, channel string, users []string) (messageSendTarget, error) {
	channel = resolveAlias(profile, strings.TrimSpace(channel))
	resolvedUsers := resolveUserTargets(profile, users)
	switch {
	case channel == "" && len(resolvedUsers) == 0:
		channel = resolveAlias(profile, strings.TrimSpace(profile.DefaultChannel))
		if channel == "" {
			return messageSendTarget{}, errors.New("channel or user is required")
		}
		return messageSendTarget{Channel: channel}, nil
	case channel != "" && len(resolvedUsers) > 0:
		return messageSendTarget{}, errors.New("channel and user are mutually exclusive")
	case channel != "":
		return messageSendTarget{Channel: channel}, nil
	}
	return messageSendTarget{Channel: strings.Join(resolvedUsers, ","), Users: resolvedUsers}, nil
}

func (t messageSendTarget) previewChannel() string {
	if t.Channel != "" {
		return t.Channel
	}
	return strings.Join(t.Users, ",")
}

func resolveUserTargets(profile config.WorkspaceProfile, values []string) []string {
	var out []string
	for _, value := range values {
		for _, part := range xstrings.SplitCSV(value) {
			out = append(out, resolveAlias(profile, part))
		}
	}
	return out
}

func messageUserTargetScopes(users []string) scopeRequirement {
	if len(users) > 1 {
		if messageTargetsNeedEmailLookup(users) {
			return allScopes("chat:write", "im:write", "mpim:write", "users:read.email")
		}
		return allScopes("chat:write", "im:write", "mpim:write")
	}
	if messageTargetsNeedEmailLookup(users) {
		return allScopes("chat:write", "im:write", "users:read.email")
	}
	return allScopes("chat:write", "im:write")
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

func resolveAlias(profile config.WorkspaceProfile, value string) string {
	if value == "" || profile.Aliases == nil {
		return value
	}
	if resolved, ok := profile.Aliases[value]; ok {
		return resolved
	}
	return value
}

func runMessageEdit(cmd *cobra.Command, runtime *RootRuntime, source messageSource, dryRun bool) error {
	ctx, profile, attribution, err := commandContext(cmd, runtime)
	if err != nil {
		return writeRuntimeError(runtime, validationCLIError(err.Error()))
	}
	channel, _ := cmd.Flags().GetString("channel")
	if strings.TrimSpace(channel) == "" {
		return writeCommandError(ctx, validationCLIError("channel is required"))
	}
	timestamp, _ := cmd.Flags().GetString("timestamp")
	if strings.TrimSpace(timestamp) == "" {
		return writeCommandError(ctx, validationCLIError("timestamp is required"))
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
		result.Message = cliMessage{Type: "message", TS: timestamp, Channel: stringPtr(channel), Text: stringPtr(strings.TrimSpace(content))}
		result.DryRun = true
	} else {
		client, err := slackClient(cmd, profile, runtime)
		if err != nil {
			return writeCommandError(ctx, authCLIError(err.Error()))
		}
		if err := requireSlackScopes(cmd.Context(), client, allScopes("chat:write")); err != nil {
			return writeCommandError(ctx, cliErrorFromSlack(cmd.Context(), err))
		}
		respChannel, respTS, respText, err := client.UpdateMessageContext(cmd.Context(), channel, timestamp, messageOptions(content, blocks, attribution)...)
		if err != nil {
			return writeCommandError(ctx, cliErrorFromSlack(cmd.Context(), err))
		}
		result.Message = cliMessage{
			Type:    "message",
			TS:      firstNonEmpty(respTS, timestamp),
			Channel: stringPtr(firstNonEmpty(respChannel, channel)),
			Text:    stringPtr(firstNonEmpty(respText, strings.TrimSpace(content))),
		}
	}
	return writeSendResult(ctx, "message.edit", sendCommandData{
		Message:     result.Message,
		DryRun:      result.DryRun,
		Attribution: attribution.Enabled,
	})
}

func runMessageDelete(cmd *cobra.Command, runtime *RootRuntime, dryRun, force bool) error {
	ctx, profile, _, err := commandContext(cmd, runtime)
	if err != nil {
		return writeRuntimeError(runtime, validationCLIError(err.Error()))
	}
	channel, _ := cmd.Flags().GetString("channel")
	if strings.TrimSpace(channel) == "" {
		return writeCommandError(ctx, validationCLIError("channel is required"))
	}
	timestamp, _ := cmd.Flags().GetString("timestamp")
	if strings.TrimSpace(timestamp) == "" {
		return writeCommandError(ctx, validationCLIError("timestamp is required"))
	}
	if !dryRun && !force {
		return writeCommandError(ctx, validationCLIError("message delete requires --force unless --dry-run is used"))
	}
	if dryRun {
		return ctx.WriteResult("message.delete", deleteMessageData{Channel: channel, Timestamp: timestamp, Deleted: true, DryRun: true})
	}
	client, err := slackClient(cmd, profile, runtime)
	if err != nil {
		return writeCommandError(ctx, authCLIError(err.Error()))
	}
	if err := requireSlackScopes(cmd.Context(), client, allScopes("chat:write")); err != nil {
		return writeCommandError(ctx, cliErrorFromSlack(cmd.Context(), err))
	}
	respChannel, respTS, err := client.DeleteMessageContext(cmd.Context(), channel, timestamp)
	if err != nil {
		return writeCommandError(ctx, cliErrorFromSlack(cmd.Context(), err))
	}
	return ctx.WriteResult("message.delete", deleteMessageData{
		Channel:   firstNonEmpty(respChannel, channel),
		Timestamp: firstNonEmpty(respTS, timestamp),
		Deleted:   true,
	})
}

type deleteMessageData struct {
	Channel   string `json:"channel"`
	Timestamp string `json:"timestamp"`
	Deleted   bool   `json:"deleted"`
	DryRun    bool   `json:"dry_run,omitempty"`
}

func readMessageSource(stdin io.Reader, source messageSource) (string, error) {
	sourceCount := 0
	if source.Message != "" {
		sourceCount++
	}
	if source.File != "" {
		sourceCount++
	}
	if sourceCount != 1 {
		return "", errors.New("exactly one message source is required")
	}
	if source.Message != "" {
		return source.Message, nil
	}
	if source.File == "-" {
		if stdin == nil {
			return "", errors.New("stdin is unavailable")
		}
		raw, err := io.ReadAll(stdin)
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
	raw, err := os.ReadFile(human.ExpandPath(source.File))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func composeBlocks(content string, raw bool, attribution Attribution) ([]slackgo.Block, error) {
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

func messageOptions(content string, blocks []slackgo.Block, attribution ...Attribution) []slackgo.MsgOption {
	_ = attribution
	options := []slackgo.MsgOption{slackgo.MsgOptionText(strings.TrimSpace(content), false)}
	if len(blocks) > 0 {
		options = append(options, slackgo.MsgOptionBlocks(blocks...))
	}
	return options
}

func permalink(ctx context.Context, client *slackgo.Client, channel, ts string) *string {
	if channel == "" || ts == "" {
		return nil
	}
	value, err := client.GetPermalinkContext(ctx, &slackgo.PermalinkParameters{Channel: channel, Ts: ts})
	if err != nil || value == "" {
		return nil
	}
	return stringPtr(value)
}

func writeSendResult(ctx *CommandContext, command string, data sendCommandData) error {
	return ctx.WriteResult(command, data)
}
