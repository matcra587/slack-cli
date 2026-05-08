package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gechr/clog"
	"github.com/gechr/x/ansi"
	"github.com/matcra587/slack-cli/internal/config"
)

func TestOutputModeSelection(t *testing.T) {
	tests := []struct {
		name  string
		flags OutputFlags
		tty   bool
		agent bool
		want  RenderMode
	}{
		{name: "tty defaults to plain", tty: true, want: RenderModePlain},
		{name: "non tty defaults to json", want: RenderModeEnvelope},
		{name: "agent defaults to json", tty: true, agent: true, want: RenderModeEnvelope},
		{name: "json flag wins", flags: OutputFlags{JSON: true}, tty: true, want: RenderModeEnvelope},
		{name: "plain flag wins", flags: OutputFlags{Plain: true}, agent: true, want: RenderModePlain},
		{name: "compact flag wins", flags: OutputFlags{Compact: true}, tty: true, want: RenderModeCompact},
		{name: "raw flag wins", flags: OutputFlags{Raw: true}, want: RenderModeRaw},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.flags.Resolve(tt.tty, tt.agent)
			if got != tt.want {
				t.Fatalf("Resolve() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWriteResultJSONEnvelope(t *testing.T) {
	ctx, stdout, stderr := newOutputTestContext(RenderModeEnvelope)

	err := ctx.WriteResult("message.send", map[string]any{
		"ts":      "1746284582.123456",
		"channel": "C123",
	})
	if err != nil {
		t.Fatalf("WriteResult returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	meta := envelope["meta"].(map[string]any)
	if meta["command"] != "message.send" {
		t.Fatalf("meta.command = %q", meta["command"])
	}
	if meta["workspace"] != "default" {
		t.Fatalf("meta.workspace = %q", meta["workspace"])
	}
	if meta["request_id"] != "test-request" {
		t.Fatalf("meta.request_id = %q", meta["request_id"])
	}
	if len(envelope["errors"].([]any)) != 0 {
		t.Fatalf("errors = %#v, want empty", envelope["errors"])
	}
}

func TestWriteResultCompactOutputsDataOnly(t *testing.T) {
	ctx, stdout, _ := newOutputTestContext(RenderModeCompact)

	err := ctx.WriteResult("message.send", map[string]any{"ts": "1746284582.123456"})
	if err != nil {
		t.Fatalf("WriteResult returned error: %v", err)
	}

	var data map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &data); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if _, ok := data["meta"]; ok {
		t.Fatalf("compact output contained envelope metadata: %s", stdout.String())
	}
	if data["ts"] != "1746284582.123456" {
		t.Fatalf("data.ts = %q", data["ts"])
	}
}

func TestWritePlainUsesClogDataOutput(t *testing.T) {
	ctx, stdout, _ := newOutputTestContext(RenderModePlain)

	err := ctx.WritePlain("Message sent to #alerts")
	if err != nil {
		t.Fatalf("WritePlain returned error: %v", err)
	}

	got := stdout.String()
	if !strings.Contains(got, "Message sent to #alerts") {
		t.Fatalf("stdout = %q, want confirmation text", got)
	}
	if strings.Contains(got, "{") {
		t.Fatalf("plain output unexpectedly looked like JSON: %q", got)
	}
}

func TestWriteErrorPlainUsesClogDiagnosticFields(t *testing.T) {
	ctx, stdout, stderr := newOutputTestContext(RenderModePlain)

	exitCode := ctx.WriteError(CLIError{
		Type:     ErrorTypeValidation,
		Message:  "channel is required",
		ExitCode: ExitCodeValidation,
		Details:  map[string]any{"flag": "channel"},
	})

	if exitCode != ExitCodeValidation {
		t.Fatalf("exit code = %d, want %d", exitCode, ExitCodeValidation)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	got := stderr.String()
	plain := ansi.Strip(got)
	for _, fragment := range []string{
		"ERR",
		"channel is required",
		"type=validation_error",
		"exit_code=4",
		"flag=channel",
	} {
		if !strings.Contains(plain, fragment) {
			t.Fatalf("stderr = %q, want fragment %q", got, fragment)
		}
	}
	if strings.Contains(plain, "{") {
		t.Fatalf("stderr = %q, did not want JSON in plain error mode", got)
	}
}

func TestWriteResultPlainAuthStatusUsesClogFields(t *testing.T) {
	ctx, stdout, stderr := newOutputTestContext(RenderModePlain, clog.ColorAlways)

	err := ctx.WriteResult("auth.status", authStatusData{Workspaces: []authWorkspaceData{
		{
			Workspace:       "default",
			Authenticated:   true,
			TokenType:       "user",
			TeamID:          "T8KQ42P9D",
			TeamName:        "Example Notifications",
			ValidationState: "valid",
		},
	}})
	if err != nil {
		t.Fatalf("WriteResult returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	got := stdout.String()
	plain := ansi.Strip(got)
	for _, fragment := range []string{
		"INF",
		"auth status",
		"workspace=default",
		"authenticated=true",
		"token_type=user",
		"team_id=T8KQ42P9D",
		"team_name=\"Example Notifications\"",
		"valid=true",
	} {
		if !strings.Contains(plain, fragment) {
			t.Fatalf("stdout = %q, want fragment %q", got, fragment)
		}
	}
	if !regexp.MustCompile(`team_id.*\x1b\[38;(?:2|5);[^m]*mT8KQ42P9D`).MatchString(got) {
		t.Fatalf("stdout = %q, want hash-colored team_id value", got)
	}
	for _, field := range []string{"authenticated", "valid"} {
		if !regexp.MustCompile(field + `.*\x1b\[[^m]*32mtrue`).MatchString(got) {
			t.Fatalf("stdout = %q, want theme-colored bool field %q", got, field)
		}
	}
	if strings.Contains(got, "{\"workspaces\"") || strings.Contains(got, "default valid user") || strings.Contains(got, "validation_state=") {
		t.Fatalf("stdout = %q, want clog field output", got)
	}
}

func TestWriteResultPlainMessageSendUsesClogFieldsAndDebugDetails(t *testing.T) {
	clog.SetVerbose(true)
	t.Cleanup(func() { clog.SetVerbose(false) })
	ctx, stdout, stderr := newOutputTestContext(RenderModePlain, clog.ColorAlways)

	err := ctx.WriteResult("message.send", sendCommandData{
		Message: cliMessage{
			TS:      "1746284582.123456",
			Channel: stringPtr("C7N2Q8L4P"),
		},
		DryRun:      false,
		Attribution: true,
		Permalink:   stringPtr("https://example.slack.com/archives/C7N2Q8L4P/p1746284582123456"),
	})
	if err != nil {
		t.Fatalf("WriteResult returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	got := stdout.String()
	plain := ansi.Strip(got)
	for _, fragment := range []string{
		"INF",
		"command=message.send",
		"channel=C7N2Q8L4P",
		"ts=1746284582.123456",
		"attribution=true",
		"time=",
		"age=",
		"permalink=",
	} {
		if !strings.Contains(plain, fragment) {
			t.Fatalf("stdout = %q, want fragment %q", got, fragment)
		}
	}
	if !regexp.MustCompile(`channel.*\x1b\[38;(?:2|5);[^m]*mC7N2Q8L4P`).MatchString(got) {
		t.Fatalf("stdout = %q, want hash-colored channel value", got)
	}
	if strings.Contains(plain, "dry_run=false") {
		t.Fatalf("stdout = %q, should omit false dry_run field", got)
	}
}

func TestWriteResultPlainActionOutputsUseConciseClogFields(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		data any
		want []string
		deny []string
	}{
		{
			name: "auth login",
			cmd:  "auth.login",
			data: authWorkspaceData{
				Workspace:     "default",
				Authenticated: true,
				TokenType:     config.TokenTypeUser,
				TeamID:        "T8KQ42P9D",
				TeamName:      "Example Notifications",
			},
			want: []string{
				"INF",
				"command=auth.login",
				"workspace=default",
				"authenticated=true",
				"token_type=user",
				"team_id=T8KQ42P9D",
				"team_name=\"Example Notifications\"",
			},
		},
		{
			name: "auth switch",
			cmd:  "auth.switch",
			data: authWorkspaceData{Workspace: "example"},
			want: []string{"INF", "command=auth.switch", "workspace=example"},
			deny: []string{"data="},
		},
		{
			name: "message delete",
			cmd:  "message.delete",
			data: deleteMessageData{
				Channel:   "C7N2Q8L4P",
				Timestamp: "1746284582.123456",
				Deleted:   true,
				DryRun:    true,
			},
			want: []string{
				"INF",
				"command=message.delete",
				"channel=C7N2Q8L4P",
				"ts=1746284582.123456",
				"deleted=true",
				"dry_run=true",
			},
		},
		{
			name: "file upload",
			cmd:  "file.upload",
			data: uploadFileResult{
				Channel: "C7N2Q8L4P",
				File: cliFile{
					ID:   "F123",
					Name: "report.txt",
					Size: 128,
				},
				DryRun: true,
			},
			want: []string{
				"INF",
				"command=file.upload",
				"channel=C7N2Q8L4P",
				"file_id=F123",
				"file_name=report.txt",
				"size=128",
				`size_human="128 B"`,
				"dry_run=true",
			},
		},
		{
			name: "react add",
			cmd:  "react.add",
			data: reactionCommandData{Reaction: &reactionResult{
				Channel:   "C7N2Q8L4P",
				Timestamp: "1746284582.123456",
				Emoji:     "thumbsup",
			}},
			want: []string{
				"INF",
				"command=react.add",
				"channel=C7N2Q8L4P",
				"ts=1746284582.123456",
				"emoji=thumbsup",
			},
			deny: []string{"removed=false", "dry_run=false"},
		},
		{
			name: "config init",
			cmd:  "config.init",
			data: configInitData{
				Path:      "/tmp/slick/config.toml",
				Profile:   "default",
				Workspace: "default",
				Written:   true,
			},
			want: []string{
				"INF",
				"command=config.init",
				"path=/tmp/slick/config.toml",
				"profile=default",
				"workspace=default",
				"written=true",
			},
		},
		{
			name: "config path",
			cmd:  "config.path",
			data: configPathData{Path: "/tmp/slick/config.toml", Exists: true},
			want: []string{
				"INF",
				"command=config.path",
				"path=/tmp/slick/config.toml",
				"exists=true",
			},
			deny: []string{"data="},
		},
		{
			name: "config get",
			cmd:  "config.get",
			data: configGetData{Key: "workspaces.default.default_channel", Value: "C7N2Q8L4P"},
			want: []string{
				"INF",
				"command=config.get",
				"key=workspaces.default.default_channel",
				"value=C7N2Q8L4P",
			},
			deny: []string{"data="},
		},
		{
			name: "config set",
			cmd:  "config.set",
			data: configMutationData{
				Path:  "/tmp/slick/config.toml",
				Key:   "workspaces.default.attribution.message",
				Value: "Sent via slick",
			},
			want: []string{
				"INF",
				"command=config.set",
				"path=/tmp/slick/config.toml",
				"key=workspaces.default.attribution.message",
				"value=\"Sent via slick\"",
			},
			deny: []string{"data="},
		},
		{
			name: "config unset",
			cmd:  "config.unset",
			data: configMutationData{
				Path: "/tmp/slick/config.toml",
				Key:  "workspaces.default.attribution.message",
			},
			want: []string{
				"INF",
				"command=config.unset",
				"path=/tmp/slick/config.toml",
				"key=workspaces.default.attribution.message",
			},
			deny: []string{"data=", "value="},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, stdout, stderr := newOutputTestContext(RenderModePlain)
			if err := ctx.WriteResult(tt.cmd, tt.data); err != nil {
				t.Fatalf("WriteResult returned error: %v", err)
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty", stderr.String())
			}
			got := stdout.String()
			plain := ansi.Strip(got)
			for _, fragment := range tt.want {
				if !strings.Contains(plain, fragment) {
					t.Fatalf("stdout = %q, want fragment %q", got, fragment)
				}
			}
			deny := append([]string{"{", "}"}, tt.deny...)
			for _, fragment := range deny {
				if strings.Contains(plain, fragment) {
					t.Fatalf("stdout = %q, did not want fragment %q", got, fragment)
				}
			}
		})
	}
}

func TestWriteResultPlainSingletonLookupUsesClogFields(t *testing.T) {
	ctx, stdout, stderr := newOutputTestContext(RenderModePlain)

	err := ctx.WriteResult("lookup.channel", channelInfoData{Channel: cliChannel{
		ID:         "C7N2Q8L4P",
		Name:       "alerts",
		Type:       "channel",
		IsMember:   new(true),
		IsArchived: new(false),
		Topic:      stringPtr("Ops alerts"),
		NumMembers: intPtr(12),
	}})
	if err != nil {
		t.Fatalf("WriteResult returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	got := stdout.String()
	plain := ansi.Strip(got)
	for _, fragment := range []string{
		"INF",
		"command=lookup.channel",
		"channel=C7N2Q8L4P",
		"name=alerts",
		"type=channel",
		"is_member=true",
		"topic=\"Ops alerts\"",
		"num_members=12",
	} {
		if !strings.Contains(plain, fragment) {
			t.Fatalf("stdout = %q, want fragment %q", got, fragment)
		}
	}
	if strings.Contains(plain, "CHANNEL") || strings.Contains(plain, "{") || strings.Contains(plain, "is_archived=false") {
		t.Fatalf("stdout = %q, want clog event output for singleton lookup", got)
	}

	stdout.Reset()
	err = ctx.WriteResult("lookup.user", userInfoData{User: cliUser{
		ID:         "U7N2Q8L4P",
		Name:       "matt",
		Deleted:    new(false),
		Timezone:   stringPtr("America/Toronto"),
		Presence:   stringPtr("active"),
		StatusText: stringPtr("Deploying"),
	}})
	if err != nil {
		t.Fatalf("WriteResult returned error: %v", err)
	}
	got = stdout.String()
	plain = ansi.Strip(got)
	for _, fragment := range []string{
		"INF",
		"command=lookup.user",
		"user=U7N2Q8L4P",
		"name=matt",
		"timezone=America/Toronto",
		"presence=active",
		"status_text=Deploying",
	} {
		if !strings.Contains(plain, fragment) {
			t.Fatalf("stdout = %q, want fragment %q", got, fragment)
		}
	}
	if strings.Contains(plain, "USER") || strings.Contains(plain, "{") || strings.Contains(plain, "deleted=false") {
		t.Fatalf("stdout = %q, want clog event output for singleton lookup", got)
	}
}

func TestWriteResultPlainConfigListUsesPerSettingClogLines(t *testing.T) {
	ctx, stdout, stderr := newOutputTestContext(RenderModePlain)

	err := ctx.WriteResult("config.list", configListData{
		Path:             "/tmp/slick/config.toml",
		DefaultWorkspace: "default",
		Settings: []configEntry{
			{Key: "default_workspace", Value: "default"},
			{Key: "workspaces.default.attribution.enabled", Value: "true"},
		},
	})
	if err != nil {
		t.Fatalf("WriteResult returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	got := stdout.String()
	plain := ansi.Strip(got)
	for _, fragment := range []string{
		"INF",
		"command=config.list",
		"path=/tmp/slick/config.toml",
		"default_workspace=default",
		"settings=2",
		"config setting",
		"key=default_workspace",
		"value=default",
		"key=workspaces.default.attribution.enabled",
		"value=true",
	} {
		if !strings.Contains(plain, fragment) {
			t.Fatalf("stdout = %q, want fragment %q", got, fragment)
		}
	}
	if strings.Contains(plain, "{") || strings.Contains(plain, "description=") {
		t.Fatalf("stdout = %q, want concise clog fields", got)
	}
	if lines := strings.Count(strings.TrimSpace(plain), "\n") + 1; lines != 3 {
		t.Fatalf("stdout = %q, want summary plus one clog line per setting", got)
	}
}

func TestWriteResultPlainConfigPathsContractHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".config", "slick", "config.toml")
	ctx, stdout, stderr := newOutputTestContext(RenderModePlain)

	err := ctx.WriteResult("config.path", configPathData{Path: path, Exists: true})
	if err != nil {
		t.Fatalf("WriteResult returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	plain := ansi.Strip(stdout.String())
	if !strings.Contains(plain, "path=~/.config/slick/config.toml") {
		t.Fatalf("stdout = %q, want contracted home path", stdout.String())
	}
	if strings.Contains(plain, home) {
		t.Fatalf("stdout = %q, should not expose absolute home path %q", stdout.String(), home)
	}
}

func TestWriteResultPlainFallbackUsesClogEvent(t *testing.T) {
	ctx, stdout, stderr := newOutputTestContext(RenderModePlain)

	err := ctx.WriteResult("unknown", map[string]any{"ok": true})
	if err != nil {
		t.Fatalf("WriteResult returned error: %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "INF") || !strings.Contains(got, "unknown") || !strings.Contains(got, "command=unknown") {
		t.Fatalf("stdout = %q, want clog event output", got)
	}
	if strings.Contains(got, "{\"ok\"") {
		t.Fatalf("stdout = %q, did not want raw JSON in plain mode", got)
	}
}

func newOutputTestContext(mode RenderMode, colorMode ...clog.ColorMode) (*CommandContext, *bytes.Buffer, *bytes.Buffer) {
	cm := clog.ColorAuto
	if len(colorMode) > 0 {
		cm = colorMode[0]
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	sl, el := buildBaseLoggers(stdout, stderr, cm)
	applyRenderMode(sl, mode)
	return &CommandContext{
		Workspace: "default",
		Mode:      mode,
		Stdout:    stdout,
		Stderr:    stderr,
		ColorMode: cm,
		Now: func() time.Time {
			return time.Date(2026, 5, 3, 13, 8, 0, 0, time.UTC)
		},
		RequestID: func() string {
			return "test-request"
		},
		stdoutLog: sl,
		stderrLog: el,
	}, stdout, stderr
}
