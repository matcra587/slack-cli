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
	Matches    []clioutput.SearchMessage `json:"matches"`
	Query      string                    `json:"query,omitempty"`
	Full       bool                      `json:"-"`
	Truncated  bool                      `json:"truncated,omitempty"`
	Pagination *clioutput.Pagination     `json:"pagination,omitempty"`
}

func (d Data) WithPagination(pagination *clioutput.Pagination) any {
	d.Pagination = pagination
	return d
}

var (
	_ clioutput.PlainRenderer  = Data{}
	_ clioutput.ResultEnricher = Data{}
)

func (d Data) EnrichResult(c *clioutput.CommandContext) any {
	for i := range d.Matches {
		c.EnrichSearchChannel(&d.Matches[i].Channel)
	}
	return d
}

func (d Data) WritePlain(c *clioutput.CommandContext, command string, pagination *clioutput.Pagination) error {
	if d.Truncated {
		c.StderrLogger().Warn().Msg("Slack search stops at page 100; narrow the query to read more matches")
	}
	if len(d.Matches) == 0 {
		clioutput.ApplyNumberKeyStyle(c.StdoutLogger(), "count")
		event := c.ResultEvent(command).
			Str("query", d.Query).
			Str("count", strconv.Itoa(len(d.Matches)))
		c.FinishResult(event, command, pagination)
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
  $ slick lookup messages --query <query> --max-items <n> --output=json

  # Paginate through results
  $ slick lookup messages --query <query> --max-items <n> --cursor <meta.pagination.next_cursor> --output=json`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSearchMessages(cmd, runtime, query, maxItems, cursor, full)
		},
	}
	messagesCmd.Flags().StringVarP(&query, "query", "q", "", "Search query")
	messagesCmd.Flags().IntVarP(&maxItems, "max-items", "M", 0, "Matches per page (1-100; 0 uses Slack default 20)")
	messagesCmd.Flags().StringVarP(&cursor, "cursor", "C", "", "Search result page number (1-100)")
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
	if maxItems < 0 || maxItems > 100 {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("--max-items must be between 0 and 100; 0 uses Slack's default"))
	}
	params := slackgo.NewSearchParameters()
	if maxItems > 0 {
		params.Count = maxItems
	}
	if cursor != "" {
		page, parseErr := strconv.Atoi(cursor)
		if parseErr != nil || page < 1 || page > 100 {
			return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("cursor must be a search result page number between 1 and 100"))
		}
		params.Page = page
	}
	if profile.TokenType != config.TokenTypeUser {
		return clioutput.WriteCommandError(ctx, clioutput.AuthCLIError("lookup messages requires a user token with search:read"))
	}

	client, err := slackclient.Client(cmd, profile, runtime)
	if err != nil {
		return clioutput.WriteCommandError(ctx, slackclient.CLIError(cmd.Context(), err))
	}
	if err := cliscope.Require(cmd.Context(), client, cliscope.AllOf("search:read")); err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err, ""))
	}

	result, err := client.SearchMessagesContext(cmd.Context(), query, params)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.CliErrorFromSlack(cmd.Context(), err, ""))
	}
	page, pages, valid := searchPageBounds(result, params.Page)
	if !valid {
		return clioutput.WriteCommandError(ctx, clioutput.RuntimeCLIError("Slack returned incomplete or inconsistent search results"))
	}
	matches := cliSearchMessagesFromSlack(result.Matches)
	hasMore := page < pages
	truncated := hasMore && page == 100
	var next *string
	if hasMore && !truncated {
		next = cliutil.StringPtr(strconv.Itoa(page + 1))
	}

	return ctx.WriteResultWithPagination("lookup.messages", Data{Matches: matches, Query: query, Full: full, Truncated: truncated}, &clioutput.Pagination{
		Cursor:        cliutil.StringPtr(cursor),
		NextCursor:    next,
		HasMore:       hasMore,
		MaxItems:      cliutil.IntPtr(maxItems),
		ItemsReturned: new(len(matches)),
	})
}

func searchPageBounds(result *slackgo.SearchMessages, requestedPage int) (int, int, bool) {
	if result == nil || result.Matches == nil {
		return 0, 0, false
	}
	current, legacy := result.Pagination, result.Paging
	if current.Page < 0 || current.PageCount < 0 || legacy.Page < 0 || legacy.Pages < 0 {
		return 0, 0, false
	}
	if current.Page > 0 && legacy.Page > 0 && current.Page != legacy.Page {
		return 0, 0, false
	}
	if current.PageCount > 0 && legacy.Pages > 0 && current.PageCount != legacy.Pages {
		return 0, 0, false
	}
	page, pages := max(current.Page, legacy.Page), max(current.PageCount, legacy.Pages)
	if page != requestedPage || (len(result.Matches) > 0 && page > pages) {
		return 0, 0, false
	}
	return page, pages, true
}

func cliSearchMessagesFromSlack(messages []slackgo.SearchMessage) []clioutput.SearchMessage {
	out := make([]clioutput.SearchMessage, 0, len(messages))
	for _, message := range messages {
		out = append(out, clioutput.SearchMessageFromSlack(message))
	}
	return out
}
