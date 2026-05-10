package output

import (
	"encoding/json"
	"io"
	"maps"
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
	event.Msg(ActionLabel(command))
	return nil
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
// are the runtime command ids passed to ResultEvent / WriteResult — usually
// the same as the dotted cobra path, except where slick uses a Slack API
// method name (e.g. "search.messages" for the cobra path "lookup.messages").
// Both forms appear here when they differ.
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
	"message.send":      "Message sent",
	"message.edit":      "Message edited",
	"message.delete":    "Message deleted",
	"reply":             "Reply posted",
	"react.add":         "Reaction added",
	"react.remove":      "Reaction removed",
	"react.list":        "Reactions retrieved",
	"history.list":      "History retrieved",
	"lookup.channel":    "Channel resolved",
	"lookup.user":       "User resolved",
	"lookup.messages":   "Messages searched", // cobra-path form (test asserts this)
	"search.messages":   "Messages searched", // runtime form emitted by search.go
	"file.upload":       "File uploaded",
	"status.set":        "Status set",
	"status.clear":      "Status cleared",
	"auth.login":        "Login complete",
	"auth.logout":       "Logout complete",
	"auth.switch":       "Workspace switched",
	"auth.status":       "Auth status retrieved",
	"workspace.list":    "Workspaces listed",
	"config.init":       "Config initialized",
	"config.path":       "Config path resolved",
	"config.list":       "Config listed",
	"config.get":        "Config value retrieved",
	"config.set":        "Config value set",
	"config.unset":      "Config value unset",
	"manifest.template": "Manifest generated",
	"cache.users":       "User cache primed",
	"cache.channels":    "Channel cache primed",
	"cache.clear":       "Cache cleared",
	"agent.guide":       "Guide retrieved",
	"agent.schema":      "Schema retrieved",
	"version":           "Version printed",
}

func (c *CommandContext) WriteMessages(command string, messages []CliMessage, pagination *Pagination) error {
	if len(messages) > 0 {
		return c.WriteMessageTable(messages)
	}
	event := c.ResultEvent(command)
	c.FinishResult(event, command, pagination)
	return nil
}

func (c *CommandContext) WriteSearch(command string, matches []CliSearchMessage, full bool, pagination *Pagination) error {
	if len(matches) > 0 {
		return c.WriteSearchTable(matches, full)
	}
	event := c.ResultEvent(command)
	c.FinishResult(event, command, pagination)
	return nil
}

func (c *CommandContext) WriteChannels(command string, channels []CliChannel, pagination *Pagination) error {
	if len(channels) > 0 {
		return c.WriteChannelTable(command, channels)
	}
	event := c.ResultEvent(command)
	c.FinishResult(event, command, pagination)
	return nil
}

func (c *CommandContext) WriteUsers(command string, users []CliUser, pagination *Pagination) error {
	if len(users) > 0 {
		return c.WriteUserTable(users)
	}
	event := c.ResultEvent(command)
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
