package status_test

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	"github.com/matcra587/slack-cli/internal/cli/runtime/runtimetest"
	clistatus "github.com/matcra587/slack-cli/internal/cli/status"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestStatusSetCommandSetsCustomStatus(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"users.profile.set": func(req testutil.SlackRequest) testutil.SlackResponse {
			var payload struct {
				StatusText       string `json:"status_text"`
				StatusEmoji      string `json:"status_emoji"`
				StatusExpiration int64  `json:"status_expiration"`
			}
			if err := json.Unmarshal([]byte(req.Form.Get("profile")), &payload); err != nil {
				t.Fatalf("profile form value is not JSON: %v", err)
			}
			if payload.StatusText != "Heads down" || payload.StatusEmoji != ":headphones:" {
				t.Fatalf("profile = %#v, want text and normalized emoji", payload)
			}
			if want := time.Date(2026, 5, 3, 15, 8, 0, 0, time.UTC).Unix(); payload.StatusExpiration != want {
				t.Fatalf("status_expiration = %d, want %d", payload.StatusExpiration, want)
			}
			return testutil.JSONResponse(`{"ok":true,"profile":{"status_text":"Heads down","status_emoji":":headphones:","status_expiration":1777820880}}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(), "",
		[]string{"status", "set", "--text", "Heads down", "--emoji", "headphones", "--expires-in", "2h"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"command":"status.set"`) || !strings.Contains(stdout, `"text":"Heads down"`) {
		t.Fatalf("stdout = %s, want status.set result", stdout)
	}
}

func TestStatusClearCommandClearsCustomStatus(t *testing.T) {
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"users.profile.set": func(req testutil.SlackRequest) testutil.SlackResponse {
			var payload struct {
				StatusText       string `json:"status_text"`
				StatusEmoji      string `json:"status_emoji"`
				StatusExpiration int64  `json:"status_expiration"`
			}
			if err := json.Unmarshal([]byte(req.Form.Get("profile")), &payload); err != nil {
				t.Fatalf("profile form value is not JSON: %v", err)
			}
			if payload.StatusText != "" || payload.StatusEmoji != "" || payload.StatusExpiration != 0 {
				t.Fatalf("profile = %#v, want cleared status", payload)
			}
			return testutil.JSONResponse(`{"ok":true,"profile":{"status_text":"","status_emoji":"","status_expiration":0}}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(), "",
		[]string{"status", "clear"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"command":"status.clear"`) {
		t.Fatalf("stdout = %s, want status.clear result", stdout)
	}
}

func TestStatusUntilUsesLocalCalendarDate(t *testing.T) {
	for _, tc := range []struct{ name, zone, now, until, want string }{
		{"Toronto spring", "America/Toronto", "2026-03-07T23:00:00", "22:00", "2026-03-08T22:00:00"},
		{"Toronto fall", "America/Toronto", "2026-10-31T23:00:00", "22:00", "2026-11-01T22:00:00"},
		{"Berlin spring", "Europe/Berlin", "2026-03-28T23:00:00", "22:00", "2026-03-29T22:00:00"},
		{"Berlin fall", "Europe/Berlin", "2026-10-24T23:00:00", "22:00", "2026-10-25T22:00:00"},
		{"Lord Howe spring", "Australia/Lord_Howe", "2026-10-03T23:00:00", "22:00", "2026-10-04T22:00:00"},
		{"Lord Howe fall", "Australia/Lord_Howe", "2026-04-04T23:00:00", "22:00", "2026-04-05T22:00:00"},
		{"Toronto spring elapsed", "America/Toronto", "2026-03-07T23:00:00", "+24h", "2026-03-09T00:00:00"},
		{"Toronto fall elapsed", "America/Toronto", "2026-10-31T23:00:00", "+24h", "2026-11-01T22:00:00"},
		{"Lord Howe spring elapsed", "Australia/Lord_Howe", "2026-10-03T23:00:00", "+24h", "2026-10-04T23:30:00"},
		{"Lord Howe fall elapsed", "Australia/Lord_Howe", "2026-04-04T23:00:00", "+24h", "2026-04-05T22:30:00"},
		{"later today", "America/Toronto", "2026-05-03T13:08:00", "14:15:16", "2026-05-03T14:15:16"},
		{"equal clock", "America/Toronto", "2026-05-03T13:08:00", "13:08", "2026-05-04T13:08:00"},
		{"earlier clock", "America/Toronto", "2026-05-03T13:08:00", "12:00", "2026-05-04T12:00:00"},
		{"month boundary", "America/Toronto", "2026-04-30T23:00:00", "22:00", "2026-05-01T22:00:00"},
		{"year boundary", "America/Toronto", "2026-12-31T23:00:00", "22:00", "2027-01-01T22:00:00"},
	} {
		for _, flag := range []string{"--until", "-U"} {
			for _, mode := range []string{"json", "compact", "human"} {
				for _, dryRun := range []bool{false, true} {
					name := tc.name + "/" + flag + "/" + mode + "/dry=" + strconv.FormatBool(dryRun)
					t.Run(name, func(t *testing.T) {
						t.Parallel()
						location, err := time.LoadLocation(tc.zone)
						if err != nil {
							t.Fatal(err)
						}
						now, err := time.ParseInLocation("2006-01-02T15:04:05", tc.now, location)
						if err != nil {
							t.Fatal(err)
						}
						want, err := time.ParseInLocation("2006-01-02T15:04:05", tc.want, location)
						if err != nil {
							t.Fatal(err)
						}
						cfg := workspaceConfig(config.TokenTypeUser)
						before, err := json.Marshal(cfg)
						if err != nil {
							t.Fatal(err)
						}
						server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
							"auth.test": func(req testutil.SlackRequest) testutil.SlackResponse {
								if req.Method != http.MethodPost || req.Form.Get("token") != "xoxp-synthetic-status-expiry" {
									t.Error("wrong status scope request")
								}
								return testutil.SlackResponse{Header: http.Header{"X-Oauth-Scopes": []string{"users.profile:write"}}, Body: `{"ok":true,"user_id":"UFIXTURE"}`}
							},
							"users.profile.set": func(req testutil.SlackRequest) testutil.SlackResponse {
								var profile map[string]any
								if req.Method != http.MethodPost || req.Form.Get("token") != "xoxp-synthetic-status-expiry" || len(req.Form) != 2 || json.Unmarshal([]byte(req.Form.Get("profile")), &profile) != nil || !reflect.DeepEqual(profile, map[string]any{"status_text": "PR review", "status_emoji": ":eyes:", "status_expiration": float64(want.Unix())}) {
									t.Error("status expiry request lost local calendar time or touched unrelated profile fields")
								}
								return testutil.JSONResponse(`{"ok":true}`)
							},
						})
						runtime, stdout, stderr := runtimetest.NewRuntime(t, runtimetest.Options{Config: cfg, SlackBaseURL: server.BaseURL(), Now: func() time.Time { return now }})
						resolutions := 0
						runtime.TokenResolver = cliruntime.TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
							resolutions++
							return "xoxp-synthetic-status-expiry", nil
						})
						root := runtimetest.NewRoot(runtime, stdout, stderr)
						root.AddCommand(clistatus.NewCommand(runtime))
						args := []string{"status", "set", "PR review", "eyes", flag, tc.until, "--output=" + mode, "--no-throttle", "--workspace=default"}
						if dryRun {
							args = append(args, "--dry-run")
						}
						out, diagnostic, err := runtimetest.Run(t, root, args, stdout, stderr)
						if err != nil || diagnostic != "" {
							t.Fatalf("status expiry failed: %v", err)
						}
						expectedRequests := 1
						if dryRun {
							expectedRequests = 0
						}
						if resolutions != expectedRequests || len(server.Requests("auth.test")) != expectedRequests || len(server.Requests("users.profile.set")) != expectedRequests {
							t.Fatal("status dry run accessed credentials or sent a mutation, or live fixture replayed")
						}
						if mode == "human" {
							if !strings.Contains(out, "expiration="+strconv.FormatInt(want.Unix(), 10)) {
								t.Fatal("status output lost local calendar expiry")
							}
						} else {
							data := json.RawMessage(out)
							if mode == "json" {
								var frame struct {
									OK     bool
									Data   json.RawMessage
									Meta   struct{ Command string }
									Errors []json.RawMessage
								}
								if json.Unmarshal(data, &frame) != nil || !frame.OK || frame.Meta.Command != "status.set" || frame.Errors == nil || len(frame.Errors) != 0 {
									t.Fatal("status expiry lost standard envelope")
								}
								data = frame.Data
							}
							var got map[string]any
							expected := map[string]any{"text": "PR review", "emoji": ":eyes:", "expiration": float64(want.Unix())}
							if dryRun {
								expected["dry_run"] = true
							}
							if json.Unmarshal(data, &got) != nil || !reflect.DeepEqual(got, expected) {
								t.Fatal("status output lost local calendar expiry")
							}
						}
						after, err := json.Marshal(cfg)
						if err != nil || string(before) != string(after) || strings.Contains(out+diagnostic, "xoxp-synthetic-status-expiry") {
							t.Fatal("status expiry changed config or exposed credentials")
						}
					})
				}
			}
		}
	}
}

// --- helpers ---

func executeTestRoot(t *testing.T, cfg *config.Config, baseURL, stdin string, args []string) (string, string, error) {
	t.Helper()
	runtime, stdout, stderr := runtimetest.NewRuntime(t, runtimetest.Options{
		Config:       cfg,
		SlackBaseURL: baseURL,
		Stdin:        strings.NewReader(stdin),
	})
	root := runtimetest.NewRoot(runtime, stdout, stderr)
	root.AddCommand(clistatus.NewCommand(runtime))
	return runtimetest.Run(t, root, args, stdout, stderr)
}

func workspaceConfig(tokenType config.TokenType) *config.Config {
	return &config.Config{
		SchemaVersion:    config.SchemaVersion,
		DefaultWorkspace: "default",
		Workspaces: map[string]config.WorkspaceProfile{
			"default": {
				Name:      "default",
				TeamID:    "T123",
				TokenType: tokenType,
				TokenRef:  "env:SLACK_TEST_TOKEN",
			},
		},
	}
}
