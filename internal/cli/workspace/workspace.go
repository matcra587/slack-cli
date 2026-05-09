package workspace

import (
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/spf13/cobra"
)

type ListData struct {
	Workspaces []config.WorkspaceProfile `json:"workspaces"`
}

var _ clioutput.PlainRenderer = ListData{}

func (d ListData) WritePlain(c *clioutput.CommandContext, command string, pagination *clioutput.Pagination) error {
	return c.WriteWorkspaces(command, d.Workspaces, pagination)
}

// NewCommand returns the workspace cobra command tree.
func NewCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	workspaceCmd := &cobra.Command{Use: "workspace", Short: "Manage workspace profiles"}
	listCmd := &cobra.Command{
		Use:          "list",
		Short:        "List configured workspaces",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, _, _, err := cliruntime.CommandContext(cmd, runtime)
			if err != nil {
				return cliruntime.WriteRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
			}
			workspaces := make([]config.WorkspaceProfile, 0)
			if runtime.Config != nil {
				for _, profile := range runtime.Config.Workspaces {
					workspaces = append(workspaces, profile)
				}
			}
			return ctx.WriteResult("workspace.list", ListData{Workspaces: workspaces})
		},
	}
	workspaceCmd.AddCommand(listCmd)
	return workspaceCmd
}
