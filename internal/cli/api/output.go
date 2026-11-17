package api

import (
	"encoding/json"
	"fmt"

	"github.com/matcra587/slack-cli/internal/cli/output"
	"github.com/matcra587/slack-cli/internal/slackcatalog"
)

// MethodData describes one published method and its execution policy.
type MethodData struct {
	Method slackcatalog.Method `json:"method"`
}

// MethodsData lists the catalog alongside its source provenance.
type MethodsData struct {
	Methods    []slackcatalog.Method `json:"methods"`
	Provenance slackcatalog.Snapshot `json:"provenance"`
}

// CallData preserves native Slack responses without inventing resource schemas.
type CallData struct {
	Method         string            `json:"method"`
	Response       json.RawMessage   `json:"response,omitempty"`
	Pages          []json.RawMessage `json:"pages,omitempty"`
	DryRun         bool              `json:"dry_run"`
	Parameters     map[string]any    `json:"parameters,omitempty"`
	OutputFile     string            `json:"output_file,omitempty"`
	CompletedPages int               `json:"completed_pages,omitempty"`
	Complete       *bool             `json:"complete,omitempty"`
	NextCursor     string            `json:"next_cursor,omitempty"`
	NextPage       int               `json:"next_page,omitempty"`
}

func (d MethodsData) WritePlain(ctx *output.CommandContext, _ string, _ *output.Pagination) error {
	for _, method := range d.Methods {
		policy := "mutation"
		if method.ReadOnly {
			policy = "read"
		}
		if reason := unsupported(method); reason != "" {
			policy = "unavailable: " + reason
		}
		if _, err := fmt.Fprintf(ctx.OutWriter(), "%s\t%s\t%s\n", method.Name, policy, method.Description); err != nil {
			return err
		}
	}
	return nil
}

func (d MethodData) WritePlain(ctx *output.CommandContext, _ string, _ *output.Pagination) error {
	encoder := json.NewEncoder(ctx.OutWriter())
	encoder.SetIndent("", "  ")
	return encoder.Encode(d.Method)
}

func (d CallData) WritePlain(ctx *output.CommandContext, _ string, _ *output.Pagination) error {
	if d.OutputFile != "" {
		_, err := fmt.Fprintln(ctx.OutWriter(), "Saved response to", d.OutputFile)
		return err
	}
	if d.DryRun {
		_, err := fmt.Fprintf(ctx.OutWriter(), "Dry run: %s\n", d.Method)
		if err != nil {
			return err
		}
		return json.NewEncoder(ctx.OutWriter()).Encode(d.Parameters)
	}
	if d.Pages != nil {
		return json.NewEncoder(ctx.OutWriter()).Encode(d)
	}
	_, err := fmt.Fprintln(ctx.OutWriter(), string(d.Response))
	return err
}
