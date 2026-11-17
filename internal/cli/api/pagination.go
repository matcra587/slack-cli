package api

import (
	"encoding/json"
	"errors"
	"maps"
	"math"
	"strconv"

	publicapi "github.com/matcra587/slack-cli/api"
	"github.com/matcra587/slack-cli/internal/slackcatalog"
	"github.com/spf13/cobra"
)

func callPages(cmd *cobra.Command, client *publicapi.Client, method slackcatalog.Method, parameters map[string]any, opts *callOptions, report func(int)) (CallData, error) {
	data := CallData{Method: method.Name}
	params := maps.Clone(parameters)
	if !opts.paginate {
		err := client.CallWithContext(cmd.Context(), method.Name, params, &data.Response)
		return data, err
	}
	complete := false
	data.Complete = &complete
	data.Pages = []json.RawMessage{}
	seen := map[string]bool{}
	if method.Pagination == "cursor" {
		if _, exists := params["cursor"]; !exists && method.CursorInitial != "" {
			params["cursor"] = method.CursorInitial
		}
	}
	for pageIndex := range opts.maxPages {
		report(pageIndex + 1)
		cursor, _ := params["cursor"].(string)
		if cursor != "" {
			seen[cursor] = true
		}
		var response json.RawMessage
		if err := client.CallWithContext(cmd.Context(), method.Name, params, &response); err != nil {
			return data, err
		}
		data.Pages = append(data.Pages, response)
		data.CompletedPages++
		var object map[string]any
		if err := json.Unmarshal(response, &object); err != nil {
			return data, errors.New("invalid pagination response")
		}
		if method.Pagination == "cursor" {
			next, err := nextCursor(object)
			if err != nil {
				return data, err
			}
			data.NextCursor = next
			if next == "" {
				if object["has_more"] == true {
					return data, errors.New("pagination response has more results but no next cursor")
				}
				complete = true
				return data, nil
			}
			if seen[next] {
				return data, errors.New("slack repeated a pagination cursor")
			}
			params["cursor"] = next
		} else {
			page, pages, ok := pageBounds(object)
			if !ok {
				return data, errors.New("missing documented pagination metadata")
			}
			if page >= pages {
				data.NextPage = 0
				complete = true
				return data, nil
			}
			next := page + 1
			key := strconv.Itoa(next)
			if seen[key] {
				return data, errors.New("slack repeated a pagination page")
			}
			seen[key] = true
			data.NextPage = next
			params["page"] = next
		}
	}
	return data, nil
}

func nextCursor(object map[string]any) (string, error) {
	for _, key := range []string{"response_metadata", "pagination"} {
		if value, exists := object[key]; exists {
			metadata, ok := value.(map[string]any)
			if !ok {
				return "", errors.New("malformed pagination metadata")
			}
			if cursor, exists := metadata["next_cursor"]; exists {
				text, ok := cursor.(string)
				if !ok {
					return "", errors.New("malformed pagination cursor")
				}
				return text, nil
			}
		}
	}
	if cursor, exists := object["next_cursor"]; exists {
		text, ok := cursor.(string)
		if !ok {
			return "", errors.New("malformed pagination cursor")
		}
		return text, nil
	}
	for _, key := range []string{"messages", "files"} {
		if resource, ok := object[key].(map[string]any); ok {
			cursor, err := nextCursor(resource)
			if err != nil || cursor != "" {
				return cursor, err
			}
		}
	}
	if object["has_more"] == true {
		return "", errors.New("pagination response has more results but no next cursor")
	}
	return "", nil
}

func pageBounds(object map[string]any) (int, int, bool) {
	for _, key := range []string{"paging", "pagination"} {
		if value, exists := object[key]; exists {
			paging, ok := value.(map[string]any)
			if !ok {
				return 0, 0, false
			}
			page, validPage := nonnegativeInteger(paging["page"])
			pages, validPages := nonnegativeInteger(paging["pages"])
			if _, exists := paging["pages"]; !exists {
				count, validCount := nonnegativeInteger(paging["count"])
				total, validTotal := nonnegativeInteger(paging["total"])
				if !validCount || !validTotal || count == 0 {
					return 0, 0, false
				}
				pages = total / count
				if total%count != 0 {
					pages++
				}
				validPages = true
			}
			if !validPage || !validPages || page < 1 || page > max(pages, 1) {
				return 0, 0, false
			}
			return page, pages, true
		}
	}
	for _, key := range []string{"messages", "files"} {
		if resource, ok := object[key].(map[string]any); ok {
			if page, pages, ok := pageBounds(resource); ok {
				return page, pages, true
			}
		}
	}
	return 0, 0, false
}

func nonnegativeInteger(value any) (int, bool) {
	number, ok := value.(float64)
	// JSON numbers above 2^53 cannot describe exact integer bounds. The
	// architecture's int bound is checked separately before conversion.
	if !ok || math.IsNaN(number) || math.IsInf(number, 0) || number < 0 || number > 1<<53 || number >= float64(math.MaxInt) || math.Trunc(number) != number {
		return 0, false
	}
	return int(number), true
}
