package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// The pinned SDK accepts missing acknowledgements and zero-value lookup data.
// Restrict this check to the POST Web API calls it makes, leaving downloads
// and native OptionHTTPClient overrides with their own contracts.
func guardSDKLookupResponse(req *http.Request, response *http.Response) (*http.Response, error) {
	if response == nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return response, nil
	}
	method := sdkResponseMethod(req)
	switch method {
	case "conversations.info", "conversations.list", "users.info", "users.lookupByEmail", "users.list", "users.getPresence":
	default:
		return response, nil
	}
	if response.Body == nil {
		return nil, errors.New("slack returned an empty lookup response")
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		if contextErr := req.Context().Err(); contextErr != nil {
			return nil, contextErr
		}
		return nil, errors.New("slack returned an incomplete lookup response")
	}
	if method == "users.info" && sdkMultiUserLookup(req) {
		method = "users.info.many"
	}
	if !validSDKLookupResponse(method, body) {
		return nil, errors.New("slack returned an invalid lookup response")
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	return response, nil
}

type lookupIdentity struct {
	ID string `json:"id"`
}

func validSDKLookupResponse(method string, body []byte) bool {
	var response struct {
		OK       *bool             `json:"ok"`
		Error    string            `json:"error"`
		Channel  *lookupIdentity   `json:"channel"`
		User     *lookupIdentity   `json:"user"`
		Channels *[]lookupIdentity `json:"channels"`
		Members  *[]lookupIdentity `json:"members"`
		Users    *[]lookupIdentity `json:"users"`
		Presence *string           `json:"presence"`
	}
	if json.Unmarshal(body, &response) != nil || response.OK == nil {
		return false
	}
	if !*response.OK {
		return strings.TrimSpace(response.Error) != ""
	}
	if response.Error != "" {
		return false
	}
	switch method {
	case "conversations.info":
		return validLookupIdentity(response.Channel)
	case "users.info", "users.lookupByEmail":
		return validLookupIdentity(response.User)
	case "users.info.many":
		return validLookupList(response.Users)
	case "users.getPresence":
		return response.Presence != nil && strings.TrimSpace(*response.Presence) != ""
	case "conversations.list":
		return validLookupList(response.Channels)
	default:
		return validLookupList(response.Members)
	}
}

func validLookupIdentity(item *lookupIdentity) bool {
	return item != nil && strings.TrimSpace(item.ID) != ""
}

func validLookupList(items *[]lookupIdentity) bool {
	if items == nil {
		return false
	}
	for _, item := range *items {
		if !validLookupIdentity(&item) {
			return false
		}
	}
	return true
}

func sdkMultiUserLookup(req *http.Request) bool {
	if req.GetBody == nil {
		return false
	}
	copy, err := req.GetBody()
	if err != nil || copy == nil {
		return false
	}
	data, err := io.ReadAll(copy)
	_ = copy.Close()
	if err != nil {
		return false
	}
	values, err := url.ParseQuery(string(data))
	return err == nil && values.Has("users") && !values.Has("user")
}
