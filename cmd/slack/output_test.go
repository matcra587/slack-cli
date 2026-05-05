package main

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gechr/clog"
	"github.com/gechr/x/ansi"
)

func TestOutputModeSelection(t *testing.T) {
	tests := []struct {
		name  string
		flags OutputFlags
		tty   bool
		agent bool
		want  OutputMode
	}{
		{name: "tty defaults to plain", tty: true, want: OutputModePlain},
		{name: "non tty defaults to json", want: OutputModeJSON},
		{name: "agent defaults to json", tty: true, agent: true, want: OutputModeJSON},
		{name: "json flag wins", flags: OutputFlags{JSON: true}, tty: true, want: OutputModeJSON},
		{name: "plain flag wins", flags: OutputFlags{Plain: true}, agent: true, want: OutputModePlain},
		{name: "compact flag wins", flags: OutputFlags{Compact: true}, tty: true, want: OutputModeCompact},
		{name: "raw flag wins", flags: OutputFlags{Raw: true}, want: OutputModeRaw},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.flags.Resolve(tt.tty, tt.agent)
			if got != tt.want {
				t.Fatalf("Resolve() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWriteResultJSONEnvelope(t *testing.T) {
	ctx, stdout, stderr := newOutputTestContext(OutputModeJSON)

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
	ctx, stdout, _ := newOutputTestContext(OutputModeCompact)

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
	ctx, stdout, _ := newOutputTestContext(OutputModePlain)

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

func TestWriteResultPlainAuthStatusUsesClogFields(t *testing.T) {
	ctx, stdout, stderr := newOutputTestContext(OutputModePlain)
	ctx.ColorMode = clog.ColorAlways

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
	t.Setenv("DEBUG", "1")
	ctx, stdout, stderr := newOutputTestContext(OutputModePlain)
	ctx.ColorMode = clog.ColorAlways

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
		"message send",
		"command=message.send",
		"channel=C7N2Q8L4P",
		"ts=1746284582.123456",
		"dry_run=false",
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
}

func TestWriteResultPlainFallbackUsesClogEvent(t *testing.T) {
	ctx, stdout, stderr := newOutputTestContext(OutputModePlain)

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

func newOutputTestContext(mode OutputMode) (*CommandContext, *bytes.Buffer, *bytes.Buffer) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	return &CommandContext{
		Workspace: "default",
		Mode:      mode,
		Stdout:    stdout,
		Stderr:    stderr,
		Now: func() time.Time {
			return time.Date(2026, 5, 3, 13, 8, 0, 0, time.UTC)
		},
		RequestID: func() string {
			return "test-request"
		},
	}, stdout, stderr
}
