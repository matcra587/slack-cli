package main

import (
	"strings"

	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
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

var _ PlainRenderer = versionData{}

func (d versionData) WritePlain(c *CommandContext, _ string, _ *Pagination) error {
	var b strings.Builder
	b.WriteString("slick " + d.Version + "\n")
	b.WriteString("  commit:  " + d.Commit + "\n")
	b.WriteString("  branch:  " + d.Branch + "\n")
	b.WriteString("  built:   " + d.BuildTime + "\n")
	b.WriteString("  built by: " + d.BuildBy)
	return c.WriteString(b.String())
}

func newVersionCommand(runtime *RootRuntime) *cobra.Command {
	return &cobra.Command{
		Use:          "version",
		Short:        "Print version information",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cliruntime.LocalContext(cmd, runtime, "version")
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
