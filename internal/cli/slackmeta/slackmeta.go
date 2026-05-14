// Package slackmeta resolves lightweight Slack metadata for human output.
package slackmeta

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	slackcache "github.com/matcra587/slack-cli/internal/cache"
	"github.com/matcra587/slack-cli/internal/cli/cliutil"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	slackgo "github.com/slack-go/slack"
)

const metadataTTLMinutes = 24 * 60

type usersPayload struct {
	Users []clioutput.User `json:"users"`
}

type channelsPayload struct {
	Channels []clioutput.Channel `json:"channels"`
}

// LoadCachedUsers returns the cached user list for the profile, or
// (nil, false) if the cache is missing, stale, or fails to decode.
func LoadCachedUsers(profile string) ([]clioutput.User, bool) {
	var payload usersPayload
	if !readCache(profile, "users", &payload) {
		return nil, false
	}
	return payload.Users, true
}

// LoadCachedChannels returns the cached channel list for the profile, or
// (nil, false) if the cache is missing, stale, or fails to decode.
func LoadCachedChannels(profile string) ([]clioutput.Channel, bool) {
	var payload channelsPayload
	if !readCache(profile, "channels", &payload) {
		return nil, false
	}
	return payload.Channels, true
}

func readCache(profile, resource string, target any) bool {
	entry, ok, stale, err := slackcache.Read(profile, resource, time.Duration(metadataTTLMinutes)*time.Minute)
	if err != nil || !ok || stale {
		return false
	}
	return json.Unmarshal(entry.Data, target) == nil
}

// ResolveConversation returns best-effort metadata for a conversation ID or
// cached channel name. It checks the local cache first and falls back to
// conversations.info when a Slack client is available.
func ResolveConversation(ctx context.Context, client *slackgo.Client, profile, value string) clioutput.SlackConversationRef {
	value = strings.TrimSpace(value)
	if value == "" {
		return clioutput.SlackConversationRef{}
	}
	if ref, ok := cachedConversation(profile, value); ok {
		ref.Name = cliutil.FirstNonEmpty(ref.Name, cachedUserName(profile, ref.User))
		return ref
	}
	ref := fallbackConversationRef(value)
	if client == nil {
		return ref
	}
	info, err := client.GetConversationInfoContext(ctx, &slackgo.GetConversationInfoInput{ChannelID: value})
	if err != nil || info == nil {
		return ref
	}
	return conversationRefFromSlack(ctx, client, profile, *info)
}

func cachedConversation(profile, value string) (clioutput.SlackConversationRef, bool) {
	channels, ok := LoadCachedChannels(profile)
	if !ok {
		return clioutput.SlackConversationRef{}, false
	}
	normalized := normalizeConversationLookup(value)
	for _, channel := range channels {
		if channel.ID == value || strings.EqualFold(channel.Name, normalized) || strings.EqualFold("#"+channel.Name, value) {
			return clioutput.SlackConversationRefFromChannel(channel), true
		}
		if channel.User != nil && *channel.User == value {
			return clioutput.SlackConversationRefFromChannel(channel), true
		}
	}
	return clioutput.SlackConversationRef{}, false
}

func normalizeConversationLookup(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "#")
	value = strings.TrimPrefix(value, "@")
	return value
}

func cachedUserName(profile, userID string) string {
	if userID == "" {
		return ""
	}
	users, ok := LoadCachedUsers(profile)
	if !ok {
		return ""
	}
	for _, user := range users {
		if user.ID == userID {
			return user.Name
		}
	}
	return ""
}

func fallbackConversationRef(value string) clioutput.SlackConversationRef {
	ref := clioutput.SlackConversationRef{ID: value}
	if strings.HasPrefix(value, "D") {
		isDM := true
		ref.IsDM = &isDM
		ref.Type = "im"
	}
	return ref
}

func conversationRefFromSlack(ctx context.Context, client *slackgo.Client, profile string, channel slackgo.Channel) clioutput.SlackConversationRef {
	ref := clioutput.SlackConversationRef{
		ID:   channel.ID,
		Name: channel.Name,
		Type: conversationType(channel),
		User: channel.User,
	}
	isDM := channel.IsIM
	ref.IsDM = &isDM
	if channel.IsIM && channel.User != "" {
		ref.Name = cliutil.FirstNonEmpty(
			cachedUserName(profile, channel.User),
			resolveUserName(ctx, client, channel.User),
			channel.User,
		)
	}
	return ref
}

func conversationType(channel slackgo.Channel) string {
	switch {
	case channel.IsIM:
		return "im"
	case channel.IsMpIM:
		return "mpim"
	case channel.IsPrivate:
		return "private_channel"
	default:
		return "channel"
	}
}

func resolveUserName(ctx context.Context, client *slackgo.Client, userID string) string {
	user, err := client.GetUserInfoContext(ctx, userID)
	if err != nil || user == nil {
		return ""
	}
	return cliutil.FirstNonEmpty(user.Profile.DisplayName, user.Profile.RealName, user.RealName, user.Name)
}
