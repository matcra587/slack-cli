package history_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	clihistory "github.com/matcra587/slack-cli/internal/cli/history"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	"github.com/matcra587/slack-cli/internal/cli/runtime/runtimetest"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

// Other invalid count/flag combinations are covered with the real application
// root in cmd/slick/history_flags_contract_test.go. Keep this distinct int64 input.
func TestHistoryRejectsInt64ReplyLimitBeforeAuth(t *testing.T) {
	runtime, stdout, stderr := runtimetest.NewRuntime(t, runtimetest.Options{Config: workspaceConfig(config.TokenTypeUser)})
	runtime.TokenResolver = cliruntime.TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
		t.Error("invalid flags reached credential resolution")
		return "", context.Canceled
	})
	root := runtimetest.NewRoot(runtime, stdout, stderr)
	root.AddCommand(clihistory.NewCommand(runtime))
	args := []string{"history", "list", "--channel=C123", "--output=json", "--include-replies", "--reply-limit=9223372036854775807"}
	output, diagnostic, err := runtimetest.Run(t, root, args, stdout, stderr)
	if err == nil || diagnostic != "" || !strings.Contains(output, `"exit_code":4`) || !strings.Contains(output, `"data":null`) {
		t.Fatalf("invalid flags did not return the validation envelope: err=%v stdout=%s stderr=%s", err, output, diagnostic)
	}
}

func TestHistoryOptionalFiltersAreRemovedAndEmptyPagesKeepCursor(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, thread := range []bool{false, true} {
		name, method := "channel", "conversations.history"
		if thread {
			name, method = "thread", "conversations.replies"
		}
		t.Run(name, func(t *testing.T) {
			requests := 0
			server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
				method: func(req testutil.SlackRequest) testutil.SlackResponse {
					requests++
					if req.Method != "POST" || req.Path != "/api/"+method || req.Form.Get("token") != "xox-test" {
						t.Error("incorrect request method, path or synthetic authentication")
					}
					if requests == 1 {
						if req.Form.Get("oldest") != "1746280000.000001" || req.Form.Get("latest") != "1746290000.000001" || req.Form.Get("limit") != "2" {
							t.Error("explicit bounds were not forwarded")
						}
					} else {
						for _, field := range []string{"oldest", "latest", "limit", "cursor"} {
							if _, exists := req.Form[field]; exists {
								t.Errorf("removed optional %s remained in request", field)
							}
						}
					}
					return testutil.JSONResponse(`{"ok":true,"messages":[{"type":"message","user":"UOTHER","text":"Review complete","ts":"1746284582.123456"}],"response_metadata":{"next_cursor":"next-page"}}`)
				},
			})
			base := []string{"history", "list", "--channel=C123", "--output=json"}
			if thread {
				base = append(base, "--thread=1746284582.123456")
			}
			first := append(append([]string(nil), base...), "--since=1746280000.000001", "--until=1746290000.000001", "--user=USELECTED", "--max-items=2")
			for i, args := range [][]string{first, base} {
				stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(), "", args)
				if err != nil || stderr != "" {
					t.Fatalf("history request failed: err=%v stderr=%s", err, stderr)
				}
				var frame struct {
					Data struct {
						Messages []struct {
							TS string `json:"ts"`
						} `json:"messages"`
						Pagination struct {
							HasMore    bool   `json:"has_more"`
							NextCursor string `json:"next_cursor"`
						} `json:"pagination"`
					} `json:"data"`
				}
				if err := json.Unmarshal([]byte(stdout), &frame); err != nil {
					t.Fatal(err)
				}
				if !frame.Data.Pagination.HasMore || frame.Data.Pagination.NextCursor != "next-page" {
					t.Fatal("empty filtered page lost continuation")
				}
				if i == 0 && len(frame.Data.Messages) != 0 {
					t.Fatal("user filter was not applied")
				}
				if i == 1 && (len(frame.Data.Messages) != 1 || frame.Data.Messages[0].TS != "1746284582.123456") {
					t.Fatal("removing user filter did not restore the exact timestamped message")
				}
			}
			if requests != 2 {
				t.Fatal("history request was replayed")
			}
		})
	}
}

func TestHistoryAcceptsPublishedPageCountBoundaries(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, tc := range []struct {
		name                     string
		args                     []string
		historyLimit, replyLimit string
	}{
		{name: "channel maximum", args: []string{"--max-items=999"}, historyLimit: "999"},
		{name: "thread maximum", args: []string{"--thread=1746284582.123456", "--max-items=1000"}, replyLimit: "1000"},
		{name: "nested reply maximum", args: []string{"--include-replies", "--reply-limit=999"}, replyLimit: "1000"},
		{name: "zero leaves Slack defaults", args: []string{"--max-items=0", "--include-replies", "--reply-limit=0"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
				"conversations.history": func(req testutil.SlackRequest) testutil.SlackResponse {
					if req.Form.Get("limit") != tc.historyLimit {
						t.Errorf("history limit=%q want=%q", req.Form.Get("limit"), tc.historyLimit)
					}
					return testutil.JSONResponse(`{"ok":true,"messages":[{"type":"message","user":"U1","ts":"1746284582.123456","reply_count":1}]}`)
				},
				"conversations.replies": func(req testutil.SlackRequest) testutil.SlackResponse {
					if req.Form.Get("limit") != tc.replyLimit {
						t.Errorf("reply limit=%q want=%q", req.Form.Get("limit"), tc.replyLimit)
					}
					return testutil.JSONResponse(`{"ok":true,"messages":[{"type":"message","user":"U1","ts":"1746284582.123456"},{"type":"message","user":"U2","ts":"1746284590.123456"}]}`)
				},
			})
			args := append([]string{"history", "list", "--channel=C123", "--output=json"}, tc.args...)
			stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(), "", args)
			if err != nil || stderr != "" || !strings.Contains(stdout, `"ok":true`) {
				t.Fatalf("valid boundary failed: err=%v stdout=%s stderr=%s", err, stdout, stderr)
			}
		})
	}
}
