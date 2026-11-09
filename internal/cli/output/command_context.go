package output

import (
	"bytes"
	"io"
	"maps"
	"strings"
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
	Command       string
	Workspace     string
	TeamID        string
	GOOS          string
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

type ListPlainWriter[T any] func(c *CommandContext, command string, items []T, pagination *Pagination) error

type ResultEnricher interface {
	EnrichResult(c *CommandContext) any
}

// PaginatedResult retains paging facts when compact output removes the envelope.
type PaginatedResult interface {
	WithPagination(*Pagination) any
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
	data = c.EnrichResult(data)
	if paginated, ok := data.(PaginatedResult); ok && pagination != nil {
		data = paginated.WithPagination(pagination)
	}
	switch c.Mode {
	case RenderModePlain:
		return c.WritePlainResult(command, data, pagination)
	case RenderModeCompact:
		return c.WriteJSON(data)
	default:
		return c.WriteJSON(Envelope{
			OK: true,
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
}

func (c *CommandContext) EnrichResult(data any) any {
	if e, ok := data.(ResultEnricher); ok {
		return e.EnrichResult(c)
	}
	return data
}

func WriteList[T any](c *CommandContext, command string, data any, items []T, pagination *Pagination, plain ListPlainWriter[T]) error {
	data = c.EnrichResult(data)
	if c.Mode == RenderModePlain {
		return c.withPlainOutput(func(render *CommandContext) error {
			if plain != nil {
				return plain(render, command, items, pagination)
			}
			event := render.ResultEvent(command)
			render.FinishResult(event, command, pagination)
			return nil
		})
	}
	return c.WriteResultWithPagination(command, data, pagination)
}

func (c *CommandContext) WritePlainResult(command string, data any, pagination *Pagination) error {
	return c.withPlainOutput(func(render *CommandContext) error {
		if r, ok := data.(PlainRenderer); ok {
			return r.WritePlain(render, command, pagination)
		}
		event := render.ResultEvent(command).Any("data", data)
		AddPaginationFields(event, pagination)
		event.Msg(ActionLabel(command))
		return nil
	})
}

func (c *CommandContext) ResultEvent(command string) *clog.Event {
	return resultEvent(c.StdoutLogger(), command)
}

func (c *CommandContext) ResultEventWithStyles(command string, styles ...FieldStyle) *clog.Event {
	logger := c.StdoutLogger()
	ApplyFieldStyles(logger, c.Theme, styles...)
	return resultEvent(logger, command)
}

// resultEvent returns an event ready for the WritePlain caller to chain
// fields onto and finish with `event.Msg(ActionLabel(command))`. The
// `command=<id>` field is suppressed unless --debug is on; agents and
// debug consumers still get the command id via the JSON envelope's
// meta.command field.
func resultEvent(logger *clog.Logger, command string) *clog.Event {
	event := logger.Info()
	if clog.IsVerbose() {
		event = event.Str("command", command)
	}
	return event
}

// ActionLabel returns the past-tense action label rendered as the leading
// message in plain-mode output (e.g. "Message sent" for "message.send").
// Falls back to the command id itself for commands without a registered
// label so unknown commands stay visible during development. The
// TestCommandActionLabelCoverage regression test in cmd/slick fails if any
// leaf cobra command lacks a label.
func ActionLabel(command string) string {
	if label, ok := commandActionLabel[command]; ok {
		return label
	}
	return command
}

// CommandActionLabels returns a copy of the registered action labels keyed
// by dotted command id (e.g. "message.send" -> "Message sent"). Exported
// for the cmd/slick TestCommandActionLabelCoverage regression test that
// walks the cobra tree and ensures every leaf has a registered label.
func CommandActionLabels() map[string]string {
	out := make(map[string]string, len(commandActionLabel))
	maps.Copy(out, commandActionLabel)
	return out
}

// FinishResult terminates a plain-mode event with the command's action
// label, threading any pagination footer fields. Use this for renderers
// that built a paginated event; renderers without pagination can call
// `event.Msg(ActionLabel(command))` directly.
func (c *CommandContext) FinishResult(event *clog.Event, command string, pagination *Pagination) {
	if pagination != nil {
		AddPaginationFields(event, pagination)
	}
	event.Msg(ActionLabel(command))
}

// commandActionLabel maps command ids to past-tense action labels. Keys
// are the runtime command ids passed to ResultEvent / WriteResult, matching
// the dotted cobra path. Legacy ids remain as display aliases for Go callers.
//
// Adding a new command? Add an entry. The TestCommandActionLabelCoverage
// regression test in cmd/slick walks the cobra tree and fails if any leaf
// command's dotted path lacks a label.
//
// A few entries below exist solely to satisfy that coverage test even
// though their renderers do not call ActionLabel:
//   - auth.status: StatusData renders a state-specific theme-rendered
//     message via authStatusMessage(...), bypassing the registry.
//   - version: versionData.WritePlain calls c.WriteString on a multi-line
//     human-readable build summary.
//   - manifest.template: emits raw JSON or YAML; no action-label line.
//   - agent.guide / agent.schema: emit the workflow runbook text or the
//     JSON schema; no action-label line.
//
// These stay registered so the coverage test keeps catching new leaf
// commands that legitimately need a label. Removing them would force
// the test to grow a parallel exception list, which is more disruptive
// than the comment.
var commandActionLabel = map[string]string{
	"message.send":             "Message sent",
	"message.edit":             "Message edited",
	"message.delete":           "Message deleted",
	"message.scheduled.list":   "Scheduled messages retrieved",
	"message.scheduled.delete": "Scheduled message deleted",
	"reply":                    "Reply posted",
	"dnd.info":                 "Do Not Disturb settings",
	"dnd.team":                 "Team Do Not Disturb settings",
	"dnd.snooze":               "Notifications snoozed",
	"dnd.end-snooze":           "Notification snooze ended",
	"dnd.end":                  "Do Not Disturb session ended",
	"pin.list":                 "Pinned messages",
	"pin.add":                  "Message pinned",
	"pin.remove":               "Message unpinned",
	"bookmark.list":            "Bookmarks",
	"bookmark.add":             "Bookmark added",
	"bookmark.edit":            "Bookmark edited",
	"bookmark.remove":          "Bookmark removed",
	"react.add":                "Reaction added",
	"react.remove":             "Reaction removed",
	"react.list":               "Reactions retrieved",
	"react.watch":              "Reaction observed",
	"history.list":             "History retrieved",
	"lookup.channel":           "Channel resolved",
	"lookup.user":              "User resolved",
	"lookup.messages":          "Messages searched",
	"search.messages":          "Messages searched", // Legacy display alias.
	"file.upload":              "File uploaded",
	"health.current":           "Slack status retrieved",
	"health.history":           "Slack incidents retrieved",
	"health.api-test":          "Slack API tested",
	"health.check":             "Slack health checked",
	"status.set":               "Status set",
	"status.clear":             "Status cleared",
	"auth.login":               "Login complete",
	"auth.logout":              "Logout complete",
	"auth.switch":              "Workspace switched",
	"auth.status":              "Auth status retrieved",
	"workspace.list":           "Workspaces listed",
	"config.init":              "Config initialized",
	"config.path":              "Config path resolved",
	"config.list":              "Config listed",
	"config.get":               "Config value retrieved",
	"config.set":               "Config value set",
	"config.unset":             "Config value unset",
	"manifest.template":        "Manifest generated",
	"cache.users":              "User cache primed",
	"cache.channels":           "Channel cache primed",
	"cache.clear":              "Cache cleared",
	"agent.export":             "Guides exported",
	"agent.guide":              "Guide retrieved",
	"agent.schema":             "Schema retrieved",
	"version":                  "Version printed",
}

func (c *CommandContext) WriteMessages(command string, messages []Message, pagination *Pagination) error {
	if len(messages) > 0 {
		if err := c.WriteMessageTable(messages); err != nil {
			return err
		}
		return c.writePaginationFooter(command, pagination)
	}
	event := c.ResultEvent(command)
	c.FinishResult(event, command, pagination)
	return nil
}

func (c *CommandContext) WriteSearch(command string, matches []SearchMessage, full bool, pagination *Pagination) error {
	if len(matches) > 0 {
		if err := c.WriteSearchTable(matches, full); err != nil {
			return err
		}
		return c.writePaginationFooter(command, pagination)
	}
	event := c.ResultEvent(command)
	c.FinishResult(event, command, pagination)
	return nil
}

func (c *CommandContext) WriteChannels(command string, channels []Channel, pagination *Pagination) error {
	if len(channels) > 0 {
		if err := c.WriteChannelTable(command, channels); err != nil {
			return err
		}
		return c.writePaginationFooter(command, pagination)
	}
	event := c.ResultEvent(command)
	c.FinishResult(event, command, pagination)
	return nil
}

func (c *CommandContext) WriteUsers(command string, users []User, pagination *Pagination) error {
	if len(users) > 0 {
		if err := c.WriteUserTable(users); err != nil {
			return err
		}
		return c.writePaginationFooter(command, pagination)
	}
	event := c.ResultEvent(command)
	c.FinishResult(event, command, pagination)
	return nil
}

func (c *CommandContext) WriteScheduledMessages(command string, messages []ScheduledMessage, pagination *Pagination) error {
	if len(messages) > 0 {
		if err := c.WriteScheduledMessageTable(messages); err != nil {
			return err
		}
		return c.writePaginationFooter(command, pagination)
	}
	ApplyNumberKeyStyle(c.StdoutLogger(), "count")
	event := c.ResultEvent(command)
	event = event.Str("count", "0")
	c.FinishResult(event, command, pagination)
	return nil
}

// writePaginationFooter buffers the log rendering so destination failures are
// reported through the same exit-8 path as the table itself.
func (c *CommandContext) writePaginationFooter(command string, pagination *Pagination) error {
	if pagination == nil {
		return nil
	}
	var buffer bytes.Buffer
	logger := c.StdoutLogger().With().Logger()
	mode := c.ColorMode
	if mode == clog.ColorAuto && c.IsTTY {
		mode = clog.ColorAlways
	}
	logger.SetOutput(clog.NewOutput(&buffer, mode))
	event := resultEvent(logger, command)
	c.FinishResult(event, command, pagination)
	return c.writeBytes(c.OutWriter(), buffer.Bytes())
}

func (c *CommandContext) WriteHealthIncidents(command string, incidents []HealthIncident, pagination *Pagination) error {
	if len(incidents) > 0 {
		return c.WriteHealthIncidentTable(incidents)
	}
	ApplyNumberKeyStyle(c.StdoutLogger(), "count")
	event := c.ResultEvent(command)
	event = event.Str("count", "0")
	c.FinishResult(event, command, pagination)
	return nil
}

func (c *CommandContext) WriteWorkspaces(command string, workspaces []config.WorkspaceProfile, pagination *Pagination) error {
	if len(workspaces) > 0 {
		return c.WriteWorkspaceTable(workspaces)
	}
	event := c.ResultEvent(command)
	c.FinishResult(event, command, pagination)
	return nil
}

func (c *CommandContext) WriteString(message string) error {
	if !strings.HasSuffix(message, "\n") {
		message += "\n"
	}
	return c.writeBytes(c.OutWriter(), []byte(message))
}

func (c *CommandContext) WriteError(err CLIError) int {
	if c.Mode == RenderModePlain {
		return c.writePlainError(err)
	} else {
		command := c.Command
		if command == "" {
			command = "slick"
		}
		if writeErr := c.WriteJSON(Envelope{
			OK:     false,
			Meta:   EnvelopeMeta{Command: command, Workspace: c.WorkspaceName(), Timestamp: c.Now().Format(time.RFC3339), RequestID: c.RequestID(), ExitCode: err.ExitCode},
			Data:   nil,
			Errors: []CLIError{err},
		}); writeErr != nil {
			return ExitCodeIO
		}
	}
	return err.ExitCode
}

func WriteCommandError(ctx *CommandContext, err CLIError) error {
	if code := ctx.WriteError(err); code == ExitCodeIO {
		return CommandError{CLIError: CLIError{Type: ErrorTypeIO, Message: "output_write_failed", ExitCode: code}}
	}
	return CommandError{CLIError: err}
}

func TruncateText(value string, limit int) string {
	return termansi.Truncate(value, limit, "...")
}

func TextPreview(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len([]rune(value)) <= limit {
		return value
	}
	if limit == 1 {
		return "…"
	}
	out := make([]rune, 0, limit)
	for _, r := range value {
		if len(out) == limit-1 {
			break
		}
		out = append(out, r)
	}
	return string(out) + "…"
}
