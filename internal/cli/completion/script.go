package completion

import (
	"bytes"

	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	"github.com/spf13/cobra"
)

// Generate before writing so unsupported shells cannot leave partial output.
// Script output uses the same protected destination as dynamic suggestions.
func printScript(root *cobra.Command, runtime *cliruntime.RootRuntime, shell string) error {
	if err := validateCompletionShell(shell); err != nil {
		return err
	}
	var script bytes.Buffer
	if err := Generator(root).Print(&script, shell); err != nil {
		return err
	}
	output := &completionOutput{runtime: runtime}
	output.reset()
	return output.command.WriteString(script.String())
}
