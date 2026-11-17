package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestOperationTerminalStatusCoversResultsAndMachineModes(t *testing.T) {
	for _, operation := range []string{"upload", "users", "channels", "pages", "add", "remove"} {
		t.Run(operation, func(t *testing.T) {
			for _, scenario := range []string{"success", "failure", "cancel", "output", "json", "compact", "dry", "pipe"} {
				t.Run(scenario, func(t *testing.T) { testOperationTerminalStatus(t, operation, scenario) })
			}
		})
	}
}

func TestCacheTerminalStatusOnlyStartsForFetchAndCoversPublication(t *testing.T) {
	for _, resource := range []string{"users", "channels"} {
		t.Run(resource, func(t *testing.T) {
			for _, writeFailure := range []bool{false, true} {
				t.Run(fmt.Sprint(writeFailure), func(t *testing.T) {
					cacheHome := t.TempDir()
					if writeFailure {
						cacheHome = filepath.Join(cacheHome, "not-a-directory")
						if err := os.WriteFile(cacheHome, nil, 0o600); err != nil {
							t.Fatal(err)
						}
					}
					t.Setenv("XDG_CACHE_HOME", cacheHome)
					server := operationStatusServer(t, "success", func() {})
					for attempt := range 2 {
						stderr, read := testutil.NewTerminal(t)
						root := NewRootCommand(WithConfig(authTestConfig()), WithTTY(true), WithSlackBaseURL(server.BaseURL()),
							WithIO(strings.NewReader(""), &bytes.Buffer{}, stderr),
							WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
								return "xoxb-synthetic-status", nil
							})))
						root.SetArgs([]string{"cache", resource, "--output=human", "--no-throttle", "--color=never"})
						err := root.ExecuteContext(t.Context())
						reports := programStatusReports(t, read())
						if writeFailure {
							if err == nil || len(reports) < 2 || !strings.HasPrefix(reports[len(reports)-1], "state=error:") {
								t.Fatal("cache publication failure did not finish with an error status")
							}
							break
						}
						if err != nil {
							t.Fatal(err)
						}
						if attempt == 1 && len(reports) != 0 {
							t.Fatal("warm cache emitted fetch status")
						}
					}
					method := "conversations.list"
					if resource == "users" {
						method = "users.list"
					}
					if len(server.Requests(method)) != 2 {
						t.Fatal("cache status changed the number of fetch requests")
					}
				})
			}
		})
	}
}

func testOperationTerminalStatus(t *testing.T, operation, scenario string) {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	stderr, read := testutil.NewTerminal(t)
	var out, diagnostic bytes.Buffer
	var stdout, statusOutput io.Writer = &out, stderr
	if scenario == "output" {
		stdout = programStatusFailWriter{}
	}
	if scenario == "pipe" {
		statusOutput = &diagnostic
	}
	caller, cancel := context.WithCancel(t.Context())
	defer cancel()
	server := operationStatusServer(t, scenario, cancel)
	mode := "human"
	if scenario == "json" || scenario == "compact" {
		mode = scenario
	}
	args := operationStatusArgs(operation)
	args = append(args, "--no-throttle", "--color=never", "--output="+mode)
	if scenario == "dry" {
		args = append(args, "--dry-run")
	}
	root := NewRootCommand(WithConfig(authTestConfig()), WithConfigPath(filepath.Join(t.TempDir(), "config.toml")),
		WithTTY(true), WithSlackBaseURL(server.BaseURL()),
		WithIO(strings.NewReader("synthetic-private-file-content"), stdout, statusOutput),
		WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
			return "xoxb-synthetic-status", nil
		})))
	root.SetArgs(args)
	err := root.ExecuteContext(caller)
	code := programStatusExitCode(t, err)
	if scenario == "failure" {
		if code == 0 {
			t.Fatal("Slack failure succeeded")
		}
	} else {
		want := 0
		switch scenario {
		case "cancel":
			want = 6
		case "output":
			want = 8
		}
		if code != want {
			t.Fatalf("operation exit=%d want=%d: %v", code, want, err)
		}
	}
	reports := programStatusReports(t, read())
	if scenario == "json" || scenario == "compact" || scenario == "dry" || scenario == "pipe" {
		if len(reports) != 0 || strings.Contains(diagnostic.String(), "\x1b]7501;") {
			t.Fatal("machine output, dry run, or redirected stderr received status")
		}
		if scenario == "json" || scenario == "compact" {
			if !json.Valid(out.Bytes()) {
				t.Fatal("machine result is not valid JSON")
			}
		}
		if scenario == "dry" {
			for _, method := range []string{"auth.test", "users.list", "conversations.list", "files.getUploadURLExternal", "reactions.add", "reactions.remove"} {
				if len(server.Requests(method)) != 0 {
					t.Fatal("dry run contacted Slack")
				}
			}
		}
		return
	}
	if len(reports) < 2 || !strings.HasPrefix(reports[0], "state=working:") {
		t.Fatal("operation did not start a terminal status")
	}
	final := "done"
	switch scenario {
	case "failure", "output":
		final = "error"
	case "cancel":
		final = "idle"
	}
	if !strings.HasPrefix(reports[len(reports)-1], "state="+final+":") {
		t.Fatalf("incorrect final status: %v", reports)
	}
	for _, report := range reports[:len(reports)-1] {
		if !strings.HasPrefix(report, "state=working:") {
			t.Fatal("automatic work reported a user-input or completion state")
		}
	}
	if operation == "add" || operation == "remove" {
		assertReactionBatchProgress(t, server, operation, scenario, reports)
	} else {
		for _, report := range reports {
			if strings.Contains(report, "progress=") {
				t.Fatal("operation with no known total invented a percentage")
			}
		}
	}
	if scenario == "success" && operation == "upload" {
		assertStatusMessages(t, reports, []string{"Preparing file upload", "Uploading file data", "Completing file upload", "Completed"})
	}
	if scenario == "success" && (operation == "users" || operation == "channels" || operation == "pages") {
		if len(reports) < 4 {
			t.Fatal("paginated operation omitted page reports")
		}
	}
}

func operationStatusArgs(operation string) []string {
	switch operation {
	case "upload":
		return []string{"file", "upload", "--channel=C123", "--file=-", "--filename=synthetic-private.txt", "--no-attribution"}
	case "users", "channels":
		return []string{"cache", operation, "--refresh", "--page-size=1"}
	case "pages":
		return []string{"api", "call", "conversations.list", "--paginate"}
	default:
		return []string{"react", operation, "--channel=C123", "--timestamp=1746284582.123456", "--emoji=one,two,three"}
	}
}

func operationStatusServer(t *testing.T, scenario string, cancel context.CancelFunc) *testutil.SlackServer {
	t.Helper()
	var server *testutil.SlackServer
	failure := func(method string) bool {
		n := len(server.Requests(method))
		if scenario == "cancel" {
			cancel()
			return true
		}
		return scenario == "failure" && (n == 2 || method == "files.completeUploadExternal")
	}
	mutation := func(method string) testutil.SlackHandler {
		return func(testutil.SlackRequest) testutil.SlackResponse {
			if failure(method) {
				return testutil.JSONResponse(`{"ok":false,"error":"no_permission"}`)
			}
			return testutil.JSONResponse(`{"ok":true}`)
		}
	}
	listing := func(method, field, items string) testutil.SlackHandler {
		return func(testutil.SlackRequest) testutil.SlackResponse {
			if failure(method) {
				return testutil.JSONResponse(`{"ok":false,"error":"no_permission"}`)
			}
			cursor := ""
			if len(server.Requests(method)) == 1 {
				cursor = "synthetic-next"
			}
			return testutil.JSONResponse(fmt.Sprintf(`{"ok":true,%q:%s,"response_metadata":{"next_cursor":%q}}`, field, items, cursor))
		}
	}
	server = testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"auth.test": func(testutil.SlackRequest) testutil.SlackResponse {
			response := testutil.JSONResponse(`{"ok":true,"team_id":"T123","user_id":"U123"}`)
			response.Header.Set("X-OAuth-Scopes", "files:write,reactions:write,users:read,channels:read,groups:read,im:read,mpim:read")
			return response
		},
		"conversations.info": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"channel":{"id":"C123","name":"synthetic-private"}}`)
		},
		"files.getUploadURLExternal": func(testutil.SlackRequest) testutil.SlackResponse {
			if scenario == "cancel" {
				cancel()
			}
			return testutil.JSONResponse(fmt.Sprintf(`{"ok":true,"file_id":"F1","upload_url":%q}`, server.BaseURL()+"/upload"))
		},
		"/upload": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{}`)
		},
		"files.completeUploadExternal": func(testutil.SlackRequest) testutil.SlackResponse {
			if failure("files.completeUploadExternal") {
				return testutil.JSONResponse(`{"ok":false,"error":"no_permission"}`)
			}
			return testutil.JSONResponse(`{"ok":true,"files":[{"id":"F1"}]}`)
		},
		"files.info": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"file":{"id":"F1","name":"synthetic-private.txt"}}`)
		},
		"reactions.add":      mutation("reactions.add"),
		"reactions.remove":   mutation("reactions.remove"),
		"users.list":         listing("users.list", "members", `[{"id":"U123","name":"synthetic-private"}]`),
		"conversations.list": listing("conversations.list", "channels", `[{"id":"C123","name":"synthetic-private"}]`),
	})
	return server
}

func assertReactionBatchProgress(t *testing.T, server *testutil.SlackServer, operation, scenario string, reports []string) {
	t.Helper()
	var progress []string
	for _, report := range reports {
		for field := range strings.SplitSeq(report, ":") {
			if value, ok := strings.CutPrefix(field, "progress="); ok {
				progress = append(progress, value)
			}
		}
	}
	want := []string{"0", "33", "66", "100"}
	count := 3
	switch scenario {
	case "failure":
		want, count = want[:2], 2
	case "cancel":
		want, count = want[:1], 1
	}
	if !slices.Equal(progress, want) || len(server.Requests("reactions."+operation)) != count {
		t.Fatalf("batch progress=%v want=%v; mutation requests=%d want=%d", progress, want, len(server.Requests("reactions."+operation)), count)
	}
}

func assertStatusMessages(t *testing.T, reports, want []string) {
	t.Helper()
	var messages []string
	for _, report := range reports {
		for field := range strings.SplitSeq(report, ":") {
			if encoded, ok := strings.CutPrefix(field, "msg="); ok {
				decoded, err := base64.StdEncoding.DecodeString(encoded)
				if err != nil {
					t.Fatal("invalid status message")
				}
				messages = append(messages, string(decoded))
			}
		}
	}
	if !slices.Equal(messages, want) {
		t.Fatalf("status messages=%v want=%v", messages, want)
	}
}
