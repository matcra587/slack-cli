package main

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestScopeDiagnosticsHaveOneNeededTypeAndPreserveRequirementMeaning(t *testing.T) {
	t.Setenv("SLICK_CONFIG", filepath.Join(t.TempDir(), "unused.toml"))
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, tc := range []struct {
		name, method, header, body, match string
		needed                            []string
		firstRequests                     int
	}{
		{name: "local all", method: "reactions.add", header: "chat:write", match: "all", needed: []string{"reactions:write"}},
		{name: "remote structured", method: "reactions.add", header: "reactions:write", body: `{"ok":false,"error":"missing_scope","needed":"reactions:write"}`, match: "reported", needed: []string{"reactions:write"}, firstRequests: 1},
		{name: "remote legacy", method: "reactions.add", header: "reactions:write", body: `{"ok":false,"error":"missing_scope","response_metadata":{"messages":["missing required scope: reactions:write"]}}`, match: "reported", needed: []string{"reactions:write"}, firstRequests: 1},
		{name: "local any", method: "conversations.history", header: "reactions:write", match: "any", needed: []string{"channels:history", "groups:history", "im:history", "mpim:history"}},
		{name: "remote channel scopes", method: "conversations.history", header: "channels:history", body: `{"ok":false,"error":"missing_scope","needed":"groups:history, channels:history"}`, match: "reported", needed: []string{"groups:history", "channels:history"}, firstRequests: 1},
		{name: "unreviewed scope omitted", method: "reactions.add", header: "reactions:write", body: `{"ok":false,"error":"missing_scope","needed":"credential:synthetic-private-value"}`, firstRequests: 1},
	} {
		for _, mode := range []string{"json", "compact"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				recovered := false
				server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
					"auth.test": func(testutil.SlackRequest) testutil.SlackResponse {
						response := testutil.JSONResponse(`{"ok":true,"user_id":"U1"}`)
						header := tc.header
						if recovered {
							header = "reactions:write,channels:history"
						}
						response.Header.Set("X-OAuth-Scopes", header)
						return response
					},
					tc.method: func(testutil.SlackRequest) testutil.SlackResponse {
						if !recovered {
							return testutil.JSONResponse(tc.body)
						}
						if tc.method == "conversations.history" {
							return testutil.JSONResponse(`{"ok":true,"messages":[]}`)
						}
						return testutil.JSONResponse(`{"ok":true}`)
					},
				})
				args := []string{"react", "add", "--channel=C123", "--timestamp=1746284582.123456", "--emoji=rocket", "--output=" + mode, "--no-throttle"}
				if tc.method == "conversations.history" {
					args = []string{"history", "list", "--channel=C123", "--output=" + mode, "--no-throttle"}
				}
				cfg := workspaceConfig(config.TokenTypeUser)
				before, err := json.Marshal(cfg)
				if err != nil {
					t.Fatal(err)
				}
				stdout, stderr, err := executeTestRoot(t, cfg, server.BaseURL(), "", args)
				if err == nil || stderr != "" {
					t.Fatalf("scope failure lost its machine error: err=%v stdout=%s stderr=%s", err, stdout, stderr)
				}
				if strings.Contains(stdout+stderr, "xox-test") || strings.Contains(stdout+stderr, "synthetic-private-value") {
					t.Fatal("scope diagnostics disclosed synthetic credentials or unreviewed scope text")
				}
				var frame map[string]json.RawMessage
				if err := json.Unmarshal([]byte(stdout), &frame); err != nil {
					t.Fatal(err)
				}
				if len(frame) != 4 || string(frame["ok"]) != "false" || string(frame["data"]) != "null" {
					t.Fatal("scope error changed the failure envelope")
				}
				var errors []struct {
					Type    string                     `json:"type"`
					Exit    int                        `json:"exit_code"`
					Details map[string]json.RawMessage `json:"details"`
				}
				if err := json.Unmarshal(frame["errors"], &errors); err != nil {
					t.Fatal(err)
				}
				if len(errors) != 1 || errors[0].Type != "auth_failure" || errors[0].Exit != 1 {
					t.Fatal("scope diagnostic changed its error class")
				}
				details := errors[0].Details
				if tc.needed == nil {
					if details["needed"] != nil || details["needed_match"] != nil {
						t.Fatal("unreviewed scope data entered diagnostics")
					}
				} else {
					var needed []string
					if err := json.Unmarshal(details["needed"], &needed); err != nil {
						t.Fatalf("needed must always be a string array: %v", err)
					}
					if !reflect.DeepEqual(needed, tc.needed) || string(details["needed_match"]) != `"`+tc.match+`"` {
						t.Fatalf("needed list or requirement meaning changed: %s", stdout)
					}
				}
				if len(server.Requests(tc.method)) != tc.firstRequests {
					t.Fatal("scope failure reached mutation unexpectedly or replayed it")
				}
				after, err := json.Marshal(cfg)
				if err != nil || string(before) != string(after) {
					t.Fatal("scope failure changed workspace settings")
				}
				recovered = true
				_, stderr, err = executeTestRoot(t, cfg, server.BaseURL(), "", args)
				if err != nil || stderr != "" || len(server.Requests(tc.method)) != tc.firstRequests+1 {
					t.Fatalf("explicit retry did not recover: err=%v stderr=%s", err, stderr)
				}
			})
		}
	}
}
