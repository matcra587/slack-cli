package user

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

// ListData is the result type for user list operations.
type ListData struct {
	Users      []clioutput.User      `json:"users"`
	Pagination *clioutput.Pagination `json:"pagination,omitempty"`
}

func (d ListData) WithPagination(pagination *clioutput.Pagination) any {
	d.Pagination = pagination
	return d
}

var _ clioutput.PlainRenderer = ListData{}

func (d ListData) WritePlain(c *clioutput.CommandContext, command string, pagination *clioutput.Pagination) error {
	return c.WriteUsers(command, d.Users, pagination)
}

// InfoData is the result type for user info operations.
type InfoData struct {
	User clioutput.User `json:"user"`
}

var _ clioutput.PlainRenderer = InfoData{}

func (d InfoData) WritePlain(c *clioutput.CommandContext, command string, _ *clioutput.Pagination) error {
	user := d.User
	event := c.ResultEventWithStyles(command, clioutput.EntityFieldStyle("user", user.ID)).
		Str("user", user.ID).
		Str("name", user.Name)
	if user.Deleted != nil {
		clioutput.ApplyBoolStateStyle(c.StdoutLogger(), c.Theme, "deleted", *user.Deleted)
	}
	event = clioutput.AddBoolField(event, "deleted", user.Deleted)
	if user.Timezone != nil {
		timezone := *user.Timezone
		if !c.StdoutLogger().Output().ColorsDisabled() {
			clioutput.ApplyPreStyledKey(c.StdoutLogger(), "timezone")
			timezone = clioutput.RenderTimezone(c.Theme, timezone)
		}
		event = event.Str("timezone", timezone)
	}
	if user.Presence != nil {
		event = event.Str("presence", *user.Presence)
	}
	if user.StatusText != nil {
		event = event.Str("status_text", *user.StatusText)
	}
	event.Msg(clioutput.ActionLabel(command))
	return nil
}

type listOptions struct {
	MaxItems       int
	Cursor         string
	Filter         string
	Presence       bool
	IncludeDeleted bool
}

// NewLookupUserCommand returns the "lookup user" subcommand.
func NewLookupUserCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	opts := listOptions{}
	userCmd := &cobra.Command{
		Use:          "user",
		Short:        "Look up Slack users",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			userID, _ := cmd.Flags().GetString("user")
			if strings.TrimSpace(userID) != "" {
				return runUserInfoValue(cmd, runtime, "lookup.user", userID, opts.Presence)
			}
			return runUserListWithCommand(cmd, runtime, "lookup.user", opts)
		},
	}
	userCmd.Flags().StringP("user", "u", "", "Slack user ID")
	userCmd.Flags().IntVarP(&opts.MaxItems, "max-items", "M", 0, "Maximum users in one list page (0 uses the default page size)")
	userCmd.Flags().StringVarP(&opts.Cursor, "cursor", "C", "", "List pagination cursor")
	userCmd.Flags().StringVarP(&opts.Filter, "filter", "f", "", "Filter the list by ID or name")
	userCmd.Flags().BoolVarP(&opts.Presence, "presence", "p", false, "Fetch presence for an explicit --user")
	userCmd.Flags().BoolVarP(&opts.IncludeDeleted, "include-deleted", "d", false, "Include deleted or deactivated users in the list")
	return userCmd
}

func runUserListWithCommand(cmd *cobra.Command, runtime *cliruntime.RootRuntime, command string, opts listOptions) error {
	ctx, profile, _, err := cliruntime.CommandContext(cmd, runtime)
	if err != nil {
		return cliruntime.WriteRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
	}
	if opts.Presence {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("--presence requires --user; Slack no longer supports presence in user lists"))
	}
	if opts.MaxItems < 0 {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("--max-items must be zero or greater"))
	}
	client, err := slackclient.Client(cmd, profile, runtime)
	if err != nil {
		return clioutput.WriteCommandError(ctx, slackclient.CLIError(cmd.Context(), err))
	}
	if err := cliscope.Require(cmd.Context(), client, cliscope.AllOf("users:read")); err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err, ""))
	}
	options := []slackgo.GetUsersOption{}
	if opts.MaxItems > 0 {
		options = append(options, slackgo.GetUsersOptionLimit(opts.MaxItems))
	}
	if opts.Cursor != "" {
		options = append(options, slackgo.GetUsersOptionCursor(opts.Cursor))
	}
	// Slack retired the users.list presence parameter; explicit user lookups
	// still fetch presence through users.getPresence below.
	pager := client.GetUsersPaginated(options...)
	page, err := pager.Next(cmd.Context())
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err, ""))
	}
	users := filterUsers(usersFromSlack(page.Users, opts.IncludeDeleted), opts.Filter)
	return ctx.WriteResultWithPagination(command, ListData{Users: users}, &clioutput.Pagination{
		Cursor:        cliutil.StringPtr(opts.Cursor),
		NextCursor:    cliutil.StringPtr(page.Cursor),
		HasMore:       page.Cursor != "",
		MaxItems:      cliutil.IntPtr(opts.MaxItems),
		ItemsReturned: new(len(users)),
	})
}

func runUserInfoValue(cmd *cobra.Command, runtime *cliruntime.RootRuntime, command, userID string, presence bool) error {
	ctx, profile, _, err := cliruntime.CommandContext(cmd, runtime)
	if err != nil {
		return cliruntime.WriteRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
	}
	for _, flag := range []string{"max-items", "cursor", "filter", "include-deleted"} {
		if cmd.Flags().Changed(flag) {
			return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("--"+flag+" is only supported when listing users; omit --user"))
		}
	}
	userID = strings.TrimSpace(climessage.ResolveAlias(profile, strings.TrimSpace(userID)))
	if userID == "" {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("user is required"))
	}
	client, err := slackclient.Client(cmd, profile, runtime)
	if err != nil {
		return clioutput.WriteCommandError(ctx, slackclient.CLIError(cmd.Context(), err))
	}
	if err := cliscope.Require(cmd.Context(), client, cliscope.AllOf("users:read")); err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err, ""))
	}
	user, err := client.GetUserInfoContext(cmd.Context(), userID)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err, ""))
	}
	result := clioutput.UserFromSlack(*user)
	if presence {
		presenceResult, err := client.GetUserPresenceContext(cmd.Context(), userID)
		if err != nil {
			return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err, ""))
		}
		if strings.TrimSpace(presenceResult.Presence) != "" {
			p := presenceResult.Presence
			result.Presence = &p
		}
	}
	return ctx.WriteResult(command, InfoData{User: result})
}

// UsersFromSlack converts a slice of slack-go Users to User DTOs.
func UsersFromSlack(users []slackgo.User, includeDeleted bool) []clioutput.User {
	return usersFromSlack(users, includeDeleted)
}

func usersFromSlack(users []slackgo.User, includeDeleted bool) []clioutput.User {
	out := make([]clioutput.User, 0, len(users))
	for _, user := range users {
		if user.Deleted && !includeDeleted {
			continue
		}
		out = append(out, clioutput.UserFromSlack(user))
	}
	return out
}

func filterUsers(users []clioutput.User, filter string) []clioutput.User {
	if filter == "" {
		return users
	}
	out := make([]clioutput.User, 0, len(users))
	for _, user := range users {
		if cliutil.ContainsAnyFold(filter, user.ID, user.Name) {
			out = append(out, user)
		}
	}
	return out
}
