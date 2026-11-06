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

func TestReactionListRejectsUnexpectedArgumentsBeforeSlack(t *testing.T) {
	t.Setenv("SLICK_CONFIG", filepath.Join(t.TempDir(), "unused.toml"))
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	server := testutil.NewSlackServer(t, nil)
	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(), "", []string{"react", "list", "ignored", "--channel=C123", "--timestamp=1746284582.123456", "--output=json", "--no-throttle"})
	if err == nil || !strings.Contains(err.Error(), `unknown command "ignored"`) || stdout != "" || stderr != "" || len(server.Requests("auth.test")) != 0 || len(server.Requests("reactions.get")) != 0 {
		t.Fatalf("unexpected argument was ignored or reached Slack: err=%v stdout=%s stderr=%s", err, stdout, stderr)
	}
}

func TestReactionListRequestsFullListAndKeepsEmptyArray(t *testing.T) {
	t.Setenv("SLICK_CONFIG", filepath.Join(t.TempDir(), "unused.toml"))
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, mode := range []string{"json", "compact"} {
		t.Run(mode, func(t *testing.T) {
			server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
				"reactions.get": func(req testutil.SlackRequest) testutil.SlackResponse {
					if req.Method != "POST" || req.Path != "/api/reactions.get" || req.Form.Get("full") != "true" || req.Form.Get("token") != "xox-test" || req.Form.Get("channel") != "C123" || req.Form.Get("timestamp") != "1746284582.123456" {
						t.Error("reaction read omitted full or changed its target/authentication")
					}
					return testutil.JSONResponse(`{"ok":true,"type":"message","message":{"ts":"1746284582.123456"}}`)
				},
			})
			stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(), "", []string{"react", "list", "--channel=C123", "--timestamp=1746284582.123456", "--output=" + mode, "--no-throttle"})
			if err != nil || stderr != "" || !strings.Contains(stdout, `"reactions":[]`) {
				t.Fatalf("empty reaction read lost its array: err=%v stdout=%s stderr=%s", err, stdout, stderr)
			}
			if len(server.Requests("reactions.get")) != 1 {
				t.Fatal("reaction read was replayed")
			}
		})
	}
}

func TestReactionPartialFailureReportsAcknowledgedAndUnattemptedWork(t *testing.T) {
	t.Setenv("SLICK_CONFIG", filepath.Join(t.TempDir(), "unused.toml"))
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, action := range []string{"add", "remove"} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(action+"/"+mode, func(t *testing.T) {
				state := map[string]bool{"eyes": true}
				if action == "remove" {
					for _, emoji := range []string{"rocket", "heart", "white_check_mark"} {
						state[emoji] = true
					}
				}
				var calls []string
				reject := true
				server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
					"reactions." + action: func(req testutil.SlackRequest) testutil.SlackResponse {
						if req.Method != "POST" || req.Path != "/api/reactions."+action || req.Form.Get("token") != "xox-test" || req.Form.Get("channel") != "C123" || req.Form.Get("timestamp") != "1746284582.123456" {
							t.Error("mutation target, authentication or method changed")
						}
						emoji := req.Form.Get("name")
						calls = append(calls, emoji)
						if reject && emoji == "heart" {
							return testutil.JSONResponse(`{"ok":false,"error":"missing_scope","needed":"reactions:write"}`)
						}
						state[emoji] = action == "add"
						return testutil.JSONResponse(`{"ok":true}`)
					},
				})
				cfg := workspaceConfig(config.TokenTypeUser)
				before, err := json.Marshal(cfg)
				if err != nil {
					t.Fatal(err)
				}
				args := []string{"react", action, "--channel=C123", "--timestamp=1746284582.123456", "--emoji=rocket,heart,white_check_mark", "--output=" + mode, "--no-throttle"}
				stdout, stderr, err := executeTestRoot(t, cfg, server.BaseURL(), "", args)
				if err == nil || !reflect.DeepEqual(calls, []string{"rocket", "heart"}) {
					t.Fatalf("failed reaction continued or replayed: err=%v calls=%v", err, calls)
				}
				if mode == "human" {
					if stdout != "" || !strings.Contains(stderr, "completed_emojis") || !strings.Contains(stderr, "unattempted_emojis") || !strings.Contains(stderr, "attempted_emoji=heart") {
						t.Fatal("human failure lost partial progress or used stdout")
					}
				} else {
					var frame struct {
						OK     bool `json:"ok"`
						Data   any  `json:"data"`
						Errors []struct {
							Exit    int `json:"exit_code"`
							Details struct {
								Completed   []string `json:"completed_emojis"`
								Attempted   string   `json:"attempted_emoji"`
								Unattempted []string `json:"unattempted_emojis"`
								Channel     string   `json:"channel"`
								TS          string   `json:"ts"`
								Needed      []string `json:"needed"`
							} `json:"details"`
						} `json:"errors"`
					}
					if decodeErr := json.Unmarshal([]byte(stdout), &frame); decodeErr != nil {
						t.Fatal(decodeErr)
					}
					if frame.OK || frame.Data != nil || len(frame.Errors) != 1 || stderr != "" {
						t.Fatal("partial failure broke the common machine envelope")
					}
					e := frame.Errors[0]
					if e.Exit != 1 || !reflect.DeepEqual(e.Details.Completed, []string{"rocket"}) || e.Details.Attempted != "heart" || !reflect.DeepEqual(e.Details.Unattempted, []string{"white_check_mark"}) || e.Details.Channel != "C123" || e.Details.TS != "1746284582.123456" || !reflect.DeepEqual(e.Details.Needed, []string{"reactions:write"}) {
						t.Fatal("partial failure lost its scope, exact target or acknowledged progress")
					}
				}
				if !state["eyes"] || state["rocket"] != (action == "add") || state["heart"] != (action == "remove") || state["white_check_mark"] != (action == "remove") {
					t.Fatal("failure reset prior progress or changed unrelated state")
				}
				after, err := json.Marshal(cfg)
				if err != nil || string(before) != string(after) {
					t.Fatal("reaction failure changed workspace settings")
				}
				reject = false
				args[4] = "--emoji=heart,white_check_mark"
				_, stderr, err = executeTestRoot(t, cfg, server.BaseURL(), "", args)
				if err != nil || stderr != "" || !reflect.DeepEqual(calls, []string{"rocket", "heart", "heart", "white_check_mark"}) || !state["eyes"] || state["heart"] != (action == "add") || state["white_check_mark"] != (action == "add") {
					t.Fatalf("explicit recovery replayed completed work or failed: err=%v calls=%v", err, calls)
				}
			})
		}
	}
}
