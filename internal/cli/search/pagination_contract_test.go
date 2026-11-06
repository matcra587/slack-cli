package search_test

import (
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestSearchRejectsUnsupportedBoundsBeforeSlack(t *testing.T) {
	for _, flags := range [][]string{
		{"--max-items", "-1"},
		{"--max-items", "101"},
		{"--cursor", "0"},
		{"--cursor", "-1"},
		{"--cursor", "101"},
		{"--cursor", "not-a-page"},
	} {
		t.Run(strings.Join(flags, " "), func(t *testing.T) {
			server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
				"search.messages": func(testutil.SlackRequest) testutil.SlackResponse {
					return testutil.JSONResponse(`{"ok":true,"messages":{"matches":[],"pagination":{"page":1,"page_count":1}}}`)
				},
			})
			args := append([]string{"lookup", "messages", "--query", "release", "--no-throttle"}, flags...)
			stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(), "", args)
			if err == nil || stderr != "" || !strings.Contains(stdout, `"type":"validation_error"`) {
				t.Fatalf("invalid bounds did not fail validation: err=%v stdout=%s stderr=%s", err, stdout, stderr)
			}
			if len(server.Requests("auth.test")) != 0 || len(server.Requests("search.messages")) != 0 {
				t.Fatal("invalid bounds reached Slack")
			}
		})
	}
}

func TestSearchPaginationNeverInventsACompleteResult(t *testing.T) {
	for _, tc := range []struct {
		name, body, cursor, next string
		more, truncated, fail    bool
	}{
		{name: "next page", body: `{"ok":true,"messages":{"matches":[],"pagination":{"page":1,"page_count":2}}}`, next: "2", more: true},
		{name: "paging fallback", body: `{"ok":true,"messages":{"matches":[],"paging":{"page":1,"pages":2}}}`, next: "2", more: true},
		{name: "empty", body: `{"ok":true,"messages":{"matches":[],"pagination":{"page":1,"page_count":0}}}`},
		{name: "last page", body: `{"ok":true,"messages":{"matches":[],"pagination":{"page":3,"page_count":3}}}`, cursor: "3"},
		{name: "results disappeared between pages", body: `{"ok":true,"messages":{"matches":[],"pagination":{"page":3,"page_count":2}}}`, cursor: "3"},
		{name: "API page cap", body: `{"ok":true,"messages":{"matches":[{"user":"U1","text":"Keep this match","ts":"1746284582.123456"}],"pagination":{"page":100,"page_count":200}}}`, cursor: "100", more: true, truncated: true},
		{name: "missing collection", body: `{"ok":true}`, fail: true},
		{name: "null collection", body: `{"ok":true,"messages":{"matches":null,"pagination":{"page":1,"page_count":1}}}`, fail: true},
		{name: "missing bounds", body: `{"ok":true,"messages":{"matches":[]}}`, fail: true},
		{name: "invalid bounds", body: `{"ok":true,"messages":{"matches":[],"pagination":{"page":3,"page_count":2}}}`, fail: true},
		{name: "wrong page", body: `{"ok":true,"messages":{"matches":[],"pagination":{"page":1,"page_count":2}}}`, cursor: "2", fail: true},
		{name: "contradictory metadata", body: `{"ok":true,"messages":{"matches":[],"pagination":{"page":1,"page_count":2},"paging":{"page":1,"pages":3}}}`, fail: true},
	} {
		for _, mode := range []string{"json", "compact"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
					"search.messages": func(testutil.SlackRequest) testutil.SlackResponse { return testutil.JSONResponse(tc.body) },
				})
				args := []string{"lookup", "messages", "--query", "release", "--output=" + mode, "--no-throttle"}
				if tc.cursor != "" {
					args = append(args, "--cursor", tc.cursor)
				}
				stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(), "", args)
				if stderr != "" {
					t.Fatalf("machine output wrote diagnostics: %s", stderr)
				}
				var frame map[string]any
				if decodeErr := json.Unmarshal([]byte(stdout), &frame); decodeErr != nil {
					t.Fatal(decodeErr)
				}
				if tc.fail {
					if err == nil || frame["ok"] != false || frame["data"] != nil || !strings.Contains(stdout, `"exit_code":5`) {
						t.Fatalf("incomplete response reported success: err=%v stdout=%s", err, stdout)
					}
					return
				}
				if err != nil {
					t.Fatalf("valid page failed: %v stdout=%s", err, stdout)
				}
				data := frame
				if mode == "json" {
					data = frame["data"].(map[string]any)
				}
				paging := data["pagination"].(map[string]any)
				if paging["has_more"] != tc.more || (tc.next != "" && paging["next_cursor"] != tc.next) || (tc.next == "" && paging["next_cursor"] != nil) || (data["truncated"] == true) != tc.truncated {
					t.Fatalf("incorrect continuation: %s", stdout)
				}
				if tc.truncated && !strings.Contains(stdout, "Keep this match") {
					t.Fatal("page cap discarded valid matches")
				}
			})
		}
	}
}

func TestSearchReportsPageCapToHumansWithoutDiscardingMatches(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"search.messages": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"messages":{"matches":[{"user":"U1","text":"Keep this match","ts":"1746284582.123456"}],"pagination":{"page":100,"page_count":200}}}`)
		},
	})
	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(), "", []string{"lookup", "messages", "--query", "release", "--cursor", "100", "--output=human", "--full", "--no-throttle"})
	if err != nil || !strings.Contains(stdout, "Keep this match") || !strings.Contains(stderr, "narrow the query") {
		t.Fatalf("page cap lost matches or failed to explain truncation: err=%v stdout=%s stderr=%s", err, stdout, stderr)
	}
}

func TestEmptyHumanSearchStillShowsItsNextPage(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"search.messages": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"messages":{"matches":[],"pagination":{"page":1,"page_count":2}}}`)
		},
	})
	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(), "", []string{"lookup", "messages", "--query", "release", "--output=human", "--no-throttle"})
	if err != nil || stderr != "" || !strings.Contains(stdout, "next_cursor=2") || !strings.Contains(stdout, "has_more=true") {
		t.Fatalf("empty search hid its next page: err=%v stdout=%s stderr=%s", err, stdout, stderr)
	}
}

func TestSearchFailureRecoveryPreservesConfigAndNewQuery(t *testing.T) {
	for _, failure := range []string{`{"ok":`, `{"ok":false,"error":"missing_scope","needed":"search:read"}`} {
		t.Run(failure, func(t *testing.T) {
			var requests atomic.Int32
			server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
				"search.messages": func(req testutil.SlackRequest) testutil.SlackResponse {
					if requests.Add(1) == 1 {
						if req.Form.Get("query") != "release-v1" {
							t.Error("first query changed")
						}
						return testutil.JSONResponse(failure)
					}
					if req.Form.Get("query") != "release-v2" {
						t.Error("recovery reused the previous query")
					}
					return testutil.JSONResponse(`{"ok":true,"messages":{"matches":[{"text":"Recovered","ts":"1746284582.123456"}],"pagination":{"page":1,"page_count":1}}}`)
				},
			})
			cfg := workspaceConfig(config.TokenTypeUser)
			before, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			stdout, stderr, err := executeTestRoot(t, cfg, server.BaseURL(), "", []string{"lookup", "messages", "--query", "release-v1", "--no-throttle"})
			if err == nil || stderr != "" || !strings.Contains(stdout, `"ok":false`) || requests.Load() != 1 {
				t.Fatalf("failed search was retried or lost its failure frame: err=%v stdout=%s stderr=%s requests=%d", err, stdout, stderr, requests.Load())
			}
			stdout, stderr, err = executeTestRoot(t, cfg, server.BaseURL(), "", []string{"lookup", "messages", "--query", "release-v2", "--no-throttle"})
			if err != nil || stderr != "" || !strings.Contains(stdout, "Recovered") || !strings.Contains(stdout, `"ts":"1746284582.123456"`) || requests.Load() != 2 {
				t.Fatalf("search did not recover: err=%v stdout=%s stderr=%s", err, stdout, stderr)
			}
			after, err := json.Marshal(cfg)
			if err != nil || string(before) != string(after) {
				t.Fatal("search changed saved workspace settings")
			}
		})
	}
}

func TestSearchRemovingPagingFlagsRestoresDefaults(t *testing.T) {
	var requests atomic.Int32
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"search.messages": func(req testutil.SlackRequest) testutil.SlackResponse {
			n := requests.Add(1)
			page := "1"
			if n == 1 {
				page = "2"
				if req.Form.Get("count") != "5" || req.Form.Get("page") != page {
					t.Error("initial paging options missing")
				}
			} else if req.Form.Has("count") || req.Form.Has("page") {
				t.Error("removed options remained in the next request")
			}
			if req.Form.Has("highlight") {
				t.Error("--full changed the Slack query instead of human rendering")
			}
			return testutil.JSONResponse(`{"ok":true,"messages":{"matches":[],"pagination":{"page":` + page + `,"page_count":2}}}`)
		},
	})
	cfg := workspaceConfig(config.TokenTypeUser)
	for _, flags := range [][]string{{"--cursor", "2", "--max-items", "5", "--full"}, nil} {
		args := append([]string{"lookup", "messages", "--query", "release", "--no-throttle"}, flags...)
		if stdout, stderr, err := executeTestRoot(t, cfg, server.BaseURL(), "", args); err != nil || stderr != "" {
			t.Fatalf("search failed: err=%v stdout=%s stderr=%s", err, stdout, stderr)
		}
	}
}
