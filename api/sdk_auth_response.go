package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

// The SDK treats absent ok/error fields as success. Preserve the response
// bytes and headers, but require an explicit authentication acknowledgement.
func guardSDKAuthResponse(req *http.Request, response *http.Response) (*http.Response, error) {
	if response == nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return response, nil
	}
	method := sdkResponseMethod(req)
	if method != "auth.test" && method != "auth.revoke" {
		return response, nil
	}
	if response.Body == nil {
		return nil, errors.New("slack returned an empty authentication response")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, defaultResponseLimit+1))
	_ = response.Body.Close()
	if err != nil {
		if contextErr := req.Context().Err(); contextErr != nil {
			return nil, contextErr
		}
		return nil, errors.New("slack returned an incomplete authentication response")
	}
	var ack struct {
		OK      *bool  `json:"ok"`
		Error   string `json:"error"`
		Revoked *bool  `json:"revoked"`
	}
	if int64(len(body)) > defaultResponseLimit || json.Unmarshal(body, &ack) != nil || ack.OK == nil || (*ack.OK && ack.Error != "") || (!*ack.OK && strings.TrimSpace(ack.Error) == "") || (*ack.OK && method == "auth.revoke" && ack.Revoked == nil) {
		return nil, errors.New("slack returned an invalid authentication response")
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	return response, nil
}
