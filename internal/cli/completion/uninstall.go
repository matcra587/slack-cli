package completion

import (
	"os"

	"github.com/gechr/x/shell"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	"github.com/spf13/cobra"
)

func uninstallScript(cmd *cobra.Command, runtime *cliruntime.RootRuntime, sh string) error {
	if sh == "" {
		sh = shell.Fish
	}
	if err := cmd.Context().Err(); err != nil {
		return cliruntime.WriteRuntimeError(runtime, clioutput.CliErrorFromSlack(cmd.Context(), err, ""))
	}
	if err := validateCompletionShell(sh); err != nil {
		return err
	}
	path, err := completionFile(sh)
	if err != nil {
		return err
	}
	data := completionAction{Action: "uninstall", Shell: sh, Path: path, DryRun: cliruntime.DryRun(cmd)}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return writeCompletionAction(cmd, runtime, data)
		}
		return completionFailure(runtime, "uninstall", path, false)
	}
	if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
		return completionFailure(runtime, "uninstall", path, false)
	}
	if data.DryRun {
		data.WouldChange = true
		return writeCompletionAction(cmd, runtime, data)
	}
	if err := cmd.Context().Err(); err != nil {
		return cliruntime.WriteRuntimeError(runtime, clioutput.CliErrorFromSlack(cmd.Context(), err, ""))
	}
	// Remove only the completion entry. A symlink's target belongs to its owner.
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return writeCompletionAction(cmd, runtime, data)
		}
		return completionFailure(runtime, "uninstall", path, true)
	}
	data.Changed = true
	return writeCompletionAction(cmd, runtime, data)
}
