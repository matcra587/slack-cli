package completion

import (
	"errors"
	"strings"

	"github.com/gechr/x/shell"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	"github.com/spf13/cobra"
)

type completionAction struct {
	Action      string `json:"action"`
	Shell       string `json:"shell"`
	Path        string `json:"path"`
	Changed     bool   `json:"changed"`
	DryRun      bool   `json:"dry_run"`
	WouldChange bool   `json:"would_change"`
}

var _ clioutput.PlainRenderer = completionAction{}

func (d completionAction) WritePlain(c *clioutput.CommandContext, command string, _ *clioutput.Pagination) error {
	message := "Completion removed"
	switch {
	case d.DryRun && d.WouldChange:
		message = "Would " + d.Action + " completion"
	case d.DryRun:
		message = "No completion change"
	case d.Action == "install":
		message = "Completion installed"
	case !d.Changed:
		message = "No completion found"
	}
	event := c.ResultEvent(command).Str("path", d.Path).Str("action", d.Action).Str("shell", d.Shell).Bool("changed", d.Changed)
	if d.DryRun {
		event = event.Bool("dry_run", true).Bool("would_change", d.WouldChange)
	}
	event.Msg(message)
	return nil
}

func writeCompletionAction(cmd *cobra.Command, runtime *cliruntime.RootRuntime, data completionAction) error {
	workspace := strings.TrimSpace(runtime.WorkspaceName)
	if runtime.Config != nil {
		if profile, err := runtime.Config.ResolveWorkspace(workspace); err == nil {
			workspace = profile.Name
		}
	}
	if workspace == "" {
		workspace = "default"
	}
	// Reading local flags must not seed the original command's flag-order cache.
	view := *cmd
	output := cliruntime.LocalContext(&view, runtime, workspace)
	return output.WriteResult("slick", data)
}

func completionFailure(runtime *cliruntime.RootRuntime, action, path string, mayHaveChanged bool) error {
	failure := clioutput.RuntimeCLIError("completion_" + action + "_failed").WithDetails(map[string]any{"path": path, "may_have_changed": mayHaveChanged})
	return cliruntime.WriteRuntimeError(runtime, failure)
}

// Do not echo unreviewed shell arguments into diagnostics.
func validateCompletionShell(sh string) error {
	switch sh {
	case "", shell.Bash, shell.Zsh, shell.Fish, shell.Nu, shell.Pwsh, shell.Elvish:
		return nil
	default:
		return errors.New("unsupported shell (supported: bash, elvish, fish, nu, pwsh, zsh)")
	}
}
