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
			ctx, _, err := commandContextFromRuntime(cmd, runtime)
			if err != nil {
				return writeRuntimeError(runtime, clioutput.ValidationCLIError(err.Error()))
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

// commandContextFromRuntime builds a minimal CommandContext for the workspace command.
// collapse when cmd/slick/main.go's commandContext moves.
func commandContextFromRuntime(cmd *cobra.Command, runtime *cliruntime.RootRuntime) (*clioutput.CommandContext, config.WorkspaceProfile, error) {
	flags := cmd.Root().PersistentFlags()
	workspace, _ := flags.GetString("workspace")
	jsonMode, _ := flags.GetBool("json")
	plain, _ := flags.GetBool("plain")
	compact, _ := flags.GetBool("compact")
	raw, _ := flags.GetBool("raw")
	outputFlags := clioutput.OutputFlags{JSON: jsonMode, Plain: plain, Compact: compact, Raw: raw}
	mode := outputFlags.Resolve(runtime.IsTTY, false)

	sl, el := clioutput.BuildBaseLoggers(runtime.Stdout, runtime.Stderr, runtime.ColorMode)
	clioutput.ApplyRenderMode(sl, mode)

	if runtime.ConfigLoadError != nil {
		ctx := &clioutput.CommandContext{
			Workspace:     "default",
			Mode:          mode,
			Stdout:        runtime.Stdout,
			Stderr:        runtime.Stderr,
			NowFunc:       runtime.Now,
			RequestIDFunc: runtime.RequestID,
			Theme:         runtime.Theme,
			StdoutLog:     sl,
			StderrLog:     el,
		}
		return ctx, config.WorkspaceProfile{}, runtime.ConfigLoadError
	}

	wsName := "default"
	var profile config.WorkspaceProfile
	if runtime.Config != nil {
		var err error
		profile, err = runtime.Config.ResolveWorkspace(workspace)
		if err != nil {
			return nil, config.WorkspaceProfile{}, err
		}
		wsName = profile.Name
	} else if workspace != "" {
		wsName = workspace
	}

	ctx := &clioutput.CommandContext{
		Workspace:     wsName,
		Mode:          mode,
		Stdout:        runtime.Stdout,
		Stderr:        runtime.Stderr,
		NowFunc:       runtime.Now,
		RequestIDFunc: runtime.RequestID,
		IsTTY:         runtime.IsTTY,
		ColorMode:     runtime.ColorMode,
		Theme:         runtime.Theme,
		StdoutLog:     sl,
		StderrLog:     el,
	}
	return ctx, profile, nil
}

func writeRuntimeError(runtime *cliruntime.RootRuntime, err clioutput.CLIError) error {
	mode := clioutput.OutputFlags{}.Resolve(runtime.IsTTY, false)
	sl, el := clioutput.BuildBaseLoggers(runtime.Stdout, runtime.Stderr, runtime.ColorMode)
	clioutput.ApplyRenderMode(sl, mode)
	ctx := &clioutput.CommandContext{
		Workspace:     "default",
		Mode:          mode,
		Stdout:        runtime.Stdout,
		Stderr:        runtime.Stderr,
		NowFunc:       runtime.Now,
		RequestIDFunc: runtime.RequestID,
		StdoutLog:     sl,
		StderrLog:     el,
	}
	return clioutput.WriteCommandError(ctx, err)
}
