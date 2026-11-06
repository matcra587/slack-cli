package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

// The SDK's custom-status helper returns nil for an absent ok/error pair.
// Require an explicit acknowledgement before reporting an applied mutation.
func guardSDKStatusResponse(req *http.Request, response *http.Response) (*http.Response, error) {
	if sdkResponseMethod(req) != "users.profile.set" || response == nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return response, nil
	}
	if response.Body == nil {
		return nil, errors.New("slack returned an empty profile update response")
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		if contextErr := req.Context().Err(); contextErr != nil {
			return nil, contextErr
		}
		return nil, errors.New("slack returned an incomplete profile update response")
	}
	var ack struct {
		OK    *bool  `json:"ok"`
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &ack) != nil || ack.OK == nil || (*ack.OK && ack.Error != "") || (!*ack.OK && strings.TrimSpace(ack.Error) == "") {
		return nil, errors.New("slack returned an invalid profile update response")
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	return response, nil
}
