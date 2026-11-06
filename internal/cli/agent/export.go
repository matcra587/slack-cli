package agent

import (
	"errors"
	"fmt"
	"os"
	"strings"

	xfilepath "github.com/gechr/x/filepath"
	"github.com/matcra587/docent"
	docentexport "github.com/matcra587/docent/export"
	"github.com/matcra587/docent/harness"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	"github.com/spf13/cobra"
)

type ExportData struct {
	Paths  []string `json:"paths"`
	DryRun bool     `json:"dry_run,omitempty"`
}

func runExport(cmd *cobra.Command, ctx *clioutput.CommandContext, guides *docent.GuideSet) error {
	format, dir, header, err := exportDestination(cmd)
	if err != nil {
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
	}
	var renderer docentexport.ValidatingRenderer
	switch format {
	case "agent-skill":
		renderer = docentexport.AgentSkill{NameQualifier: "slick"}
	case "claude-skill":
		renderer = docentexport.ClaudeSkill{NameQualifier: "slick"}
	default:
		return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("unsupported export format: "+format))
	}
	paths := []string{}
	for _, guide := range guides.Guides() {
		if err := renderer.Validate(guide); err != nil {
			return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
		}
		paths = append(paths, renderer.RelPath(guide))
	}
	dryRun := cliruntime.DryRun(cmd)
	if dryRun {
		header = "# dry run; no files written\n" + header
	} else {
		paths, err = docentexport.Write(dir, renderer, guides.Guides())
		if err != nil {
			if paths == nil {
				paths = []string{}
			}
			failure := clioutput.RuntimeCLIError(err.Error()).WithDetails(map[string]any{"completed_paths": paths})
			return clioutput.WriteCommandError(ctx, failure)
		}
	}
	if ctx.Mode == clioutput.RenderModePlain {
		return ctx.WriteString(header + strings.Join(paths, "\n") + "\n")
	}
	return ctx.WriteResult("agent.export", ExportData{Paths: paths, DryRun: dryRun})
}

// Keep Docent's flag constraints and harness conventions; resolve explicit paths
// through the same expansion helper as slick's other path inputs.
func exportDestination(cmd *cobra.Command) (format, dir, header string, err error) {
	format, _ = cmd.Flags().GetString("format")
	dir, _ = cmd.Flags().GetString("dir")
	scope, _ := cmd.Flags().GetString("scope")
	name, _ := cmd.Flags().GetString("harness")
	if dir == "" && scope == "" {
		return "", "", "", errors.New("one of --dir or --scope must be non-empty")
	}
	if name != "" && scope == "" {
		return "", "", "", errors.New("--harness is valid only with --scope")
	}
	if dir != "" {
		if format == "" {
			return "", "", "", errors.New("--format is required with --dir")
		}
		dir = xfilepath.Expand(dir)
		if dir == "" {
			return "", "", "", errors.New("--dir expands to an empty path")
		}
		return format, dir, "", nil
	}
	if err = harness.ValidateScope(scope); err != nil {
		return "", "", "", err
	}
	var selected harness.Harness
	source := "selected"
	found := false
	names := []string{}
	for _, candidate := range harness.Supported() {
		names = append(names, candidate.Name)
		if candidate.Name == name {
			selected = candidate
			found = true
		}
	}
	if name == "" {
		selected, found = harness.Detect(os.LookupEnv)
		source = "detected"
	}
	if !found {
		if name == "" {
			return "", "", "", fmt.Errorf("no agent harness detected (supported: %s); pass --harness with --scope, or pass explicit --dir and --format", strings.Join(names, ", "))
		}
		return "", "", "", fmt.Errorf("unsupported harness %q; supported harnesses: %s", name, strings.Join(names, ", "))
	}
	if format == "" {
		format = selected.DefaultFormat
	}
	dir, err = selected.SkillsPath(scope)
	if err != nil {
		return "", "", "", err
	}
	return format, dir, fmt.Sprintf("# %s %s; exporting to %s\n", source, selected.Name, dir), nil
}
