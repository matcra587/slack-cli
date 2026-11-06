package output_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gechr/clog"
	clifile "github.com/matcra587/slack-cli/internal/cli/file"
	clihealth "github.com/matcra587/slack-cli/internal/cli/health"
	climessage "github.com/matcra587/slack-cli/internal/cli/message"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	cliworkspace "github.com/matcra587/slack-cli/internal/cli/workspace"
)

func TestHumanResultFailuresReturnIOWithoutLeakingWriterDiagnostics(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, command string
		data          any
	}{
		{"message", "message.send", climessage.SendData{}},
		{"upload", "file.upload", clifile.UploadData{}},
		{"health", "health.check", clihealth.CheckData{}},
		{"empty workspaces", "workspace.list", cliworkspace.ListData{}},
		{"fallback", "synthetic", map[string]string{"result": "accepted"}},
	} {
		for _, failure := range []string{"error", "short", "partial"} {
			t.Run(tc.name+"/"+failure, func(t *testing.T) {
				t.Parallel()
				destination := &humanFailureWriter{failure: failure}
				var diagnostic bytes.Buffer
				outlog, errlog := clioutput.BuildBaseLoggers(destination, &diagnostic, clog.ColorNever)
				c := &clioutput.CommandContext{Mode: clioutput.RenderModePlain, Stdout: destination, Stderr: &diagnostic, StdoutLog: outlog, StderrLog: errlog, ColorMode: clog.ColorNever}
				err := c.WriteResult(tc.command, tc.data)
				commandErr, ok := errors.AsType[clioutput.CommandError](err)
				if !ok || commandErr.CLIError.ExitCode != 8 || commandErr.CLIError.Type != "io" {
					t.Fatalf("human result lost write failure: %v", err)
				}
				if destination.calls != 1 || strings.Count(diagnostic.String(), "output_write_failed") != 1 {
					t.Fatal("failed result was replayed or diagnostic duplicated")
				}
				if strings.Contains(fmt.Sprintf("%v %#v %s", err, err, &diagnostic), "synthetic-private-writer") {
					t.Fatal("writer diagnostics exposed private content")
				}
			})
		}
	}
}

func TestHumanListCallbackAndFallbackPropagateWriteFailures(t *testing.T) {
	t.Parallel()
	for _, callback := range []bool{false, true} {
		t.Run(fmt.Sprint(callback), func(t *testing.T) {
			t.Parallel()
			destination := &humanFailureWriter{failure: "error"}
			var diagnostic bytes.Buffer
			outlog, errlog := clioutput.BuildBaseLoggers(destination, &diagnostic, clog.ColorNever)
			c := &clioutput.CommandContext{Mode: clioutput.RenderModePlain, Stdout: destination, Stderr: &diagnostic, StdoutLog: outlog, StderrLog: errlog}
			var render clioutput.ListPlainWriter[string]
			if callback {
				render = func(c *clioutput.CommandContext, command string, _ []string, p *clioutput.Pagination) error {
					c.FinishResult(c.ResultEvent(command), command, p)
					return nil
				}
			}
			err := clioutput.WriteList(c, "message.scheduled.list", nil, []string{}, nil, render)
			e, ok := errors.AsType[clioutput.CommandError](err)
			if !ok || e.CLIError.ExitCode != 8 || destination.calls != 1 {
				t.Fatalf("list result lost write failure: %v", err)
			}
		})
	}
}

func TestMachineWriterFailuresOmitUntrustedErrorText(t *testing.T) {
	t.Parallel()
	for _, mode := range []clioutput.RenderMode{clioutput.RenderModeEnvelope, clioutput.RenderModeCompact} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			t.Parallel()
			destination := &humanFailureWriter{failure: "error"}
			var diagnostic bytes.Buffer
			outlog, errlog := clioutput.BuildBaseLoggers(destination, &diagnostic, clog.ColorNever)
			c := &clioutput.CommandContext{Mode: mode, Stdout: destination, StdoutLog: outlog, StderrLog: errlog}
			err := c.WriteResult("message.send", map[string]string{"result": "accepted"})
			e, ok := errors.AsType[clioutput.CommandError](err)
			if !ok || e.CLIError.ExitCode != 8 || destination.calls != 1 {
				t.Fatal("machine result lost write failure")
			}
			if strings.Contains(fmt.Sprintf("%v %#v %s", err, err, &diagnostic), "synthetic-private-writer") {
				t.Fatal("writer diagnostics exposed private content")
			}
		})
	}
}

type humanFailureWriter struct {
	failure string
	calls   int
}

func (w *humanFailureWriter) Write(p []byte) (int, error) {
	w.calls++
	switch w.failure {
	case "short":
		return len(p) - 1, nil
	case "partial":
		return len(p) / 2, errors.New("synthetic-private-writer")
	default:
		return 0, errors.New("synthetic-private-writer")
	}
}

var _ io.Writer = (*humanFailureWriter)(nil)

func TestHumanRenderingStopsAfterFailureAndKeepsPriorOutput(t *testing.T) {
	t.Parallel()
	destination := &humanSequenceWriter{}
	var diagnostic bytes.Buffer
	outlog, errlog := clioutput.BuildBaseLoggers(destination, &diagnostic, clog.ColorNever)
	outlog = outlog.With().Str("context", "retained").Logger()
	c := &clioutput.CommandContext{Mode: clioutput.RenderModePlain, Stdout: destination, StdoutLog: outlog, StderrLog: errlog}
	err := c.WriteResult("synthetic", humanSequenceResult{})
	e, ok := errors.AsType[clioutput.CommandError](err)
	if !ok || e.CLIError.ExitCode != 8 || destination.calls != 2 || !strings.Contains(destination.accepted.String(), "First result") || !strings.Contains(destination.accepted.String(), "context=retained") || strings.Contains(destination.accepted.String(), "Third result") {
		t.Fatal("renderer replayed after failure, lost prior output, or discarded logger fields")
	}
	outlog.Info().Msg("Original logger remains usable")
	if destination.calls != 3 || !strings.Contains(destination.accepted.String(), "Original logger remains usable") {
		t.Fatal("rendering replaced the caller's logger output")
	}
}

type humanSequenceResult struct{}

func (humanSequenceResult) WritePlain(c *clioutput.CommandContext, _ string, _ *clioutput.Pagination) error {
	c.StdoutLogger().Info().Msg("First result")
	c.StdoutLogger().Info().Msg("Second result")
	c.StdoutLogger().Info().Msg("Third result")
	return nil
}

type humanSequenceWriter struct {
	calls    int
	accepted bytes.Buffer
}

func (w *humanSequenceWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls == 2 {
		return 0, io.ErrClosedPipe
	}
	return w.accepted.Write(p)
}

func TestHumanResultKeepsColorAndLoggerSettings(t *testing.T) {
	t.Parallel()
	for _, mode := range []clog.ColorMode{clog.ColorNever, clog.ColorAlways, clog.ColorAuto} {
		for _, tty := range []bool{false, true} {
			t.Run(fmt.Sprintf("%v/tty=%t", mode, tty), func(t *testing.T) {
				t.Parallel()
				var out, diagnostic bytes.Buffer
				loggerMode := mode
				if mode == clog.ColorAuto {
					// Model terminal detection independently of the process's
					// NO_COLOR and CLICOLOR_FORCE environment settings.
					loggerMode = clog.ColorNever
					if tty {
						loggerMode = clog.ColorAlways
					}
				}
				outlog, errlog := clioutput.BuildBaseLoggers(&out, &diagnostic, loggerMode)
				outlog.SetOmitZero(false)
				outlog = outlog.With().Str("context", "retained").Logger()
				c := &clioutput.CommandContext{Mode: clioutput.RenderModePlain, Stdout: &out, StdoutLog: outlog, StderrLog: errlog, ColorMode: mode, IsTTY: tty}
				if err := c.WriteResult("synthetic", healthyHumanResult{}); err != nil {
					t.Fatal(err)
				}
				colored := strings.Contains(out.String(), "\x1b[")
				if colored != (mode == clog.ColorAlways || mode == clog.ColorAuto && tty) {
					t.Fatal("result lost the selected color policy")
				}
				// Strip SGR sequences before checking the visible fields.
				visible := ansi.Strip(out.String())
				for _, want := range []string{"Result ready", "context=retained", "count=0", "state=ready"} {
					if !strings.Contains(visible, want) {
						t.Fatal("result lost logger settings or visible fields")
					}
				}
				if diagnostic.Len() != 0 {
					t.Fatal("healthy result emitted diagnostics")
				}
			})
		}
	}
}

type healthyHumanResult struct{}

func (healthyHumanResult) WritePlain(c *clioutput.CommandContext, _ string, _ *clioutput.Pagination) error {
	c.StdoutLogger().Info().Str("state", "ready").Int("count", 0).Msg("Result ready")
	return nil
}

func TestHumanErrorDestinationFailureReturnsIOWithoutRetryOrStdoutFallback(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"error", "short", "partial"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			destination := &humanFailureWriter{failure: failure}
			var out bytes.Buffer
			outlog, errlog := clioutput.BuildBaseLoggers(&out, destination, clog.ColorNever)
			c := &clioutput.CommandContext{Mode: clioutput.RenderModePlain, Stdout: &out, Stderr: destination, StdoutLog: outlog, StderrLog: errlog, ColorMode: clog.ColorNever}
			err := clioutput.WriteCommandError(c, clioutput.ValidationCLIError("invalid fixture input"))
			e, ok := errors.AsType[clioutput.CommandError](err)
			if !ok || e.CLIError.ExitCode != 8 || e.CLIError.Type != "io" || destination.calls != 1 || out.Len() != 0 {
				t.Fatal("human error output failure lost exit 8, retried, or spilled onto stdout")
			}
			if strings.Contains(fmt.Sprintf("%v %#v", err, err), "synthetic-private-writer") {
				t.Fatal("failed error output leaked writer details")
			}
		})
	}
}

func TestHumanResultRespectsOriginalLoggerColorDetection(t *testing.T) {
	t.Parallel()
	var out, diagnostic bytes.Buffer
	// Keep the original logger's decision deterministic, even if the host
	// environment asks for forced colors on nonterminal writers.
	outlog, errlog := clioutput.BuildBaseLoggers(&out, &diagnostic, clog.ColorNever)
	c := &clioutput.CommandContext{Mode: clioutput.RenderModePlain, Stdout: &out, StdoutLog: outlog, StderrLog: errlog, ColorMode: clog.ColorAuto, IsTTY: true}
	if err := c.WriteResult("synthetic", healthyHumanResult{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Fatal("result ignored the original logger's nonterminal color decision")
	}
	if !strings.Contains(out.String(), "Result ready") || diagnostic.Len() != 0 {
		t.Fatal("healthy result lost its output")
	}
}
