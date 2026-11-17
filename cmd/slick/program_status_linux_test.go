package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestOAuthTerminalStatusCoversPersistenceAndMachineModes(t *testing.T) {
	for _, row := range []struct {
		name, mode, final string
		exit              int
	}{
		{"success", "human", "done", 0},
		{"cancel", "human", "idle", 6},
		{"exchange failure", "human", "error", 5},
		{"save failure", "human", "error", 5},
		{"output failure", "human", "error", 8},
		{"JSON", "json", "", 0},
		{"compact", "compact", "", 0},
	} {
		t.Run(row.name, func(t *testing.T) {
			stderr, read := testutil.NewTerminal(t)
			var out bytes.Buffer
			var stdout io.Writer = &out
			if row.name == "output failure" {
				stdout = programStatusFailWriter{}
			}
			caller, cancel := context.WithCancel(t.Context())
			defer cancel()
			opener := oauthTestCallbackOpener(t, "synthetic-status-code")
			if row.name == "cancel" {
				opener = func(string) error { cancel(); return nil }
			}
			client := &http.Client{Transport: authStatusTransport(func(r *http.Request) (*http.Response, error) {
				if row.name == "exchange failure" {
					return nil, errors.New("synthetic-private-status-provider")
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":true,"access_token":"xoxb-synthetic-status","token_type":"bot","team":{"id":"T_STATUS","name":"Status"}}`)), Request: r}, nil
			})}
			path := filepath.Join(t.TempDir(), "config.toml")
			if row.name == "save failure" {
				path = t.TempDir() // A directory cannot be replaced by config publication.
			}
			root := NewRootCommand(WithConfig(authTestConfig()), WithConfigPath(path),
				WithCredentialStore(config.NewMemoryCredentialStore()), WithTTY(true),
				WithIO(strings.NewReader(""), stdout, stderr), WithURLOpener(opener), WithOAuthTimeout(2*time.Second),
				func(runtime *RootRuntime) { runtime.HTTPClient = client })
			root.SetArgs([]string{
				"auth", "login", "--workspace-name=Status", "--method=oauth",
				"--oauth-client-id=synthetic-client", "--oauth-callback-port=0", "--output=" + row.mode, "--color=never",
			})
			err := root.ExecuteContext(caller)
			if code := programStatusExitCode(t, err); code != row.exit {
				t.Fatalf("OAuth exit=%d want=%d error=%v", code, row.exit, err)
			}
			reports := programStatusReports(t, read())
			if row.final == "" {
				if len(reports) != 0 || !json.Valid(out.Bytes()) {
					t.Fatal("machine OAuth emitted terminal status or invalid JSON")
				}
				return
			}
			want := []string{"working", "blocked", "working", row.final}
			if row.name == "cancel" {
				want = []string{"working", "blocked", "idle"}
			}
			assertProgramStates(t, reports, want)
			if !strings.Contains(reports[1], ":kind=auth:") {
				t.Fatal("OAuth wait did not identify the authentication action")
			}
		})
	}
}

func TestReactionWatchTerminalStatusCoversResultsAndCancellation(t *testing.T) {
	for _, row := range []struct {
		name, mode, final string
		exit              int
	}{
		{"success", "human", "done", 0},
		{"cancel", "human", "idle", 6},
		{"failure", "human", "error", 1},
		{"output failure", "human", "error", 8},
		{"JSON", "json", "", 0},
		{"compact", "compact", "", 0},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			stderr, read := testutil.NewTerminal(t)
			var out bytes.Buffer
			var stdout io.Writer = &out
			if row.name == "output failure" {
				stdout = programStatusFailWriter{}
			}
			caller, cancel := context.WithCancel(t.Context())
			defer cancel()
			server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
				"auth.test": func(testutil.SlackRequest) testutil.SlackResponse {
					response := testutil.JSONResponse(`{"ok":true,"team_id":"T123","user_id":"U123"}`)
					response.Header.Set("X-OAuth-Scopes", "reactions:read")
					return response
				},
				"reactions.get": func(testutil.SlackRequest) testutil.SlackResponse {
					if row.name == "cancel" {
						cancel()
					}
					if row.name == "failure" {
						return testutil.JSONResponse(`{"ok":false,"error":"no_permission"}`)
					}
					return testutil.JSONResponse(`{"ok":true,"type":"message","message":{"reactions":[{"name":"+1","count":1,"users":["U123"]}]}}`)
				},
				"conversations.info": func(testutil.SlackRequest) testutil.SlackResponse {
					return testutil.JSONResponse(`{"ok":true,"channel":{"id":"C123","name":"fixture"}}`)
				},
			})
			root := NewRootCommand(WithConfig(authTestConfig()), WithTTY(true), WithSlackBaseURL(server.BaseURL()),
				WithIO(strings.NewReader(""), stdout, stderr), WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
					return "xoxb-synthetic-status", nil
				})))
			root.SetArgs([]string{
				"react", "watch", "--channel=C123", "--timestamp=1746284582.123456", "--emoji=+1",
				"--no-throttle", "--output=" + row.mode, "--color=never",
			})
			err := root.ExecuteContext(caller)
			if code := programStatusExitCode(t, err); code != row.exit {
				t.Fatalf("watch exit=%d want=%d error=%v", code, row.exit, err)
			}
			reports := programStatusReports(t, read())
			if row.final == "" {
				if len(reports) != 0 || !json.Valid(out.Bytes()) {
					t.Fatal("machine watch emitted terminal status or invalid JSON")
				}
				return
			}
			assertProgramStates(t, reports, []string{"working", row.final})
		})
	}
}

func programStatusReports(t *testing.T, data string) []string {
	t.Helper()
	var reports []string
	for _, match := range regexp.MustCompile("\x1b\\]7501;([^\a]*)\a").FindAllStringSubmatch(data, -1) {
		reports = append(reports, match[1])
		for field := range strings.SplitSeq(match[1], ":") {
			key, value, _ := strings.Cut(field, "=")
			if key == "msg" || key == "title" {
				decoded, err := base64.StdEncoding.DecodeString(value)
				if err != nil || strings.Contains(string(decoded), "synthetic") || strings.Contains(string(decoded), "xoxb-") {
					t.Fatal("terminal status contained private or invalid metadata")
				}
			}
		}
	}
	return reports
}

func assertProgramStates(t *testing.T, reports, want []string) {
	t.Helper()
	var states []string
	for _, report := range reports {
		state, _, _ := strings.Cut(report, ":")
		states = append(states, strings.TrimPrefix(state, "state="))
	}
	if !slices.Equal(states, want) {
		t.Fatalf("terminal states=%v want=%v", states, want)
	}
}

type programStatusFailWriter struct{}

func programStatusExitCode(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		return 0
	}
	failure, ok := errors.AsType[clioutput.CommandError](err)
	if !ok {
		t.Fatalf("unstructured command error: %v", err)
	}
	return failure.CLIError.ExitCode
}

func (programStatusFailWriter) Write([]byte) (int, error) {
	return 0, errors.New("synthetic-output-unavailable")
}
