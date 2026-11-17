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
		if err := ctx.WriteString(fmt.Sprintf("%s\t%s\t%s\n", method.Name, policy, method.Description)); err != nil {
			return err
		}
	}
	return nil
}

func (d MethodData) WritePlain(ctx *output.CommandContext, _ string, _ *output.Pagination) error {
	body, err := json.MarshalIndent(d.Method, "", "  ")
	if err != nil {
		return err
	}
	return ctx.WriteString(string(body))
}

func (d CallData) WritePlain(ctx *output.CommandContext, _ string, _ *output.Pagination) error {
	if d.OutputFile != "" {
		return ctx.WriteString("Saved response to " + d.OutputFile)
	}
	var value any
	if d.DryRun {
		if err := ctx.WriteString("Dry run: " + d.Method); err != nil {
			return err
		}
		value = d.Parameters
	} else if d.Pages != nil {
		value = d
	} else {
		return ctx.WriteString(string(d.Response))
	}
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return ctx.WriteString(string(body))
}
