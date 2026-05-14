package output

import (
	"net/url"
	goruntime "runtime"
	"strings"

	"github.com/gechr/clog"
	"github.com/gechr/primer/table"
	termansi "github.com/gechr/x/ansi"
)

// SlackConversationRef carries the metadata needed to render a Slack
// conversation without replacing the raw Slack ID that automation consumes.
type SlackConversationRef struct {
	ID   string
	Name string
	Type string
	User string
	IsDM *bool
}

type SlackConversationFields struct {
	ChannelName string `json:"channel_name,omitempty"`
	ChannelHR   string `json:"channel_hr,omitempty"`
	ChannelURL  string `json:"channel_url,omitempty"`
}

func SlackConversationRefFromChannel(channel Channel) SlackConversationRef {
	ref := SlackConversationRef{
		ID:   channel.ID,
		Name: channel.Name,
		Type: channel.Type,
		IsDM: channel.IsIM,
	}
	if channel.User != nil {
		ref.User = *channel.User
	}
	return ref
}

func SlackConversationRefFromScheduled(message ScheduledMessage) SlackConversationRef {
	ref := SlackConversationRef{
		ID:   message.Channel,
		Name: message.ChannelName,
		Type: message.ChannelType,
		IsDM: message.IsDM,
	}
	if message.ChannelUser != nil {
		ref.User = *message.ChannelUser
	}
	return ref
}

func SlackConversationRefFromSearch(channel SearchChannel) SlackConversationRef {
	return SlackConversationRef{ID: channel.ID, Name: channel.Name}
}

// DisplayText returns the compact human label for a Slack conversation:
// #channel for channels, @user for DMs, and the raw ID when metadata is
// unavailable.
func (r SlackConversationRef) DisplayText() string {
	name := strings.TrimSpace(r.Name)
	if name == "" && r.isDirectMessage() {
		name = strings.TrimSpace(r.User)
	}
	if name == "" {
		return strings.TrimSpace(r.ID)
	}
	if r.isDirectMessage() {
		if strings.HasPrefix(name, "@") {
			return name
		}
		return "@" + name
	}
	if isChannelConversation(r) {
		if strings.HasPrefix(name, "#") {
			return name
		}
		return "#" + name
	}
	return name
}

func (r SlackConversationRef) IsDirectMessage() bool {
	return r.isDirectMessage()
}

func (r SlackConversationRef) HasFriendlyDisplay() bool {
	text := r.DisplayText()
	return text != "" && text != strings.TrimSpace(r.ID)
}

func (r SlackConversationRef) SlackName() string {
	name := strings.TrimSpace(r.Name)
	if name == "" && r.isDirectMessage() {
		name = strings.TrimSpace(r.User)
	}
	if strings.HasPrefix(name, "#") || strings.HasPrefix(name, "@") {
		return name[1:]
	}
	return name
}

func ShouldShowSlackConversationField(ref SlackConversationRef, verbose bool) bool {
	return verbose || !ref.IsDirectMessage() || ref.HasFriendlyDisplay()
}

func (r SlackConversationRef) isDirectMessage() bool {
	if r.Type == "mpim" {
		return false
	}
	if r.IsDM != nil && *r.IsDM {
		return true
	}
	switch r.Type {
	case "im":
		return true
	}
	return strings.HasPrefix(strings.TrimSpace(r.ID), "D")
}

func isChannelConversation(r SlackConversationRef) bool {
	switch r.Type {
	case "channel", "private_channel":
		return true
	}
	id := strings.TrimSpace(r.ID)
	return strings.HasPrefix(id, "C") || strings.HasPrefix(id, "G")
}

// SlackConversationURL returns a clickable Slack URL for a conversation.
// macOS gets Slack's native scheme when the docs-required team ID is known.
// Other platforms open Slack's web app directly. Without a team ID, slick
// cannot build a workspace-specific link and leaves the field as plain text.
func (c *CommandContext) SlackConversationURL(ref SlackConversationRef) string {
	channelID := strings.TrimSpace(ref.ID)
	if channelID == "" || !isSlackConversationID(channelID) {
		return ""
	}
	teamID := ""
	if c != nil {
		teamID = strings.TrimSpace(c.TeamID)
	}
	if teamID == "" {
		return ""
	}
	if c.goos() == "darwin" {
		if ref.isDirectMessage() && isSlackUserID(strings.TrimSpace(ref.User)) {
			return "slack://user?team=" + url.QueryEscape(teamID) + "&id=" + url.QueryEscape(strings.TrimSpace(ref.User))
		}
		if !ref.isDirectMessage() {
			return "slack://channel?team=" + url.QueryEscape(teamID) + "&id=" + url.QueryEscape(channelID)
		}
	}
	return slackWebAppURL(teamID, channelID)
}

func slackWebAppURL(teamID, channelID string) string {
	return "https://app.slack.com/client/" + url.PathEscape(teamID) + "/" + url.PathEscape(channelID)
}

func isSlackConversationID(value string) bool {
	switch {
	case strings.HasPrefix(value, "C"), strings.HasPrefix(value, "G"), strings.HasPrefix(value, "D"):
		return true
	default:
		return false
	}
}

func isSlackUserID(value string) bool {
	return strings.HasPrefix(value, "U") || strings.HasPrefix(value, "W")
}

func (c *CommandContext) goos() string {
	if c != nil && c.GOOS != "" {
		return c.GOOS
	}
	return goruntime.GOOS
}

// AddSlackConversationField emits a clog field for a conversation. The
// visible value is friendly when metadata is available, and OSC8-linked
// when the context has enough workspace metadata to build a Slack URL.
func AddSlackConversationField(event *clog.Event, c *CommandContext, key string, ref SlackConversationRef) *clog.Event {
	if event == nil {
		return event
	}
	text := ref.DisplayText()
	if text == "" {
		return event
	}
	if c == nil {
		return event.Str(key, text)
	}
	if link := c.SlackConversationURL(ref); link != "" {
		return event.Link(key, link, c.HyperlinkText(text))
	}
	return event.Str(key, text)
}

func (c *CommandContext) SlackConversationFields(ref SlackConversationRef) SlackConversationFields {
	fields := SlackConversationFields{
		ChannelName: ref.SlackName(),
		ChannelURL:  c.SlackConversationURL(ref),
	}
	if ref.HasFriendlyDisplay() {
		fields.ChannelHR = ref.DisplayText()
	}
	return fields
}

func (c *CommandContext) EnrichMessageConversation(message *Message, ref SlackConversationRef) {
	if message == nil {
		return
	}
	if ref.ID == "" && message.Channel != nil {
		ref.ID = *message.Channel
	}
	message.SlackConversationFields = c.SlackConversationFields(ref)
}

func (c *CommandContext) EnrichChannelConversation(channel *Channel) {
	if channel == nil {
		return
	}
	fields := c.SlackConversationFields(SlackConversationRefFromChannel(*channel))
	channel.HR = fields.ChannelHR
	channel.URL = fields.ChannelURL
}

func (c *CommandContext) EnrichSearchChannel(channel *SearchChannel) {
	if channel == nil {
		return
	}
	ref := SlackConversationRefFromSearch(*channel)
	fields := c.SlackConversationFields(ref)
	channel.HR = fields.ChannelHR
	channel.URL = fields.ChannelURL
}

func (c *CommandContext) EnrichScheduledConversation(message *ScheduledMessage, ref SlackConversationRef) {
	if message == nil {
		return
	}
	if ref.ID == "" {
		ref = SlackConversationRefFromScheduled(*message)
	}
	fields := c.SlackConversationFields(ref)
	if message.ChannelName == "" {
		message.ChannelName = fields.ChannelName
	}
	message.ChannelHR = fields.ChannelHR
	message.ChannelURL = fields.ChannelURL
}

func (c *CommandContext) slackConversationCell(ref SlackConversationRef) table.Cell {
	text := ref.DisplayText()
	return c.slackConversationCellText(ref, text)
}

func (c *CommandContext) slackConversationCellText(ref SlackConversationRef, text string) table.Cell {
	if text == "" {
		return table.TextCell("")
	}
	rendered := text
	link := c.SlackConversationURL(ref)
	if c.tableStyled() {
		if style := HashEntityStyle(c.Theme, "channel:"+ref.ID); style != nil {
			if link != "" {
				withUnderline := style.Underline(true)
				rendered = withUnderline.Render(rendered)
			} else {
				rendered = style.Render(rendered)
			}
		} else if link != "" {
			rendered = HyperlinkText(rendered)
		}
	}
	if link != "" {
		rendered = c.slackTableANSI().Hyperlink(link, rendered)
	}
	if rendered == text {
		return table.TextCell(text)
	}
	return table.StyledCell(rendered, text)
}

func (c *CommandContext) slackTableANSI() *termansi.ANSI {
	return termansi.New(
		termansi.WithTerminal(c.tableStyled()),
		termansi.WithHyperlinkFallback(termansi.HyperlinkFallbackText),
	)
}
