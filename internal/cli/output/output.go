package output

import (
	"hash/fnv"
	"io"
	"maps"
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

// WithDetails attaches structured details to the error and returns the
// updated CLIError so callers can chain builder helpers.
func (e CLIError) WithDetails(details map[string]any) CLIError {
	if len(details) == 0 {
		return e
	}
	merged := make(map[string]any, len(e.Details)+len(details))
	maps.Copy(merged, e.Details)
	maps.Copy(merged, details)
	e.Details = merged
	return e
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

// TrimInputName trims surrounding whitespace from a user-supplied
// identifier and emits a debug log on the given logger when the trim
// changed the input. Use at boundaries where a flag or arg becomes a
// lookup key (workspace name, profile name, etc.) so the user can see
// under --debug exactly when their stray whitespace was normalized.
// Returns the trimmed name.
func TrimInputName(logger *clog.Logger, kind, name string) string {
	trimmed := strings.TrimSpace(name)
	if logger != nil && trimmed != name {
		logger.Debug().
			Str("kind", kind).
			Str("input", name).
			Str("trimmed", trimmed).
			Msg("trimmed user-supplied name")
	}
	return trimmed
}

// Field-rendering convention: WritePlain methods (and any helper that
// terminates a chain with event.Msg) emit fields left-to-right in this
// canonical order so output reads consistently across commands:
//
//  1. where        — the location/identity context (workspace, channel, …)
//  2. what         — the subject (ts, id, name, type, …)
//  3. when         — time of the event (age, fetched_at, …)
//  4. state        — current/result booleans (authenticated, dry_run, …)
//  5. detail       — free-text payload (text, topic, permalink, …)
//  6. numbers      — counts and sizes (count, size, members, …)
//  7. diagnostics  — validation_error
//  8. pagination   — appended last by AddPaginationFields
//
// Action label (Msg) renders FIRST. The TestPlainRendererFieldOrderIsCanonical
// test in field_order_test.go enforces this on every CI run; the canonical
// category table lives there.
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

	// Slack timestamps and time-derived fields render as strings (the
	// Slack-native form like "1778441603.561129" is required for piping
	// back into --timestamp). Style those keys with FieldTime so they
	// match the color clog uses for genuine time.Time values.
	timeKeys := []string{"ts", "age", "fetched_at", "expiration"}
	ApplyTimeKeyStyle(sl, timeKeys...)
	ApplyTimeKeyStyle(el, timeKeys...)

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

// ApplyBoolStateStyle paints key based on an alarm-on-true polarity:
// true=red (alarming), false=dim (routine). Use it for state fields
// where true is the alarming side and false is the routine/expected
// state (e.g. is_archived, deleted user, truncated). Falls back to a
// no-op when the theme is missing.
func ApplyBoolStateStyle(logger *clog.Logger, th *theme.Theme, key string, value bool) {
	if logger == nil || th == nil || key == "" {
		return
	}
	style := th.Dim
	if value {
		style = th.Red
	}
	if style == nil {
		return
	}
	logger.SetStyles(&clogstyle.Config{Keys: clogstyle.Map{key: style}})
}

// ApplyBoolValueStyle paints key green when value is true (a "good" state
// such as authenticated) and red when false. Use only when both sides
// matter — true is the desirable outcome and false is itself alarming
// (action required), e.g. authenticated, exists. For routine-state
// fields where false is merely informational, use ApplyBoolStateStyle
// (true=alarm, false=dim) or ApplyDimWhen (true=dim, false=default).
// Falls back to no-op when the theme is missing.
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

// ApplyDimWhen paints key dim when value is true, otherwise leaves
// styling default. Use for bool fields where true is the routine/
// expected state and false is mildly informational (e.g. is_member:
// being a member is the normal state for any channel you've joined).
func ApplyDimWhen(logger *clog.Logger, th *theme.Theme, key string, value bool) {
	if logger == nil || th == nil || th.Dim == nil || key == "" || !value {
		return
	}
	logger.SetStyles(&clogstyle.Config{Keys: clogstyle.Map{key: th.Dim}})
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

// ApplyTimeKeyStyle paints each named key with clog's default FieldTime
// style (typically magenta). Use when a time-shaped value is emitted as
// a Str — Slack timestamps render as "1778441603.561129", so we keep
// the native form for plumbing back into --timestamp flags rather than
// reformatting through event.Time(...) and losing the round-trip.
func ApplyTimeKeyStyle(logger *clog.Logger, keys ...string) {
	if logger == nil || len(keys) == 0 {
		return
	}
	defaults := clog.DefaultStyles()
	if defaults == nil || defaults.FieldTime == nil {
		return
	}
	styles := clogstyle.Map{}
	for _, key := range keys {
		if key == "" {
			continue
		}
		styles[key] = defaults.FieldTime
	}
	if len(styles) == 0 {
		return
	}
	logger.SetStyles(&clogstyle.Config{Keys: styles})
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
	event = event.Str("ts", ts)
	parsed, ok := parseSlackTimestamp(ts)
	if !ok {
		return event
	}
	event = event.Str("age", human.FormatTimeAgoCompactFrom(parsed, now))
	if clog.IsVerbose() {
		event = event.Time("time", parsed)
	}
	return event
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

// RuntimeCLIError wraps a non-user, non-validation failure (filesystem
// permission errors, keychain backend errors, or other system resources
// the user did not directly control). Distinguishing from
// ValidationCLIError lets callers see "this failed because of the
// system, not because of bad input" without inspecting message text.
// Maps to ErrorTypeServer / ExitCodeServer.
func RuntimeCLIError(message string) CLIError {
	return CLIError{Type: ErrorTypeServer, Message: message, ExitCode: ExitCodeServer}
}
