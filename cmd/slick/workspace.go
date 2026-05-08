package main

import (
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/spf13/cobra"
)

type workspaceListData struct {
	Workspaces []config.WorkspaceProfile `json:"workspaces"`
}

func newWorkspaceCommand(runtime *RootRuntime) *cobra.Command {
	workspaceCmd := &cobra.Command{Use: "workspace", Short: "Manage workspace profiles"}
	listCmd := &cobra.Command{
		Use:          "list",
		Short:        "List configured workspaces",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, _, _, err := commandContext(cmd, runtime)
			if err != nil {
				return writeRuntimeError(runtime, validationCLIError(err.Error()))
			}
			workspaces := make([]config.WorkspaceProfile, 0)
			if runtime.Config != nil {
				for _, profile := range runtime.Config.Workspaces {
					workspaces = append(workspaces, profile)
				}
			}
			return ctx.WriteResult("workspace.list", workspaceListData{Workspaces: workspaces})
		},
	}
	workspaceCmd.AddCommand(listCmd)
	return workspaceCmd
}
