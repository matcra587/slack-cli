package main

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"

	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/ratelimit"
	slackgo "github.com/slack-go/slack"
	"github.com/spf13/cobra"
)

type cliMessage struct {
	Type       string               `json:"type,omitempty"`
	Subtype    *string              `json:"subtype,omitempty"`
	User       *string              `json:"user,omitempty"`
	BotID      *string              `json:"bot_id,omitempty"`
	Text       *string              `json:"text,omitempty"`
	TS         string               `json:"ts,omitempty"`
	ThreadTS   *string              `json:"thread_ts,omitempty"`
	Channel    *string              `json:"channel,omitempty"`
	Permalink  *string              `json:"permalink,omitempty"`
	ReplyCount *int                 `json:"reply_count,omitempty"`
	Replies    []cliMessage         `json:"replies,omitempty"`
	Reactions  []cliReactionSummary `json:"reactions,omitempty"`
}

type cliReactionSummary struct {
	Name  string   `json:"name,omitempty"`
	Count int      `json:"count,omitempty"`
	Users []string `json:"users,omitempty"`
}

type cliChannel struct {
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

type cliUser struct {
	ID         string  `json:"id,omitempty"`
	Name       string  `json:"name,omitempty"`
	Deleted    *bool   `json:"deleted,omitempty"`
	Timezone   *string `json:"tz,omitempty"`
	Presence   *string `json:"presence,omitempty"`
	StatusText *string `json:"status_text,omitempty"`
}

type cliSearchMessage struct {
	Channel   cliSearchChannel `json:"channel"`
	User      string           `json:"user,omitempty"`
	Text      string           `json:"text,omitempty"`
	TS        string           `json:"ts,omitempty"`
	Permalink string           `json:"permalink,omitempty"`
	Snippet   string           `json:"snippet,omitempty"`
}

type cliSearchChannel struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

type uploadFileResult struct {
	File    cliFile `json:"file"`
	Channel string  `json:"channel"`
	DryRun  bool    `json:"dry_run,omitempty"`
}

type cliFile struct {
	ID        string  `json:"id,omitempty"`
	Name      string  `json:"name,omitempty"`
	Size      int     `json:"size,omitempty"`
	Permalink *string `json:"permalink,omitempty"`
}

type reactionTarget struct {
	Channel   string `json:"channel"`
	Timestamp string `json:"timestamp"`
}

type reactionResult struct {
	Channel   string `json:"channel"`
	Timestamp string `json:"timestamp"`
	Emoji     string `json:"emoji,omitempty"`
	Removed   bool   `json:"removed,omitempty"`
	DryRun    bool   `json:"dry_run,omitempty"`
}

var errNotOwned = errors.New("message is not owned by authenticated actor")

func slackClient(cmd *cobra.Command, profile config.WorkspaceProfile, runtime *RootRuntime) (*slackgo.Client, error) {
	resolver := runtime.TokenResolver
	if resolver == nil {
		resolver = CredentialTokenResolver{
			Store:        runtime.CredentialStore,
			SlackBaseURL: runtime.SlackBaseURL,
			HTTPClient:   runtime.HTTPClient,
			Now:          runtime.Now,
		}
	}
	token, err := resolver.ResolveToken(profile)
	if err != nil {
		if errors.Is(err, config.ErrCredentialNotFound) {
			workspace := profile.Name
			if workspace == "" {
				workspace = "selected workspace"
			}
			return nil, fmt.Errorf("workspace %s is not authenticated; run slack auth login or switch to an authenticated workspace: %w", workspace, err)
		}
		return nil, err
	}

	options := []slackgo.Option{
		slackgo.OptionHTTPClient(slackHTTPClient(cmd, runtime)),
		slackgo.OptionRetryConfig(slackRetryConfig()),
	}
	if runtime.SlackBaseURL != "" {
		options = append(options, slackgo.OptionAPIURL(slackAPIURL(runtime.SlackBaseURL)))
	}
	return slackgo.New(token, options...), nil
}

func slackHTTPClient(cmd *cobra.Command, runtime *RootRuntime) *http.Client {
	base := http.DefaultClient
	if runtime.HTTPClient != nil {
		base = runtime.HTTPClient
	}
	client := *base
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	noThrottle, _ := cmd.Root().PersistentFlags().GetBool("no-throttle")
	client.Transport = rateLimitTransport{
		base:      transport,
		throttler: ratelimit.NewThrottler(),
		disabled:  noThrottle,
	}
	return &client
}

type rateLimitTransport struct {
	base      http.RoundTripper
	throttler *ratelimit.Throttler
	disabled  bool
}

func (t rateLimitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !t.disabled && strings.HasPrefix(req.URL.Path, "/api/") {
		method := strings.TrimPrefix(req.URL.Path, "/api/")
		if err := t.throttler.Wait(req.Context(), ratelimit.TierForMethod(method)); err != nil {
			return nil, err
		}
	}
	return t.base.RoundTrip(req)
}

func slackRetryConfig() slackgo.RetryConfig {
	cfg := slackgo.DefaultRetryConfig()
	cfg.MaxRetries = 2
	cfg.RetryAfterJitter = 0
	cfg.BackoffJitter = 0
	cfg.Handlers = slackgo.DefaultRetryHandlers(cfg)
	return cfg
}

func slackAPIURL(baseURL string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(baseURL, "/api") {
		return baseURL + "/"
	}
	return baseURL + "/api/"
}

func cliErrorFromSlack(err error) CLIError {
	if errors.Is(err, errNotOwned) {
		return CLIError{Type: ErrorTypeValidation, Message: err.Error(), ExitCode: ExitCodeValidation}
	}
	var rateErr *slackgo.RateLimitedError
	if errors.As(err, &rateErr) {
		seconds := int(math.Ceil(rateErr.RetryAfter.Seconds()))
		if seconds < 0 {
			seconds = 0
		}
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
		case "channel_not_found", "user_not_found", "message_not_found":
			return CLIError{Type: ErrorTypeNotFound, Message: slackErr.Err, ExitCode: ExitCodeNotFound}
		case "ratelimited":
			return CLIError{Type: ErrorTypeRateLimit, Message: slackErr.Err, ExitCode: ExitCodeRateLimit}
		default:
			return CLIError{Type: ErrorTypeServer, Message: slackErr.Err, ExitCode: ExitCodeServer}
		}
	}
	return CLIError{Type: ErrorTypeServer, Message: err.Error(), ExitCode: ExitCodeServer}
}

func cliMessageFromSlack(message slackgo.Message, fallbackChannel string) cliMessage {
	out := cliMessage{
		Type: message.Type,
		TS:   message.Timestamp,
	}
	if message.SubType != "" {
		out.Subtype = stringPtr(message.SubType)
	}
	if message.User != "" {
		out.User = stringPtr(message.User)
	}
	if message.BotID != "" {
		out.BotID = stringPtr(message.BotID)
	}
	if message.Text != "" {
		out.Text = stringPtr(message.Text)
	}
	if message.ThreadTimestamp != "" {
		out.ThreadTS = stringPtr(message.ThreadTimestamp)
	}
	channel := firstNonEmpty(message.Channel, fallbackChannel)
	if channel != "" {
		out.Channel = stringPtr(channel)
	}
	if message.Permalink != "" {
		out.Permalink = stringPtr(message.Permalink)
	}
	if message.ReplyCount > 0 {
		out.ReplyCount = intPtr(message.ReplyCount)
	}
	out.Reactions = cliReactionsFromSlack(message.Reactions)
	return out
}

func cliReactionsFromSlack(reactions []slackgo.ItemReaction) []cliReactionSummary {
	if len(reactions) == 0 {
		return nil
	}
	out := make([]cliReactionSummary, 0, len(reactions))
	for _, reaction := range reactions {
		out = append(out, cliReactionSummary{Name: reaction.Name, Count: reaction.Count, Users: reaction.Users})
	}
	return out
}

func cliChannelFromSlack(channel slackgo.Channel) cliChannel {
	out := cliChannel{
		ID:         channel.ID,
		Name:       channel.Name,
		Type:       conversationType(channel),
		IsMember:   boolPtr(channel.IsMember),
		IsIM:       boolPtr(channel.IsIM),
		NumMembers: intPtr(channel.NumMembers),
		IsArchived: boolPtr(channel.IsArchived),
	}
	if channel.User != "" {
		out.User = stringPtr(channel.User)
	}
	if channel.Topic.Value != "" {
		out.Topic = stringPtr(channel.Topic.Value)
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

func cliUserFromSlack(user slackgo.User) cliUser {
	out := cliUser{
		ID:       user.ID,
		Name:     user.Name,
		Deleted:  boolPtr(user.Deleted),
		Timezone: stringPtr(user.TZ),
	}
	if user.Presence != "" {
		out.Presence = stringPtr(user.Presence)
	}
	if user.Profile.StatusText != "" {
		out.StatusText = stringPtr(user.Profile.StatusText)
	}
	return out
}

func cliSearchMessageFromSlack(message slackgo.SearchMessage) cliSearchMessage {
	return cliSearchMessage{
		Channel: cliSearchChannel{
			ID:   message.Channel.ID,
			Name: message.Channel.Name,
		},
		User:      message.User,
		Text:      message.Text,
		TS:        message.Timestamp,
		Permalink: message.Permalink,
	}
}

func boolPtr(value bool) *bool {
	return &value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
