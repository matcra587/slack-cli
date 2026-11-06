package completion

import (
	"os"

	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
)

// completionOutput retains the first write failure across clib's void callback.
// A new callback invocation resets it so explicitly retried operations recover.
type completionOutput struct {
	runtime *cliruntime.RootRuntime
	command *clioutput.CommandContext
	err     error
}

func (o *completionOutput) reset() {
	runtime := o.runtime
	if runtime == nil {
		runtime = &cliruntime.RootRuntime{}
	}
	stdout, stderr := runtime.Stdout, runtime.Stderr
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	outlog, errlog := clioutput.BuildBaseLoggers(stdout, stderr, runtime.ColorMode)
	o.command = &clioutput.CommandContext{Stdout: stdout, Stderr: stderr, StdoutLog: outlog, StderrLog: errlog, ColorMode: runtime.ColorMode}
	o.err = nil
}

func (o *completionOutput) print(shell, value, description string) {
	if o.err != nil || value == "" {
		return
	}
	line := value
	if shell == "fish" && description != "" {
		line += "\t" + description
	}
	o.err = o.command.WriteString(line)
}
