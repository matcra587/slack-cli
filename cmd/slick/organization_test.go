package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

type organizationCase struct {
	name, method, reply string
	args                []string
	params              url.Values
	mutation            bool
}

func organizationCases() []organizationCase {
	return []organizationCase{
		{"dnd info", "dnd.info", `{"ok":true,"dnd_enabled":false,"next_dnd_start_ts":0,"next_dnd_end_ts":0}`, []string{"dnd", "info", "--user=U123"}, url.Values{"user": {"U123"}}, false},
		{"dnd team", "dnd.teamInfo", `{"ok":true,"users":{"U123":{"dnd_enabled":true}}}`, []string{"dnd", "team", "--users=U123,U456"}, url.Values{"users": {"U123,U456"}}, false},
		{"dnd snooze", "dnd.setSnooze", `{"ok":true,"snooze_enabled":true,"snooze_endtime":1800000000,"snooze_remaining":3600}`, []string{"dnd", "snooze", "--minutes=60"}, url.Values{"num_minutes": {"60"}}, true},
		{"dnd end snooze", "dnd.endSnooze", `{"ok":true,"snooze_enabled":false}`, []string{"dnd", "end-snooze"}, nil, true},
		{"dnd end", "dnd.endDnd", `{"ok":true}`, []string{"dnd", "end"}, nil, true},
		{"pin list", "pins.list", `{"ok":true,"items":[]}`, []string{"pin", "list", "--channel=C123"}, url.Values{"channel": {"C123"}}, false},
		{"pin add", "pins.add", `{"ok":true}`, []string{"pin", "add", "--channel=C123", "--timestamp=1746284500.000001"}, url.Values{"channel": {"C123"}, "timestamp": {"1746284500.000001"}}, true},
		{"pin remove", "pins.remove", `{"ok":true}`, []string{"pin", "remove", "--channel=C123", "--timestamp=1746284500.000001"}, url.Values{"channel": {"C123"}, "timestamp": {"1746284500.000001"}}, true},
		{"bookmark list", "bookmarks.list", `{"ok":true,"bookmarks":[]}`, []string{"bookmark", "list", "--channel=C123"}, url.Values{"channel_id": {"C123"}}, false},
		{"bookmark add", "bookmarks.add", `{"ok":true,"bookmark":{"id":"Bk123","channel_id":"C123","title":"Runbook","link":"https://example.com"}}`, []string{"bookmark", "add", "--channel=C123", "--title=Runbook", "--link=https://example.com"}, url.Values{"channel_id": {"C123"}, "title": {"Runbook"}, "link": {"https://example.com"}, "type": {"link"}}, true},
		{"bookmark edit", "bookmarks.edit", `{"ok":true,"bookmark":{"id":"Bk123","channel_id":"C123","title":"Runbook","emoji":""}}`, []string{"bookmark", "edit", "--channel=C123", "--id=Bk123", "--emoji="}, url.Values{"channel_id": {"C123"}, "bookmark_id": {"Bk123"}, "emoji": {""}}, true},
		{"bookmark remove", "bookmarks.remove", `{"ok":true}`, []string{"bookmark", "remove", "--channel=C123", "--id=Bk123"}, url.Values{"channel_id": {"C123"}, "bookmark_id": {"Bk123"}}, true},
	}
}

func organizationServer(t *testing.T, row organizationCase, reply testutil.SlackResponse) *testutil.SlackServer {
	t.Helper()
	return testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"auth.test": func(testutil.SlackRequest) testutil.SlackResponse {
			r := testutil.JSONResponse(`{"ok":true,"user_id":"U123","team_id":"T123","url":"https://example.slack.com","user":"fixture","team":"Fixture"}`)
			r.Header.Set("X-OAuth-Scopes", "dnd:read,dnd:write,pins:read,pins:write,bookmarks:read,bookmarks:write")
			return r
		},
		row.method: func(req testutil.SlackRequest) testutil.SlackResponse {
			if req.Method != http.MethodPost || req.Path != "/api/"+row.method || req.Form.Get("token") != "xox-test" {
				t.Error("wrong SDK request method, path or synthetic token")
			}
			for key, values := range row.params {
				if !reflect.DeepEqual(req.Form[key], values) {
					t.Errorf("parameter %s differs: %v", key, req.Form[key])
				}
			}
			if row.method == "bookmarks.edit" {
				if _, exists := req.Form["title"]; exists {
					t.Error("omitted title was sent")
				}
				if _, exists := req.Form["link"]; exists {
					t.Error("omitted link was sent")
				}
			}
			return reply
		},
	})
}

func TestOrganizationCommandsUseSDKAndSharedEnvelope(t *testing.T) {
	for _, row := range organizationCases() {
		t.Run(row.name, func(t *testing.T) {
			server := organizationServer(t, row, testutil.JSONResponse(row.reply))
			out, diagnostics, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(), "", append(row.args, "--output=json", "--no-throttle"))
			if err != nil || diagnostics != "" {
				t.Fatalf("command failed: %v, stderr=%s", err, diagnostics)
			}
			var envelope struct {
				OK   bool `json:"ok"`
				Meta struct {
					Command string `json:"command"`
				} `json:"meta"`
				Data   json.RawMessage   `json:"data"`
				Errors []json.RawMessage `json:"errors"`
			}
			if json.Unmarshal([]byte(out), &envelope) != nil || !envelope.OK || envelope.Meta.Command != row.args[0]+"."+row.args[1] || envelope.Errors == nil {
				t.Fatalf("invalid envelope: %s", out)
			}
			if strings.Contains(out, "xox-test") || len(server.Requests(row.method)) != 1 {
				t.Fatal("credential leaked or operation replayed")
			}
			if row.method == "pins.list" && !strings.Contains(string(envelope.Data), `"items":[]`) {
				t.Fatal("empty pins not represented as an array")
			}
			if row.method == "bookmarks.list" && !strings.Contains(string(envelope.Data), `"bookmarks":[]`) {
				t.Fatal("empty bookmarks not represented as an array")
			}
		})
	}
}

func TestOrganizationFailuresNeverBecomeSuccessOrReplay(t *testing.T) {
	for _, row := range organizationCases() {
		t.Run(row.name, func(t *testing.T) {
			for _, failure := range []struct {
				name, body string
				status     int
			}{
				{"permission", `{"ok":false,"error":"missing_scope","needed":"synthetic:scope"}`, 200},
				{"missing acknowledgement", `{}`, 200},
				{"conflicting acknowledgement", `{"ok":true,"error":"no_permission"}`, 200},
				{"malformed", `{"ok":`, 200},
				{"server failure", `{"ok":false,"error":"internal_error"}`, 503},
			} {
				t.Run(failure.name, func(t *testing.T) {
					reply := testutil.JSONResponse(failure.body)
					reply.Status = failure.status
					server := organizationServer(t, row, reply)
					out, diagnostics, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(), "", append(row.args, "--output=json", "--no-throttle"))
					if err == nil || !strings.Contains(out, `"ok":false`) || diagnostics != "" || strings.Contains(out, "xox-test") {
						t.Fatalf("failure contract violated: err=%v stdout=%s stderr=%s", err, out, diagnostics)
					}
					if len(server.Requests(row.method)) != 1 {
						t.Fatal("operation was retried")
					}
					if failure.name == "permission" && !strings.Contains(out, `"type":"auth_failure"`) {
						t.Fatalf("permission failure lost its classification: %s", out)
					}
				})
			}
		})
	}
}

func TestOrganizationDryRunsAndValidationAvoidCredentials(t *testing.T) {
	for _, row := range organizationCases() {
		if !row.mutation {
			continue
		}
		t.Run(row.name, func(t *testing.T) {
			cfg := workspaceConfig(config.TokenTypeBot)
			out, diagnostics, err := executeTestRoot(t, cfg, "http://127.0.0.1:1", "", append(row.args, "--dry-run", "--output=json"))
			if err != nil || diagnostics != "" || !strings.Contains(out, `"dry_run":true`) {
				t.Fatalf("dry run tried to access Slack or failed: %v %s %s", err, out, diagnostics)
			}
		})
	}
	for _, args := range [][]string{
		{"dnd", "snooze", "--minutes=0"},
		{"dnd", "snooze", "--minutes=-1"},
		{"dnd", "team"},
		{"pin", "add", "--channel=C123", "--timestamp=bad"},
		{"pin", "list"},
		{"bookmark", "add", "--channel=C123", "--title=Runbook", "--link=javascript:alert(1)"},
		{"bookmark", "edit", "--channel=C123", "--id=Bk123"},
		{"bookmark", "edit", "--channel=C123", "--id=Bk123", "--title="},
		{"bookmark", "remove", "--channel=C123"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			out, _, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), "http://127.0.0.1:1", "", append(args, "--output=json", "--dry-run"))
			if err == nil || !strings.Contains(out, `"type":"validation_error"`) {
				t.Fatalf("invalid input reached credentials or Slack: %v %s", err, out)
			}
		})
	}
}

func TestOrganizationMissingResultsAreNotEmptySuccess(t *testing.T) {
	for _, row := range organizationCases() {
		if row.method == "pins.add" || row.method == "pins.remove" || row.method == "bookmarks.remove" || row.method == "dnd.endDnd" {
			continue
		}
		t.Run(row.name, func(t *testing.T) {
			server := organizationServer(t, row, testutil.JSONResponse(`{"ok":true}`))
			out, _, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(), "", append(row.args, "--output=json", "--no-throttle"))
			if err == nil || !strings.Contains(out, `"ok":false`) {
				t.Fatal("missing result reported as success")
			}
		})
	}
}

func TestOrganizationPreflightAndOutputModes(t *testing.T) {
	for _, index := range []int{0, 5, 8} {
		row := organizationCases()[index]
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(row.name+"/"+mode, func(t *testing.T) {
				server := organizationServer(t, row, testutil.JSONResponse(row.reply))
				out, diagnostics, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(), "", append(row.args, "--output="+mode))
				if err != nil || diagnostics != "" || strings.TrimSpace(out) == "" {
					t.Fatalf("output mode failed: %v %s", err, diagnostics)
				}
				if mode != "human" && !json.Valid([]byte(out)) {
					t.Fatal("machine output is not JSON")
				}
				if mode == "compact" && strings.Contains(out, `"meta"`) {
					t.Fatal("compact success unexpectedly contains envelope")
				}
			})
		}
	}
	for _, row := range organizationCases() {
		t.Run(row.name+"/missing scope", func(t *testing.T) {
			server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
				"auth.test": func(testutil.SlackRequest) testutil.SlackResponse {
					r := testutil.JSONResponse(`{"ok":true,"user_id":"U123","team_id":"T123"}`)
					r.Header.Set("X-OAuth-Scopes", "chat:write")
					return r
				},
			})
			out, _, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(), "", append(row.args, "--output=json"))
			if err == nil || !strings.Contains(out, `"ok":false`) || len(server.Requests(row.method)) != 0 {
				t.Fatal("missing scope did not stop the operation")
			}
		})
	}
}

func TestOrganizationDryRunCannotResolveCredentials(t *testing.T) {
	for _, row := range organizationCases() {
		if !row.mutation {
			continue
		}
		t.Run(row.name, func(t *testing.T) {
			var out, diagnostics bytes.Buffer
			calls := 0
			root := NewRootCommand(WithConfig(workspaceConfig(config.TokenTypeUser)), WithTTY(false), WithIO(strings.NewReader(""), &out, &diagnostics),
				WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
					calls++
					return "", errors.New("synthetic credential provider must not run")
				})))
			root.SetArgs(append(row.args, "--dry-run", "--output=json"))
			if err := root.Execute(); err != nil || calls != 0 || !strings.Contains(out.String(), `"dry_run":true`) {
				t.Fatalf("dry run resolved credentials: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestDNDMutationsRequireUserToken(t *testing.T) {
	for _, action := range []string{"snooze", "end-snooze", "end"} {
		t.Run(action, func(t *testing.T) {
			args := []string{"dnd", action, "--output=json"}
			if action == "snooze" {
				args = append(args, "--minutes=60")
			}
			out, _, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), "http://127.0.0.1:1", "", args)
			if err == nil || !strings.Contains(out, "requires a user token") {
				t.Fatal("bot token attempted to change DND")
			}
		})
	}
}

func TestBookmarkEditPreservesUnrelatedFieldsAndFailedState(t *testing.T) {
	state := map[string]string{"id": "Bk123", "channel_id": "C123", "title": "Runbook", "link": "https://example.com/runbook", "emoji": ":book:"}
	fail := false
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"bookmarks.edit": func(req testutil.SlackRequest) testutil.SlackResponse {
			if fail {
				return testutil.JSONResponse(`{"ok":false,"error":"no_permission"}`)
			}
			for _, key := range []string{"title", "link", "emoji"} {
				if values, exists := req.Form[key]; exists {
					state[key] = values[0]
				}
			}
			body, _ := json.Marshal(map[string]any{"ok": true, "bookmark": state})
			return testutil.JSONResponse(string(body))
		},
		"bookmarks.list": func(testutil.SlackRequest) testutil.SlackResponse {
			body, _ := json.Marshal(map[string]any{"ok": true, "bookmarks": []map[string]string{state}})
			return testutil.JSONResponse(string(body))
		},
	})
	for _, step := range []struct {
		args        []string
		wantFailure bool
	}{
		{[]string{"bookmark", "list", "--channel=C123"}, false},
		{[]string{"bookmark", "edit", "--channel=C123", "--id=Bk123", "--emoji="}, false},
		{[]string{"bookmark", "edit", "--channel=C123", "--id=Bk123", "--title=New title"}, true},
		{[]string{"bookmark", "list", "--channel=C123"}, false},
	} {
		fail = step.wantFailure
		out, _, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(), "", append(step.args, "--output=json"))
		if (err != nil) != step.wantFailure {
			t.Fatalf("unexpected command result: %v %s", err, out)
		}
	}
	if state["title"] != "Runbook" || state["link"] != "https://example.com/runbook" || state["emoji"] != "" {
		t.Fatal("partial edit or failure changed unrelated bookmark fields")
	}
	if len(server.Requests("bookmarks.edit")) != 2 {
		t.Fatal("failed edit was retried")
	}
}
