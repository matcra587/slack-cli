package history_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestHistoryKeepsTimeContinuationBeforeLocalFiltering(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, thread := range []bool{false, true} {
		name, method, nextKey := "channel", "conversations.history", "next_until"
		if thread {
			name, method, nextKey = "thread", "conversations.replies", "next_since"
		}
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(name+"/"+mode, func(t *testing.T) {
				calls := 0
				server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
					method: func(req testutil.SlackRequest) testutil.SlackResponse {
						calls++
						if calls == 1 {
							return testutil.JSONResponse(`{"ok":true,"has_more":true,"messages":[{"type":"message","user":"UOTHER","ts":"1746284582.123456"}]}`)
						}
						key := "latest"
						if thread {
							key = "oldest"
						}
						if req.Form.Get(key) != "1746284582.123456" || req.Form.Get("cursor") != "" {
							t.Error("time continuation used the wrong bound or retained a stale cursor")
						}
						ts := "1746284570.123456"
						if thread {
							ts = "1746284590.123456"
						}
						return testutil.JSONResponse(`{"ok":true,"has_more":false,"messages":[{"type":"message","user":"USELECTED","text":"Review approved","ts":"` + ts + `"}]}`)
					},
				})
				args := []string{"history", "list", "--channel=C123", "--user=USELECTED", "--output=" + mode}
				if thread {
					args = append(args, "--thread=1746284500.000001")
				}
				stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(), "", args)
				if err != nil || stderr != "" {
					t.Fatalf("empty filtered page failed: err=%v stderr=%s", err, stderr)
				}
				if mode == "human" {
					if !strings.Contains(stdout, nextKey+"=1746284582.123456") || !strings.Contains(stdout, "has_more=true") {
						t.Fatal("human output lost the time continuation")
					}
				} else {
					var data struct {
						Messages   []json.RawMessage          `json:"messages"`
						Pagination map[string]json.RawMessage `json:"pagination"`
					}
					payload := []byte(stdout)
					if mode == "json" {
						var frame struct {
							Data json.RawMessage `json:"data"`
							Meta struct {
								Pagination json.RawMessage `json:"pagination"`
							} `json:"meta"`
						}
						if err := json.Unmarshal(payload, &frame); err != nil {
							t.Fatal(err)
						}
						payload = frame.Data
						if !strings.Contains(string(frame.Meta.Pagination), nextKey) {
							t.Fatal("envelope pagination lost the time boundary")
						}
					}
					if err := json.Unmarshal(payload, &data); err != nil {
						t.Fatal(err)
					}
					if len(data.Messages) != 0 || string(data.Pagination["has_more"]) != "true" || string(data.Pagination[nextKey]) != `"1746284582.123456"` || string(data.Pagination["items_returned"]) != "0" {
						t.Fatalf("filtered page lost its exact boundary or zero count: %s", stdout)
					}
				}
				bound := "--until=1746284582.123456"
				if thread {
					bound = "--since=1746284582.123456"
				}
				stdout, stderr, err = executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(), "", append(args, bound))
				if err != nil || stderr != "" || !strings.Contains(stdout, "Review approved") {
					t.Fatalf("time continuation did not recover the next page: err=%v stdout=%s stderr=%s", err, stdout, stderr)
				}
				if calls != 2 {
					t.Fatal("one-page reads were unexpectedly repeated")
				}
			})
		}
	}
}

func TestHistoryRejectsMissingPagesButAcceptsExplicitEmptyResults(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, thread := range []bool{false, true} {
		name, method := "channel", "conversations.history"
		if thread {
			name, method = "thread", "conversations.replies"
		}
		for _, tc := range []struct {
			name, body string
			valid      bool
		}{
			{"missing messages", `{"ok":true}`, false},
			{"null messages", `{"ok":true,"messages":null}`, false},
			{"missing time boundary", `{"ok":true,"messages":[],"has_more":true}`, false},
			{"malformed JSON", `{"ok":true,"messages":[`, false},
			{"wrong messages type", `{"ok":true,"messages":{}}`, false},
			{"empty", `{"ok":true,"messages":[],"has_more":false}`, true},
			{"empty cursor page", `{"ok":true,"messages":[],"has_more":true,"response_metadata":{"next_cursor":"next-page"}}`, true},
		} {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{method: func(testutil.SlackRequest) testutil.SlackResponse { return testutil.JSONResponse(tc.body) }})
				args := []string{"history", "list", "--channel=C123", "--output=json"}
				if thread {
					args = append(args, "--thread=1746284500.000001")
				}
				stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(), "", args)
				if stderr != "" || (err == nil) != tc.valid {
					t.Fatalf("page validity wrong: err=%v stdout=%s stderr=%s", err, stdout, stderr)
				}
				if !tc.valid && (!strings.Contains(stdout, `"exit_code":5`) || !strings.Contains(stdout, `"data":null`)) {
					t.Fatal("invalid page did not return the common server failure envelope")
				}
				if tc.valid && (!strings.Contains(stdout, `"messages":[]`) || !strings.Contains(stdout, `"items_returned":0`)) {
					t.Fatal("empty success lost its explicit empty array or zero count")
				}
				if len(server.Requests(method)) != 1 {
					t.Fatal("invalid or empty page was replayed")
				}
			})
		}
	}
}

func TestHistoryNestedRepliesKeepTheirOwnPagination(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"conversations.history": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"messages":[{"type":"message","ts":"1746284500.000001","reply_count":20}],"response_metadata":{"next_cursor":"next-channel"}}`)
		},
		"conversations.replies": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"has_more":true,"messages":[{"type":"message","ts":"1746284500.000001"},{"type":"message","ts":"1746284582.123456"}]}`)
		},
	})
	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(), "", []string{"history", "list", "--channel=C123", "--include-replies", "--reply-limit=1", "--output=json"})
	if err != nil || stderr != "" {
		t.Fatalf("nested history failed: err=%v stderr=%s", err, stderr)
	}
	var frame struct {
		Data struct {
			Messages []struct {
				Replies []struct {
					TS string `json:"ts"`
				} `json:"replies"`
				Pagination struct {
					HasMore       bool   `json:"has_more"`
					NextSince     string `json:"next_since"`
					ItemsReturned int    `json:"items_returned"`
					MaxItems      int    `json:"max_items"`
				} `json:"replies_pagination"`
			} `json:"messages"`
			Pagination struct {
				NextCursor string `json:"next_cursor"`
			} `json:"pagination"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &frame); err != nil {
		t.Fatal(err)
	}
	if len(frame.Data.Messages) != 1 {
		t.Fatal("missing parent")
	}
	parent := frame.Data.Messages[0]
	if len(parent.Replies) != 1 || parent.Replies[0].TS != "1746284582.123456" || !parent.Pagination.HasMore || parent.Pagination.NextSince != "1746284582.123456" || parent.Pagination.ItemsReturned != 1 || parent.Pagination.MaxItems != 1 || frame.Data.Pagination.NextCursor != "next-channel" {
		t.Fatalf("nested page overwrote parent continuation or lost its own: %s", stdout)
	}
}

func TestHistoryNestedFailurePreservesSettingsAndRecoversWithoutReplay(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	cfg := workspaceConfig(config.TokenTypeUser)
	profile := cfg.Workspaces["default"]
	profile.Aliases = map[string]string{"ops": "C123"}
	cfg.Workspaces["default"] = profile
	before, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	replyCalls := 0
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"conversations.history": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"messages":[{"type":"message","ts":"1746284500.000001","reply_count":1}]}`)
		},
		"conversations.replies": func(testutil.SlackRequest) testutil.SlackResponse {
			replyCalls++
			if replyCalls == 1 {
				return testutil.JSONResponse(`{"ok":false,"error":"missing_scope","needed":"channels:history"}`)
			}
			return testutil.JSONResponse(`{"ok":true,"messages":[{"type":"message","ts":"1746284500.000001"},{"type":"message","text":"Review approved","ts":"1746284582.123456"}]}`)
		},
	})
	args := []string{"history", "list", "--channel=ops", "--include-replies", "--reply-limit=1", "--output=json"}
	stdout, stderr, err := executeTestRoot(t, cfg, server.BaseURL(), "", args)
	if err == nil || stderr != "" || !strings.Contains(stdout, `"exit_code":1`) || !strings.Contains(stdout, `"data":null`) || replyCalls != 1 {
		t.Fatalf("nested permission failure returned partial success or retried: err=%v stdout=%s stderr=%s", err, stdout, stderr)
	}
	after, err := json.Marshal(cfg)
	if err != nil || string(before) != string(after) {
		t.Fatal("failed history altered workspace settings")
	}
	stdout, stderr, err = executeTestRoot(t, cfg, server.BaseURL(), "", args)
	if err != nil || stderr != "" || !strings.Contains(stdout, "Review approved") || replyCalls != 2 || len(server.Requests("conversations.history")) != 2 {
		t.Fatalf("explicit retry failed to recover: err=%v stdout=%s stderr=%s", err, stdout, stderr)
	}
}

func TestHistoryRejectsTimeContinuationThatCannotAdvance(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, thread := range []bool{false, true} {
		name, method, bound := "channel", "conversations.history", "--until=1746284582.123456"
		if thread {
			name, method, bound = "thread", "conversations.replies", "--since=1746284582.123456"
		}
		t.Run(name, func(t *testing.T) {
			server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{method: func(testutil.SlackRequest) testutil.SlackResponse {
				return testutil.JSONResponse(`{"ok":true,"has_more":true,"messages":[{"type":"message","ts":"1746284582.123456"}]}`)
			}})
			args := []string{"history", "list", "--channel=C123", "--output=json", bound}
			if thread {
				args = append(args, "--thread=1746284500.000001")
			}
			stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(), "", args)
			if err == nil || stderr != "" || !strings.Contains(stdout, `"exit_code":5`) || !strings.Contains(stdout, `"data":null`) || len(server.Requests(method)) != 1 {
				t.Fatalf("nonadvancing continuation was accepted: err=%v stdout=%s stderr=%s", err, stdout, stderr)
			}
		})
	}
}
