// Package dnd implements Do Not Disturb commands.
package dnd

import (
	"errors"
	"strings"

	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	cliscope "github.com/matcra587/slack-cli/internal/cli/scope"
	"github.com/matcra587/slack-cli/internal/cli/slackclient"
	"github.com/matcra587/slack-cli/internal/config"
	slack "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

type result struct {
	User    string                     `json:"user,omitempty"`
	Minutes int                        `json:"minutes,omitempty"`
	Status  *slack.DNDStatus           `json:"status,omitempty"`
	Snooze  *snoozeStatus              `json:"snooze,omitempty"`
	Users   map[string]slack.DNDStatus `json:"users,omitzero"`
	DryRun  bool                       `json:"dry_run,omitempty"`
}

// Snooze responses do not carry scheduled DND state. Keep them separate rather
// than emitting the native SDK's zero-value dnd_enabled as an observed false.
type snoozeStatus struct {
	Enabled   bool `json:"snooze_enabled"`
	EndTime   int  `json:"snooze_endtime,omitempty"`
	Remaining int  `json:"snooze_remaining,omitempty"`
}

func (d result) WritePlain(c *clioutput.CommandContext, command string, _ *clioutput.Pagination) error {
	event := c.ResultEvent(command)
	if d.User != "" {
		event = event.Str("user", d.User)
	}
	event = event.Bool("dry_run", d.DryRun)
	if d.Status != nil {
		event = event.Bool("enabled", d.Status.Enabled).Bool("snooze_enabled", d.Status.SnoozeEnabled)
	}
	if d.Snooze != nil {
		event = event.Bool("snooze_enabled", d.Snooze.Enabled)
	}
	if d.Users != nil {
		event = event.Any("users", d.Users)
	}
	if d.Minutes > 0 {
		event = event.Int("minutes", d.Minutes)
	}
	if d.Status != nil {
		event = event.Int("snooze_remaining", d.Status.SnoozeRemaining)
	}
	if d.Snooze != nil {
		event = event.Int("snooze_remaining", d.Snooze.Remaining)
	}
	event.Msg(clioutput.ActionLabel(command))
	return nil
}

// NewCommand returns the dnd command tree.
func NewCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	parent := &cobra.Command{Use: "dnd", Short: "Inspect or change Do Not Disturb"}
	for _, action := range []string{"info", "team", "snooze", "end-snooze", "end"} {
		cmd := &cobra.Command{Use: action, Args: cobra.NoArgs, SilenceUsage: true, RunE: func(cmd *cobra.Command, _ []string) error { return run(cmd, runtime, action) }}
		switch action {
		case "info":
			cmd.Short = "Show a user's Do Not Disturb settings"
			cmd.Flags().StringP("user", "u", "", "User ID; defaults to the authenticated user")
		case "team":
			cmd.Short = "Show Do Not Disturb settings for selected users"
			cmd.Flags().StringSliceP("users", "U", nil, "User IDs; repeat or comma-separate")
		case "snooze":
			cmd.Short = "Snooze notifications for the authenticated user"
			cmd.Flags().IntP("minutes", "m", 0, "Snooze duration in positive whole minutes")
		case "end-snooze":
			cmd.Short = "End the authenticated user's notification snooze"
		case "end":
			cmd.Short = "End the authenticated user's scheduled Do Not Disturb session"
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
	d := result{}
	var users []string
	switch action {
	case "info":
		d.User, _ = cmd.Flags().GetString("user")
		d.User = strings.TrimSpace(d.User)
	case "team":
		users, _ = cmd.Flags().GetStringSlice("users")
		if len(users) == 0 || len(users) > 50 {
			return clioutput.WriteCommandError(c, clioutput.ValidationCLIError("users requires between 1 and 50 user IDs"))
		}
		for i := range users {
			users[i] = strings.TrimSpace(users[i])
			if users[i] == "" {
				return clioutput.WriteCommandError(c, clioutput.ValidationCLIError("users must contain nonempty user IDs"))
			}
		}
	case "snooze":
		d.Minutes, _ = cmd.Flags().GetInt("minutes")
		if d.Minutes < 1 {
			return clioutput.WriteCommandError(c, clioutput.ValidationCLIError("minutes must be positive"))
		}
	}
	mutation := action != "info" && action != "team"
	if mutation && cliruntime.DryRun(cmd) {
		d.DryRun = true
		return c.WriteResult(c.Command, d)
	}
	if mutation && profile.TokenType != config.TokenTypeUser {
		return clioutput.WriteCommandError(c, clioutput.AuthCLIError("changing Do Not Disturb requires a user token with dnd:write"))
	}
	client, err := slackclient.Client(cmd, profile, runtime)
	if err != nil {
		return clioutput.WriteCommandError(c, slackclient.CLIError(cmd.Context(), err))
	}
	scope := "dnd:read"
	if mutation {
		scope = "dnd:write"
	}
	if err = cliscope.Require(cmd.Context(), client, cliscope.AllOf(scope)); err != nil {
		return clioutput.WriteCommandError(c, clioutput.CliErrorFromSlack(cmd.Context(), err, ""))
	}
	switch action {
	case "info":
		var user *string
		if d.User != "" {
			user = &d.User
		}
		d.Status, err = client.GetDNDInfoContext(cmd.Context(), user, slack.DNDOptionTeamID(profile.TeamID))
	case "team":
		d.Users, err = client.GetDNDTeamInfoContext(cmd.Context(), users, slack.DNDOptionTeamID(profile.TeamID))
	case "snooze", "end-snooze":
		var status *slack.DNDStatus
		if action == "snooze" {
			status, err = client.SetSnoozeContext(cmd.Context(), d.Minutes)
		} else {
			status, err = client.EndSnoozeContext(cmd.Context())
		}
		if status != nil {
			d.Snooze = &snoozeStatus{Enabled: status.SnoozeEnabled, EndTime: status.SnoozeEndTime, Remaining: status.SnoozeRemaining}
		}
	case "end":
		err = client.EndDNDContext(cmd.Context())
	}
	if err == nil && action != "end" && d.Status == nil && d.Users == nil && d.Snooze == nil {
		err = errors.New("slack returned missing Do Not Disturb settings")
	}
	if err != nil {
		return clioutput.WriteCommandError(c, clioutput.CliErrorFromSlack(cmd.Context(), err, ""))
	}
	return c.WriteResult(c.Command, d)
}
