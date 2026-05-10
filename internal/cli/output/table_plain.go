package output

import (
	"image/color"
	"os"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	clibtheme "github.com/gechr/clib/theme"
	"github.com/gechr/clog"
	clogstyle "github.com/gechr/clog/style"
	"github.com/gechr/primer/table"
	termansi "github.com/gechr/x/ansi"
	"github.com/gechr/x/terminal"
	"github.com/matcra587/slack-cli/internal/cli/cliutil"
	"github.com/matcra587/slack-cli/internal/config"
)

// clogDefaults caches clog's authoritative default style config so table
// cells inherit the same FieldTime/FieldNumber colors clog uses for
// in-event rendering.
var clogDefaults = clog.DefaultStyles()

func clogFieldTimeStyle() *lipgloss.Style   { return styleFromConfig(clogDefaults, fieldTime) }
func clogFieldNumberStyle() *lipgloss.Style { return styleFromConfig(clogDefaults, fieldNumber) }

type fieldStyleSelector int

const (
	fieldTime fieldStyleSelector = iota
	fieldNumber
)

func styleFromConfig(cfg *clogstyle.Config, sel fieldStyleSelector) *lipgloss.Style {
	if cfg == nil {
		return nil
	}
	switch sel {
	case fieldTime:
		return cfg.FieldTime
	case fieldNumber:
		return cfg.FieldNumber
	}
	return nil
}

type slackTableTheme struct {
	theme  *clibtheme.Theme
	styled bool
}

func (t slackTableTheme) RenderBold(s string) string {
	if !t.styled || t.theme == nil || t.theme.Bold == nil {
		return s
	}
	return t.theme.Bold.Render(s)
}

func (t slackTableTheme) RenderDim(s string) string {
	if !t.styled || t.theme == nil || t.theme.Dim == nil {
		return s
	}
	return t.theme.Dim.Render(s)
}

func (t slackTableTheme) EntityColors() []color.Color {
	if !t.styled || t.theme == nil {
		return nil
	}
	return t.theme.EntityColors
}

func (c *CommandContext) tableContext() *table.RenderContext {
	styled := c.ColorMode == clog.ColorAlways || c.ColorMode == clog.ColorAuto && c.IsTTY
	return table.NewRenderContext(
		slackTableTheme{theme: c.Theme, styled: styled},
		termansi.New(termansi.WithTerminal(styled), termansi.WithHyperlinkFallback(termansi.HyperlinkFallbackText)),
	)
}

// tableStyled reports whether the current command context renders ANSI
// styles for table cells.
func (c *CommandContext) tableStyled() bool {
	return c.ColorMode == clog.ColorAlways || c.ColorMode == clog.ColorAuto && c.IsTTY
}

// hashCell builds a table cell whose display text is hash-colored from
// seed using the active theme's entity palette. Falls back to plain text
// when styling is disabled or the seed/text is empty.
func (c *CommandContext) hashCell(seed, text string) table.Cell {
	if text == "" {
		return table.TextCell("")
	}
	if !c.tableStyled() {
		return table.TextCell(text)
	}
	style := HashEntityStyle(c.Theme, seed)
	if style == nil {
		return table.TextCell(text)
	}
	return table.StyledCell(style.Render(text), text)
}

// dottedHashCell renders a dotted path with the parent segments dimmed
// and the trailing leaf segment hash-colored, so the cell reads like a
// filesystem path — namespace fades, the actual setting pops. Single-
// segment text is hash-colored as a whole.
func (c *CommandContext) dottedHashCell(text string) table.Cell {
	if text == "" {
		return table.TextCell("")
	}
	if !c.tableStyled() {
		return table.TextCell(text)
	}
	idx := strings.LastIndex(text, ".")
	if idx < 0 {
		style := HashEntityStyle(c.Theme, "segment:"+text)
		if style == nil {
			return table.TextCell(text)
		}
		return table.StyledCell(style.Render(text), text)
	}
	prefix, leaf := text[:idx+1], text[idx+1:]
	var prefixOut string
	if c.Theme != nil && c.Theme.Dim != nil {
		prefixOut = c.Theme.Dim.Render(prefix)
	} else {
		prefixOut = prefix
	}
	leafOut := leaf
	if leaf != "" {
		if style := HashEntityStyle(c.Theme, "segment:"+leaf); style != nil {
			leafOut = style.Render(leaf)
		}
	}
	return table.StyledCell(prefixOut+leafOut, text)
}

// timestampCell renders a Slack timestamp using clog's FieldTime style
// so tables match the color clog uses for time fields inside events.
func (c *CommandContext) timestampCell(ts string) table.Cell {
	if ts == "" {
		return table.TextCell("")
	}
	if !c.tableStyled() {
		return table.TextCell(ts)
	}
	style := clogFieldTimeStyle()
	if style == nil {
		return table.TextCell(ts)
	}
	return table.StyledCell(style.Render(ts), ts)
}

// numberCell renders an integer using clog's FieldNumber style. Nil or
// zero values render dim so they fade into the background.
func (c *CommandContext) numberCell(value *int) table.Cell {
	if value == nil {
		return table.TextCell("")
	}
	text := strconv.Itoa(*value)
	if !c.tableStyled() {
		return table.TextCell(text)
	}
	style := clogFieldNumberStyle()
	if value != nil && *value == 0 && c.Theme != nil && c.Theme.Dim != nil {
		style = c.Theme.Dim
	}
	if style == nil {
		return table.TextCell(text)
	}
	return table.StyledCell(style.Render(text), text)
}

// numberCellInt is the value-typed sibling of numberCell for callers that
// hold a plain int and never need an "absent" empty cell. Behavior is
// otherwise identical: clog FieldNumber styling, with zero rendered dim.
func (c *CommandContext) numberCellInt(value int) table.Cell {
	return c.numberCell(&value)
}

// boolStateCell renders a boolean as styled text on an alarm-on-true
// polarity: true is the "alarming" state (red) and false is the
// routine/expected state (dim). Empty input renders as an empty cell.
func (c *CommandContext) boolStateCell(value *bool) table.Cell {
	text := ptrBool(value)
	if text == "" {
		return table.TextCell("")
	}
	if !c.tableStyled() || c.Theme == nil || value == nil {
		return table.TextCell(text)
	}
	style := c.Theme.Dim
	if *value {
		style = c.Theme.Red
	}
	if style == nil {
		return table.TextCell(text)
	}
	return table.StyledCell(style.Render(text), text)
}

// dimWhenCell renders a boolean dim when true, default otherwise.
// Mirrors ApplyDimWhen for table cells; use for routine-state fields
// where the true side should fade and the false side stays neutral.
func (c *CommandContext) dimWhenCell(value *bool) table.Cell {
	text := ptrBool(value)
	if text == "" {
		return table.TextCell("")
	}
	if !c.tableStyled() || c.Theme == nil || c.Theme.Dim == nil || value == nil || !*value {
		return table.TextCell(text)
	}
	return table.StyledCell(c.Theme.Dim.Render(text), text)
}

func (c *CommandContext) tableWidth() int {
	if f, ok := c.OutWriter().(*os.File); ok {
		return terminal.Width(f)
	}
	return 0
}

func (c *CommandContext) WriteMessageTable(messages []CliMessage) error {
	columns := []table.Column[CliMessage]{
		{Name: "ts", Header: "TS", Render: func(row CliMessage, _ *table.RenderContext) table.Cell { return c.timestampCell(row.TS) }},
		{Name: "user", Header: "USER", Render: func(row CliMessage, _ *table.RenderContext) table.Cell {
			id := ptrString(row.User)
			return c.hashCell("user:"+id, id)
		}},
		{Name: "text", Header: "TEXT", Flex: true, Render: func(row CliMessage, _ *table.RenderContext) table.Cell { return table.TextCell(ptrString(row.Text)) }},
		{Name: "replies", Header: "REPLIES", Render: func(row CliMessage, _ *table.RenderContext) table.Cell { return c.numberCell(row.ReplyCount) }},
	}
	return c.WriteString(table.NewRenderer(columns, c.tableContext(), table.WithTTY(c.IsTTY), table.WithTermWidth(c.tableWidth())).Render(messages).String())
}

func (c *CommandContext) WriteSearchTable(matches []CliSearchMessage, full bool) error {
	columns := []table.Column[CliSearchMessage]{
		{Name: "ts", Header: "TS", Render: func(row CliSearchMessage, _ *table.RenderContext) table.Cell { return c.timestampCell(row.TS) }},
		{Name: "channel", Header: "CHANNEL", Render: func(row CliSearchMessage, _ *table.RenderContext) table.Cell {
			return c.hashCell("channel:"+row.Channel.ID, cliutil.FirstNonEmpty(row.Channel.Name, row.Channel.ID))
		}},
		{Name: "user", Header: "USER", Render: func(row CliSearchMessage, _ *table.RenderContext) table.Cell {
			return c.hashCell("user:"+row.User, row.User)
		}},
		{Name: "text", Header: "TEXT", Flex: true, Render: func(row CliSearchMessage, _ *table.RenderContext) table.Cell {
			text := row.Text
			if !full {
				text = termansi.Truncate(text, 300, "...")
			}
			return table.TextCell(text)
		}},
	}
	return c.WriteString(table.NewRenderer(columns, c.tableContext(), table.WithTTY(c.IsTTY), table.WithTermWidth(c.tableWidth())).Render(matches).String())
}

func (c *CommandContext) WriteChannelTable(command string, channels []CliChannel) error {
	columns := []table.Column[CliChannel]{
		{Name: "channel", Header: "CHANNEL", Render: func(row CliChannel, _ *table.RenderContext) table.Cell {
			return c.hashCell("channel:"+row.ID, row.ID)
		}},
		{Name: "name", Header: "NAME", Render: func(row CliChannel, _ *table.RenderContext) table.Cell {
			return table.TextCell(row.Name)
		}},
		{Name: "type", Header: "TYPE", Render: func(row CliChannel, _ *table.RenderContext) table.Cell {
			return c.hashCell("channel_type:"+row.Type, row.Type)
		}},
		{Name: "user", Header: "USER", Render: func(row CliChannel, _ *table.RenderContext) table.Cell {
			id := ptrString(row.User)
			return c.hashCell("user:"+id, id)
		}},
		{Name: "member", Header: "MEMBER", Render: func(row CliChannel, _ *table.RenderContext) table.Cell {
			return c.dimWhenCell(row.IsMember)
		}},
		{Name: "archived", Header: "ARCHIVED", Render: func(row CliChannel, _ *table.RenderContext) table.Cell {
			return c.boolStateCell(row.IsArchived)
		}},
		{Name: "members", Header: "MEMBERS", Render: func(row CliChannel, _ *table.RenderContext) table.Cell { return c.numberCell(row.NumMembers) }},
		{Name: "topic", Header: "TOPIC", Flex: true, Render: func(row CliChannel, _ *table.RenderContext) table.Cell { return table.TextCell(ptrString(row.Topic)) }},
	}
	return c.WriteString(table.NewRenderer(columns, c.tableContext(), table.WithTTY(c.IsTTY), table.WithTermWidth(c.tableWidth())).Render(channels).String())
}

func (c *CommandContext) WriteUserTable(users []CliUser) error {
	columns := []table.Column[CliUser]{
		{Name: "user", Header: "USER", Render: func(row CliUser, _ *table.RenderContext) table.Cell {
			return c.hashCell("user:"+row.ID, row.ID)
		}},
		{Name: "name", Header: "NAME", Render: func(row CliUser, _ *table.RenderContext) table.Cell {
			return table.TextCell(row.Name)
		}},
	}
	if usersHavePresence(users) {
		columns = append(columns, table.Column[CliUser]{
			Name:   "presence",
			Header: "PRESENCE",
			Render: func(row CliUser, _ *table.RenderContext) table.Cell {
				return table.TextCell(ptrString(row.Presence))
			},
		})
	}
	columns = append(columns,
		table.Column[CliUser]{Name: "tz", Header: "TZ", Render: func(row CliUser, _ *table.RenderContext) table.Cell {
			tz := ptrString(row.Timezone)
			if tz == "" {
				return table.TextCell("")
			}
			if !c.tableStyled() {
				return table.TextCell(tz)
			}
			return table.StyledCell(RenderTimezone(c.Theme, tz), tz)
		}},
		table.Column[CliUser]{Name: "status", Header: "STATUS", Flex: true, Render: func(row CliUser, _ *table.RenderContext) table.Cell { return table.TextCell(ptrString(row.StatusText)) }},
	)
	return c.WriteString(table.NewRenderer(columns, c.tableContext(), table.WithTTY(c.IsTTY), table.WithTermWidth(c.tableWidth())).Render(users).String())
}

func usersHavePresence(users []CliUser) bool {
	for _, user := range users {
		if strings.TrimSpace(ptrString(user.Presence)) != "" {
			return true
		}
	}
	return false
}

func (c *CommandContext) WriteReactionTable(reactions []CliReactionSummary) error {
	columns := []table.Column[CliReactionSummary]{
		{Name: "emoji", Header: "EMOJI", Render: func(row CliReactionSummary, _ *table.RenderContext) table.Cell {
			return c.hashCell("emoji:"+row.Name, row.Name)
		}},
		{Name: "count", Header: "COUNT", Render: func(row CliReactionSummary, _ *table.RenderContext) table.Cell {
			return c.numberCellInt(row.Count)
		}},
		{Name: "users", Header: "USERS", Flex: true, Render: func(row CliReactionSummary, _ *table.RenderContext) table.Cell {
			return c.reactionUsersCell(row.Users)
		}},
	}
	return c.WriteString(table.NewRenderer(columns, c.tableContext(), table.WithTTY(c.IsTTY), table.WithTermWidth(c.tableWidth())).Render(reactions).String())
}

// reactionUsersCell hash-colors each comma-separated user ID using the
// shared "user:<id>" seed so the same user is the same color across all
// rows and across the message/search tables. Falls back to plain joined
// text when styling is disabled.
func (c *CommandContext) reactionUsersCell(users []string) table.Cell {
	if len(users) == 0 {
		return table.TextCell("")
	}
	plain := strings.Join(users, ",")
	if !c.tableStyled() {
		return table.TextCell(plain)
	}
	rendered := make([]string, 0, len(users))
	for _, id := range users {
		if id == "" {
			rendered = append(rendered, "")
			continue
		}
		style := HashEntityStyle(c.Theme, "user:"+id)
		if style == nil {
			rendered = append(rendered, id)
			continue
		}
		rendered = append(rendered, style.Render(id))
	}
	return table.StyledCell(strings.Join(rendered, ","), plain)
}

// ConfigEntry is a key/value pair for the config list table. Defined here
// (rather than reusing internal/cli/config.Entry) so the output package
// remains free of a back-edge into the config CLI package.
type ConfigEntry struct {
	Key         string
	Value       string
	Description string
}

func (c *CommandContext) WriteConfigEntriesTable(entries []ConfigEntry) error {
	columns := []table.Column[ConfigEntry]{
		{Name: "key", Header: "KEY", Render: func(row ConfigEntry, _ *table.RenderContext) table.Cell {
			return c.dottedHashCell(row.Key)
		}},
		{Name: "value", Header: "VALUE", Flex: true, Render: func(row ConfigEntry, _ *table.RenderContext) table.Cell {
			return c.configValueCell(row.Value)
		}},
		{Name: "description", Header: "DESCRIPTION", Flex: true, Render: func(row ConfigEntry, _ *table.RenderContext) table.Cell {
			text := row.Description
			if !c.tableStyled() || c.Theme == nil || c.Theme.Dim == nil || text == "" {
				return table.TextCell(text)
			}
			return table.StyledCell(c.Theme.Dim.Render(text), text)
		}},
	}
	return c.WriteString(table.NewRenderer(columns, c.tableContext(), table.WithTTY(c.IsTTY), table.WithTermWidth(c.tableWidth())).Render(entries).String())
}

// configValueCell renders a config value: bool values get red/green by
// value, empty values get a dim "(unset)" marker, and dotted values get
// per-segment hash coloring.
func (c *CommandContext) configValueCell(value string) table.Cell {
	if value == "" {
		const marker = "(unset)"
		if !c.tableStyled() || c.Theme == nil || c.Theme.Dim == nil {
			return table.TextCell(marker)
		}
		return table.StyledCell(c.Theme.Dim.Render(marker), marker)
	}
	if c.tableStyled() && c.Theme != nil {
		switch value {
		case "true":
			if c.Theme.Green != nil {
				return table.StyledCell(c.Theme.Green.Render(value), value)
			}
		case "false":
			if c.Theme.Red != nil {
				return table.StyledCell(c.Theme.Red.Render(value), value)
			}
		}
	}
	return c.dottedHashCell(value)
}

func (c *CommandContext) WriteWorkspaceTable(workspaces []config.WorkspaceProfile) error {
	columns := []table.Column[config.WorkspaceProfile]{
		{Name: "profile", Header: "PROFILE", Render: func(row config.WorkspaceProfile, _ *table.RenderContext) table.Cell {
			return c.hashCell("workspace:"+row.Name, row.Name)
		}},
		{Name: "team", Header: "WORKSPACE", Render: func(row config.WorkspaceProfile, _ *table.RenderContext) table.Cell {
			return c.hashCell("team_id:"+row.TeamID, row.TeamID)
		}},
		{Name: "name", Header: "NAME", Flex: true, Render: func(row config.WorkspaceProfile, _ *table.RenderContext) table.Cell {
			return table.TextCell(row.TeamName)
		}},
		{Name: "token", Header: "TOKEN", Render: func(row config.WorkspaceProfile, _ *table.RenderContext) table.Cell {
			return c.hashCell("token_type:"+string(row.TokenType), string(row.TokenType))
		}},
	}
	return c.WriteString(table.NewRenderer(columns, c.tableContext(), table.WithTTY(c.IsTTY), table.WithTermWidth(c.tableWidth())).Render(workspaces).String())
}

func ptrString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func ptrBool(value *bool) string {
	if value == nil {
		return ""
	}
	return strconv.FormatBool(*value)
}
