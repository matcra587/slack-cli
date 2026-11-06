// Package agent serves docent discovery through slick's shared result writer.
package agent

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/matcra587/docent"
	docentcobra "github.com/matcra587/docent/cobra"
	agentpkg "github.com/matcra587/slack-cli/internal/agent"
	"github.com/matcra587/slack-cli/internal/agentguides"
	"github.com/matcra587/slack-cli/internal/agenthelp"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// SchemaData contains the canonical docent artifact; local refs resolve here.
type SchemaData struct {
	Schema json.RawMessage `json:"schema"`
}

type GuideData struct {
	ContractVersion string `json:"contract_version"`
	Kind            string `json:"kind"`
	Document        string `json:"document"`
}

type ExportData struct {
	Paths []string `json:"paths"`
}

// NewCommand mounts docent's guide/schema/export surface without credentials.
func NewCommand(runtime *cliruntime.RootRuntime) *cobra.Command {
	guides, loadErr := agentguides.Load()
	cmd := docentcobra.NewCommand(docent.Config{Guides: guides, ContractVersion: agenthelp.ContractVersion}, docentcobra.WithSkillNameQualifier("slick"))
	cmd.SilenceUsage = true
	for _, child := range cmd.Commands() {
		if child.Name() == "schema" {
			var compact bool
			child.Flags().BoolVarP(&compact, "compact", "c", false, "Output a smaller routing schema without verbose metadata")
			child.MarkFlagsMutuallyExclusive("compact", "path")
			child.MarkFlagsMutuallyExclusive("compact", "shapes")
			child.RunE = func(c *cobra.Command, _ []string) error {
				ctx := cliruntime.LocalContextForceAgent(c, runtime, "agent")
				if loadErr != nil {
					return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(loadErr.Error()))
				}
				tree := agenthelp.GenerateSchema(c.Root())
				path, _ := c.Flags().GetString("path")
				shapes, _ := c.Flags().GetBool("shapes")
				switch {
				case path != "":
					selected, ok := docent.FindByPath(tree, path)
					if !ok {
						selected, ok = docent.FindByPath(tree, tree.Name+" "+path)
					}
					if !ok {
						return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError("unknown command path: "+path))
					}
					tree = selected.PoolShapes()
				case compact:
					tree = agenthelp.GenerateCompactSchema(c.Root())
				case shapes:
					tree = tree.PoolShapes()
				default:
					tree = tree.StripShapes()
				}
				if tree.Extensions == nil {
					tree.Extensions = map[string]any{}
				}
				tree.Extensions["contract_version"] = agenthelp.ContractVersion
				raw, err := docent.MarshalSchema(tree)
				if err != nil {
					return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
				}
				if ctx.Mode == clioutput.RenderModePlain {
					return ctx.WriteString(string(raw))
				}
				return ctx.WriteResult("agent.schema", SchemaData{Schema: raw})
			}
			configureDocentFlags(child)
			continue
		}
		configureDocentFlags(child)
		if child.Name() == "guide" {
			child.Long += "\n\nAvailable workflows: " + strings.Join(agentguides.Names(), ", ")
		}
		run := child.RunE
		child.RunE = func(c *cobra.Command, args []string) error {
			ctx := cliruntime.LocalContextForceAgent(c, runtime, "agent")
			if loadErr != nil {
				return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(loadErr.Error()))
			}
			var buf bytes.Buffer
			destination := c.OutOrStdout()
			c.SetOut(&buf)
			defer c.SetOut(destination)
			if err := run(c, args); err != nil {
				return clioutput.WriteCommandError(ctx, clioutput.ValidationCLIError(err.Error()))
			}
			if ctx.Mode == clioutput.RenderModePlain {
				return ctx.WriteString(buf.String())
			}
			if c.Name() == "export" {
				paths := []string{}
				for line := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
					if line != "" && !strings.HasPrefix(line, "#") {
						paths = append(paths, line)
					}
				}
				return ctx.WriteResult("agent.export", ExportData{Paths: paths})
			}
			kind := "index"
			if len(args) > 0 {
				kind = "runbook"
			}
			if all, _ := c.Flags().GetBool("all"); all {
				kind = "all"
			}
			if section, _ := c.Flags().GetString("section"); section != "" {
				kind = "section"
			}
			return ctx.WriteResult("agent.guide", GuideData{ContractVersion: agenthelp.ContractVersion, Kind: kind, Document: buf.String()})
		}
	}
	return cmd
}

// DetectAttribution computes the attribution settings (label/emoji/message)
// for the supplied flags. Used by main.go and tests that exercise attribution
// fallthrough.
func DetectAttribution(flags cliruntime.AttributionFlags) agentpkg.Attribution {
	detection := agentpkg.Detect(agentpkg.Options{
		Attribution:        flags.Attribution,
		ProfileAttribution: flags.ProfileAttribution,
		Label:              flags.Label,
		Emoji:              flags.Emoji,
		Message:            flags.Message,
	})
	return agentpkg.NewAttribution(detection, agentpkg.Options{
		Label:   flags.Label,
		Emoji:   flags.Emoji,
		Message: flags.Message,
	})
}

// DetectOutputMode reports whether output should be rendered in agent mode.
// True when an agent/CI env trigger is set; the user can also pick a
// machine-friendly mode explicitly via --output=json|compact.
func DetectOutputMode() bool {
	return agentpkg.Detect(agentpkg.Options{}).Active
}

// Keep docent's values and constraints while adapting its flags to slick's CLI.
func configureDocentFlags(cmd *cobra.Command) {
	short := map[string]string{"all": "a", "section": "s", "path": "p", "shapes": "s", "dir": "d", "format": "f", "harness": "H", "scope": "s"}
	flags := pflag.NewFlagSet(cmd.Name(), pflag.ContinueOnError)
	cmd.Flags().VisitAll(func(flag *pflag.Flag) {
		copy := *flag
		if copy.Shorthand == "" {
			copy.Shorthand = short[copy.Name]
		}
		flags.AddFlag(&copy)
	})
	cmd.ResetFlags()
	cmd.Flags().AddFlagSet(flags)
	cmd.ValidArgsFunction = nil
}
