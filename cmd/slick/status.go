package main

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gechr/clog"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/spf13/cobra"
)

type statusCommandData struct {
	Text       string `json:"text,omitempty"`
	Emoji      string `json:"emoji,omitempty"`
	Expiration int64  `json:"expiration,omitempty"`
	Cleared    bool   `json:"cleared,omitempty"`
	DryRun     bool   `json:"dry_run,omitempty"`
}

var _ PlainRenderer = statusCommandData{}

func (d statusCommandData) WritePlain(c *CommandContext, command string, _ *Pagination) error {
	event := c.ResultEvent(command).
		Str("text", d.Text).
		Str("emoji", d.Emoji).
		Bool("cleared", d.Cleared).
		Bool("dry_run", d.DryRun).
		When(d.Expiration > 0, func(e *clog.Event) {
			e.Int64("expiration", d.Expiration)
		})
	event.Msg(actionLabel(command))
	return nil
}

type statusSetOptions struct {
	Text      string
	Emoji     string
	ExpiresIn time.Duration
	Until     string
	DryRun    bool
}

func newStatusCommand(runtime *RootRuntime) *cobra.Command {
	statusCmd := &cobra.Command{
		Use:          "status",
		Short:        "Set or clear Slack status",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	var setOpts statusSetOptions
	setCmd := &cobra.Command{
		Use:          "set [text] [emoji]",
		Short:        "Set Slack status",
		Args:         cobra.MaximumNArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if setOpts.Text == "" && len(args) > 0 {
				setOpts.Text = args[0]
			}
			if setOpts.Emoji == "" && len(args) > 1 {
				setOpts.Emoji = args[1]
			}
			return runStatusSet(cmd, runtime, setOpts)
		},
	}
	setCmd.Flags().StringVarP(&setOpts.Text, "text", "t", "", "Status text")
	setCmd.Flags().StringVarP(&setOpts.Emoji, "emoji", "e", "", "Status emoji")
	setCmd.Flags().DurationVarP(&setOpts.ExpiresIn, "expires-in", "x", 0, "Status expiration duration")
	setCmd.Flags().StringVarP(&setOpts.Until, "until", "U", "", "Status expiration time")
	setCmd.Flags().BoolVarP(&setOpts.DryRun, "dry-run", "n", false, "Preview without mutating")

	var clearDryRun bool
	clearCmd := &cobra.Command{
		Use:          "clear",
		Short:        "Clear Slack status",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStatusClear(cmd, runtime, clearDryRun)
		},
	}
	clearCmd.Flags().BoolVarP(&clearDryRun, "dry-run", "n", false, "Preview without mutating")

	statusCmd.AddCommand(setCmd)
	statusCmd.AddCommand(clearCmd)
	return statusCmd
}

func runStatusSet(cmd *cobra.Command, runtime *RootRuntime, opts statusSetOptions) error {
	ctx, profile, _, err := commandContext(cmd, runtime)
	if err != nil {
		return writeRuntimeError(runtime, validationCLIError(err.Error()))
	}
	text := strings.TrimSpace(opts.Text)
	emoji := normalizeStatusEmoji(opts.Emoji)
	if text == "" && emoji == "" {
		return writeCommandError(ctx, validationCLIError("status text or emoji is required"))
	}
	expiration, err := parseStatusExpiration(ctx.Now(), opts.ExpiresIn, opts.Until)
	if err != nil {
		return writeCommandError(ctx, validationCLIError(err.Error()))
	}
	return mutateSlackStatus(cmd, runtime, ctx, profile, text, emoji, expiration, opts.DryRun, "status.set")
}

func runStatusClear(cmd *cobra.Command, runtime *RootRuntime, dryRun bool) error {
	ctx, profile, _, err := commandContext(cmd, runtime)
	if err != nil {
		return writeRuntimeError(runtime, validationCLIError(err.Error()))
	}
	return mutateSlackStatus(cmd, runtime, ctx, profile, "", "", 0, dryRun, "status.clear")
}

func mutateSlackStatus(cmd *cobra.Command, runtime *RootRuntime, ctx *CommandContext, profile config.WorkspaceProfile, text, emoji string, expiration int64, dryRun bool, command string) error {
	data := statusCommandData{Text: text, Emoji: emoji, Expiration: expiration, DryRun: dryRun}
	if text == "" && emoji == "" && expiration == 0 {
		data.Cleared = true
	}
	if dryRun {
		return ctx.WriteResult(command, data)
	}
	if profile.TokenType != config.TokenTypeUser {
		return writeCommandError(ctx, authCLIError("status requires a user token with users.profile:write"))
	}
	client, err := slackClient(cmd, profile, runtime)
	if err != nil {
		return writeCommandError(ctx, authCLIError(err.Error()))
	}
	if err := requireSlackScopes(cmd.Context(), client, allScopes("users.profile:write")); err != nil {
		return writeCommandError(ctx, cliErrorFromSlack(cmd.Context(), err))
	}
	if err := client.SetUserCustomStatusContext(cmd.Context(), text, emoji, expiration); err != nil {
		return writeCommandError(ctx, cliErrorFromSlack(cmd.Context(), err))
	}
	return ctx.WriteResult(command, data)
}

func normalizeStatusEmoji(value string) string {
	value = strings.Trim(strings.TrimSpace(value), ":")
	if value == "" {
		return ""
	}
	return ":" + value + ":"
}

func parseStatusExpiration(now time.Time, expiresIn time.Duration, until string) (int64, error) {
	until = strings.TrimSpace(until)
	if expiresIn != 0 && until != "" {
		return 0, errors.New("expires-in and until are mutually exclusive")
	}
	if expiresIn < 0 {
		return 0, errors.New("expires-in must be positive")
	}
	if expiresIn > 0 {
		return now.Add(expiresIn).Unix(), nil
	}
	if until == "" {
		return 0, nil
	}
	if after, ok := strings.CutPrefix(until, "+"); ok {
		duration, err := time.ParseDuration(after)
		if err != nil {
			return 0, errors.New("until relative duration is invalid")
		}
		return now.Add(duration).Unix(), nil
	}
	if unix, err := strconv.ParseInt(until, 10, 64); err == nil {
		return unix, nil
	}
	if parsed, err := time.Parse(time.RFC3339, until); err == nil {
		return parsed.Unix(), nil
	}
	for _, layout := range []string{"15:04", "15:04:05"} {
		clock, err := time.ParseInLocation(layout, until, now.Location())
		if err != nil {
			continue
		}
		parsed := time.Date(now.Year(), now.Month(), now.Day(), clock.Hour(), clock.Minute(), clock.Second(), 0, now.Location())
		if !parsed.After(now) {
			parsed = parsed.Add(24 * time.Hour)
		}
		return parsed.Unix(), nil
	}
	return 0, errors.New("until must be +duration, Unix timestamp, RFC3339, or HH:MM")
}
