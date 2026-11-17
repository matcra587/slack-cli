package output_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/gechr/clog"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestProgramStatusHonorsMachineAndTerminalBoundaries(t *testing.T) {
	for _, row := range []struct {
		name string
		mode clioutput.RenderMode
		tty  bool
		want bool
	}{
		{"human terminal", clioutput.RenderModePlain, true, true},
		{"JSON terminal", clioutput.RenderModeEnvelope, true, false},
		{"compact terminal", clioutput.RenderModeCompact, true, false},
		{"human pipe", clioutput.RenderModePlain, false, false},
	} {
		t.Run(row.name, func(t *testing.T) {
			stderr, read := testutil.NewTerminal(t)
			var stdout bytes.Buffer
			outlog, errlog := clioutput.BuildBaseLoggers(&stdout, stderr, clog.ColorNever)
			ctx := &clioutput.CommandContext{Command: "react.watch", Mode: row.mode, IsTTY: row.tty, StdoutLog: outlog, StderrLog: errlog}
			ctx.ReportProgramStatus(clog.ProgramStateWorking, "", "Waiting for a Slack reaction")
			ctx.ReportProgramProgress("Applying Slack reactions", 1, 2)
			ctx.FinishProgramStatus(nil)
			got := read()
			if stdout.Len() != 0 || strings.Contains(got, "\x1b]7501;") != row.want {
				t.Fatalf("status used the wrong stream or output mode: stdout=%q stderr=%q", stdout.String(), got)
			}
		})
	}
	var out, diagnostic bytes.Buffer
	outlog, errlog := clioutput.BuildBaseLoggers(&out, &diagnostic, clog.ColorAlways)
	ctx := &clioutput.CommandContext{Mode: clioutput.RenderModePlain, IsTTY: true, StdoutLog: outlog, StderrLog: errlog}
	ctx.ReportProgramStatus(clog.ProgramStateWorking, "", "Waiting")
	ctx.ReportProgramProgress("Applying Slack reactions", 1, 2)
	ctx.FinishProgramStatus(nil)
	if out.Len() != 0 || diagnostic.Len() != 0 {
		t.Fatal("forced colors enabled status on redirected stderr")
	}
}

func TestProgramProgressUsesKnownTotalsAndBounds(t *testing.T) {
	for _, row := range []struct {
		name             string
		completed, total int
		progress         string
	}{
		{"start", 0, 3, ":progress=0"},
		{"partial", 1, 3, ":progress=33"},
		{"decimal fraction", 29, 100, ":progress=29"},
		{"complete", 3, 3, ":progress=100"},
		{"negative", -1, 3, ":progress=0"},
		{"overflow", 4, 3, ":progress=100"},
		{"unknown", 0, 0, ""},
	} {
		t.Run(row.name, func(t *testing.T) {
			stderr, read := testutil.NewTerminal(t)
			_, logger := clioutput.BuildBaseLoggers(&bytes.Buffer{}, stderr, clog.ColorNever)
			ctx := &clioutput.CommandContext{Command: "react.add", Mode: clioutput.RenderModePlain, IsTTY: true, StderrLog: logger}
			ctx.ReportProgramProgress("Applying Slack reactions", row.completed, row.total)
			got := read()
			if !strings.Contains(got, "state=working:") || !strings.Contains(got, ":app=slick:") {
				t.Fatal("progress omitted the working state or application")
			}
			if row.progress == "" {
				if strings.Contains(got, "progress=") {
					t.Fatal("unknown total produced a percentage")
				}
			} else if !strings.Contains(got, row.progress) {
				t.Fatalf("incorrect batch progress: %q", got)
			}
		})
	}
}

func TestProgramStatusFinalStatesNeverExposeErrorDetails(t *testing.T) {
	for _, row := range []struct {
		name, state string
		err         error
	}{
		{"success", "done", nil},
		{"cancel", "idle", context.Canceled},
		{"classified cancel", "idle", clioutput.CommandError{CLIError: clioutput.CLIError{ExitCode: clioutput.ExitCodeCanceled}}},
		{"timeout", "error", context.DeadlineExceeded},
		{"failure", "error", errors.New("synthetic-private-provider")},
	} {
		t.Run(row.name, func(t *testing.T) {
			stderr, read := testutil.NewTerminal(t)
			_, errlog := clioutput.BuildBaseLoggers(&bytes.Buffer{}, stderr, clog.ColorNever)
			ctx := &clioutput.CommandContext{Command: "auth.login", Mode: clioutput.RenderModePlain, IsTTY: true, StderrLog: errlog}
			ctx.FinishProgramStatus(row.err)
			got := read()
			if !strings.HasPrefix(got, "\x1b]7501;state="+row.state+":") {
				t.Fatalf("incorrect final status: %q", got)
			}
			for field := range strings.SplitSeq(strings.TrimSuffix(got, "\a"), ":") {
				if encoded, ok := strings.CutPrefix(field, "msg="); ok {
					message, err := base64.StdEncoding.DecodeString(encoded)
					if err != nil || strings.Contains(string(message), "synthetic-private-provider") {
						t.Fatal("status exposed error details or an invalid message")
					}
				}
			}
		})
	}
}
