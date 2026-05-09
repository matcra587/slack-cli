package main

import (
	"strconv"
	"strings"

	"github.com/matcra587/slack-cli/internal/config"
	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

type searchCommandData struct {
	Matches []cliSearchMessage `json:"matches"`
	Full    bool               `json:"-"`
}

var _ PlainRenderer = searchCommandData{}

func (d searchCommandData) WritePlain(c *CommandContext, command string, pagination *Pagination) error {
	return c.WriteSearch(command, d, pagination)
}

func newLookupMessagesCommand(runtime *RootRuntime) *cobra.Command {
	var query string
	var maxItems int
	var cursor string
	var full bool
	messagesCmd := &cobra.Command{
		Use:   "messages",
		Short: "Search Slack messages",
		Args:  cobra.NoArgs,
		Example: `  # Search for messages matching a query
  $ slick lookup messages --query <query> --max-items <n> --json

  # Paginate through results
  $ slick lookup messages --query <query> --max-items <n> --cursor <meta.pagination.next_cursor> --json`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSearchMessages(cmd, runtime, query, maxItems, cursor, full)
		},
	}
	messagesCmd.Flags().StringVarP(&query, "query", "q", "", "Search query")
	messagesCmd.Flags().IntVarP(&maxItems, "max-items", "M", 0, "Maximum matches to return")
	messagesCmd.Flags().StringVarP(&cursor, "cursor", "C", "", "Pagination cursor")
	messagesCmd.Flags().BoolVarP(&full, "full", "F", false, "Show full text in plain mode")
	return messagesCmd
}

func runSearchMessages(cmd *cobra.Command, runtime *RootRuntime, query string, maxItems int, cursor string, full bool) error {
	ctx, profile, _, err := commandContext(cmd, runtime)
	if err != nil {
		return writeRuntimeError(runtime, validationCLIError(err.Error()))
	}
	if strings.TrimSpace(query) == "" {
		return writeCommandError(ctx, validationCLIError("query is required"))
	}
	if profile.TokenType != config.TokenTypeUser {
		return writeCommandError(ctx, authCLIError("lookup messages requires a user token with search:read"))
	}

	client, err := slackClient(cmd, profile, runtime)
	if err != nil {
		return writeCommandError(ctx, authCLIError(err.Error()))
	}
	if err := requireSlackScopes(cmd.Context(), client, allScopes("search:read")); err != nil {
		return writeCommandError(ctx, cliErrorFromSlack(cmd.Context(), err))
	}

	params := slackgo.SearchParameters{}
	if maxItems > 0 {
		params.Count = maxItems
	}
	if cursor != "" {
		page, parseErr := strconv.Atoi(cursor)
		if parseErr != nil {
			return writeCommandError(ctx, validationCLIError("cursor must be a search result page number"))
		}
		params.Page = page
	}

	result, err := client.SearchMessagesContext(cmd.Context(), query, params)
	if err != nil {
		return writeCommandError(ctx, cliErrorFromSlack(cmd.Context(), err))
	}
	matches := cliSearchMessagesFromSlack(result.Matches)
	var next *string
	if result.Pagination.Page > 0 && result.Pagination.Page < result.PageCount {
		next = stringPtr(strconv.Itoa(result.Pagination.Page + 1))
	}

	return ctx.WriteResultWithPagination("search.messages", searchCommandData{Matches: matches, Full: full}, &Pagination{
		Cursor:        stringPtr(cursor),
		NextCursor:    next,
		HasMore:       next != nil,
		MaxItems:      intPtr(maxItems),
		ItemsReturned: intPtr(len(matches)),
	})
}

func cliSearchMessagesFromSlack(messages []slackgo.SearchMessage) []cliSearchMessage {
	out := make([]cliSearchMessage, 0, len(messages))
	for _, message := range messages {
		out = append(out, cliSearchMessageFromSlack(message))
	}
	return out
}
