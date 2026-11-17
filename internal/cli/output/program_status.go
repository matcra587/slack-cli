package output

import (
	"context"
	"errors"

	"github.com/gechr/clog"
)

// ReportProgramStatus updates the terminal's status area for human sessions.
// Both stdout's human-terminal policy and stderr's real TTY detection apply;
// forced colors never enable reports in pipes or machine output modes.
// Callers supply fixed descriptions rather than Slack or credential data.
func (c *CommandContext) ReportProgramStatus(state clog.ProgramState, kind clog.ProgramStatusKind, message string) {
	c.reportProgramStatus(clog.ProgramStatus{State: state, Kind: kind, Message: message})
}

// ReportProgramProgress reports completed items in a batch with a known size.
// Unknown totals use a working report without a percentage.
func (c *CommandContext) ReportProgramProgress(message string, completed, total int) {
	status := clog.ProgramStatus{State: clog.ProgramStateWorking, Message: message}
	if total > 0 {
		status.HasProgress = true
		status.Progress = int(float64(max(0, min(completed, total))) * 100 / float64(total))
	}
	c.reportProgramStatus(status)
}

func (c *CommandContext) reportProgramStatus(status clog.ProgramStatus) {
	if c.Mode != RenderModePlain || !c.IsTTY || c.StderrLog == nil {
		return
	}
	status.App, status.Title = "slick", c.Command
	c.StderrLog.SetProgramStatus(status)
}

// FinishProgramStatus resolves a started report after the complete command,
// including result writing. Cancellation returns the terminal to idle.
func (c *CommandContext) FinishProgramStatus(err error) {
	state, message := clog.ProgramStateDone, "Completed"
	if err != nil {
		state, message = clog.ProgramStateError, "Failed"
		failure, classified := errors.AsType[CommandError](err)
		if errors.Is(err, context.Canceled) || classified && failure.CLIError.ExitCode == ExitCodeCanceled {
			state, message = clog.ProgramStateIdle, "Canceled"
		}
	}
	c.ReportProgramStatus(state, "", message)
}
