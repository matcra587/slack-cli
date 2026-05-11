package main

import (
	"time"

	"github.com/gechr/x/human"
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
	logger := c.StdoutLogger()
	logger.Info().Msg("slick " + d.Version)
	sub := logger.With().Indent().Logger()
	sub.Info().Str("commit", d.Commit).Send()
	sub.Info().Str("branch", d.Branch).Send()
	sub.Info().Str("built", humanBuildTime(d.BuildTime)).Send()
	sub.Info().Str("built by", d.BuildBy).Send()
	return nil
}

// humanBuildTime renders an RFC3339 build timestamp as a relative phrase
// (e.g. "3 days ago"). The JSON envelope keeps the precise timestamp;
// plain mode is for humans. Falls back to the raw string when the value
// can't be parsed (e.g. "unknown" for non-release builds).
func humanBuildTime(buildTime string) string {
	parsed, err := time.Parse(time.RFC3339, buildTime)
	if err != nil {
		return buildTime
	}
	return human.FormatTimeAgo(parsed)
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
