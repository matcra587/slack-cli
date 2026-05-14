package output

import (
	"strings"

	"github.com/matcra587/slack-cli/internal/cli/cliutil"
	slackgo "github.com/slack-go/slack"
)

type Message struct {
	Type       string            `json:"type,omitempty"`
	Subtype    *string           `json:"subtype,omitempty"`
	User       *string           `json:"user,omitempty"`
	BotID      *string           `json:"bot_id,omitempty"`
	Text       *string           `json:"text,omitempty"`
	TS         string            `json:"ts,omitempty"`
	ThreadTS   *string           `json:"thread_ts,omitempty"`
	Channel    *string           `json:"channel,omitempty"`
	Permalink  *string           `json:"permalink,omitempty"`
	ReplyCount *int              `json:"reply_count,omitempty"`
	Replies    []Message         `json:"replies,omitempty"`
	Reactions  []ReactionSummary `json:"reactions,omitempty"`
	Blocks     *slackgo.Blocks   `json:"blocks,omitempty"`
}

type ReactionSummary struct {
	Name  string   `json:"name,omitempty"`
	Count int      `json:"count,omitempty"`
	Users []string `json:"users,omitempty"`
}

type Channel struct {
	ID         string  `json:"id,omitempty"`
	Name       string  `json:"name,omitempty"`
	Type       string  `json:"type,omitempty"`
	IsMember   *bool   `json:"is_member,omitempty"`
	IsIM       *bool   `json:"is_im,omitempty"`
	User       *string `json:"user,omitempty"`
	Topic      *string `json:"topic,omitempty"`
	NumMembers *int    `json:"num_members,omitempty"`
	IsArchived *bool   `json:"is_archived,omitempty"`
}

type User struct {
	ID         string  `json:"id,omitempty"`
	Name       string  `json:"name,omitempty"`
	Deleted    *bool   `json:"deleted,omitempty"`
	Timezone   *string `json:"tz,omitempty"`
	Presence   *string `json:"presence,omitempty"`
	StatusText *string `json:"status_text,omitempty"`
}

type SearchMessage struct {
	Channel   SearchChannel `json:"channel"`
	User      string        `json:"user,omitempty"`
	Text      string        `json:"text,omitempty"`
	TS        string        `json:"ts,omitempty"`
	Permalink string        `json:"permalink,omitempty"`
	Snippet   string        `json:"snippet,omitempty"`
}

type SearchChannel struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

type ScheduledMessage struct {
	ID          string  `json:"id"`
	Channel     string  `json:"channel"`
	ChannelName string  `json:"channel_name,omitempty"`
	ChannelType string  `json:"channel_type,omitempty"`
	ChannelUser *string `json:"channel_user,omitempty"`
	IsDM        *bool   `json:"is_dm,omitempty"`
	PostAt      int64   `json:"post_at"`
	PostAtISO   string  `json:"post_at_iso"`
	TextPreview string  `json:"text_preview,omitempty"`
}

type HealthIncident struct {
	ID          string   `json:"id,omitempty"`
	Title       string   `json:"title,omitempty"`
	Type        string   `json:"type,omitempty"`
	Status      string   `json:"status,omitempty"`
	URL         string   `json:"url,omitempty"`
	DateCreated string   `json:"date_created,omitempty"`
	DateUpdated string   `json:"date_updated,omitempty"`
	Services    []string `json:"services,omitempty"`
	NoteCount   int      `json:"note_count,omitempty"`
}

func MessageFromSlack(message slackgo.Message, fallbackChannel string) Message {
	out := Message{
		Type: message.Type,
		TS:   message.Timestamp,
	}
	if message.SubType != "" {
		out.Subtype = new(message.SubType)
	}
	if message.User != "" {
		out.User = new(message.User)
	}
	if message.BotID != "" {
		out.BotID = new(message.BotID)
	}
	if message.Text != "" {
		out.Text = new(message.Text)
	}
	if message.ThreadTimestamp != "" {
		out.ThreadTS = new(message.ThreadTimestamp)
	}
	channel := cliutil.FirstNonEmpty(message.Channel, fallbackChannel)
	if channel != "" {
		out.Channel = new(channel)
	}
	if message.Permalink != "" {
		out.Permalink = new(message.Permalink)
	}
	if message.ReplyCount > 0 {
		out.ReplyCount = new(message.ReplyCount)
	}
	out.Reactions = ReactionsFromSlack(message.Reactions)
	if len(message.Blocks.BlockSet) > 0 {
		blocks := message.Blocks
		out.Blocks = &blocks
	}
	return out
}

func ReactionsFromSlack(reactions []slackgo.ItemReaction) []ReactionSummary {
	if len(reactions) == 0 {
		return nil
	}
	out := make([]ReactionSummary, 0, len(reactions))
	for _, reaction := range reactions {
		out = append(out, ReactionSummary{Name: reaction.Name, Count: reaction.Count, Users: reaction.Users})
	}
	return out
}

func ChannelFromSlack(channel slackgo.Channel) Channel {
	out := Channel{
		ID:         channel.ID,
		Name:       channel.Name,
		Type:       conversationType(channel),
		IsMember:   new(channel.IsMember),
		IsIM:       new(channel.IsIM),
		NumMembers: new(channel.NumMembers),
		IsArchived: new(channel.IsArchived),
	}
	if channel.User != "" {
		out.User = new(channel.User)
	}
	if channel.Topic.Value != "" {
		out.Topic = new(channel.Topic.Value)
	}
	return out
}

func conversationType(channel slackgo.Channel) string {
	if channel.IsIM {
		return "im"
	}
	if channel.IsPrivate {
		return "private_channel"
	}
	return "channel"
}

func UserFromSlack(user slackgo.User) User {
	out := User{
		ID:       user.ID,
		Name:     user.Name,
		Deleted:  new(user.Deleted),
		Timezone: new(user.TZ),
	}
	if user.Presence != "" {
		out.Presence = new(user.Presence)
	}
	if user.Profile.StatusText != "" {
		out.StatusText = new(user.Profile.StatusText)
	}
	return out
}

func SearchMessageFromSlack(message slackgo.SearchMessage) SearchMessage {
	text := message.Text
	if text == "" {
		// Slack's search.messages leaves Text empty for Block Kit
		// messages and places the body inside Blocks. Walk the blocks
		// to recover something readable. Only the common section /
		// header / context shapes are flattened; richer block types
		// fall through silently rather than producing garbled output.
		text = searchTextFromBlocks(message.Blocks)
	}
	return SearchMessage{
		Channel: SearchChannel{
			ID:   message.Channel.ID,
			Name: message.Channel.Name,
		},
		User:      message.User,
		Text:      text,
		TS:        message.Timestamp,
		Permalink: message.Permalink,
	}
}

// searchTextFromBlocks pulls human-readable text out of a Block Kit
// payload returned by search.messages. Handles the block shapes Slack
// emits in practice:
//
//   - section block:        text.text
//   - header block:         text.text
//   - context block:        each mrkdwn element's text
//   - rich_text block:      flattened section/quote/preformatted/list
//     content, with inline mention/emoji/link tokens preserved in
//     readable form
//
// Joined with newlines so multi-block messages stay legible.
func searchTextFromBlocks(blocks slackgo.Blocks) string {
	var parts []string
	for _, block := range blocks.BlockSet {
		switch b := block.(type) {
		case *slackgo.SectionBlock:
			if b.Text != nil && b.Text.Text != "" {
				parts = append(parts, b.Text.Text)
			}
		case *slackgo.HeaderBlock:
			if b.Text != nil && b.Text.Text != "" {
				parts = append(parts, b.Text.Text)
			}
		case *slackgo.ContextBlock:
			for _, el := range b.ContextElements.Elements {
				if t, ok := el.(*slackgo.TextBlockObject); ok && t.Text != "" {
					parts = append(parts, t.Text)
				}
			}
		case *slackgo.RichTextBlock:
			if text := richTextBlockToString(b); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

// richTextBlockToString flattens a RichTextBlock — the format Slack's
// composer uses for human-typed messages — into a readable string.
// Each top-level child (section, quote, preformatted, list) renders on
// its own line; lists prefix each item with "- ".
func richTextBlockToString(block *slackgo.RichTextBlock) string {
	var parts []string
	for _, el := range block.Elements {
		if text := richTextElementToString(el); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func richTextElementToString(el slackgo.RichTextElement) string {
	switch e := el.(type) {
	case *slackgo.RichTextSection:
		return richTextSectionElementsToString(e.Elements)
	case *slackgo.RichTextQuote:
		body := richTextSectionElementsToString(e.Elements)
		if body == "" {
			return ""
		}
		return "> " + body
	case *slackgo.RichTextPreformatted:
		return richTextSectionElementsToString(e.Elements)
	case *slackgo.RichTextList:
		var items []string
		for _, item := range e.Elements {
			if text := richTextElementToString(item); text != "" {
				items = append(items, "- "+text)
			}
		}
		return strings.Join(items, "\n")
	}
	return ""
}

func richTextSectionElementsToString(elems []slackgo.RichTextSectionElement) string {
	var b strings.Builder
	for _, el := range elems {
		switch e := el.(type) {
		case *slackgo.RichTextSectionTextElement:
			b.WriteString(e.Text)
		case *slackgo.RichTextSectionLinkElement:
			if e.Text != "" {
				b.WriteString(e.Text)
			} else {
				b.WriteString(e.URL)
			}
		case *slackgo.RichTextSectionEmojiElement:
			b.WriteString(":" + e.Name + ":")
		case *slackgo.RichTextSectionUserElement:
			b.WriteString("<@" + e.UserID + ">")
		case *slackgo.RichTextSectionChannelElement:
			b.WriteString("<#" + e.ChannelID + ">")
		case *slackgo.RichTextSectionBroadcastElement:
			b.WriteString("<!" + e.Range + ">")
		}
	}
	return b.String()
}

// invalidNameMessage returns a context-tailored message for Slack's
// invalid_name code. The caller hint (e.g. "emoji", "channel", "user")
// drives the wording so the user knows which field Slack rejected.
func invalidNameMessage(subject string) string {
	switch subject {
	case "emoji":
		return "invalid_name: Slack does not recognize that emoji name"
	case "channel":
		return "invalid_name: channel name is malformed or already in use"
	case "user", "username":
		return "invalid_name: username is invalid"
	case "file":
		return "invalid_name: file name is invalid"
	case "":
		return "invalid_name: Slack rejected the value as not recognized"
	default:
		return "invalid_name: Slack rejected the " + subject + " as not recognized"
	}
}
