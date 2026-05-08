package main

import (
	"image/color"
	"os"
	"strconv"
	"strings"

	clibtheme "github.com/gechr/clib/theme"
	"github.com/gechr/clog"
	"github.com/gechr/primer/table"
	termansi "github.com/gechr/x/ansi"
	"github.com/gechr/x/terminal"
	"github.com/matcra587/slack-cli/internal/config"
)

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

func (c *CommandContext) tableWidth() int {
	if f, ok := c.stdout().(*os.File); ok {
		return terminal.Width(f)
	}
	return 0
}

func (c *CommandContext) WriteMessageTable(messages []cliMessage) error {
	columns := []table.Column[cliMessage]{
		{Name: "ts", Header: "TS", Render: func(row cliMessage, _ *table.RenderContext) table.Cell { return table.TextCell(row.TS) }},
		{Name: "user", Header: "USER", Render: func(row cliMessage, _ *table.RenderContext) table.Cell { return table.TextCell(ptrString(row.User)) }},
		{Name: "text", Header: "TEXT", Flex: true, Render: func(row cliMessage, _ *table.RenderContext) table.Cell { return table.TextCell(ptrString(row.Text)) }},
		{Name: "replies", Header: "REPLIES", Render: func(row cliMessage, _ *table.RenderContext) table.Cell { return table.TextCell(ptrInt(row.ReplyCount)) }},
	}
	return c.WritePlain(table.NewRenderer(columns, c.tableContext(), table.WithTTY(c.IsTTY), table.WithTermWidth(c.tableWidth())).Render(messages).String())
}

func (c *CommandContext) WriteSearchTable(data searchCommandData) error {
	columns := []table.Column[cliSearchMessage]{
		{Name: "ts", Header: "TS", Render: func(row cliSearchMessage, _ *table.RenderContext) table.Cell { return table.TextCell(row.TS) }},
		{Name: "channel", Header: "CHANNEL", Render: func(row cliSearchMessage, _ *table.RenderContext) table.Cell {
			return table.TextCell(firstNonEmpty(row.Channel.Name, row.Channel.ID))
		}},
		{Name: "user", Header: "USER", Render: func(row cliSearchMessage, _ *table.RenderContext) table.Cell { return table.TextCell(row.User) }},
		{Name: "text", Header: "TEXT", Flex: true, Render: func(row cliSearchMessage, _ *table.RenderContext) table.Cell {
			text := row.Text
			if !data.Full {
				text = termansi.Truncate(text, 300, "...")
			}
			return table.TextCell(text)
		}},
	}
	return c.WritePlain(table.NewRenderer(columns, c.tableContext(), table.WithTTY(c.IsTTY), table.WithTermWidth(c.tableWidth())).Render(data.Matches).String())
}

func (c *CommandContext) WriteChannelTable(command string, channels []cliChannel) error {
	columns := []table.Column[cliChannel]{
		{Name: "channel", Header: "CHANNEL", Render: func(row cliChannel, _ *table.RenderContext) table.Cell { return table.TextCell(row.ID) }},
		{Name: "name", Header: "NAME", Render: func(row cliChannel, _ *table.RenderContext) table.Cell { return table.TextCell(row.Name) }},
		{Name: "type", Header: "TYPE", Render: func(row cliChannel, _ *table.RenderContext) table.Cell { return table.TextCell(row.Type) }},
		{Name: "user", Header: "USER", Render: func(row cliChannel, _ *table.RenderContext) table.Cell { return table.TextCell(ptrString(row.User)) }},
		{Name: "member", Header: "MEMBER", Render: func(row cliChannel, _ *table.RenderContext) table.Cell { return table.TextCell(ptrBool(row.IsMember)) }},
		{Name: "archived", Header: "ARCHIVED", Render: func(row cliChannel, _ *table.RenderContext) table.Cell {
			return table.TextCell(ptrBool(row.IsArchived))
		}},
		{Name: "members", Header: "MEMBERS", Render: func(row cliChannel, _ *table.RenderContext) table.Cell { return table.TextCell(ptrInt(row.NumMembers)) }},
		{Name: "topic", Header: "TOPIC", Flex: true, Render: func(row cliChannel, _ *table.RenderContext) table.Cell { return table.TextCell(ptrString(row.Topic)) }},
	}
	return c.WritePlain(table.NewRenderer(columns, c.tableContext(), table.WithTTY(c.IsTTY), table.WithTermWidth(c.tableWidth())).Render(channels).String())
}

func (c *CommandContext) WriteUserTable(users []cliUser) error {
	columns := []table.Column[cliUser]{
		{Name: "user", Header: "USER", Render: func(row cliUser, _ *table.RenderContext) table.Cell { return table.TextCell(row.ID) }},
		{Name: "name", Header: "NAME", Render: func(row cliUser, _ *table.RenderContext) table.Cell { return table.TextCell(row.Name) }},
	}
	if usersHavePresence(users) {
		columns = append(columns, table.Column[cliUser]{
			Name:   "presence",
			Header: "PRESENCE",
			Render: func(row cliUser, _ *table.RenderContext) table.Cell {
				return table.TextCell(ptrString(row.Presence))
			},
		})
	}
	columns = append(columns,
		table.Column[cliUser]{Name: "tz", Header: "TZ", Render: func(row cliUser, _ *table.RenderContext) table.Cell { return table.TextCell(ptrString(row.Timezone)) }},
		table.Column[cliUser]{Name: "status", Header: "STATUS", Flex: true, Render: func(row cliUser, _ *table.RenderContext) table.Cell { return table.TextCell(ptrString(row.StatusText)) }},
	)
	return c.WritePlain(table.NewRenderer(columns, c.tableContext(), table.WithTTY(c.IsTTY), table.WithTermWidth(c.tableWidth())).Render(users).String())
}

func usersHavePresence(users []cliUser) bool {
	for _, user := range users {
		if strings.TrimSpace(ptrString(user.Presence)) != "" {
			return true
		}
	}
	return false
}

func (c *CommandContext) WriteReactionTable(reactions []cliReactionSummary) error {
	columns := []table.Column[cliReactionSummary]{
		{Name: "emoji", Header: "EMOJI", Render: func(row cliReactionSummary, _ *table.RenderContext) table.Cell { return table.TextCell(row.Name) }},
		{Name: "count", Header: "COUNT", Render: func(row cliReactionSummary, _ *table.RenderContext) table.Cell {
			return table.TextCell(strconv.Itoa(row.Count))
		}},
		{Name: "users", Header: "USERS", Flex: true, Render: func(row cliReactionSummary, _ *table.RenderContext) table.Cell {
			return table.TextCell(strings.Join(row.Users, ","))
		}},
	}
	return c.WritePlain(table.NewRenderer(columns, c.tableContext(), table.WithTTY(c.IsTTY), table.WithTermWidth(c.tableWidth())).Render(reactions).String())
}

func (c *CommandContext) WriteWorkspaceTable(workspaces []config.WorkspaceProfile) error {
	columns := []table.Column[config.WorkspaceProfile]{
		{Name: "profile", Header: "PROFILE", Render: func(row config.WorkspaceProfile, _ *table.RenderContext) table.Cell { return table.TextCell(row.Name) }},
		{Name: "team", Header: "WORKSPACE", Render: func(row config.WorkspaceProfile, _ *table.RenderContext) table.Cell {
			return table.TextCell(row.TeamID)
		}},
		{Name: "name", Header: "NAME", Flex: true, Render: func(row config.WorkspaceProfile, _ *table.RenderContext) table.Cell {
			return table.TextCell(row.TeamName)
		}},
		{Name: "token", Header: "TOKEN", Render: func(row config.WorkspaceProfile, _ *table.RenderContext) table.Cell {
			return table.TextCell(string(row.TokenType))
		}},
	}
	return c.WritePlain(table.NewRenderer(columns, c.tableContext(), table.WithTTY(c.IsTTY), table.WithTermWidth(c.tableWidth())).Render(workspaces).String())
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

func ptrInt(value *int) string {
	if value == nil {
		return ""
	}
	return strconv.Itoa(*value)
}
