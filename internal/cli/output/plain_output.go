package output

import (
	"io"
	"strconv"

	"github.com/gechr/clog"
)

// Clog events do not return destination errors. Capture their first failure
// around each result renderer without replacing Stdout: tables still need the
// original terminal file for their width calculation. Use a child logger so
// field styles, color, and the caller's logger configuration remain intact.
func (c *CommandContext) withPlainOutput(render func(*CommandContext) error) error {
	destination := &plainResultWriter{dst: c.OutWriter()}
	child := *c
	child.StdoutLog = c.StdoutLogger().With().Logger()
	mode := c.plainColorMode(c.StdoutLogger().Output())
	child.StdoutLog.SetOutput(clog.NewOutput(destination, mode))
	if err := render(&child); err != nil {
		return err
	}
	if destination.err != nil {
		return c.outputFailure()
	}
	return nil
}

type plainResultWriter struct {
	dst io.Writer
	err error
}

func (w *plainResultWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	n, err := w.dst.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.err = err
	}
	return n, err
}

func (c *CommandContext) writePlainError(cliErr CLIError) int {
	output := c.StderrLogger().Output()
	destination := &plainResultWriter{dst: output.Writer()}
	logger := c.StderrLogger().With().Logger()
	mode := c.plainColorMode(output)
	logger.SetOutput(clog.NewOutput(destination, mode))
	event := logger.Error().Str("type", cliErr.Type).Int("exit_code", cliErr.ExitCode)
	if cliErr.RetryAfterSeconds != nil {
		event = event.Str("retry_after_seconds", strconv.Itoa(*cliErr.RetryAfterSeconds))
	}
	event = AddCLIErrorDetails(event, cliErr.Details)
	event.Msg(cliErr.Message)
	if destination.err != nil {
		// The diagnostic destination itself failed. Do not retry it or place a
		// human error on stdout; the process exit remains available to the caller.
		return ExitCodeIO
	}
	return cliErr.ExitCode
}

// Fd preserves terminal detection and dimensions through the write observer.
// Non-file writers use the same invalid descriptor as Clog's own Output.
func (w *plainResultWriter) Fd() uintptr {
	if file, ok := w.dst.(interface{ Fd() uintptr }); ok {
		return file.Fd()
	}
	return ^uintptr(0)
}

// Retain the original no-color decision. Real terminals keep automatic color
// depth detection through Fd; explicitly colored nonterminal loggers stay colored.
func (c *CommandContext) plainColorMode(output *clog.Output) clog.ColorMode {
	if output.ColorsDisabled() {
		return clog.ColorNever
	}
	if c.ColorMode == clog.ColorAuto && !output.IsTTY() {
		return clog.ColorAlways
	}
	return c.ColorMode
}

// WriteToStdout captures failures from renderers whose callbacks cannot return
// errors. It preserves terminal identity and stops after the first failure.
func (c *CommandContext) WriteToStdout(render func(io.Writer)) error {
	destination := &plainResultWriter{dst: c.OutWriter()}
	render(destination)
	if destination.err != nil {
		return c.outputFailure()
	}
	return nil
}
