package search

import (
	"strconv"
	"strings"

	"github.com/matcra587/slack-cli/internal/cli/cliutil"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	cliscope "github.com/matcra587/slack-cli/internal/cli/scope"
	slackclient "github.com/matcra587/slack-cli/internal/cli/slackclient"
	"github.com/matcra587/slack-cli/internal/config"
	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

// Data is the result type for search messages operations.
type Data struct {
	Matches []clioutput.SearchMessage `json:"matches"`
	Query   string                    `json:"query,omitempty"`
	Full    bool                      `json:"-"`
}

var _ clioutput.PlainRenderer = Data{}

func (d Data) WritePlain(c *clioutput.CommandContext, command string, pagination *clioutput.Pagination) error {
	if len(d.Matches) == 0 {
		clioutput.ApplyNumberKeyStyle(c.StdoutLogger(), "count")
		c.ResultEvent(command).
			Str("query", d.Query).
			Str("count", strconv.Itoa(len(d.Matches))).
			Msg(clioutput.ActionLabel(command))
		return nil
	}
	return c.WriteSearch(command, d.Matches, d.Full, pagination)
}

// NewLookupMessagesCommand returns the "lookup messages" subcommand.
func NewLookupMessagesCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
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

func runSearchMessages(cmd *cobra.Command, runtime *cliruntime.RootRuntime, query string, maxItems int, cursor string, full bool) error {
	ctx, profile, _, err := cliruntime.CommandContext(cmd, runtime)
	if err != nil {
		return cliruntime.WriteRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
	}
	if strings.TrimSpace(query) == "" {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("query is required"))
	}
	if profile.TokenType != config.TokenTypeUser {
		return clioutput.WriteCommandError(ctx, clioutput.AuthCLIError("lookup messages requires a user token with search:read"))
	}

	client, err := slackclient.Client(cmd, profile, runtime)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.AuthCLIError(err.Error()))
	}
	if err := cliscope.Require(cmd.Context(), client, cliscope.AllOf("search:read")); err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err))
	}

	params := slackgo.SearchParameters{}
	if maxItems > 0 {
		params.Count = maxItems
	}
	if cursor != "" {
		page, parseErr := strconv.Atoi(cursor)
		if parseErr != nil {
			return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("cursor must be a search result page number"))
		}
		params.Page = page
	}

	result, err := client.SearchMessagesContext(cmd.Context(), query, params)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err))
	}
	matches := cliSearchMessagesFromSlack(result.Matches)
	var next *string
	if result.Pagination.Page > 0 && result.Pagination.Page < result.PageCount {
		next = cliutil.StringPtr(strconv.Itoa(result.Pagination.Page + 1))
	}

	return ctx.WriteResultWithPagination("search.messages", Data{Matches: matches, Query: query, Full: full}, &clioutput.Pagination{
		Cursor:        cliutil.StringPtr(cursor),
		NextCursor:    next,
		HasMore:       next != nil,
		MaxItems:      cliutil.IntPtr(maxItems),
		ItemsReturned: cliutil.IntPtr(len(matches)),
	})
}

func cliSearchMessagesFromSlack(messages []slackgo.SearchMessage) []clioutput.SearchMessage {
	out := make([]clioutput.SearchMessage, 0, len(messages))
	for _, message := range messages {
		out = append(out, clioutput.SearchMessageFromSlack(message))
	}
	return out
}
