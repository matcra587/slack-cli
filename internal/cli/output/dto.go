package output

import (
	"context"
	"errors"
	"math"
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
	return SearchMessage{
		Channel: SearchChannel{
			ID:   message.Channel.ID,
			Name: message.Channel.Name,
		},
		User:      message.User,
		Text:      message.Text,
		TS:        message.Timestamp,
		Permalink: message.Permalink,
	}
}

func CliErrorFromSlack(ctx context.Context, err error) CLIError {
	var scopeErr MissingScopeError
	if errors.As(err, &scopeErr) {
		return CLIError{Type: ErrorTypeAuth, Message: scopeErr.Error(), ExitCode: ExitCodeAuthFailure}
	}
	var rateErr *slackgo.RateLimitedError
	if errors.As(err, &rateErr) {
		seconds := max(int(math.Ceil(rateErr.RetryAfter.Seconds())), 0)
		return CLIError{
			Type:              ErrorTypeRateLimit,
			Message:           "ratelimited",
			RetryAfterSeconds: &seconds,
			ExitCode:          ExitCodeRateLimit,
		}
	}
	var slackErr slackgo.SlackErrorResponse
	if errors.As(err, &slackErr) {
		switch slackErr.Err {
		case "channel_not_found", "user_not_found", "message_not_found", "not_in_channel":
			return CLIError{Type: ErrorTypeNotFound, Message: slackErr.Err, ExitCode: ExitCodeNotFound}
		case "not_allowed_token_type", "invalid_arguments", "cant_update_message", "cant_delete_message":
			return CLIError{Type: ErrorTypeValidation, Message: slackErr.Err, ExitCode: ExitCodeValidation}
		case "invalid_auth", "not_authed", "account_inactive", "token_revoked", "missing_scope", "no_permission":
			return CLIError{Type: ErrorTypeAuth, Message: slackErr.Err, ExitCode: ExitCodeAuthFailure}
		case "ratelimited":
			return CLIError{Type: ErrorTypeRateLimit, Message: slackErr.Err, ExitCode: ExitCodeRateLimit}
		default:
			return CLIError{Type: ErrorTypeServer, Message: slackErr.Err, ExitCode: ExitCodeServer}
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || ctx.Err() == context.DeadlineExceeded {
		return CLIError{Type: ErrorTypeTimeout, Message: "timeout", ExitCode: ExitCodeTimeout}
	}
	// context.Canceled covers both explicit cancel and signal cancellation;
	// errors.Is against context.Cause catches signal.signalError wrapped by url.Error.
	if ctx.Err() == context.Canceled || errors.Is(err, context.Cause(ctx)) {
		return CLIError{Type: ErrorTypeCanceled, Message: "canceled", ExitCode: ExitCodeCanceled}
	}
	return CLIError{Type: ErrorTypeServer, Message: err.Error(), ExitCode: ExitCodeServer}
}

// MissingScopeError is returned by scope-checking helpers when required Slack
// OAuth scopes are absent. Defined here so CliErrorFromSlack can match it via
// errors.As across packages.
type MissingScopeError struct {
	All []string
	Any []string
}

func (e MissingScopeError) Error() string {
	if len(e.All) == 1 {
		return "missing required Slack scope: " + e.All[0]
	}
	if len(e.All) > 1 {
		return "missing required Slack scopes: " + strings.Join(e.All, ",")
	}
	return "missing one of required Slack scopes: " + strings.Join(e.Any, ",")
}
