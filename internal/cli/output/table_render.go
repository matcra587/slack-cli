package output

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/gechr/primer/table"
)

// renderTable keeps terminal padding protected from hard-tab optimization.
// Primer supplies cell rendering and column widths; slick owns the terminal
// spacing now that Primer no longer exposes its TTY padding option.
func renderTable[T any](c *CommandContext, columns []table.Column[T], items []T) string {
	ctx := c.tableContext()
	rendered := table.NewRenderer(columns, ctx, table.WithTermWidth(c.tableWidth())).Render(items)
	if !c.IsTTY || len(rendered.Rows) == 0 {
		return rendered.String()
	}
	header := make([]string, len(columns))
	for i, column := range columns {
		header[i] = ctx.Theme.RenderBold(column.Header)
	}
	lines := make([]string, 0, len(rendered.Rows)+1)
	lines = append(lines, terminalTableRow(header, rendered.ColWidths))
	for _, row := range rendered.Rows {
		cells := make([]string, len(row.Cells))
		for i, cell := range row.Cells {
			cells[i] = cell.Text
		}
		lines = append(lines, terminalTableRow(cells, rendered.ColWidths))
	}
	return strings.Join(lines, "\n")
}

func terminalTableRow(cells []string, widths []int) string {
	var line strings.Builder
	for i, cell := range cells {
		if i > 0 {
			line.WriteString("\x1b[8m  \x1b[28m")
		}
		cell = ansi.WcWidth.Truncate(cell, widths[i], "…")
		line.WriteString(cell)
		if padding := widths[i] - ansi.WcWidth.StringWidth(cell); i < len(cells)-1 && padding > 0 {
			line.WriteString("\x1b[8m")
			line.WriteString(strings.Repeat(" ", padding))
			line.WriteString("\x1b[28m")
		}
	}
	return line.String()
}
