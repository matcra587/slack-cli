package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	slackgo "github.com/slack-go/slack"
)

// slack-go treats an absent ok/error pair as success, and its reaction decoder
// dereferences an embedded message pointer without checking it. Validate these
// three response contracts before handing the unchanged body to the SDK.
func guardSDKReactionResponse(req *http.Request, response *http.Response) (*http.Response, error) {
	method := sdkResponseMethod(req)
	if response == nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return response, nil
	}
	if method != "reactions.add" && method != "reactions.remove" && method != "reactions.get" {
		return response, nil
	}
	if response.Body == nil {
		return nil, errors.New("slack returned an empty reaction response")
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		if contextErr := req.Context().Err(); contextErr != nil {
			return nil, contextErr
		}
		return nil, errors.New("slack returned an incomplete reaction response")
	}
	if !validSDKReactionResponse(method, body) {
		return nil, errors.New("slack returned an invalid reaction response")
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	return response, nil
}

func validSDKReactionResponse(method string, body []byte) bool {
	var response struct {
		OK      *bool           `json:"ok"`
		Error   string          `json:"error"`
		Type    string          `json:"type"`
		Message json.RawMessage `json:"message"`
		File    json.RawMessage `json:"file"`
		Comment json.RawMessage `json:"comment"`
	}
	if json.Unmarshal(body, &response) != nil || response.OK == nil {
		return false
	}
	if !*response.OK {
		return response.Error != ""
	}
	if response.Error != "" {
		return false
	}
	if method != "reactions.get" {
		return true
	}
	switch response.Type {
	case "message":
		// Match the SDK's embedded pointer so even a syntactically valid
		// object with no recognized message fields cannot trigger its panic.
		var message struct{ *slackgo.Message }
		return json.Unmarshal(response.Message, &message) == nil && message.Message != nil
	case "file":
		return validJSONObject(response.File)
	case "file_comment":
		return validJSONObject(response.File) && validJSONObject(response.Comment)
	default:
		return false
	}
}

func validJSONObject(raw json.RawMessage) bool {
	var object map[string]json.RawMessage
	return json.Unmarshal(raw, &object) == nil && object != nil
}
