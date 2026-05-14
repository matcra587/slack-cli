package output_test

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/gechr/clog"
	"github.com/gechr/x/ansi"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
)

func TestSlackConversationURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ctx  clioutput.CommandContext
		ref  clioutput.SlackConversationRef
		want string
	}{
		{
			name: "macos channel uses native slack uri",
			ctx:  clioutput.CommandContext{TeamID: "T123", GOOS: "darwin"},
			ref:  clioutput.SlackConversationRef{ID: "C123"},
			want: "slack://channel?team=T123&id=C123",
		},
		{
			name: "macos dm uses user uri when user is known",
			ctx:  clioutput.CommandContext{TeamID: "T123", GOOS: "darwin"},
			ref:  clioutput.SlackConversationRef{ID: "D123", User: "U123", IsDM: new(true)},
			want: "slack://user?team=T123&id=U123",
		},
		{
			name: "non macos opens slack web app when team is known",
			ctx:  clioutput.CommandContext{TeamID: "T123", GOOS: "linux"},
			ref:  clioutput.SlackConversationRef{ID: "C123"},
			want: "https://app.slack.com/client/T123/C123",
		},
		{
			name: "missing team id cannot build a precise link",
			ctx:  clioutput.CommandContext{GOOS: "darwin"},
			ref:  clioutput.SlackConversationRef{ID: "C123"},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.ctx.SlackConversationURL(tt.ref); got != tt.want {
				t.Fatalf("SlackConversationURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSlackConversationDisplayText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ref  clioutput.SlackConversationRef
		want string
	}{
		{name: "channel name gets hash prefix", ref: clioutput.SlackConversationRef{ID: "C123", Name: "slack_test", Type: "channel"}, want: "#slack_test"},
		{name: "private channel name gets hash prefix", ref: clioutput.SlackConversationRef{ID: "G123", Name: "ops", Type: "private_channel"}, want: "#ops"},
		{name: "dm name gets at prefix", ref: clioutput.SlackConversationRef{ID: "D123", Name: "matcra587", IsDM: new(true)}, want: "@matcra587"},
		{name: "unknown falls back to id", ref: clioutput.SlackConversationRef{ID: "C123"}, want: "C123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.ref.DisplayText(); got != tt.want {
				t.Fatalf("DisplayText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSlackConversationFields(t *testing.T) {
	t.Parallel()

	ctx := clioutput.CommandContext{TeamID: "T123", GOOS: "linux"}
	fields := ctx.SlackConversationFields(clioutput.SlackConversationRef{
		ID:   "D123",
		Name: "matcra587",
		IsDM: new(true),
	})
	if fields.ChannelName != "matcra587" {
		t.Fatalf("ChannelName = %q, want matcra587", fields.ChannelName)
	}
	if fields.ChannelHR != "@matcra587" {
		t.Fatalf("ChannelHR = %q, want @matcra587", fields.ChannelHR)
	}
	if fields.ChannelURL != "https://app.slack.com/client/T123/D123" {
		t.Fatalf("ChannelURL = %q, want Slack web URL", fields.ChannelURL)
	}
}

func TestWriteScheduledMessageTableLinksChannel(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	sl, el := clioutput.BuildBaseLoggers(stdout, stderr, clog.ColorAlways)
	ctx := &clioutput.CommandContext{
		Workspace: "default",
		TeamID:    "T123",
		GOOS:      "darwin",
		Stdout:    stdout,
		Stderr:    io.Discard,
		ColorMode: clog.ColorAlways,
		IsTTY:     true,
		StdoutLog: sl,
		StderrLog: el,
	}

	isDM := false
	err := ctx.WriteScheduledMessageTable([]clioutput.ScheduledMessage{{
		ID:          "Q123",
		Channel:     "C123",
		ChannelName: "slack_test",
		ChannelType: "channel",
		IsDM:        &isDM,
		PostAtISO:   "2026-05-13T04:16:35Z",
		TextPreview: "scheduled hello",
	}})
	if err != nil {
		t.Fatalf("WriteScheduledMessageTable returned error: %v", err)
	}

	got := stdout.String()
	plain := ansi.Strip(got)
	if !strings.Contains(plain, "#slack_test") {
		t.Fatalf("stdout = %q, want friendly channel label", got)
	}
	if !strings.Contains(got, "slack://channel?team=T123&id=C123") {
		t.Fatalf("stdout = %q, want Slack channel hyperlink", got)
	}
	if !strings.Contains(got, "\x1b[4") {
		t.Fatalf("stdout = %q, want underlined hyperlink text", got)
	}
}
