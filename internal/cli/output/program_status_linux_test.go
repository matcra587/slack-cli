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
	ctx.FinishProgramStatus(nil)
	if out.Len() != 0 || diagnostic.Len() != 0 {
		t.Fatal("forced colors enabled status on redirected stderr")
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
