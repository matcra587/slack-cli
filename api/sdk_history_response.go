package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

// The SDK replies helper discards Slack's acknowledgement. Validate it before
// decoding, preserving explicit empty pages and the original response bytes.
func guardSDKHistoryResponse(req *http.Request, response *http.Response) (*http.Response, error) {
	if response == nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return response, nil
	}
	switch sdkResponseMethod(req) {
	case "conversations.history", "conversations.replies":
	default:
		return response, nil
	}
	if response.Body == nil {
		return nil, errors.New("slack returned an empty history response")
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		if contextErr := req.Context().Err(); contextErr != nil {
			return nil, contextErr
		}
		return nil, errors.New("slack returned an incomplete history response")
	}
	if !validSDKHistoryResponse(body) {
		return nil, errors.New("slack returned an invalid history response")
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	return response, nil
}

func validSDKHistoryResponse(body []byte) bool {
	var data struct {
		OK       *bool             `json:"ok"`
		Error    string            `json:"error"`
		Messages []json.RawMessage `json:"messages"`
	}
	if json.Unmarshal(body, &data) != nil || data.OK == nil {
		return false
	}
	if !*data.OK {
		return strings.TrimSpace(data.Error) != ""
	}
	return data.Error == "" && data.Messages != nil
}
