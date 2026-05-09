package output

import (
	"encoding/json"
	"io"
	"time"

	"github.com/gechr/clib/theme"
	"github.com/gechr/clog"
	termansi "github.com/gechr/x/ansi"
	"github.com/matcra587/slack-cli/internal/config"
)

// CommandContext holds per-command rendering state. Function-typed fields
// carry the trailing Func suffix so the accessor methods (Now, RequestID) keep
// the names callers expect.
type CommandContext struct {
	Workspace     string
	Mode          RenderMode
	Stdout        io.Writer
	Stderr        io.Writer
	NowFunc       func() time.Time
	RequestIDFunc func() string
	ColorMode     clog.ColorMode
	IsTTY         bool
	Theme         *theme.Theme
	StdoutLog     *clog.Logger
	StderrLog     *clog.Logger
}

func (c *CommandContext) StdoutLogger() *clog.Logger { return c.StdoutLog }
func (c *CommandContext) StderrLogger() *clog.Logger { return c.StderrLog }

func (c *CommandContext) OutWriter() io.Writer {
	if c.Stdout != nil {
		return c.Stdout
	}
	return io.Discard
}

func (c *CommandContext) Now() time.Time {
	if c.NowFunc != nil {
		return c.NowFunc()
	}
	return time.Now().UTC()
}

func (c *CommandContext) RequestID() string {
	if c.RequestIDFunc != nil {
		return c.RequestIDFunc()
	}
	return ""
}

func (c *CommandContext) WorkspaceName() string {
	if c.Workspace != "" {
		return c.Workspace
	}
	return "default"
}

func (c *CommandContext) WriteResult(command string, data any) error {
	return c.WriteResultWithPagination(command, data, nil)
}

func (c *CommandContext) WriteResultWithPagination(command string, data any, pagination *Pagination) error {
	switch c.Mode {
	case RenderModePlain:
		return c.WritePlainResult(command, data, pagination)
	case RenderModeCompact:
		c.StdoutLogger().Print().JSON(data)
	case RenderModeRaw:
		switch raw := data.(type) {
		case []byte:
			c.StdoutLogger().Print().RawJSON(raw)
		case json.RawMessage:
			c.StdoutLogger().Print().RawJSON(raw)
		default:
			c.StdoutLogger().Print().JSON(data)
		}
	default:
		c.StdoutLogger().Print().JSON(Envelope{
			Meta: EnvelopeMeta{
				Command:    command,
				Workspace:  c.WorkspaceName(),
				Timestamp:  c.Now().Format(time.RFC3339),
				RequestID:  c.RequestID(),
				Pagination: pagination,
			},
			Data:   data,
			Errors: []CLIError{},
		})
	}
	return nil
}

func (c *CommandContext) WritePlainResult(command string, data any, pagination *Pagination) error {
	if r, ok := data.(PlainRenderer); ok {
		return r.WritePlain(c, command, pagination)
	}
	event := c.ResultEvent(command).Any("data", data)
	AddPaginationFields(event, pagination)
	event.Send()
	return nil
}

func (c *CommandContext) ResultEvent(command string) *clog.Event {
	return c.StdoutLogger().Info().
		Str("command", command)
}

func (c *CommandContext) ResultEventWithStyles(command string, styles ...FieldStyle) *clog.Event {
	logger := c.StdoutLogger()
	ApplyFieldStyles(logger, c.Theme, styles...)
	return logger.Info().
		Str("command", command)
}

func (c *CommandContext) WriteMessages(command string, messages []CliMessage, pagination *Pagination) error {
	if len(messages) > 0 {
		return c.WriteMessageTable(messages)
	}
	event := c.ResultEvent(command)
	AddPaginationFields(event, pagination)
	event.Send()
	return nil
}

func (c *CommandContext) WriteSearch(command string, matches []CliSearchMessage, full bool, pagination *Pagination) error {
	if len(matches) > 0 {
		return c.WriteSearchTable(matches, full)
	}
	event := c.ResultEvent(command)
	AddPaginationFields(event, pagination)
	event.Send()
	return nil
}

func (c *CommandContext) WriteChannels(command string, channels []CliChannel, pagination *Pagination) error {
	if len(channels) > 0 {
		return c.WriteChannelTable(command, channels)
	}
	event := c.ResultEvent(command)
	AddPaginationFields(event, pagination)
	event.Send()
	return nil
}

func (c *CommandContext) WriteUsers(command string, users []CliUser, pagination *Pagination) error {
	if len(users) > 0 {
		return c.WriteUserTable(users)
	}
	event := c.ResultEvent(command)
	AddPaginationFields(event, pagination)
	event.Send()
	return nil
}

func (c *CommandContext) WriteWorkspaces(command string, workspaces []config.WorkspaceProfile, pagination *Pagination) error {
	if len(workspaces) > 0 {
		return c.WriteWorkspaceTable(workspaces)
	}
	event := c.ResultEvent(command)
	AddPaginationFields(event, pagination)
	event.Send()
	return nil
}

func (c *CommandContext) WriteString(message string) error {
	c.StdoutLogger().Info().Parts(clog.PartMessage).Msg(message)
	return nil
}

func (c *CommandContext) WriteError(err CLIError) int {
	if c.Mode == RenderModePlain {
		event := c.StderrLogger().Error().
			Str("type", err.Type).
			Int("exit_code", err.ExitCode)
		event = AddCLIErrorDetails(event, err.Details)
		event.Msg(err.Message)
	} else {
		c.StderrLogger().Print().JSON(struct {
			Errors []CLIError `json:"errors"`
		}{
			Errors: []CLIError{err},
		})
	}
	return err.ExitCode
}

func WriteCommandError(ctx *CommandContext, err CLIError) error {
	ctx.WriteError(err)
	return CommandError{CLIError: err}
}

func TruncateText(value string, limit int) string {
	return termansi.Truncate(value, limit, "...")
}
