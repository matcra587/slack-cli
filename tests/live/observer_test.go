package live_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"strings"
	"sync"

	slack "github.com/slack-go/slack"
)

type slackObserver struct {
	client          *slack.Client
	scopes          []string
	runID, actor    string
	team            string
	public, private string
}

// Slack may report a positive count with only some users. Omission from that
// partial list cannot prove that the selected actor has no reaction.
func reactionForActor(items []slack.ItemReaction, emoji, actor string) (bool, error) {
	unknown := false
	for _, item := range items {
		if item.Name != emoji {
			continue
		}
		users := map[string]bool{}
		for _, id := range item.Users {
			users[id] = true
		}
		if item.Count < 0 || item.Count < len(users) {
			unknown = true
			continue
		}
		if item.Count > 0 && users[actor] {
			return true, nil
		}
		if item.Count > len(users) {
			unknown = true
		}
	}
	if unknown {
		return false, errors.New("reaction actor absence is unverified")
	}
	return false, nil
}

func (s *slackObserver) Conversation(ctx context.Context, ids []string) (string, error) {
	for _, id := range ids {
		user, err := s.client.GetUserInfoContext(ctx, id)
		if err != nil || user == nil || user.ID != id || user.Deleted || (user.TeamID != "" && user.TeamID != s.team) {
			return "", errLiveSafety
		}
	}
	// Opening/resuming these exact consenting recipients is explicitly approved.
	// No automatic retry and no conversation close/archive cleanup.
	channel, _, _, err := s.client.OpenConversationContext(ctx, &slack.OpenConversationParameters{Users: ids, ReturnIM: true})
	if err != nil || channel == nil || !validID(channel.ID, "CDG") {
		return "", errLiveSafety
	}
	info, err := s.client.GetConversationInfoContext(ctx, &slack.GetConversationInfoInput{ChannelID: channel.ID})
	if err != nil || info == nil || info.ID != channel.ID || info.IsArchived ||
		(len(ids) == 1 && (!info.IsIM || info.IsMpIM)) ||
		(len(ids) == 2 && (!info.IsMpIM || info.IsIM)) {
		return "", errLiveSafety
	}
	cursor := ""
	seen := map[string]bool{"": true}
	members := map[string]bool{}
	for {
		rows, next, err := s.client.GetUsersInConversationContext(ctx, &slack.GetUsersInConversationParameters{ChannelID: channel.ID, Cursor: cursor, Limit: 100})
		if err != nil {
			return "", errLiveSafety
		}
		for _, id := range rows {
			members[id] = true
		}
		if next == "" {
			break
		}
		if seen[next] {
			return "", errLiveSafety
		}
		seen[next] = true
		cursor = next
	}
	if len(members) != len(ids)+1 || !members[s.actor] {
		return "", errLiveSafety
	}
	for _, id := range ids {
		if !members[id] {
			return "", errLiveSafety
		}
	}
	return channel.ID, nil
}

func (s *slackObserver) Identity(ctx context.Context) (string, string, error) {
	v, err := s.client.AuthTestContext(ctx)
	if err != nil || v == nil {
		return "", "", errLiveSafety
	}
	s.scopes = strings.Split(v.Header.Get("X-OAuth-Scopes"), ",")
	return v.TeamID, v.UserID, nil
}

func (s *slackObserver) Channel(ctx context.Context, id string) error {
	v, err := s.client.GetConversationInfoContext(ctx, &slack.GetConversationInfoInput{ChannelID: id})
	if err != nil || v == nil || v.ID != id || v.IsArchived || !v.IsMember {
		return errLiveSafety
	}
	if (id == s.public && v.IsPrivate) || (id == s.private && !v.IsPrivate) {
		return errLiveSafety
	}
	return nil
}

func (s *slackObserver) message(ctx context.Context, v liveReceipt) (*slack.Message, error) {
	if v.ParentTS != "" {
		cursor := ""
		seen := map[string]bool{"": true}
		for {
			messages, more, next, err := s.client.GetConversationRepliesContext(ctx, &slack.GetConversationRepliesParameters{ChannelID: v.Channel, Timestamp: v.ParentTS, Cursor: cursor, Limit: 100})
			if err != nil {
				return nil, err
			}
			if messages == nil {
				return nil, errLiveSafety
			}
			for i := range messages {
				if messages[i].Timestamp == v.TS {
					return &messages[i], nil
				}
			}
			if !more && next == "" {
				return nil, nil
			}
			if next == "" || seen[next] {
				return nil, errLiveSafety
			}
			seen[next] = true
			cursor = next
		}
	}
	cursor := ""
	seen := map[string]bool{"": true}
	for {
		page, err := s.client.GetConversationHistoryContext(ctx, &slack.GetConversationHistoryParameters{ChannelID: v.Channel, Oldest: v.TS, Latest: v.TS, Inclusive: true, Limit: 100, Cursor: cursor})
		if err != nil {
			return nil, err
		}
		if page == nil || !page.Ok || page.Messages == nil {
			return nil, errLiveSafety
		}
		for i := range page.Messages {
			if page.Messages[i].Timestamp == v.TS {
				return &page.Messages[i], nil
			}
		}
		next := page.ResponseMetaData.NextCursor
		if !page.HasMore && next == "" {
			return nil, nil
		}
		if next == "" || seen[next] {
			return nil, errLiveSafety
		}
		seen[next] = true
		cursor = next
	}
}

func (s *slackObserver) Inspect(ctx context.Context, v liveReceipt) (bool, error) {
	switch v.Kind {
	case "reaction":
		m, err := s.message(ctx, v)
		if err != nil || m == nil {
			return false, err
		}
		body, _ := json.Marshal(m)
		if m.User != s.actor || !strings.Contains(string(body), s.runID) {
			return false, errLiveSafety
		}
		item, err := s.client.GetReactionsContext(ctx, slack.NewRefToMessage(v.Channel, v.TS), slack.GetReactionsParameters{Full: true})
		if err != nil {
			return false, err
		}
		return reactionForActor(item.Reactions, v.Emoji, s.actor)
	case "message":
		m, err := s.message(ctx, v)
		if err == nil && m != nil {
			body, _ := json.Marshal(m)
			if m.User != s.actor || !strings.Contains(string(body), s.runID) {
				return false, errLiveSafety
			}
		}
		return m != nil, err
	case "file":
		f, _, _, err := s.client.GetFileInfoContext(ctx, v.FileID, 1, 1)
		if isSlackError(err, "file_not_found") || isSlackError(err, "file_deleted") {
			return false, nil
		}
		if err == nil && (f == nil || f.ID != v.FileID || f.User != s.actor || (!strings.Contains(f.Name, s.runID) && !strings.Contains(f.Title, s.runID))) {
			return false, errLiveSafety
		}
		return f != nil && f.ID == v.FileID, err
	case "scheduled":
		cursor := ""
		seen := map[string]bool{"": true}
		for {
			rows, next, err := s.client.GetScheduledMessagesContext(ctx, &slack.GetScheduledMessagesParameters{Channel: v.Channel, Cursor: cursor, Limit: 100})
			if err != nil {
				return false, err
			}
			if rows == nil {
				return false, errLiveSafety
			}
			for _, row := range rows {
				if row.ID == v.ScheduledID {
					if row.Channel != v.Channel || !strings.Contains(row.Text, s.runID) {
						return false, errLiveSafety
					}
					return true, nil
				}
			}
			if next == "" {
				return false, nil
			}
			if seen[next] {
				return false, errLiveSafety
			}
			seen[next] = true
			cursor = next
		}
	}
	return false, errLiveSafety
}

func (s *slackObserver) Delete(ctx context.Context, v liveReceipt) error {
	switch v.Kind {
	case "reaction":
		return s.client.RemoveReactionContext(ctx, v.Emoji, slack.NewRefToMessage(v.Channel, v.TS))
	case "message":
		_, _, err := s.client.DeleteMessageContext(ctx, v.Channel, v.TS)
		return err
	case "file":
		return s.client.DeleteFileContext(ctx, v.FileID)
	case "scheduled":
		_, err := s.client.DeleteScheduledMessageContext(ctx, &slack.DeleteScheduledMessageParameters{Channel: v.Channel, ScheduledMessageID: v.ScheduledID})
		return err
	}
	return errLiveSafety
}

func isSlackError(err error, code string) bool {
	var v slack.SlackErrorResponse
	return errors.As(err, &v) && v.Err == code
}

// The independent reader must never interpret a missing acknowledgement/list as
// absence. This test-only transport does not reuse slick's production guards.
type liveReadTransport struct {
	base   http.RoundTripper
	origin string
}

func (t liveReadTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme+"://"+req.URL.Host != t.origin {
		return nil, errLiveSafety
	}
	response, err := t.base.RoundTrip(req)
	if err != nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return response, err
	}
	if response.Body == nil {
		return nil, errLiveSafety
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	_ = response.Body.Close()
	if err != nil || len(body) > 8<<20 {
		return nil, errLiveSafety
	}
	var ack struct {
		OK    *bool
		Error string
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &ack) != nil || ack.OK == nil || (*ack.OK && ack.Error != "") || (!*ack.OK && strings.TrimSpace(ack.Error) == "") {
		return nil, errLiveSafety
	}
	if *ack.OK {
		if json.Unmarshal(body, &fields) != nil {
			return nil, errLiveSafety
		}
		key := map[string]string{"conversations.history": "messages", "conversations.replies": "messages", "chat.scheduledMessages.list": "scheduled_messages", "conversations.members": "members"}[path.Base(req.URL.Path)]
		if key != "" {
			var list []json.RawMessage
			if json.Unmarshal(fields[key], &list) != nil || list == nil {
				return nil, errLiveSafety
			}
		}
		if path.Base(req.URL.Path) == "reactions.get" {
			var message map[string]json.RawMessage
			if string(fields["type"]) != `"message"` || json.Unmarshal(fields["message"], &message) != nil || message == nil {
				return nil, errLiveSafety
			}
		}
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	return response, nil
}

// Reconciliation only inspects the exact approved target, actor, unique
// operation marker and creation window. It never replays a mutation.
func (s *slackObserver) Recover(ctx context.Context, pending liveReceipt) ([]liveReceipt, error) {
	if pending.Marker == "" || !strings.Contains(pending.Marker, s.runID) {
		return nil, errLiveSafety
	}
	found := []liveReceipt{}
	seenMatches := map[string]bool{}
	cursor := ""
	seen := map[string]bool{"": true}
	if pending.Kind == "uncertain-schedule" || pending.Kind == "uncertain-schedule-users" {
		for {
			rows, next, err := s.client.GetScheduledMessagesContext(ctx, &slack.GetScheduledMessagesParameters{Channel: pending.Channel, Cursor: cursor, Limit: 100})
			if err != nil || rows == nil {
				return nil, errLiveSafety
			}
			for _, row := range rows {
				if row.Channel == pending.Channel && validID(row.ID, "Q") && strings.Contains(row.Text, pending.Marker) && int64(row.PostAt) >= pending.PostFrom && int64(row.PostAt) <= pending.PostTo && !seenMatches[row.ID] {
					found = append(found, liveReceipt{Kind: "scheduled", Channel: pending.Channel, ScheduledID: row.ID})
					seenMatches[row.ID] = true
				}
			}
			if next == "" {
				return found, nil
			}
			if seen[next] {
				return nil, errLiveSafety
			}
			seen[next] = true
			cursor = next
		}
	}
	if pending.Kind == "uncertain-upload" {
		return nil, errLiveSafety
	}
	for {
		var rows []slack.Message
		var more bool
		var next string
		var err error
		if pending.ParentTS != "" {
			rows, more, next, err = s.client.GetConversationRepliesContext(ctx, &slack.GetConversationRepliesParameters{ChannelID: pending.Channel, Timestamp: pending.ParentTS, Oldest: pending.Oldest, Latest: pending.Latest, Inclusive: true, Cursor: cursor, Limit: 100})
		} else {
			var page *slack.GetConversationHistoryResponse
			page, err = s.client.GetConversationHistoryContext(ctx, &slack.GetConversationHistoryParameters{ChannelID: pending.Channel, Oldest: pending.Oldest, Latest: pending.Latest, Inclusive: true, Cursor: cursor, Limit: 100})
			if err == nil && page != nil && page.Ok {
				rows, more, next = page.Messages, page.HasMore, page.ResponseMetaData.NextCursor
			} else {
				return nil, errLiveSafety
			}
		}
		if err != nil || rows == nil {
			return nil, errLiveSafety
		}
		for _, message := range rows {
			content, _ := json.Marshal(message)
			if message.User == s.actor && slackTS.MatchString(message.Timestamp) && message.Timestamp >= pending.Oldest && message.Timestamp <= pending.Latest && strings.Contains(string(content), pending.Marker) && !seenMatches[message.Timestamp] {
				if pending.ParentTS != "" && message.ThreadTimestamp != pending.ParentTS {
					continue
				}
				found = append(found, liveReceipt{Kind: "message", Channel: pending.Channel, TS: message.Timestamp, ParentTS: pending.ParentTS})
				seenMatches[message.Timestamp] = true
			}
		}
		if !more && next == "" {
			return found, nil
		}
		if next == "" || seen[next] {
			return nil, errLiveSafety
		}
		seen[next] = true
		cursor = next
	}
}

func newWatchRelay(token string, target liveReceipt, actor string, upstream *http.Client) (*httptest.Server, <-chan struct{}) {
	ready := make(chan struct{})
	var once sync.Once
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, in *http.Request) {
		body, err := io.ReadAll(io.LimitReader(in.Body, (1<<20)+1))
		values, parseErr := url.ParseQuery(string(body))
		allowed := in.URL.Path == "/api/auth.test" || in.URL.Path == "/api/reactions.get" || in.URL.Path == "/api/conversations.info"
		if err != nil || parseErr != nil || len(body) > 1<<20 || in.Method != http.MethodPost || in.URL.RawQuery != "" || !allowed || token == "" || values.Get("token") != token || len(values["token"]) != 1 {
			w.WriteHeader(400)
			return
		}
		for key, items := range values {
			approved := key == "token" || (in.URL.Path == "/api/reactions.get" && (key == "channel" || key == "timestamp" || key == "full")) || (in.URL.Path == "/api/conversations.info" && (key == "channel" || key == "include_locale" || key == "include_num_members"))
			if !approved || len(items) != 1 {
				w.WriteHeader(400)
				return
			}
		}
		if in.URL.Path != "/api/auth.test" && values.Get("channel") != target.Channel {
			w.WriteHeader(400)
			return
		}
		if in.URL.Path == "/api/reactions.get" && (values.Get("timestamp") != target.TS || values.Get("full") != "true") {
			w.WriteHeader(400)
			return
		}
		request, err := http.NewRequestWithContext(in.Context(), http.MethodPost, "https://slack.com"+in.URL.Path, bytes.NewReader(body))
		if err != nil {
			w.WriteHeader(500)
			return
		}
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response, err := upstream.Do(request)
		if err != nil {
			w.WriteHeader(502)
			return
		}
		defer func() { _ = response.Body.Close() }()
		payload, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
		if err != nil || len(payload) > 8<<20 {
			w.WriteHeader(502)
			return
		}
		for _, key := range []string{"Content-Type", "X-OAuth-Scopes", "Retry-After"} {
			for _, value := range response.Header.Values(key) {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(response.StatusCode)
		if _, err := w.Write(payload); err != nil {
			return
		}
		if response.StatusCode == 200 && in.URL.Path == "/api/reactions.get" {
			var poll struct {
				OK      bool
				Message struct{ Reactions []slack.ItemReaction }
			}
			if json.Unmarshal(payload, &poll) != nil || !poll.OK {
				return
			}
			present, stateErr := reactionForActor(poll.Message.Reactions, "eyes", actor)
			if stateErr == nil && !present {
				if http.NewResponseController(w).Flush() == nil {
					once.Do(func() { close(ready) })
				}
			}
		}
	}))
	return relay, ready
}
