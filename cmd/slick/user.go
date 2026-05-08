package main

import (
	"strings"

	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

type userListData struct {
	Users []cliUser `json:"users"`
}

type userInfoData struct {
	User cliUser `json:"user"`
}

type userListOptions struct {
	MaxItems       int
	Cursor         string
	Filter         string
	Presence       bool
	IncludeDeleted bool
}

func newLookupUserCommand(runtime *RootRuntime) *cobra.Command {
	opts := userListOptions{}
	userCmd := &cobra.Command{
		Use:          "user",
		Short:        "Look up Slack users",
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

func runUserListWithCommand(cmd *cobra.Command, runtime *RootRuntime, command string, opts userListOptions) error {
	ctx, profile, _, err := commandContext(cmd, runtime)
	if err != nil {
		return writeRuntimeError(runtime, validationCLIError(err.Error()))
	}
	client, err := slackClient(cmd, profile, runtime)
	if err != nil {
		return writeCommandError(ctx, authCLIError(err.Error()))
	}
	if err := requireSlackScopes(cmd.Context(), client, allScopes("users:read")); err != nil {
		return writeCommandError(ctx, cliErrorFromSlack(cmd.Context(), err))
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
		return writeCommandError(ctx, cliErrorFromSlack(cmd.Context(), err))
	}
	users := filterUsers(cliUsersFromSlack(page.Users, opts.IncludeDeleted), opts.Filter)
	return ctx.WriteResultWithPagination(command, userListData{Users: users}, &Pagination{
		Cursor:        stringPtr(opts.Cursor),
		NextCursor:    stringPtr(page.Cursor),
		HasMore:       page.Cursor != "",
		MaxItems:      intPtr(opts.MaxItems),
		ItemsReturned: intPtr(len(users)),
	})
}

func runUserInfoValue(cmd *cobra.Command, runtime *RootRuntime, command, userID string, presence bool) error {
	ctx, profile, _, err := commandContext(cmd, runtime)
	if err != nil {
		return writeRuntimeError(runtime, validationCLIError(err.Error()))
	}
	if strings.TrimSpace(userID) == "" {
		return writeCommandError(ctx, validationCLIError("user is required"))
	}
	userID = resolveAlias(profile, strings.TrimSpace(userID))
	client, err := slackClient(cmd, profile, runtime)
	if err != nil {
		return writeCommandError(ctx, authCLIError(err.Error()))
	}
	if err := requireSlackScopes(cmd.Context(), client, allScopes("users:read")); err != nil {
		return writeCommandError(ctx, cliErrorFromSlack(cmd.Context(), err))
	}
	user, err := client.GetUserInfoContext(cmd.Context(), userID)
	if err != nil {
		return writeCommandError(ctx, cliErrorFromSlack(cmd.Context(), err))
	}
	result := cliUserFromSlack(*user)
	if presence {
		presenceResult, err := client.GetUserPresenceContext(cmd.Context(), userID)
		if err != nil {
			return writeCommandError(ctx, cliErrorFromSlack(cmd.Context(), err))
		}
		if strings.TrimSpace(presenceResult.Presence) != "" {
			result.Presence = stringPtr(presenceResult.Presence)
		}
	}
	return ctx.WriteResult(command, userInfoData{User: result})
}

func cliUsersFromSlack(users []slackgo.User, includeDeleted bool) []cliUser {
	out := make([]cliUser, 0, len(users))
	for _, user := range users {
		if user.Deleted && !includeDeleted {
			continue
		}
		out = append(out, cliUserFromSlack(user))
	}
	return out
}

func filterUsers(users []cliUser, filter string) []cliUser {
	if filter == "" {
		return users
	}
	filter = strings.ToLower(filter)
	var out []cliUser
	for _, user := range users {
		if strings.Contains(strings.ToLower(user.ID), filter) || strings.Contains(strings.ToLower(user.Name), filter) {
			out = append(out, user)
		}
	}
	return out
}
