package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	slackgo "github.com/slack-go/slack"
)

// Require an explicit chat acknowledgement and the identifiers/list data
// callers need, while preserving the original response for the SDK decoder.
func guardSDKMessageResponse(req *http.Request, response *http.Response) (*http.Response, error) {
	if response == nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return response, nil
	}
	method := sdkResponseMethod(req)
	switch method {
	case "chat.postMessage", "chat.update", "chat.delete", "chat.scheduleMessage", "chat.scheduledMessages.list", "chat.deleteScheduledMessage":
	default:
		return response, nil
	}
	if response.Body == nil {
		return nil, errors.New("slack returned an empty message response")
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		if contextErr := req.Context().Err(); contextErr != nil {
			return nil, contextErr
		}
		return nil, errors.New("slack returned an incomplete message response")
	}
	if !validSDKMessageResponse(method, body) {
		return nil, errors.New("slack returned an invalid message response")
	}
	if method == "chat.scheduledMessages.list" {
		// The SDK's cursor metadata shadows SlackResponse.ResponseMetadata.
		// Return the native typed failure before that decoder drops messages.
		var status slackgo.SlackResponse
		if err := json.Unmarshal(body, &status); err != nil {
			return nil, errors.New("slack returned an invalid message response")
		}
		if !status.Ok {
			return nil, status.Err()
		}
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	return response, nil
}

func validSDKMessageResponse(method string, body []byte) bool {
	var data struct {
		OK               *bool  `json:"ok"`
		Error            string `json:"error"`
		Channel          string `json:"channel"`
		Timestamp        string `json:"ts"`
		MessageTimestamp string `json:"message_ts"`
		ScheduledID      string `json:"scheduled_message_id"`
		Scheduled        *[]struct {
			ID      string `json:"id"`
			Channel string `json:"channel_id"`
			PostAt  *int64 `json:"post_at"`
		} `json:"scheduled_messages"`
	}
	if json.Unmarshal(body, &data) != nil || data.OK == nil {
		return false
	}
	if !*data.OK {
		return strings.TrimSpace(data.Error) != ""
	}
	if data.Error != "" {
		return false
	}
	switch method {
	case "chat.postMessage":
		return strings.TrimSpace(data.Channel) != "" && (strings.TrimSpace(data.Timestamp) != "" || strings.TrimSpace(data.MessageTimestamp) != "")
	case "chat.scheduleMessage":
		return strings.TrimSpace(data.Channel) != "" && strings.TrimSpace(data.ScheduledID) != ""
	case "chat.scheduledMessages.list":
		if data.Scheduled == nil {
			return false
		}
		for _, entry := range *data.Scheduled {
			if strings.TrimSpace(entry.ID) == "" || strings.TrimSpace(entry.Channel) == "" || entry.PostAt == nil || *entry.PostAt <= 0 {
				return false
			}
		}
	}
	return true
}
