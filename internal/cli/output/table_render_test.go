package output

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/gechr/clog"
	"github.com/gechr/primer/table"
)

func TestRenderTablePreservesTerminalPadding(t *testing.T) {
	t.Parallel()
	columns := []table.Column[string]{
		{Name: "name", Header: "NAME", Render: func(s string, _ *table.RenderContext) table.Cell { return table.TextCell(s) }},
		{Name: "value", Header: "VALUE", Render: func(_ string, _ *table.RenderContext) table.Cell { return table.TextCell("a b") }},
	}
	c := &CommandContext{ColorMode: clog.ColorNever}
	items := []string{"界", "longer"}
	plain := renderTable(c, columns, items)
	if want := "NAME    VALUE\n界      a b\nlonger  a b"; plain != want {
		t.Fatalf("plain table = %q, want %q", plain, want)
	}
	c.IsTTY = true
	terminal := renderTable(c, columns, items)
	if ansi.Strip(terminal) != plain {
		t.Fatalf("terminal table changed visible text: %q", terminal)
	}
	if !strings.Contains(terminal, "界\x1b[8m    \x1b[28m\x1b[8m  \x1b[28ma b") {
		t.Fatalf("terminal padding is not protected: %q", terminal)
	}
	if got := renderTable(c, columns, nil); got != "" {
		t.Fatalf("empty table = %q", got)
	}
}

func TestTerminalTableRowPreservesStylesAndTruncation(t *testing.T) {
	t.Parallel()
	link := "\x1b]8;;https://example.invalid\x1b\\界abcdef\x1b]8;;\x1b\\"
	for _, first := range []string{"abcdef", "界abcdef", "e\u0301abcdef", "\x1b[31mabcdef\x1b[0m", link} {
		t.Run(first, func(t *testing.T) {
			cells := []string{first, "x"}
			grid := table.NewGrid([][]string{cells})
			grid.FlexCols = []int{0}
			grid.MaxWidth = 7
			plain, widths := grid.AlignColumns()
			got := terminalTableRow([]string{first, "x"}, widths)
			unprotected := strings.NewReplacer("\x1b[8m", "", "\x1b[28m", "").Replace(got)
			if unprotected != plain[0] {
				t.Fatalf("terminal row = %q, want same text and styles as %q", got, plain[0])
			}
			if ansi.WcWidth.StringWidth(got) != 7 || !strings.Contains(got, "…") {
				t.Fatalf("terminal row exceeds width or lacks truncation: %q", got)
			}
		})
	}
}
