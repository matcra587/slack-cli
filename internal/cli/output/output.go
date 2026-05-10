package output

import (
	"hash/fnv"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/gechr/clib/theme"
	"github.com/gechr/clog"
	clogstyle "github.com/gechr/clog/style"
	"github.com/gechr/x/human"
)

type RenderMode int

const (
	RenderModePlain    RenderMode = iota // human-readable clog fields
	RenderModeEnvelope                   // JSON with meta envelope (default non-TTY)
	RenderModeCompact                    // JSON data only, no envelope
	RenderModeRaw                        // raw Slack JSON pass-through
)

type OutputFlags struct {
	JSON    bool
	Plain   bool
	Compact bool
	Raw     bool
}

func (f OutputFlags) Resolve(isTTY, agentMode bool) RenderMode {
	switch {
	case f.Raw:
		return RenderModeRaw
	case f.Compact:
		return RenderModeCompact
	case f.Plain:
		return RenderModePlain
	case f.JSON || !isTTY || agentMode:
		return RenderModeEnvelope
	default:
		return RenderModePlain
	}
}

const (
	ExitCodeAuthFailure = 1
	ExitCodeNotFound    = 2
	ExitCodeRateLimit   = 3
	ExitCodeValidation  = 4
	ExitCodeServer      = 5
	ExitCodeCanceled    = 6
	ExitCodeTimeout     = 7
)

const (
	ErrorTypeAuth       = "auth_failure"
	ErrorTypeNotFound   = "not_found"
	ErrorTypeRateLimit  = "rate_limit"
	ErrorTypeValidation = "validation_error"
	ErrorTypeServer     = "server_error"
	ErrorTypeCanceled   = "canceled"
	ErrorTypeTimeout    = "timeout"
)

type Envelope struct {
	Meta   EnvelopeMeta `json:"meta"`
	Data   any          `json:"data"`
	Errors []CLIError   `json:"errors"`
}

type EnvelopeMeta struct {
	Command    string      `json:"command"`
	Workspace  string      `json:"workspace"`
	Timestamp  string      `json:"timestamp"`
	RequestID  string      `json:"request_id"`
	Pagination *Pagination `json:"pagination,omitempty"`
}

type Pagination struct {
	Cursor        *string `json:"cursor,omitempty"`
	NextCursor    *string `json:"next_cursor,omitempty"`
	HasMore       bool    `json:"has_more"`
	MaxItems      *int    `json:"max_items,omitempty"`
	ItemsReturned *int    `json:"items_returned,omitempty"`
}

type CLIError struct {
	Type              string         `json:"type"`
	Message           string         `json:"message"`
	Details           map[string]any `json:"details,omitempty"`
	RetryAfterSeconds *int           `json:"retry_after_seconds,omitempty"`
	ExitCode          int            `json:"exit_code"`
}

type CommandError struct {
	CLIError CLIError
}

func (e CommandError) Error() string {
	return e.CLIError.Message
}

type FieldStyle struct {
	Field string
	Seed  string
}

func EntityFieldStyle(field, value string) FieldStyle {
	return FieldStyle{Field: field, Seed: field + ":" + value}
}

func BuildBaseLoggers(stdout, stderr io.Writer, colorMode clog.ColorMode) (*clog.Logger, *clog.Logger) {
	sl := clog.New(clog.NewOutput(stdout, colorMode))
	sl.SetOmitZero(true)
	// Success events on stdout read as actions ("Message sent  ts=...") with
	// no level prefix. Warning and error events still go through stderr's
	// logger which keeps the slog-style level prefix.
	sl.SetParts(clog.PartMessage, clog.PartFields)

	el := clog.New(clog.NewOutput(stderr, colorMode))
	el.SetOmitZero(true)
	el.SetParts(clog.PartLevel, clog.PartMessage, clog.PartFields)
	el.SetNonTTYLevel(clog.LevelWarn)
	el.SetJSONPrintMode(clog.JSONFlat)

	return sl, el
}

// underlinedHyperlink is the style applied to the visible text of a
// terminal hyperlink so the click affordance is obvious even when the
// terminal does not auto-underline OSC 8 hyperlinks. Used by HyperlinkText.
var underlinedHyperlink = lipgloss.NewStyle().Underline(true)

// HyperlinkText returns text rendered with the slick hyperlink underline
// style. Pass the result as the third argument to clog's event.Link so the
// underlined text sits inside the OSC 8 wrapper rather than around it
// (lipgloss does not understand OSC 8 byte sequences and would otherwise
// style each escape byte individually).
func HyperlinkText(text string) string { return underlinedHyperlink.Render(text) }

func ApplyRenderMode(sl *clog.Logger, mode RenderMode) {
	switch mode {
	case RenderModeRaw:
		sl.SetJSONPrintMode(clog.JSONPreserve)
	case RenderModeCompact, RenderModeEnvelope:
		sl.SetJSONPrintMode(clog.JSONFlat)
	}
	// RenderModePlain: no JSON print mode needed; logger emits human-readable clog events.
}

func ApplyTeamIDStyle(logger *clog.Logger, th *theme.Theme, teamID string) {
	ApplyFieldStyles(logger, th, EntityFieldStyle("team_id", teamID))
}

func ApplyFieldStyles(logger *clog.Logger, th *theme.Theme, fields ...FieldStyle) {
	styles := clogstyle.Map{}
	for _, field := range fields {
		if field.Field == "" || field.Seed == "" {
			continue
		}
		if style := HashEntityStyle(th, field.Seed); style != nil {
			styles[field.Field] = style
		}
	}
	if len(styles) > 0 {
		logger.SetStyles(&clogstyle.Config{Keys: styles})
	}
}

// HashedFieldStyle returns a FieldStyle that hashes the seed directly to
// pick from the theme's entity color palette. Use it to make two distinct
// fields share a color (e.g. `name` and `user` for the same user).
func HashedFieldStyle(field, seed string) FieldStyle {
	return FieldStyle{Field: field, Seed: seed}
}

// ApplyPreStyledKey suppresses clog's FieldString styling on key so a
// caller-rendered ANSI value passes through unchanged.
func ApplyPreStyledKey(logger *clog.Logger, key string) {
	if logger == nil || key == "" {
		return
	}
	identity := lipgloss.NewStyle()
	logger.SetStyles(&clogstyle.Config{Keys: clogstyle.Map{key: &identity}})
}

// ApplyBoolStateStyle paints key red when value is true (a "bad" state
// such as archived) and green when false. Falls back to no-op when the
// theme is missing.
func ApplyBoolStateStyle(logger *clog.Logger, th *theme.Theme, key string, value bool) {
	if logger == nil || th == nil || key == "" {
		return
	}
	style := th.Green
	if value {
		style = th.Red
	}
	if style == nil {
		return
	}
	logger.SetStyles(&clogstyle.Config{Keys: clogstyle.Map{key: style}})
}

// ApplyBoolValueStyle paints key green when value is true (a "good" state
// such as authenticated) and red when false. This is the inverse of
// ApplyBoolStateStyle; use it for fields where true is the desirable
// outcome. Falls back to no-op when the theme is missing.
func ApplyBoolValueStyle(logger *clog.Logger, th *theme.Theme, key string, value bool) {
	if logger == nil || th == nil || key == "" {
		return
	}
	style := th.Red
	if value {
		style = th.Green
	}
	if style == nil {
		return
	}
	logger.SetStyles(&clogstyle.Config{Keys: clogstyle.Map{key: style}})
}

// ApplyNumberKeyStyle paints key with clog's default FieldNumber style
// (typically magenta). Useful when the value is rendered as a string but
// should look like a numeric field (e.g. count=0 surviving OmitZero).
func ApplyNumberKeyStyle(logger *clog.Logger, key string) {
	if logger == nil || key == "" {
		return
	}
	defaults := clog.DefaultStyles()
	if defaults == nil || defaults.FieldNumber == nil {
		return
	}
	logger.SetStyles(&clogstyle.Config{Keys: clogstyle.Map{key: defaults.FieldNumber}})
}

// ApplyConfigValueStyle paints key based on the config value: green/red
// for bools, dim for unset, otherwise leaves it to the default styling.
func ApplyConfigValueStyle(logger *clog.Logger, th *theme.Theme, key, value string) {
	if logger == nil || th == nil || key == "" {
		return
	}
	var style *lipgloss.Style
	switch value {
	case "":
		style = th.Dim
	case "true":
		style = th.Green
	case "false":
		style = th.Red
	}
	if style == nil {
		return
	}
	logger.SetStyles(&clogstyle.Config{Keys: clogstyle.Map{key: style}})
}

// RenderTimezone renders an IANA "Region/City" timezone with the region
// dim and the city bold. Returns the input unchanged when the value has
// no slash, or when the theme lacks the required Bold/Dim styles.
func RenderTimezone(th *theme.Theme, value string) string {
	if th == nil || th.Dim == nil || th.Bold == nil {
		return value
	}
	region, city, ok := strings.Cut(value, "/")
	if !ok {
		return value
	}
	return th.Dim.Render(region+"/") + th.Bold.Render(city)
}

func HashEntityStyle(th *theme.Theme, key string) *lipgloss.Style {
	if th == nil {
		th = theme.Default()
	}
	if len(th.EntityColors) == 0 || strings.TrimSpace(key) == "" {
		return nil
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(strings.TrimSpace(key))))
	style := lipgloss.NewStyle().Foreground(th.EntityColors[h.Sum32()%uint32(len(th.EntityColors))])
	return &style
}

func AddPaginationFields(event *clog.Event, pagination *Pagination) *clog.Event {
	if pagination == nil {
		return event
	}
	return event.
		When(pagination.Cursor != nil, func(e *clog.Event) {
			e.Str("cursor", *pagination.Cursor)
		}).
		When(pagination.NextCursor != nil, func(e *clog.Event) {
			e.Str("next_cursor", *pagination.NextCursor)
		}).
		Bool("has_more", pagination.HasMore).
		When(pagination.MaxItems != nil, func(e *clog.Event) {
			e.Int("max_items", *pagination.MaxItems)
		}).
		When(pagination.ItemsReturned != nil, func(e *clog.Event) {
			e.Int("items_returned", *pagination.ItemsReturned)
		})
}

func AddSlackTimestampFields(event *clog.Event, ts string, now time.Time) *clog.Event {
	return event.Str("ts", ts).
		When(clog.IsVerbose(), func(e *clog.Event) {
			parsed, ok := parseSlackTimestamp(ts)
			if !ok {
				return
			}
			e.Time("time", parsed).
				Str("age", human.FormatTimeAgoCompactFrom(parsed, now))
		})
}

func parseSlackTimestamp(ts string) (time.Time, bool) {
	secondsText, fractionText, ok := strings.Cut(strings.TrimSpace(ts), ".")
	if !ok {
		return time.Time{}, false
	}
	seconds, err := strconv.ParseInt(secondsText, 10, 64)
	if err != nil || seconds < 0 {
		return time.Time{}, false
	}
	if len(fractionText) > 9 {
		fractionText = fractionText[:9]
	}
	for len(fractionText) < 9 {
		fractionText += "0"
	}
	nanos, err := strconv.ParseInt(fractionText, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(seconds, nanos).UTC(), true
}

func AddBoolField(event *clog.Event, key string, value *bool) *clog.Event {
	return event.When(value != nil, func(e *clog.Event) {
		e.Bool(key, *value)
	})
}

func AddIntField(event *clog.Event, key string, value *int) *clog.Event {
	return event.When(value != nil, func(e *clog.Event) {
		e.Int(key, *value)
	})
}

func AddCLIErrorDetails(event *clog.Event, details map[string]any) *clog.Event {
	if len(details) == 0 {
		return event
	}
	keys := make([]string, 0, len(details))
	for key := range details {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		switch value := details[key].(type) {
		case string:
			event = event.Str(key, value)
		case bool:
			event = event.Bool(key, value)
		case int:
			event = event.Int(key, value)
		default:
			event = event.Any(key, value)
		}
	}
	return event
}

func ValidationCLIError(message string) CLIError {
	return CLIError{Type: ErrorTypeValidation, Message: message, ExitCode: ExitCodeValidation}
}

func AuthCLIError(message string) CLIError {
	return CLIError{Type: ErrorTypeAuth, Message: message, ExitCode: ExitCodeAuthFailure}
}
