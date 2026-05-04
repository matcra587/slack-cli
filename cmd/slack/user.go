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

func newUserCommand(runtime *RootRuntime) *cobra.Command {
	userCmd := &cobra.Command{
		Use:   "user",
		Short: "Discover Slack users",
	}

	var maxItems int
	var cursor string
	var filter string
	var presence bool
	listCmd := &cobra.Command{
		Use:          "list",
		Short:        "List Slack users",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUserList(cmd, runtime, maxItems, cursor, filter, presence)
		},
	}
	listCmd.Flags().IntVar(&maxItems, "max-items", 0, "Maximum users to return")
	listCmd.Flags().StringVar(&cursor, "cursor", "", "Pagination cursor")
	listCmd.Flags().StringVar(&filter, "filter", "", "Filter by ID or name")
	listCmd.Flags().BoolVar(&presence, "presence", false, "Fetch presence for each user")

	var infoPresence bool
	infoCmd := &cobra.Command{
		Use:          "info",
		Short:        "Show Slack user metadata",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUserInfo(cmd, runtime, infoPresence)
		},
	}
	infoCmd.Flags().String("user", "", "Slack user ID")
	infoCmd.Flags().BoolVar(&infoPresence, "presence", false, "Fetch presence")

	userCmd.AddCommand(listCmd)
	userCmd.AddCommand(infoCmd)
	return userCmd
}

func runUserList(cmd *cobra.Command, runtime *RootRuntime, maxItems int, cursor string, filter string, presence bool) error {
	ctx, profile, _, err := commandContext(cmd, runtime)
	if err != nil {
		return writeRuntimeError(runtime, validationCLIError(err.Error()))
	}
	client, err := slackClient(cmd, profile, runtime)
	if err != nil {
		return writeCommandError(ctx, authCLIError(err.Error()))
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
	return ctx.WriteResultWithPagination("user.list", userListData{Users: users}, &Pagination{
		Cursor:        stringPtr(cursor),
		NextCursor:    stringPtr(page.Cursor),
		HasMore:       page.Cursor != "",
		MaxItems:      intPtr(maxItems),
		ItemsReturned: intPtr(len(users)),
	})
}

func runUserInfo(cmd *cobra.Command, runtime *RootRuntime, presence bool) error {
	ctx, profile, _, err := commandContext(cmd, runtime)
	if err != nil {
		return writeRuntimeError(runtime, validationCLIError(err.Error()))
	}
	userID, _ := cmd.Flags().GetString("user")
	if strings.TrimSpace(userID) == "" {
		return writeCommandError(ctx, validationCLIError("user is required"))
	}
	client, err := slackClient(cmd, profile, runtime)
	if err != nil {
		return writeCommandError(ctx, authCLIError(err.Error()))
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
	return ctx.WriteResult("user.info", userInfoData{User: result})
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
