package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

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
		Use:          "send",
		Short:        "Send a Slack message",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_ = filename
			return runMessageSend(cmd, runtime, source, dryRun)
		},
	}
	sendCmd.Flags().StringVar(&source.Message, "message", "", "Message body")
	sendCmd.Flags().StringVar(&source.File, "file", "", "Read message body from file or - for stdin")
	sendCmd.Flags().String("channel", "", "Channel ID, name, or alias")
	sendCmd.Flags().String("user", "", "User ID or alias for DM target")
	sendCmd.Flags().StringVar(&filename, "filename", "", "Filename metadata for stdin sources")
	sendCmd.Flags().BoolVar(&source.Blocks, "blocks", false, "Treat message source as raw Block Kit JSON")
	sendCmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview without sending")
	sendCmd.MarkFlagsOneRequired("channel", "user")
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
	editCmd.Flags().String("channel", "", "Channel ID, name, or alias")
	editCmd.Flags().String("timestamp", "", "Message timestamp")
	editCmd.Flags().StringVar(&editSource.Message, "message", "", "Message body")
	editCmd.Flags().StringVar(&editSource.File, "file", "", "Read message body from file or - for stdin")
	editCmd.Flags().BoolVar(&editSource.Blocks, "blocks", false, "Treat message source as raw Block Kit JSON")
	editCmd.Flags().BoolVar(&editDryRun, "dry-run", false, "Preview without mutating")
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
	deleteCmd.Flags().String("channel", "", "Channel ID, name, or alias")
	deleteCmd.Flags().String("timestamp", "", "Message timestamp")
	deleteCmd.Flags().BoolVar(&deleteDryRun, "dry-run", false, "Preview without mutating")
	deleteCmd.Flags().BoolVar(&force, "force", false, "Confirm deletion")
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
	user, _ := cmd.Flags().GetString("user")
	target, err := resolveMessageSendTarget(profile, channel, user)
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
		result.Message = cliMessage{Type: "message", TS: "dry-run", Channel: stringPtr(target.Channel), Text: stringPtr(strings.TrimSpace(content))}
		result.DryRun = true
	} else {
		client, err := slackClient(cmd, profile, runtime)
		if err != nil {
			return writeCommandError(ctx, authCLIError(err.Error()))
		}
		if target.User != "" {
			if err := requireSlackScopes(cmd.Context(), client, allScopes("chat:write", "im:write")); err != nil {
				return writeCommandError(ctx, cliErrorFromSlack(err))
			}
			opened, _, _, err := client.OpenConversationContext(cmd.Context(), &slackgo.OpenConversationParameters{
				Users:    []string{target.User},
				ReturnIM: true,
			})
			if err != nil {
				return writeCommandError(ctx, cliErrorFromSlack(err))
			}
			target.Channel = opened.ID
		} else if err := requireSlackScopes(cmd.Context(), client, allScopes("chat:write")); err != nil {
			return writeCommandError(ctx, cliErrorFromSlack(err))
		}
		channel, ts, err := client.PostMessageContext(context.Background(), target.Channel, messageOptions(content, blocks, attribution)...)
		if err != nil {
			return writeCommandError(ctx, cliErrorFromSlack(err))
		}
		result.Message = cliMessage{Type: "message", TS: ts, Channel: stringPtr(channel), Text: stringPtr(strings.TrimSpace(content))}
		result.Permalink = permalink(context.Background(), client, channel, ts)
	}

	return writeSendResult(ctx, "message.send", result)
}

type messageSendTarget struct {
	Channel string
	User    string
}

func resolveMessageSendTarget(profile config.WorkspaceProfile, channel, user string) (messageSendTarget, error) {
	channel = resolveAlias(profile, strings.TrimSpace(channel))
	user = resolveAlias(profile, strings.TrimSpace(user))
	switch {
	case channel == "" && user == "":
		return messageSendTarget{}, errors.New("channel or user is required")
	case channel != "" && user != "":
		return messageSendTarget{}, errors.New("channel and user are mutually exclusive")
	case channel != "":
		return messageSendTarget{Channel: channel}, nil
	}
	return messageSendTarget{Channel: user, User: user}, nil
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
			return writeCommandError(ctx, cliErrorFromSlack(err))
		}
		if err := requireMessageOwnership(context.Background(), client, channel, timestamp); err != nil {
			return writeCommandError(ctx, cliErrorFromSlack(err))
		}
		respChannel, respTS, respText, err := client.UpdateMessageContext(context.Background(), channel, timestamp, messageOptions(content, blocks, attribution)...)
		if err != nil {
			return writeCommandError(ctx, cliErrorFromSlack(err))
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
		return writeCommandError(ctx, cliErrorFromSlack(err))
	}
	if err := requireMessageOwnership(context.Background(), client, channel, timestamp); err != nil {
		return writeCommandError(ctx, cliErrorFromSlack(err))
	}
	respChannel, respTS, err := client.DeleteMessageContext(context.Background(), channel, timestamp)
	if err != nil {
		return writeCommandError(ctx, cliErrorFromSlack(err))
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
	raw, err := os.ReadFile(source.File)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func composeBlocks(content string, raw bool, attribution Attribution) ([]slackgo.Block, error) {
	if raw {
		var blocks []map[string]any
		if err := json.Unmarshal([]byte(content), &blocks); err != nil {
			return nil, err
		}
		if attribution.Enabled {
			blocks = append(blocks, attributionMap(attribution))
		}
		if err := blockkit.ValidateRawBlocks(blocks); err != nil {
			return nil, err
		}
		return rawBlocks(blocks)
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

func rawBlocks(blocks []map[string]any) ([]slackgo.Block, error) {
	out := make([]slackgo.Block, 0, len(blocks))
	for _, block := range blocks {
		raw, err := json.Marshal(block)
		if err != nil {
			return nil, err
		}
		parsed, err := slackgo.BlockFromJSON(string(raw))
		if err != nil {
			return nil, err
		}
		out = append(out, parsed)
	}
	return out, nil
}

func attributionMap(attribution Attribution) map[string]any {
	raw, err := json.Marshal(blockkit.AttributionBlockWithMessage(attribution.Emoji, attribution.Message))
	if err != nil {
		return map[string]any{}
	}
	var block map[string]any
	if err := json.Unmarshal(raw, &block); err != nil {
		return map[string]any{}
	}
	return block
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

func requireMessageOwnership(ctx context.Context, client *slackgo.Client, channel, timestamp string) error {
	actor, err := client.AuthTestContext(ctx)
	if err != nil {
		return err
	}
	messages, _, _, err := client.GetConversationRepliesContext(ctx, &slackgo.GetConversationRepliesParameters{
		ChannelID: channel,
		Timestamp: timestamp,
		Limit:     1,
	})
	if err != nil {
		return err
	}
	if len(messages) == 0 {
		return errNotOwned
	}
	message := messages[0]
	if actor.UserID != "" && message.User == actor.UserID {
		return nil
	}
	if actor.BotID != "" && message.BotID == actor.BotID {
		return nil
	}
	return errNotOwned
}

func writeSendResult(ctx *CommandContext, command string, data sendCommandData) error {
	return ctx.WriteResult(command, data)
}
