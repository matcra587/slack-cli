package output_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/gechr/clog"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
)

func TestRateLimitDelayIsPreservedAcrossOutputModes(t *testing.T) {
	t.Parallel()
	zero, seven := 0, 7
	for _, mode := range []clioutput.RenderMode{clioutput.RenderModePlain, clioutput.RenderModeEnvelope, clioutput.RenderModeCompact} {
		for _, tc := range []struct {
			name  string
			delay *int
		}{{"unknown", nil}, {"zero", &zero}, {"positive", &seven}} {
			t.Run(fmt.Sprint(mode)+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				var out, diagnostic bytes.Buffer
				outlog, errlog := clioutput.BuildBaseLoggers(&out, &diagnostic, clog.ColorNever)
				c := &clioutput.CommandContext{Command: "synthetic.rate", Mode: mode, Stdout: &out, Stderr: &diagnostic, StdoutLog: outlog, StderrLog: errlog, ColorMode: clog.ColorNever}
				if code := c.WriteError(clioutput.CLIError{Type: "rate_limit", Message: "ratelimited", ExitCode: 3, RetryAfterSeconds: tc.delay}); code != 3 {
					t.Fatal("rate error changed exit code")
				}
				if mode == clioutput.RenderModePlain {
					if out.Len() != 0 || !strings.Contains(diagnostic.String(), "ratelimited") {
						t.Fatal("human rate error used wrong stream")
					}
					if tc.delay == nil {
						if strings.Contains(diagnostic.String(), "retry_after_seconds") {
							t.Fatal("unknown delay was invented")
						}
					} else if !strings.Contains(diagnostic.String(), "retry_after_seconds="+strconv.Itoa(*tc.delay)) {
						t.Fatal("human rate-limit delay was lost")
					}
				} else {
					var frame struct {
						OK     bool
						Data   json.RawMessage
						Errors []struct {
							Delay *int `json:"retry_after_seconds"`
						}
						Meta struct {
							Command string
							Exit    int `json:"exit_code"`
						}
					}
					if json.Unmarshal(out.Bytes(), &frame) != nil || frame.OK || string(frame.Data) != "null" || frame.Meta.Command != "synthetic.rate" || frame.Meta.Exit != 3 || len(frame.Errors) != 1 || diagnostic.Len() != 0 {
						t.Fatal("machine rate error lost common frame")
					}
					got := frame.Errors[0].Delay
					if (got == nil) != (tc.delay == nil) || (got != nil && *got != *tc.delay) {
						t.Fatal("machine rate-limit delay changed")
					}
				}
			})
		}
	}
}

func TestHumanRateLimitDiagnosticFailureDoesNotRetryOrUseStdout(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"error", "short", "partial"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			destination := &humanFailureWriter{failure: failure}
			outlog, errlog := clioutput.BuildBaseLoggers(&out, destination, clog.ColorNever)
			c := &clioutput.CommandContext{Mode: clioutput.RenderModePlain, Stdout: &out, Stderr: destination, StdoutLog: outlog, StderrLog: errlog, ColorMode: clog.ColorNever}
			delay := 7
			if code := c.WriteError(clioutput.CLIError{Type: "rate_limit", Message: "ratelimited", ExitCode: 3, RetryAfterSeconds: &delay}); code != 8 || destination.calls != 1 || out.Len() != 0 {
				t.Fatal("failed rate diagnostic was retried, used stdout, or lost IO exit")
			}
		})
	}
}
