package main

import (
	"context"
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

func newLookupUserCommand(runtime *RootRuntime) *cobra.Command {
	var maxItems int
	var cursor string
	var filter string
	var presence bool
	userCmd := &cobra.Command{
		Use:          "user",
		Short:        "Look up Slack users",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			userID, _ := cmd.Flags().GetString("user")
			if strings.TrimSpace(userID) != "" {
				return runUserInfoValue(cmd, runtime, "lookup.user", userID, presence)
			}
			return runUserListWithCommand(cmd, runtime, "lookup.user", maxItems, cursor, filter, presence)
		},
	}
	userCmd.Flags().String("user", "", "Slack user ID")
	userCmd.Flags().IntVar(&maxItems, "max-items", 0, "Maximum users to return")
	userCmd.Flags().StringVar(&cursor, "cursor", "", "Pagination cursor")
	userCmd.Flags().StringVar(&filter, "filter", "", "Filter by ID or name")
	userCmd.Flags().BoolVar(&presence, "presence", false, "Fetch presence")
	return userCmd
}

func runUserListWithCommand(cmd *cobra.Command, runtime *RootRuntime, command string, maxItems int, cursor, filter string, presence bool) error {
	ctx, profile, _, err := commandContext(cmd, runtime)
	if err != nil {
		return writeRuntimeError(runtime, validationCLIError(err.Error()))
	}
	client, err := slackClient(cmd, profile, runtime)
	if err != nil {
		return writeCommandError(ctx, authCLIError(err.Error()))
	}
	if err := requireSlackScopes(cmd.Context(), client, allScopes("users:read")); err != nil {
		return writeCommandError(ctx, cliErrorFromSlack(err))
	}
	options := []slackgo.GetUsersOption{}
	if maxItems > 0 {
		options = append(options, slackgo.GetUsersOptionLimit(maxItems))
	}
	if cursor != "" {
		options = append(options, slackgo.GetUsersOptionCursor(cursor))
	}
	options = append(options, slackgo.GetUsersOptionPresence(presence))
	pager := client.GetUsersPaginated(options...)
	page, err := pager.Next(context.Background())
	if err != nil {
		return writeCommandError(ctx, cliErrorFromSlack(err))
	}
	users := filterUsers(cliUsersFromSlack(page.Users), filter)
	return ctx.WriteResultWithPagination(command, userListData{Users: users}, &Pagination{
		Cursor:        stringPtr(cursor),
		NextCursor:    stringPtr(page.Cursor),
		HasMore:       page.Cursor != "",
		MaxItems:      intPtr(maxItems),
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
		return writeCommandError(ctx, cliErrorFromSlack(err))
	}
	user, err := client.GetUserInfoContext(context.Background(), userID)
	if err != nil {
		return writeCommandError(ctx, cliErrorFromSlack(err))
	}
	result := cliUserFromSlack(*user)
	if presence {
		presenceResult, err := client.GetUserPresenceContext(context.Background(), userID)
		if err != nil {
			return writeCommandError(ctx, cliErrorFromSlack(err))
		}
		result.Presence = stringPtr(presenceResult.Presence)
	}
	return ctx.WriteResult(command, userInfoData{User: result})
}

func cliUsersFromSlack(users []slackgo.User) []cliUser {
	out := make([]cliUser, 0, len(users))
	for _, user := range users {
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
