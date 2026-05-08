package main

import (
	"github.com/matcra587/slack-cli/internal/version"
	"github.com/spf13/cobra"
)

type versionData struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Branch    string `json:"branch"`
	BuildTime string `json:"build_time"`
	BuildBy   string `json:"build_by"`
}

func newVersionCommand(runtime *RootRuntime) *cobra.Command {
	return &cobra.Command{
		Use:          "version",
		Short:        "Print version information",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := localVersionContext(cmd, runtime)
			return ctx.WriteResult("version", versionData{
				Version:   version.Version,
				Commit:    version.Commit,
				Branch:    version.Branch,
				BuildTime: version.BuildTime,
				BuildBy:   version.BuildBy,
			})
		},
	}
}

func localVersionContext(cmd *cobra.Command, runtime *RootRuntime) *CommandContext {
	opts := rootOptionsFromCommand(cmd, runtime)
	mode := opts.Output.Resolve(runtime.IsTTY, DetectAgentOutputMode(opts.Agent))
	sl, el := buildBaseLoggers(runtime.Stdout, runtime.Stderr, runtime.ColorMode)
	applyRenderMode(sl, mode)
	return &CommandContext{
		Workspace: "version",
		Mode:      mode,
		Stdout:    runtime.Stdout,
		Stderr:    runtime.Stderr,
		Now:       runtime.Now,
		RequestID: runtime.RequestID,
		stdoutLog: sl,
		stderrLog: el,
	}
}
