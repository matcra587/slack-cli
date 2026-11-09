//go:build live

package live_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	slack "github.com/slack-go/slack"
)

// liveCacheResult is independent of production DTOs; underscore keys matter.
type liveCacheResult struct {
	FromCache bool `json:"from_cache"`
	Truncated bool `json:"truncated"`
	Count     int  `json:"count"`
}

func TestLiveAdditionalPagination(t *testing.T) {
	r, s := setupLive(t)
	t.Run("users", func(t *testing.T) {
		expected := liveInventoryIDs(t, r.ctx, s, "users", false)
		rows, pages, err := r.listPages(r.ctx, []string{"lookup", "user", "--max-items", "1", "--include-deleted"}, "users", "id")
		if err != nil || pages < 2 || !sameIDs(rows, expected) {
			t.Fatalf("user pagination incomplete: pages=%d, error=%v", pages, err)
		}
		t.Logf("PAGINATION users pages=%d rows=%d", pages, len(rows))
	})
	t.Run("channels", func(t *testing.T) {
		expected := liveInventoryIDs(t, r.ctx, s, "channels", false)
		rows, pages, err := r.listPages(r.ctx, []string{"lookup", "channel", "--types", "all", "--max-items", "1"}, "channels", "id")
		if err != nil || pages < 2 || !sameIDs(rows, expected) {
			t.Fatalf("channel pagination incomplete: pages=%d, error=%v", pages, err)
		}
		t.Logf("PAGINATION channels pages=%d rows=%d", pages, len(rows))
	})
	t.Run("scheduled", func(t *testing.T) {
		expected := map[string]bool{}
		var receipts []liveReceipt
		for i := range 3 {
			liveCall(t, r, "schedule", r.plan.PublicChannel, "", fmt.Sprintf("Queued pagination review %d %s", i+1, r.plan.RunID))
			v := lastReceipt(t, r)
			expected[v.ScheduledID] = true
			receipts = append(receipts, v)
		}
		rows, pages, err := r.listPages(r.ctx, []string{"message", "scheduled", "list", "--channel", r.plan.PublicChannel, "--limit", "1"}, "scheduled_messages", "id")
		if err != nil || pages < 2 {
			t.Fatalf("scheduled pagination incomplete: pages=%d, error=%v", pages, err)
		}
		for id := range expected {
			if rows[id] == nil {
				t.Fatal("scheduled pages lost an owned schedule")
			}
		}
		for _, v := range receipts {
			liveCall(t, r, "cancel", v.Channel, v.ScheduledID, "")
			present, err := s.Inspect(r.ctx, v)
			if err != nil || present || r.markAbsent(v) != nil {
				t.Fatal("scheduled pagination cleanup not verified")
			}
		}
		t.Logf("PAGINATION scheduled pages=%d owned=%d", pages, len(expected))
	})
	t.Run("search", func(t *testing.T) {
		if !slices.Contains(s.scopes, "search:read") {
			t.Fatal("search:read is required for the requested live pagination test")
		}
		expected := map[string]bool{}
		for i := range 3 {
			liveCall(t, r, "send", r.plan.PublicChannel, "", fmt.Sprintf("Pagination search review %d %s", i+1, r.plan.RunID))
			expected[lastReceipt(t, r).TS] = true
		}
		query := `"` + r.plan.RunID + `"`
		deadline := time.Now().Add(45 * time.Second)
		for {
			params := slack.NewSearchParameters()
			params.Count = 100
			result, err := s.client.SearchMessagesContext(r.ctx, query, params)
			if err != nil {
				t.Fatal("independent search indexing check failed")
			}
			found := map[string]bool{}
			for _, match := range result.Matches {
				if expected[match.Timestamp] {
					found[match.Timestamp] = true
				}
			}
			if maps.Equal(found, expected) {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("owned messages were not indexed within the search budget")
			}
			select {
			case <-r.ctx.Done():
				t.Fatal("search indexing canceled")
			case <-time.After(5 * time.Second):
			}
		}
		rows, pages, err := r.listPages(r.ctx, []string{"lookup", "messages", "--query", query, "--max-items", "1"}, "matches", "ts")
		if err != nil || pages < 2 || !sameIDs(rows, expected) {
			t.Fatalf("search pagination incomplete: pages=%d, error=%v", pages, err)
		}
		deleteOwnedMessages(t, r, s)
		t.Logf("PAGINATION search pages=%d rows=%d", pages, len(rows))
	})
	for _, resource := range []string{"users", "channels"} {
		t.Run("cache "+resource, func(t *testing.T) {
			expected := liveInventoryIDs(t, r.ctx, s, resource, true)
			for _, args := range [][]string{{"cache", "clear", resource}, {"cache", resource, "--page-size", "1", "--max-pages", "1"}} {
				data := liveRead(t, r, args...)
				if args[1] != "clear" {
					var limited liveCacheResult
					if json.Unmarshal(data, &limited) != nil || !limited.Truncated || limited.FromCache {
						t.Fatal("one-page cache priming did not report truncation")
					}
				}
			}
			full := liveRead(t, r, "cache", resource, "--page-size", "1", "--max-pages", "100", "--refresh")
			var result liveCacheResult
			var fields map[string]json.RawMessage
			var rows []struct{ ID string }
			if json.Unmarshal(full, &result) != nil || result.Truncated || result.FromCache || result.Count < 2 || json.Unmarshal(full, &fields) != nil || json.Unmarshal(fields[resource], &rows) != nil || len(rows) != result.Count {
				t.Fatal("complete cache priming did not collect multiple pages")
			}
			ids := map[string]bool{}
			for _, row := range rows {
				if row.ID == "" || ids[row.ID] {
					t.Fatal("cache pagination lost or duplicated identities")
				}
				ids[row.ID] = true
			}
			if len(expected) == 0 || !maps.Equal(ids, expected) {
				t.Fatal("cache pages differ from independent complete inventory")
			}
			cacheRoot := ""
			for _, value := range r.env {
				if after, ok := strings.CutPrefix(value, "XDG_CACHE_HOME="); ok {
					cacheRoot = after
				}
			}
			cacheFile := filepath.Join(cacheRoot, "slick", r.plan.Workspace, resource+".json")
			before, err := os.ReadFile(cacheFile)
			if err != nil {
				t.Fatal("cache file missing after full priming")
			}
			warm := liveRead(t, r, "cache", resource, "--page-size", "1", "--max-pages", "100")
			var warmResult liveCacheResult
			after, err := os.ReadFile(cacheFile)
			if json.Unmarshal(warm, &warmResult) != nil || !warmResult.FromCache || warmResult.Truncated || warmResult.Count != result.Count || err != nil || !bytes.Equal(before, after) {
				t.Fatal("warm cache changed complete results or rewrote state")
			}
			liveRead(t, r, "cache", "clear", resource)
			t.Logf("PAGINATION cache_%s page_size=1 count=%d truncated_bound=verified warm=verified", resource, result.Count)
		})
	}
}

func sameIDs(rows map[string]json.RawMessage, expected map[string]bool) bool {
	if len(rows) != len(expected) {
		return false
	}
	for id := range expected {
		if rows[id] == nil {
			return false
		}
	}
	return true
}

// Each subtest reads its own independent inventory, including focused reruns.
func liveInventoryIDs(t *testing.T, ctx context.Context, s *slackObserver, resource string, cache bool) map[string]bool {
	t.Helper()
	expected := map[string]bool{}
	if resource == "users" {
		users, err := s.client.GetUsersContext(ctx)
		if err != nil {
			t.Fatal("independent user inventory failed")
		}
		for _, user := range users {
			if !cache || !user.Deleted {
				expected[user.ID] = true
			}
		}
		return expected
	}
	cursor := ""
	seen := map[string]bool{"": true}
	for range 100 {
		channels, next, err := s.client.GetConversationsContext(ctx, &slack.GetConversationsParameters{Types: []string{"public_channel", "private_channel", "im", "mpim"}, Limit: 200, Cursor: cursor, ExcludeArchived: cache})
		if err != nil {
			t.Fatal("independent conversation inventory failed")
		}
		for _, channel := range channels {
			expected[channel.ID] = true
		}
		if next == "" {
			return expected
		}
		if seen[next] {
			t.Fatal("independent inventory repeated cursor")
		}
		seen[next] = true
		cursor = next
	}
	t.Fatal("independent inventory exceeded page budget")
	return nil
}
