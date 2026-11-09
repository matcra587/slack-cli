// Package pin manages pinned Slack messages.
package pin

import (
	"regexp"
	"strings"

	climessage "github.com/matcra587/slack-cli/internal/cli/message"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	cliscope "github.com/matcra587/slack-cli/internal/cli/scope"
	"github.com/matcra587/slack-cli/internal/cli/slackclient"
	slack "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

var timestamp = regexp.MustCompile(`^[0-9]+\.[0-9]{6}$`)

type result struct {
	Channel   string       `json:"channel"`
	Timestamp string       `json:"ts,omitempty"`
	Items     []slack.Item `json:"items,omitzero"`
	DryRun    bool         `json:"dry_run,omitempty"`
}

func (d result) WritePlain(c *clioutput.CommandContext, command string, _ *clioutput.Pagination) error {
	if d.Items != nil {
		for _, item := range d.Items {
			event := c.ResultEvent(command).Str("channel", d.Channel)
			if item.Message != nil {
				event.Str("ts", item.Message.Timestamp).Str("text", item.Message.Text)
			}
			event.Msg("Pinned message")
		}
		if len(d.Items) == 0 {
			c.ResultEvent(command).Str("channel", d.Channel).Msg("No pinned messages")
		}
		return nil
	}
	c.ResultEvent(command).Str("channel", d.Channel).Str("ts", d.Timestamp).Bool("dry_run", d.DryRun).Msg(clioutput.ActionLabel(command))
	return nil
}

// NewCommand returns the pin command tree.
func NewCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	parent := &cobra.Command{Use: "pin", Short: "Manage pinned messages"}
	for _, action := range []string{"list", "add", "remove"} {
		cmd := &cobra.Command{Use: action, Short: action + " pinned messages", Args: cobra.NoArgs, SilenceUsage: true, RunE: func(cmd *cobra.Command, _ []string) error { return run(cmd, runtime, action) }}
		cmd.Flags().StringP("channel", "c", "", "Channel ID or configured alias")
		if action != "list" {
			cmd.Flags().StringP("timestamp", "t", "", "Message timestamp")
		}
		parent.AddCommand(cmd)
	}
	return parent
}

func run(cmd *cobra.Command, runtime *cliruntime.RootRuntime, action string) error {
	c, profile, _, err := cliruntime.CommandContext(cmd, runtime)
	if err != nil {
		return cliruntime.WriteRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
	}
	channel, _ := cmd.Flags().GetString("channel")
	d := result{Channel: strings.TrimSpace(climessage.ResolveAlias(profile, strings.TrimSpace(channel)))}
	if d.Channel == "" {
		return clioutput.WriteCommandError(c, clioutput.ValidationCLIError("channel is required"))
	}
	if action != "list" {
		d.Timestamp, _ = cmd.Flags().GetString("timestamp")
		d.Timestamp = strings.TrimSpace(d.Timestamp)
		if !timestamp.MatchString(d.Timestamp) {
			return clioutput.WriteCommandError(c, clioutput.ValidationCLIError("timestamp must be a Slack message timestamp"))
		}
		if cliruntime.DryRun(cmd) {
			d.DryRun = true
			return c.WriteResult(c.Command, d)
		}
	}
	client, err := slackclient.Client(cmd, profile, runtime)
	if err != nil {
		return clioutput.WriteCommandError(c, slackclient.CLIError(cmd.Context(), err))
	}
	scope := "pins:write"
	if action == "list" {
		scope = "pins:read"
	}
	if err = cliscope.Require(cmd.Context(), client, cliscope.AllOf(scope)); err != nil {
		return clioutput.WriteCommandError(c, clioutput.CliErrorFromSlack(cmd.Context(), err, ""))
	}
	switch action {
	case "list":
		d.Items, _, err = client.ListPinsContext(cmd.Context(), d.Channel)
		if d.Items == nil {
			d.Items = []slack.Item{}
		}
	case "add":
		err = client.AddPinContext(cmd.Context(), d.Channel, slack.ItemRef{Timestamp: d.Timestamp})
	case "remove":
		err = client.RemovePinContext(cmd.Context(), d.Channel, slack.ItemRef{Timestamp: d.Timestamp})
	}
	if err != nil {
		return clioutput.WriteCommandError(c, clioutput.CliErrorFromSlack(cmd.Context(), err, ""))
	}
	return c.WriteResult(c.Command, d)
}
