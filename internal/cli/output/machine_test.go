package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/gechr/clog"
)

func TestMachineOutputPreservesTextWithForcedColor(t *testing.T) {
	for _, mode := range []RenderMode{RenderModeEnvelope, RenderModeCompact} {
		t.Run(map[RenderMode]string{RenderModeEnvelope: "json", RenderModeCompact: "compact"}[mode], func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			sl, el := BuildBaseLoggers(&stdout, &stderr, clog.ColorAlways)
			ctx := &CommandContext{Mode: mode, Stdout: &stdout, Stderr: &stderr, StdoutLog: sl, StderrLog: el}
			text := "<@U123> & `go test`\n\x1b[31m"
			if err := ctx.WriteResult("message.send", map[string]string{"text": text}); err != nil {
				t.Fatal(err)
			}
			var value map[string]json.RawMessage
			if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
				t.Fatalf("machine output is not JSON: %v: %q", err, stdout.String())
			}
			if mode == RenderModeEnvelope {
				if err := json.Unmarshal(value["data"], &value); err != nil {
					t.Fatal(err)
				}
			}
			var got string
			if err := json.Unmarshal(value["text"], &got); err != nil || got != text {
				t.Fatalf("text = %q, error = %v, want %q", got, err, text)
			}
			if strings.Contains(stdout.String(), `\u003c`) || stderr.Len() != 0 {
				t.Fatalf("escaped HTML or unexpected diagnostics: stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestMachineOutputPropagatesDestinationFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "failed write", err: io.ErrClosedPipe},
		{name: "short write"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writer := &failingWriter{err: tc.err}
			var stderr bytes.Buffer
			sl, el := BuildBaseLoggers(writer, &stderr, clog.ColorNever)
			ctx := &CommandContext{Mode: RenderModeEnvelope, Stdout: writer, Stderr: &stderr, StdoutLog: sl, StderrLog: el}
			err := ctx.WriteResult("message.send", map[string]bool{"sent": true})
			var commandErr CommandError
			if !errors.As(err, &commandErr) || commandErr.CLIError.ExitCode != ExitCodeIO {
				t.Fatalf("error = %v, want output failure exit 8", err)
			}
			if writer.calls != 1 || !strings.Contains(stderr.String(), "output_write_failed") {
				t.Fatalf("writes=%d stderr=%q; want one attempt and diagnostic", writer.calls, stderr.String())
			}
		})
	}
}

func TestMachineSerializationFailureWritesNoPartialResult(t *testing.T) {
	var stdout, stderr bytes.Buffer
	sl, el := BuildBaseLoggers(&stdout, &stderr, clog.ColorNever)
	ctx := &CommandContext{Mode: RenderModeCompact, Stdout: &stdout, Stderr: &stderr, StdoutLog: sl, StderrLog: el}
	if err := ctx.WriteResult("test", make(chan int)); err == nil || stdout.Len() != 0 {
		t.Fatalf("error=%v stdout=%q; want failure before writing", err, stdout.String())
	}
}

func TestMachineFailuresUseCompleteEnvelopeOnStdout(t *testing.T) {
	for _, mode := range []RenderMode{RenderModeEnvelope, RenderModeCompact} {
		var stdout, stderr bytes.Buffer
		sl, el := BuildBaseLoggers(&stdout, &stderr, clog.ColorAlways)
		ctx := &CommandContext{Command: "message.send", Workspace: "test", Mode: mode, Stdout: &stdout, Stderr: &stderr, StdoutLog: sl, StderrLog: el}
		err := WriteCommandError(ctx, ValidationCLIError("invalid input"))
		var commandErr CommandError
		if !errors.As(err, &commandErr) || commandErr.CLIError.ExitCode != ExitCodeValidation {
			t.Fatalf("mode=%v error=%v; want validation exit", mode, err)
		}
		var envelope Envelope
		if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.OK || envelope.Data != nil || len(envelope.Errors) != 1 || envelope.Errors[0].Message != "invalid input" || envelope.Meta.Command != "message.send" || envelope.Meta.Workspace != "test" || envelope.Meta.ExitCode != ExitCodeValidation || envelope.Meta.Timestamp == "" || stderr.Len() != 0 {
			t.Fatalf("mode=%v stdout=%s stderr=%s; want full failure envelope and clean stderr", mode, &stdout, &stderr)
		}
	}
}

func TestMachineFailureDestinationFailureOverridesOriginalExit(t *testing.T) {
	writer := &failingWriter{err: io.ErrClosedPipe}
	ctx := &CommandContext{Mode: RenderModeEnvelope, Stdout: writer}
	var commandErr CommandError
	err := WriteCommandError(ctx, ValidationCLIError("invalid input"))
	if !errors.As(err, &commandErr) || commandErr.CLIError.ExitCode != ExitCodeIO || writer.calls != 1 {
		t.Fatalf("error=%v writes=%d; want exit 8 after one attempt", err, writer.calls)
	}
}

type failingWriter struct {
	err   error
	calls int
}

func (w *failingWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.err != nil {
		return 0, w.err
	}
	return len(p) - 1, nil
}
