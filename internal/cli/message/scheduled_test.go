package message_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestScheduledSendEnvelopeIncludesAttribution(t *testing.T) {
	t.Parallel()

	postAt := time.Date(2026, 5, 3, 15, 8, 0, 0, time.UTC).Unix()
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"chat.scheduleMessage": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("channel"); got != "C123" {
				t.Fatalf("channel = %q, want C123", got)
			}
			if got := req.Form.Get("post_at"); got != "1777820880" {
				t.Fatalf("post_at = %q, want 1777820880", got)
			}
			if got := req.Form.Get("text"); got != "Deploy later" {
				t.Fatalf("text = %q, want Deploy later", got)
			}
			var blocks []map[string]any
			if err := json.Unmarshal([]byte(req.Form.Get("blocks")), &blocks); err != nil {
				t.Fatalf("blocks form value is not JSON: %v", err)
			}
			if len(blocks) != 2 || blocks[1]["type"] != "context" {
				t.Fatalf("blocks = %#v, want message block plus attribution context", blocks)
			}
			return testutil.JSONResponse(`{"ok":true,"channel":"C123","scheduled_message_id":"Q123","text":"Deploy later"}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "",
		[]string{
			"message", "send",
			"--channel", "C123",
			"--message", "Deploy later",
			"--schedule", "2h",
			"--attribution",
		},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	envelope := decodeMessageEnvelope(t, stdout)
	data := envelope["data"].(map[string]any)
	if data["scheduled_message_id"] != "Q123" {
		t.Fatalf("scheduled_message_id = %q, want Q123", data["scheduled_message_id"])
	}
	if int64(data["post_at"].(float64)) != postAt {
		t.Fatalf("post_at = %#v, want %d", data["post_at"], postAt)
	}
	attribution := data["attribution"].(map[string]any)
	if attribution["enabled"] != true || attribution["label"] != "slick" {
		t.Fatalf("attribution = %#v, want enabled slick attribution", attribution)
	}
}

func TestScheduledSendRejectsInvalidTargetsAndSchedulesBeforeSlack(t *testing.T) {
	t.Parallel()

	cfg := workspaceConfig(config.TokenTypeBot)
	profile := cfg.Workspaces["default"]
	profile.DefaultChannel = "CDEFAULT"
	cfg.Workspaces["default"] = profile

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "default channel not used for scheduled send",
			args: []string{"message", "send", "--message", "Later", "--schedule", "90m"},
			want: "channel or user is required",
		},
		{
			name: "natural language rejected",
			args: []string{"message", "send", "--channel", "C123", "--message", "Later", "--schedule", "tomorrow at 9am"},
			want: "schedule time must be RFC3339, Go duration, or Unix seconds",
		},
		{
			name: "date only rejected",
			args: []string{"message", "send", "--channel", "C123", "--message", "Later", "--schedule", "2026-06-01"},
			want: "schedule time must be RFC3339, Go duration, or Unix seconds",
		},
		{
			name: "past duration rejected",
			args: []string{"message", "send", "--channel", "C123", "--message", "Later", "--schedule", "-1m"},
			want: "schedule time must be in the future",
		},
		{
			name: "beyond 120 days rejected",
			args: []string{"message", "send", "--channel", "C123", "--message", "Later", "--schedule", "2881h"},
			want: "schedule time must be within 120 days",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
				"chat.scheduleMessage": func(testutil.SlackRequest) testutil.SlackResponse {
					t.Fatalf("%s reached chat.scheduleMessage", tt.name)
					return testutil.JSONResponse(`{"ok":false}`)
				},
				"users.lookupByEmail": func(testutil.SlackRequest) testutil.SlackResponse {
					t.Fatalf("%s reached users.lookupByEmail", tt.name)
					return testutil.JSONResponse(`{"ok":false}`)
				},
			})

			stdout, stderr, err := executeTestRoot(t, cfg, server.BaseURL(), "", tt.args)
			if err == nil {
				t.Fatalf("Execute returned nil error, want validation failure\nstdout=%s", stdout)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, `"type":"validation_error"`) || !strings.Contains(stderr, tt.want) {
				t.Fatalf("stderr = %s, want validation_error containing %q", stderr, tt.want)
			}
			if got := len(server.Requests("chat.scheduleMessage")); got != 0 {
				t.Fatalf("chat.scheduleMessage requests = %d, want 0", got)
			}
		})
	}
}

func TestScheduledSendSupportsUserAndEmailTargets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		args          []string
		wantOpenUsers string
		wantChannel   string
	}{
		{
			name:          "user alias",
			args:          []string{"message", "send", "--user", "oncall", "--message", "DM later", "--schedule", "90m"},
			wantOpenUsers: "UONCALL",
			wantChannel:   "D123",
		},
		{
			name:          "slack profile email",
			args:          []string{"message", "send", "--user", "dev@example.com", "--message", "Email later", "--schedule", "90m"},
			wantOpenUsers: "UDEV",
			wantChannel:   "D456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := workspaceConfig(config.TokenTypeUser)
			cfg.Workspaces["default"] = withAliases(cfg.Workspaces["default"], map[string]string{"oncall": "UONCALL"})
			server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
				"users.lookupByEmail": func(req testutil.SlackRequest) testutil.SlackResponse {
					if got := req.Form.Get("email"); got != "dev@example.com" {
						t.Fatalf("email = %q, want dev@example.com", got)
					}
					return testutil.JSONResponse(`{"ok":true,"user":{"id":"UDEV","name":"dev"}}`)
				},
				"conversations.open": func(req testutil.SlackRequest) testutil.SlackResponse {
					if got := req.Form.Get("users"); got != tt.wantOpenUsers {
						t.Fatalf("users = %q, want %s", got, tt.wantOpenUsers)
					}
					return testutil.JSONResponse(`{"ok":true,"channel":{"id":"` + tt.wantChannel + `"}}`)
				},
				"chat.scheduleMessage": func(req testutil.SlackRequest) testutil.SlackResponse {
					if got := req.Form.Get("channel"); got != tt.wantChannel {
						t.Fatalf("channel = %q, want %s", got, tt.wantChannel)
					}
					if got := req.Form.Get("post_at"); got == "" {
						t.Fatal("post_at is empty")
					}
					return testutil.JSONResponse(`{"ok":true,"channel":"` + tt.wantChannel + `","scheduled_message_id":"Q123","text":"scheduled"}`)
				},
			})

			stdout, stderr, err := executeTestRoot(t, cfg, server.BaseURL(), "", tt.args)
			if err != nil {
				t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
			}
			envelope := decodeMessageEnvelope(t, stdout)
			data := envelope["data"].(map[string]any)
			if data["channel"] != tt.wantChannel {
				t.Fatalf("data.channel = %q, want %s", data["channel"], tt.wantChannel)
			}
			if got := len(server.Requests("chat.scheduleMessage")); got != 1 {
				t.Fatalf("chat.scheduleMessage requests = %d, want 1", got)
			}
		})
	}
}

func TestScheduledSendRawBlocksArePassedToScheduleMessage(t *testing.T) {
	t.Parallel()

	rawBlocks := `[{"type":"section","text":{"type":"mrkdwn","text":"Deploy *later*"}}]`
	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"chat.scheduleMessage": func(req testutil.SlackRequest) testutil.SlackResponse {
			var blocks []map[string]any
			if err := json.Unmarshal([]byte(req.Form.Get("blocks")), &blocks); err != nil {
				t.Fatalf("blocks form value is not JSON: %v", err)
			}
			if len(blocks) != 1 || blocks[0]["type"] != "section" {
				t.Fatalf("blocks = %#v, want raw section block only", blocks)
			}
			if got := req.Form.Get("post_at"); got != "1780000000" {
				t.Fatalf("post_at = %q, want 1780000000", got)
			}
			return testutil.JSONResponse(`{"ok":true,"channel":"C123","scheduled_message_id":"Q123","text":"Deploy later"}`)
		},
	})

	_, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "",
		[]string{"message", "send", "--channel", "C123", "--message", rawBlocks, "--blocks", "--schedule", "1780000000"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
}

func TestScheduledSendDryRunUsesRootShortFlag(t *testing.T) {
	t.Parallel()

	server := testutil.NewSlackServer(t, nil)
	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "",
		[]string{"message", "send", "--channel", "C123", "--message", "Preview", "--schedule", "90m", "-n"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	if got := len(server.Requests("chat.scheduleMessage")); got != 0 {
		t.Fatalf("chat.scheduleMessage requests = %d, want 0", got)
	}
	if !strings.Contains(stdout, `"scheduled_message_id":"Q-dry-run"`) || !strings.Contains(stdout, `"dry_run":true`) {
		t.Fatalf("stdout = %s, want dry-run scheduled envelope", stdout)
	}
}

func TestScheduledSendPlainOutputUsesScheduledActionLabel(t *testing.T) {
	t.Parallel()

	server := testutil.NewSlackServer(t, nil)
	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "",
		[]string{"--output=human", "message", "send", "--channel", "C123", "--message", "Preview", "--schedule", "90m", "-n"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, "Message scheduled") || strings.Contains(stdout, "Message sent") {
		t.Fatalf("stdout = %q, want scheduled-send action label only", stdout)
	}
	if !strings.Contains(stdout, "scheduled_message_id=Q-dry-run") || !strings.Contains(stdout, "dry_run=true") {
		t.Fatalf("stdout = %q, want scheduled dry-run fields", stdout)
	}
}

func TestScheduledSendUserDryRunSkipsDMOpen(t *testing.T) {
	t.Parallel()

	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"users.lookupByEmail": func(testutil.SlackRequest) testutil.SlackResponse {
			t.Fatal("dry-run scheduled user send reached users.lookupByEmail")
			return testutil.JSONResponse(`{"ok":false}`)
		},
		"conversations.open": func(testutil.SlackRequest) testutil.SlackResponse {
			t.Fatal("dry-run scheduled user send reached conversations.open")
			return testutil.JSONResponse(`{"ok":false}`)
		},
		"chat.scheduleMessage": func(testutil.SlackRequest) testutil.SlackResponse {
			t.Fatal("dry-run scheduled user send reached chat.scheduleMessage")
			return testutil.JSONResponse(`{"ok":false}`)
		},
	})
	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeUser), server.BaseURL(), "",
		[]string{"message", "send", "--user", "dev@example.com", "--message", "Preview", "--schedule", "90m", "-n"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	if !strings.Contains(stdout, `"channel":"dev@example.com"`) || !strings.Contains(stdout, `"dry_run":true`) {
		t.Fatalf("stdout = %s, want dry-run scheduled user preview", stdout)
	}
	if got := len(server.Requests("conversations.open")); got != 0 {
		t.Fatalf("conversations.open requests = %d, want 0", got)
	}
}

func TestScheduledListWithDryRunStillReadsScheduledMessages(t *testing.T) {
	t.Parallel()

	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"chat.scheduledMessages.list": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("channel"); got != "C123" {
				t.Fatalf("channel = %q, want C123", got)
			}
			return testutil.JSONResponse(`{"ok":true,"scheduled_messages":[{"id":"Q123","channel_id":"C123","post_at":1770000000,"text":"Deploy later"}]}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "",
		[]string{"-n", "message", "scheduled", "list", "--channel", "C123"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	if got := len(server.Requests("chat.scheduledMessages.list")); got != 1 {
		t.Fatalf("chat.scheduledMessages.list requests = %d, want 1", got)
	}
	envelope := decodeMessageEnvelope(t, stdout)
	rows := envelope["data"].(map[string]any)["scheduled_messages"].([]any)
	if len(rows) != 1 {
		t.Fatalf("scheduled_messages length = %d, want 1", len(rows))
	}
	if strings.Contains(stdout, `"dry_run"`) {
		t.Fatalf("stdout = %s, list output should not grow dry_run fields", stdout)
	}
}

func TestScheduledListEnvelopePagination(t *testing.T) {
	t.Parallel()

	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"chat.scheduledMessages.list": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("channel"); got != "C123" {
				t.Fatalf("channel = %q, want C123", got)
			}
			if got := req.Form.Get("cursor"); got != "cur-1" {
				t.Fatalf("cursor = %q, want cur-1", got)
			}
			if got := req.Form.Get("limit"); got != "1" {
				t.Fatalf("limit = %q, want 1", got)
			}
			return testutil.JSONResponse(`{"ok":true,"scheduled_messages":[{"id":"Q123","channel_id":"C123","post_at":1770000000,"text":"` + strings.Repeat("x", 205) + `"}],"response_metadata":{"next_cursor":"cur-2"}}`)
		},
		"conversations.info": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("channel"); got != "C123" {
				t.Fatalf("conversations.info channel = %q, want C123", got)
			}
			return testutil.JSONResponse(`{"ok":true,"channel":{"id":"C123","name":"alerts","is_channel":true}}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "",
		[]string{"message", "scheduled", "list", "--channel", "C123", "--cursor", "cur-1", "--limit", "1"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	envelope := decodeMessageEnvelope(t, stdout)
	meta := envelope["meta"].(map[string]any)
	pagination := meta["pagination"].(map[string]any)
	if pagination["next_cursor"] != "cur-2" || pagination["has_more"] != true {
		t.Fatalf("pagination = %#v, want next cursor", pagination)
	}
	rows := envelope["data"].(map[string]any)["scheduled_messages"].([]any)
	row := rows[0].(map[string]any)
	if row["id"] != "Q123" || row["channel"] != "C123" {
		t.Fatalf("row = %#v, want scheduled message row", row)
	}
	if row["channel_name"] != "alerts" || row["channel_type"] != "channel" || row["is_dm"] != false {
		t.Fatalf("row channel metadata = %#v, want friendly channel metadata", row)
	}
	preview := row["text_preview"].(string)
	if len([]rune(preview)) != 200 || !strings.HasSuffix(preview, "…") {
		t.Fatalf("text_preview length/suffix = %d/%q, want 200 runes with ellipsis", len([]rune(preview)), preview)
	}
}

func TestScheduledListBestEffortMetadataFallsBackToRawChannel(t *testing.T) {
	t.Parallel()

	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"chat.scheduledMessages.list": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"scheduled_messages":[{"id":"Q123","channel_id":"C404","post_at":1770000000,"text":"Deploy later"}]}`)
		},
		"conversations.info": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":false,"error":"channel_not_found"}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "",
		[]string{"message", "scheduled", "list", "--channel", "C404"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	envelope := decodeMessageEnvelope(t, stdout)
	row := envelope["data"].(map[string]any)["scheduled_messages"].([]any)[0].(map[string]any)
	if row["channel"] != "C404" {
		t.Fatalf("row channel = %#v, want raw channel C404", row["channel"])
	}
	for _, field := range []string{"channel_name", "channel_type", "channel_user", "is_dm"} {
		if _, ok := row[field]; ok {
			t.Fatalf("row = %#v, did not want unresolved metadata field %q", row, field)
		}
	}
}

func TestScheduledListCachesChannelMetadata(t *testing.T) {
	t.Parallel()

	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"chat.scheduledMessages.list": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"scheduled_messages":[{"id":"Q123","channel_id":"C123","post_at":1770000000,"text":"first"},{"id":"Q124","channel_id":"C123","post_at":1770000060,"text":"second"}]}`)
		},
		"conversations.info": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"channel":{"id":"C123","name":"alerts","is_channel":true}}`)
		},
	})

	_, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "",
		[]string{"message", "scheduled", "list", "--channel", "C123"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	if got := len(server.Requests("conversations.info")); got != 1 {
		t.Fatalf("conversations.info requests = %d, want 1 for repeated channel", got)
	}
}

func TestScheduledListResolvesDMFriendlyNameBestEffort(t *testing.T) {
	t.Parallel()

	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"chat.scheduledMessages.list": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":true,"scheduled_messages":[{"id":"Q123","channel_id":"D123","post_at":1770000000,"text":"DM later"}]}`)
		},
		"conversations.info": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("channel"); got != "D123" {
				t.Fatalf("conversations.info channel = %q, want D123", got)
			}
			return testutil.JSONResponse(`{"ok":true,"channel":{"id":"D123","is_im":true,"user":"U123"}}`)
		},
		"users.info": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("user"); got != "U123" {
				t.Fatalf("users.info user = %q, want U123", got)
			}
			return testutil.JSONResponse(`{"ok":true,"user":{"id":"U123","name":"matt","profile":{"display_name":"matcra"}}}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "",
		[]string{"message", "scheduled", "list"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	envelope := decodeMessageEnvelope(t, stdout)
	rows := envelope["data"].(map[string]any)["scheduled_messages"].([]any)
	row := rows[0].(map[string]any)
	if row["channel"] != "D123" || row["channel_name"] != "matcra" || row["channel_type"] != "im" || row["channel_user"] != "U123" || row["is_dm"] != true {
		t.Fatalf("row = %#v, want raw channel plus friendly DM metadata", row)
	}
}

func TestScheduledDeleteDryRunSkipsSlackMutation(t *testing.T) {
	t.Parallel()

	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"auth.test": func(testutil.SlackRequest) testutil.SlackResponse {
			t.Fatal("dry-run scheduled delete reached auth.test")
			return testutil.JSONResponse(`{"ok":false}`)
		},
		"chat.deleteScheduledMessage": func(testutil.SlackRequest) testutil.SlackResponse {
			t.Fatal("dry-run scheduled delete reached chat.deleteScheduledMessage")
			return testutil.JSONResponse(`{"ok":false}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "",
		[]string{"message", "scheduled", "delete", "--channel", "C123", "--scheduled-id", "Q123", "-n"},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
	if got := len(server.Requests("chat.deleteScheduledMessage")); got != 0 {
		t.Fatalf("chat.deleteScheduledMessage requests = %d, want 0", got)
	}
	if !strings.Contains(stdout, `"deleted":true`) || !strings.Contains(stdout, `"dry_run":true`) {
		t.Fatalf("stdout = %s, want dry-run scheduled delete envelope", stdout)
	}
}

func TestScheduledDeleteEnvelopeAndValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		args       []string
		wantErr    bool
		wantDelete bool
	}{
		{
			name:       "success",
			args:       []string{"message", "scheduled", "delete", "--channel", "C123", "--scheduled-id", "Q123"},
			wantDelete: true,
		},
		{
			name:    "missing channel",
			args:    []string{"message", "scheduled", "delete", "--scheduled-id", "Q123"},
			wantErr: true,
		},
		{
			name:    "missing scheduled id",
			args:    []string{"message", "scheduled", "delete", "--channel", "C123"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
				"chat.deleteScheduledMessage": func(req testutil.SlackRequest) testutil.SlackResponse {
					if got := req.Form.Get("scheduled_message_id"); got != "Q123" {
						t.Fatalf("scheduled_message_id = %q, want Q123", got)
					}
					if got := req.Form.Get("as_user"); got != "true" {
						t.Fatalf("as_user = %q, want true", got)
					}
					return testutil.JSONResponse(`{"ok":true}`)
				},
			})
			stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "", tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Execute returned nil error, want validation failure\nstdout=%s", stdout)
				}
				if !strings.Contains(stderr, `"type":"validation_error"`) {
					t.Fatalf("stderr = %s, want validation_error", stderr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
			}
			if !strings.Contains(stdout, `"deleted":true`) {
				t.Fatalf("stdout = %s, want deleted true", stdout)
			}
			if tt.wantDelete && len(server.Requests("chat.deleteScheduledMessage")) != 1 {
				t.Fatalf("delete requests = %d, want 1", len(server.Requests("chat.deleteScheduledMessage")))
			}
		})
	}
}

func TestScheduledDeleteResolvesAliasAndTrimsInputs(t *testing.T) {
	t.Parallel()

	cfg := workspaceConfig(config.TokenTypeBot)
	profile := cfg.Workspaces["default"]
	profile.Aliases = map[string]string{"deploy": "C123"}
	cfg.Workspaces["default"] = profile

	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"chat.deleteScheduledMessage": func(req testutil.SlackRequest) testutil.SlackResponse {
			if got := req.Form.Get("channel"); got != "C123" {
				t.Fatalf("channel = %q, want alias-resolved C123", got)
			}
			if got := req.Form.Get("scheduled_message_id"); got != "Q123" {
				t.Fatalf("scheduled_message_id = %q, want trimmed Q123", got)
			}
			return testutil.JSONResponse(`{"ok":true}`)
		},
	})

	_, stderr, err := executeTestRoot(t, cfg, server.BaseURL(), "",
		[]string{"message", "scheduled", "delete", "--channel", " deploy ", "--scheduled-id", " Q123 "},
	)
	if err != nil {
		t.Fatalf("Execute returned error: %v\nstderr=%s", err, stderr)
	}
}

func TestScheduledSendMissingScopeIncludesNeeded(t *testing.T) {
	t.Parallel()

	server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{
		"chat.scheduleMessage": func(testutil.SlackRequest) testutil.SlackResponse {
			return testutil.JSONResponse(`{"ok":false,"error":"missing_scope","response_metadata":{"messages":["[ERROR] missing required scope: chat:write"]}}`)
		},
	})

	stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "",
		[]string{"message", "send", "--channel", "C123", "--message", "Later", "--schedule", "90m"},
	)
	if err == nil {
		t.Fatal("Execute returned nil error, want missing_scope")
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "missing_scope") || !strings.Contains(stderr, "chat:write") || !strings.Contains(stderr, `"needed":"chat:write"`) {
		t.Fatalf("stderr = %s, want missing_scope with needed chat:write", stderr)
	}
}

func TestScheduledCommandsMapSlackErrorsToFixedContract(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		method   string
		args     []string
		slackErr string
		wantType string
		wantText string
	}{
		{
			name:     "list missing scope",
			method:   "chat.scheduledMessages.list",
			args:     []string{"message", "scheduled", "list", "--channel", "C123"},
			slackErr: "missing_scope",
			wantType: "auth_failure",
			wantText: "chat:write",
		},
		{
			name:     "list not in channel",
			method:   "chat.scheduledMessages.list",
			args:     []string{"message", "scheduled", "list", "--channel", "C123"},
			slackErr: "not_in_channel",
			wantType: "not_found",
			wantText: "not_in_channel",
		},
		{
			name:     "delete scheduled message not found",
			method:   "chat.deleteScheduledMessage",
			args:     []string{"message", "scheduled", "delete", "--channel", "C123", "--scheduled-id", "Q404"},
			slackErr: "scheduled_message_not_found",
			wantType: "not_found",
			wantText: "scheduled_message_not_found",
		},
		{
			name:     "delete invalid arguments",
			method:   "chat.deleteScheduledMessage",
			args:     []string{"message", "scheduled", "delete", "--channel", "C123", "--scheduled-id", "Q123"},
			slackErr: "invalid_arguments",
			wantType: "validation_error",
			wantText: "invalid_arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handlers := map[string]testutil.SlackHandler{
				tt.method: func(testutil.SlackRequest) testutil.SlackResponse {
					if tt.slackErr == "missing_scope" {
						return testutil.JSONResponse(`{"ok":false,"error":"missing_scope","response_metadata":{"messages":["[ERROR] missing required scope: chat:write"]}}`)
					}
					return testutil.JSONResponse(`{"ok":false,"error":"` + tt.slackErr + `"}`)
				},
			}
			if tt.slackErr == "missing_scope" {
				handlers["auth.test"] = func(testutil.SlackRequest) testutil.SlackResponse {
					return testutil.SlackResponse{
						Body: `{"ok":true,"user_id":"U123"}`,
						Header: http.Header{
							"Content-Type":   []string{"application/json"},
							"X-OAuth-Scopes": []string{"channels:read"},
						},
					}
				}
			}
			server := testutil.NewSlackServer(t, handlers)

			stdout, stderr, err := executeTestRoot(t, workspaceConfig(config.TokenTypeBot), server.BaseURL(), "", tt.args)
			if err == nil {
				t.Fatalf("Execute returned nil error, want Slack error\nstdout=%s", stdout)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, `"type":"`+tt.wantType+`"`) || !strings.Contains(stderr, tt.wantText) {
				t.Fatalf("stderr = %s, want %s containing %s", stderr, tt.wantType, tt.wantText)
			}
			if tt.slackErr == "missing_scope" && !strings.Contains(stderr, `"needed":["chat:write"]`) {
				t.Fatalf("stderr = %s, want needed chat:write detail", stderr)
			}
		})
	}
}

func decodeMessageEnvelope(t *testing.T, stdout string) map[string]any {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	return envelope
}
