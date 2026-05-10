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
	Users []clioutput.CliUser `json:"users"`
}

var _ clioutput.PlainRenderer = ListData{}

func (d ListData) WritePlain(c *clioutput.CommandContext, command string, pagination *clioutput.Pagination) error {
	return c.WriteUsers(command, d.Users, pagination)
}

// InfoData is the result type for user info operations.
type InfoData struct {
	User clioutput.CliUser `json:"user"`
}

var _ clioutput.PlainRenderer = InfoData{}

func (d InfoData) WritePlain(c *clioutput.CommandContext, command string, _ *clioutput.Pagination) error {
	user := d.User
	event := c.ResultEventWithStyles(command, clioutput.EntityFieldStyle("user", user.ID)).
		Str("user", user.ID).
		Str("name", user.Name)
	event = clioutput.AddBoolField(event, "deleted", user.Deleted)
	if user.Timezone != nil {
		event = event.Str("timezone", *user.Timezone)
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
	userCmd.Flags().IntVarP(&opts.MaxItems, "max-items", "M", 0, "Maximum users to return")
	userCmd.Flags().StringVarP(&opts.Cursor, "cursor", "C", "", "Pagination cursor")
	userCmd.Flags().StringVarP(&opts.Filter, "filter", "f", "", "Filter by ID or name")
	userCmd.Flags().BoolVarP(&opts.Presence, "presence", "p", false, "Fetch presence")
	userCmd.Flags().BoolVarP(&opts.IncludeDeleted, "include-deleted", "d", false, "Include deleted or deactivated users")
	return userCmd
}

func runUserListWithCommand(cmd *cobra.Command, runtime *cliruntime.RootRuntime, command string, opts listOptions) error {
	ctx, profile, _, err := cliruntime.CommandContext(cmd, runtime)
	if err != nil {
		return cliruntime.WriteRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
	}
	client, err := slackclient.Client(cmd, profile, runtime)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.AuthCLIError(err.Error()))
	}
	if err := cliscope.Require(cmd.Context(), client, cliscope.AllOf("users:read")); err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err))
	}
	options := []slackgo.GetUsersOption{}
	if opts.MaxItems > 0 {
		options = append(options, slackgo.GetUsersOptionLimit(opts.MaxItems))
	}
	if opts.Cursor != "" {
		options = append(options, slackgo.GetUsersOptionCursor(opts.Cursor))
	}
	options = append(options, slackgo.GetUsersOptionPresence(opts.Presence))
	pager := client.GetUsersPaginated(options...)
	page, err := pager.Next(cmd.Context())
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err))
	}
	users := filterUsers(cliUsersFromSlack(page.Users, opts.IncludeDeleted), opts.Filter)
	return ctx.WriteResultWithPagination(command, ListData{Users: users}, &clioutput.Pagination{
		Cursor:        cliutil.StringPtr(opts.Cursor),
		NextCursor:    cliutil.StringPtr(page.Cursor),
		HasMore:       page.Cursor != "",
		MaxItems:      cliutil.IntPtr(opts.MaxItems),
		ItemsReturned: cliutil.IntPtr(len(users)),
	})
}

func runUserInfoValue(cmd *cobra.Command, runtime *cliruntime.RootRuntime, command, userID string, presence bool) error {
	ctx, profile, _, err := cliruntime.CommandContext(cmd, runtime)
	if err != nil {
		return cliruntime.WriteRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
	}
	if strings.TrimSpace(userID) == "" {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("user is required"))
	}
	userID = climessage.ResolveAlias(profile, strings.TrimSpace(userID))
	client, err := slackclient.Client(cmd, profile, runtime)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.AuthCLIError(err.Error()))
	}
	if err := cliscope.Require(cmd.Context(), client, cliscope.AllOf("users:read")); err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err))
	}
	user, err := client.GetUserInfoContext(cmd.Context(), userID)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err))
	}
	result := clioutput.CliUserFromSlack(*user)
	if presence {
		presenceResult, err := client.GetUserPresenceContext(cmd.Context(), userID)
		if err != nil {
			return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err))
		}
		if strings.TrimSpace(presenceResult.Presence) != "" {
			p := presenceResult.Presence
			result.Presence = &p
		}
	}
	return ctx.WriteResult(command, InfoData{User: result})
}

// CliUsersFromSlack converts a slice of slack-go Users to CliUser DTOs.
func CliUsersFromSlack(users []slackgo.User, includeDeleted bool) []clioutput.CliUser {
	return cliUsersFromSlack(users, includeDeleted)
}

func cliUsersFromSlack(users []slackgo.User, includeDeleted bool) []clioutput.CliUser {
	out := make([]clioutput.CliUser, 0, len(users))
	for _, user := range users {
		if user.Deleted && !includeDeleted {
			continue
		}
		out = append(out, clioutput.CliUserFromSlack(user))
	}
	return out
}

func filterUsers(users []clioutput.CliUser, filter string) []clioutput.CliUser {
	if filter == "" {
		return users
	}
	out := make([]clioutput.CliUser, 0, len(users))
	for _, user := range users {
		if cliutil.ContainsAnyFold(filter, user.ID, user.Name) {
			out = append(out, user)
		}
	}
	return out
}
