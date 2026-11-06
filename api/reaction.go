// Package api provides Slack API access for Go programs using slick.
package api

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	slackgo "github.com/slack-go/slack"
)

// ReactionWatchOptions identifies a reaction to wait for. Existing matching
// reactions satisfy the watch immediately. Set a context deadline to bound it.
type ReactionWatchOptions struct {
	Channel   string
	Timestamp string
	Emoji     string
	User      string
	Interval  time.Duration
}

// ReactionMatch describes an observed match. Slack may omit some reacting users;
// Users is the list Slack actually returned, not a complete membership claim.
type ReactionMatch struct {
	Channel   string   `json:"channel"`
	Timestamp string   `json:"ts"`
	Emoji     string   `json:"emoji"`
	Count     int      `json:"count"`
	Users     []string `json:"users"`
}

// WatchReaction polls Slack until a matching reaction appears or ctx ends.
// It never adds or removes reactions. User-filtered watches succeed only when
// Slack explicitly includes that user; an incomplete user list can delay them.
func WatchReaction(ctx context.Context, client *slackgo.Client, opts ReactionWatchOptions) (ReactionMatch, error) {
	if client == nil {
		return ReactionMatch{}, errors.New("slack client is required")
	}
	opts.Channel = strings.TrimSpace(opts.Channel)
	opts.Timestamp = strings.TrimSpace(opts.Timestamp)
	opts.Emoji = canonicalReaction(opts.Emoji)
	opts.User = strings.TrimSpace(opts.User)
	if opts.Channel == "" || opts.Timestamp == "" || opts.Emoji == "" {
		return ReactionMatch{}, errors.New("channel, timestamp, and emoji are required")
	}
	if strings.ContainsAny(opts.Emoji, ", \t\r\n") {
		return ReactionMatch{}, errors.New("one emoji name is required")
	}
	if opts.Interval < 0 {
		return ReactionMatch{}, errors.New("watch interval must be positive")
	}
	if opts.Interval == 0 {
		opts.Interval = 5 * time.Second
	}
	for {
		if err := ctx.Err(); err != nil {
			return ReactionMatch{}, err
		}
		item, err := client.GetReactionsContext(ctx, slackgo.NewRefToMessage(opts.Channel, opts.Timestamp), slackgo.GetReactionsParameters{Full: true})
		if err != nil {
			return ReactionMatch{}, err
		}
		for _, reaction := range item.Reactions {
			if canonicalReaction(reaction.Name) != opts.Emoji || reaction.Count < 1 {
				continue
			}
			if opts.User != "" && !slices.Contains(reaction.Users, opts.User) {
				continue
			}
			return ReactionMatch{
				Channel: opts.Channel, Timestamp: opts.Timestamp, Emoji: opts.Emoji,
				Count: reaction.Count, Users: slices.Clone(reaction.Users),
			}, nil
		}
		if err := waitForReactionPoll(ctx, opts.Interval); err != nil {
			return ReactionMatch{}, err
		}
	}
}

func canonicalReaction(emoji string) string {
	emoji = strings.Trim(strings.TrimSpace(emoji), ":")
	switch emoji {
	case "thumbsup":
		return "+1"
	case "thumbsdown":
		return "-1"
	default:
		return emoji
	}
}

func waitForReactionPoll(ctx context.Context, interval time.Duration) error {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
