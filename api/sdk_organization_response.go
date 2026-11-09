package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	slack "github.com/slack-go/slack"
)

// The native SDK accepts missing acknowledgement fields and missing result
// collections. Reject them before a task can report an empty success.
func guardSDKOrganizationResponse(req *http.Request, response *http.Response) (*http.Response, error) {
	method := sdkResponseMethod(req)
	switch method {
	case "dnd.info", "dnd.teamInfo", "dnd.setSnooze", "dnd.endSnooze", "dnd.endDnd", "pins.add", "pins.remove", "pins.list", "bookmarks.add", "bookmarks.edit", "bookmarks.remove", "bookmarks.list":
	default:
		return response, nil
	}
	if response == nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return response, nil
	}
	if response.Body == nil {
		return nil, errors.New("slack returned an empty response")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	_ = response.Body.Close()
	if err != nil || len(body) > 8<<20 {
		return nil, errors.New("slack returned an incomplete or oversized response")
	}
	if !validSDKOrganizationResponse(method, body) {
		return nil, errors.New("slack returned an invalid response")
	}
	// ListPinsContext turns Slack errors into plain strings. Preserve typed scope
	// and permission diagnostics before that lossy SDK projection takes place.
	if method == "pins.list" {
		var ack slack.SlackResponse
		if err = json.Unmarshal(body, &ack); err != nil {
			return nil, err
		}
		if err = ack.Err(); err != nil {
			return nil, err
		}
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	return response, nil
}

func validSDKOrganizationResponse(method string, body []byte) bool {
	var value struct {
		OK        *bool                       `json:"ok"`
		Error     string                      `json:"error"`
		DND       *bool                       `json:"dnd_enabled"`
		Snooze    *bool                       `json:"snooze_enabled"`
		Users     *map[string]json.RawMessage `json:"users"`
		Items     *[]json.RawMessage          `json:"items"`
		Bookmarks *[]struct {
			ID      string `json:"id"`
			Channel string `json:"channel_id"`
		} `json:"bookmarks"`
		Bookmark *struct {
			ID      string `json:"id"`
			Channel string `json:"channel_id"`
		} `json:"bookmark"`
	}
	if json.Unmarshal(body, &value) != nil || value.OK == nil {
		return false
	}
	if !*value.OK {
		return strings.TrimSpace(value.Error) != ""
	}
	if value.Error != "" {
		return false
	}
	switch method {
	case "dnd.info":
		return value.DND != nil
	case "dnd.setSnooze", "dnd.endSnooze":
		return value.Snooze != nil
	case "dnd.teamInfo":
		if value.Users == nil {
			return false
		}
		for id, raw := range *value.Users {
			var status struct {
				Enabled *bool `json:"dnd_enabled"`
			}
			if strings.TrimSpace(id) == "" || json.Unmarshal(raw, &status) != nil || status.Enabled == nil {
				return false
			}
		}
		return true
	case "pins.list":
		if value.Items == nil {
			return false
		}
		for _, raw := range *value.Items {
			var item struct {
				Type    string `json:"type"`
				Message *struct {
					TS string `json:"ts"`
				} `json:"message"`
			}
			if json.Unmarshal(raw, &item) != nil || strings.TrimSpace(item.Type) == "" {
				return false
			}
			if item.Type == "message" && (item.Message == nil || strings.TrimSpace(item.Message.TS) == "") {
				return false
			}
		}
		return true
	case "bookmarks.list":
		if value.Bookmarks == nil {
			return false
		}
		for _, item := range *value.Bookmarks {
			if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Channel) == "" {
				return false
			}
		}
		return true
	case "bookmarks.add", "bookmarks.edit":
		return value.Bookmark != nil && strings.TrimSpace(value.Bookmark.ID) != "" && strings.TrimSpace(value.Bookmark.Channel) != ""
	default:
		return true
	}
}
