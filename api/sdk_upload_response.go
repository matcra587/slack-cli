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

// Validate only Web API replies, never multipart uploads or file downloads.
func guardSDKUploadResponse(req *http.Request, response *http.Response) (*http.Response, error) {
	if response == nil || response.StatusCode < 200 || response.StatusCode >= 300 {
		return response, nil
	}
	method := sdkResponseMethod(req)
	if method != "files.getUploadURLExternal" && method != "files.completeUploadExternal" && method != "files.info" {
		return response, nil
	}
	if response.Body == nil {
		return nil, errors.New("slack returned an empty file response")
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		if contextErr := req.Context().Err(); contextErr != nil {
			return nil, contextErr
		}
		return nil, errors.New("slack returned an incomplete file response")
	}
	if !validSDKUploadResponse(method, body) {
		return nil, errors.New("slack returned an invalid file response")
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	return response, nil
}

func validSDKUploadResponse(method string, body []byte) bool {
	var response struct {
		OK        *bool  `json:"ok"`
		Error     string `json:"error"`
		FileID    string `json:"file_id"`
		UploadURL string `json:"upload_url"`
		File      *struct {
			ID string `json:"id"`
		} `json:"file"`
		Files []struct {
			ID string `json:"id"`
		} `json:"files"`
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
	case "files.getUploadURLExternal":
		target, err := url.Parse(response.UploadURL)
		return err == nil && (target.Scheme == "https" || target.Scheme == "http") && target.Host != "" && validSDKFileID(response.FileID)
	case "files.info":
		return response.File != nil && validSDKFileID(response.File.ID)
	default:
		if len(response.Files) == 0 {
			return false
		}
		for _, file := range response.Files {
			if !validSDKFileID(file.ID) {
				return false
			}
		}
		return true
	}
}

func validSDKFileID(id string) bool {
	if len(id) < 2 || id[0] != 'F' {
		return false
	}
	for _, c := range id[1:] {
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}
