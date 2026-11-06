package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// WriteJSON writes one complete machine-readable record without log rendering.
func (c *CommandContext) WriteJSON(value any) error {
	return c.writeJSON(c.OutWriter(), value)
}

func (c *CommandContext) writeJSON(dst io.Writer, value any) error {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return c.outputFailure(fmt.Errorf("encode result: %w", err))
	}
	return c.writeBytes(dst, buf.Bytes())
}

func (c *CommandContext) writeBytes(dst io.Writer, data []byte) error {
	n, err := dst.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return c.outputFailure(err)
	}
	return nil
}

func (c *CommandContext) outputFailure(err error) error {
	cliErr := CLIError{Type: ErrorTypeIO, Message: "output_write_failed: " + err.Error(), ExitCode: ExitCodeIO}
	// A mutation may already have completed. Do not retry the failed destination
	// or emit a second result; diagnostics are best-effort on the other stream.
	if c.StderrLog != nil {
		c.StderrLog.Error().Str("type", ErrorTypeIO).Int("exit_code", ExitCodeIO).Msg(cliErr.Message)
	}
	return CommandError{CLIError: cliErr}
}
