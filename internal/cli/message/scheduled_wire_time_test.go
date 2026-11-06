package message_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	climessage "github.com/matcra587/slack-cli/internal/cli/message"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	"github.com/matcra587/slack-cli/internal/cli/runtime/runtimetest"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestScheduledSendValidatesWholeSecondWireTime(t *testing.T) {
	isolatedRoot := t.TempDir()
	for key, value := range map[string]string{
		"HOME":                        isolatedRoot,
		"USERPROFILE":                 isolatedRoot,
		"XDG_CACHE_HOME":              filepath.Join(isolatedRoot, "cache"),
		"XDG_CONFIG_HOME":             filepath.Join(isolatedRoot, "config"),
		"SLICK_CONFIG":                filepath.Join(isolatedRoot, "config", "slick.toml"),
		"SLACK_CLI_CONFIG":            filepath.Join(isolatedRoot, "config", "slick.toml"),
		"SLACK_CLI_DEFAULT_WORKSPACE": "",
	} {
		t.Setenv(key, value)
	}
	now := time.Date(2026, 5, 3, 13, 8, 0, 750_000_000, time.UTC)
	maxSeconds := int64(120 * 24 * 60 * 60)
	for _, tc := range []struct {
		name, input string
		offset      int64
		invalid     bool
	}{
		{"nanosecond same wire second", "1ns", 0, true},
		{"millisecond same wire second", "249ms", 0, true},
		{"crosses whole-second boundary", "250ms", 1, false},
		{"fraction above boundary", "251ms", 1, false},
		{"fractional duration", "1.25s", 2, false},
		{"fractional RFC3339 same second", now.Add(100 * time.Millisecond).Format(time.RFC3339Nano), 0, true},
		{"fractional RFC3339 next second", now.Add(300 * time.Millisecond).Format(time.RFC3339Nano), 1, false},
		{"exact current Unix second", strconv.FormatInt(now.Unix(), 10), 0, true},
		{"next Unix second", strconv.FormatInt(now.Unix()+1, 10), 1, false},
		{"maximum duration", "2880h", maxSeconds, false},
		{"maximum wire second fractional input", "2880h0.249s", maxSeconds, false},
		{"next wire second exceeds maximum", "2880h0.250s", 0, true},
		{"beyond maximum", "2881h", 0, true},
		{"negative duration", "-1ns", 0, true},
	} {
		for _, scheduleFlag := range []string{"--schedule", "-s"} {
			for _, mode := range []string{"json", "compact", "human"} {
				for _, dry := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/%s/dry=%t", tc.name, scheduleFlag, mode, dry), func(t *testing.T) {
						const token = "xoxb-synthetic-schedule-seconds"
						server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
							"auth.test": func(testutil.SlackRequest) testutil.SlackResponse {
								response := testutil.JSONResponse(`{"ok":true,"user_id":"U1"}`)
								response.Header.Set("X-OAuth-Scopes", "chat:write")
								return response
							},
							"chat.scheduleMessage": func(request testutil.SlackRequest) testutil.SlackResponse {
								if request.Method != http.MethodPost || request.Path != "/api/chat.scheduleMessage" || request.Form.Get("channel") != "C123" || request.Form.Get("token") != token {
									t.Error("schedule wire method/target/authentication changed")
								}
								if request.Form.Get("post_at") != strconv.FormatInt(now.Unix()+tc.offset, 10) {
									t.Errorf("wire scheduling time differs: got %q want %d", request.Form.Get("post_at"), now.Unix()+tc.offset)
								}
								return testutil.JSONResponse(`{"ok":true,"channel":"C123","scheduled_message_id":"Q1"}`)
							},
						})
						cfg := workspaceConfig(config.TokenTypeBot)
						before, err := json.Marshal(cfg)
						if err != nil {
							t.Fatal(err)
						}
						runtime, stdout, stderr := runtimetest.NewRuntime(t, runtimetest.Options{Config: cfg, SlackBaseURL: server.BaseURL(), Now: func() time.Time { return now }})
						resolutions := 0
						runtime.TokenResolver = cliruntime.TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) { resolutions++; return token, nil })
						root := runtimetest.NewRoot(runtime, stdout, stderr)
						root.AddCommand(climessage.NewCommand(runtime))
						args := []string{"message", "send", "--channel=C123", "--message=Release follow-up", scheduleFlag, tc.input, "--no-attribution", "--no-throttle", "--output=" + mode}
						if dry {
							args = append(args, "--dry-run")
						}
						out, diagnostic, err := runtimetest.Run(t, root, args, stdout, stderr)
						if tc.invalid {
							var commandErr clioutput.CommandError
							if !errors.As(err, &commandErr) || commandErr.CLIError.ExitCode != 4 {
								t.Fatalf("invalid whole-second schedule did not fail locally: err=%v output=%s", err, out)
							}
						} else if err != nil {
							t.Fatalf("valid whole-second schedule rejected: %v %s", err, out)
						}
						wantCalls := 1
						if dry || tc.invalid {
							wantCalls = 0
						}
						if len(server.Requests("chat.scheduleMessage")) != wantCalls || len(server.Requests("auth.test")) != wantCalls || resolutions != wantCalls {
							t.Fatal("wire-time validation reached credentials/Slack, omitted schedule, or retried")
						}
						if mode == "human" {
							if tc.invalid && (out != "" || diagnostic == "") {
								t.Fatal("wrong human validation streams")
							}
						} else {
							if diagnostic != "" {
								t.Fatal("machine validation escaped stdout envelope")
							}
							raw := []byte(out)
							if mode == "json" || tc.invalid {
								var frame struct {
									OK   bool
									Data json.RawMessage
									Meta struct {
										Command string
										Code    int `json:"exit_code"`
									}
								}
								if json.Unmarshal(raw, &frame) != nil || frame.OK == tc.invalid || frame.Meta.Command != "message.send" {
									t.Fatal("invalid schedule envelope")
								}
								wantCode := 0
								if tc.invalid {
									wantCode = 4
								}
								if frame.Meta.Code != wantCode {
									t.Fatal("wrong schedule exit metadata")
								}
								raw = frame.Data
							}
							if !tc.invalid {
								var data struct {
									PostAt int64  `json:"post_at"`
									ISO    string `json:"post_at_iso"`
									Dry    bool   `json:"dry_run"`
								}
								if json.Unmarshal(raw, &data) != nil || data.PostAt != now.Unix()+tc.offset || data.ISO != time.Unix(data.PostAt, 0).UTC().Format(time.RFC3339) || data.Dry != dry {
									t.Fatalf("receipt differs from exact whole-second request: %s", raw)
								}
							}
						}
						if strings.Contains(out+diagnostic, token) {
							t.Fatal("schedule result leaked credentials")
						}
						after, err := json.Marshal(cfg)
						if err != nil || string(after) != string(before) {
							t.Fatal("schedule validation changed prior configuration")
						}
					})
				}
			}
		}
	}
}
