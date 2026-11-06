package api_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/matcra587/slack-cli/api"
	slackgo "github.com/slack-go/slack"
)

func TestSDKUploadResponsesRequireAcknowledgementAndRealFileObjects(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, method, body string
		valid              bool
	}{
		{"prepare missing ack", "files.getUploadURLExternal", `{"file_id":"F1","upload_url":"https://files.slack.com/upload/test"}`, false},
		{"prepare missing ID", "files.getUploadURLExternal", `{"ok":true,"upload_url":"https://files.slack.com/upload/test"}`, false},
		{"prepare unsafe ID", "files.getUploadURLExternal", `{"ok":true,"file_id":"synthetic-private-id","upload_url":"https://files.slack.com/upload/test"}`, false},
		{"prepare missing URL", "files.getUploadURLExternal", `{"ok":true,"file_id":"F1"}`, false},
		{"prepare invalid URL", "files.getUploadURLExternal", `{"ok":true,"file_id":"F1","upload_url":"synthetic-private-url"}`, false},
		{"prepare valid", "files.getUploadURLExternal", `{"ok":true,"file_id":"F1","upload_url":"https://files.slack.com/upload/test"}`, true},
		{"complete missing ack", "files.completeUploadExternal", `{"files":[{"id":"F1"}]}`, false},
		{"complete whitespace error", "files.completeUploadExternal", `{"ok":false,"error":"  ","files":[{"id":"F1"}]}`, false},
		{"complete false without error", "files.completeUploadExternal", `{"ok":false,"files":[{"id":"F1"}]}`, false},
		{"complete contradictory error", "files.completeUploadExternal", `{"ok":true,"error":"synthetic-private-error","files":[{"id":"F1"}]}`, false},
		{"complete null", "files.completeUploadExternal", `{"ok":true,"files":null}`, false},
		{"complete empty", "files.completeUploadExternal", `{"ok":true,"files":[]}`, false},
		{"complete empty file", "files.completeUploadExternal", `{"ok":true,"files":[{}]}`, false},
		{"complete valid native batch", "files.completeUploadExternal", `{"ok":true,"files":[{"id":"F1"},{"id":"F2"}]}`, true},
		{"info missing ack", "files.info", `{"file":{"id":"F1","name":"synthetic-private-name"}}`, false},
		{"info null", "files.info", `{"ok":true,"file":null}`, false},
		{"info empty", "files.info", `{"ok":true,"file":{}}`, false},
		{"info valid", "files.info", `{"ok":true,"file":{"id":"F1"}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != "/api/"+tc.method {
					t.Error("wrong SDK method")
				}
				w.Header().Set("Content-Type", "application/json")
				if _, err := io.WriteString(w, tc.body); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			client := api.NewSDKClient("xoxp-synthetic-upload-guard", api.SDKOptions{APIURL: server.URL, NoThrottle: true})
			var err error
			switch tc.method {
			case "files.getUploadURLExternal":
				_, err = client.GetUploadURLExternalContext(t.Context(), slackgo.GetUploadURLExternalParameters{FileName: "artifact.txt", FileSize: 8})
			case "files.completeUploadExternal":
				_, err = client.CompleteUploadExternalContext(t.Context(), slackgo.CompleteUploadExternalParameters{Files: []slackgo.FileSummary{{ID: "F1"}, {ID: "F2"}}})
			case "files.info":
				_, _, _, err = client.GetFileInfoContext(t.Context(), "F1", 0, 0)
			}
			if (err == nil) != tc.valid || calls.Load() != 1 {
				t.Fatalf("malformed response accepted, valid batch rejected or request replayed: err=%v calls=%d", err, calls.Load())
			}
			if err != nil && strings.Contains(err.Error(), "synthetic-private") {
				t.Fatal("unreviewed response data entered SDK error")
			}
		})
	}
}

func TestSDKUploadGuardClosesBodiesAndHidesReadErrors(t *testing.T) {
	t.Parallel()
	body := &uploadGuardBody{}
	client := api.NewSDKClient("xoxp-synthetic-body-guard", api.SDKOptions{HTTPClient: &http.Client{Transport: sdkTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: body, Request: r}, nil
	})}, NoThrottle: true})
	_, err := client.GetUploadURLExternalContext(t.Context(), slackgo.GetUploadURLExternalParameters{FileName: "artifact.txt", FileSize: 8})
	if err == nil || strings.Contains(err.Error(), "synthetic-private") || body.closes != 1 {
		t.Fatal("upload response body was not closed or read diagnostics leaked")
	}
}

type uploadGuardBody struct{ closes int }

func (*uploadGuardBody) Read([]byte) (int, error) {
	return 0, errors.New("synthetic-private-read-error")
}
func (b *uploadGuardBody) Close() error { b.closes++; return nil }

func TestSDKUploadGuardPreservesStructuredPermissionErrors(t *testing.T) {
	t.Parallel()
	var log bytes.Buffer
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.WriteString(w, `{"ok":false,"error":"missing_scope","needed":"files:write"}`); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	client := api.NewSDKClient("xoxp-synthetic-scope-guard", api.SDKOptions{APIURL: server.URL, Log: &log, NoThrottle: true})
	_, err := client.GetUploadURLExternalContext(t.Context(), slackgo.GetUploadURLExternalParameters{FileName: "artifact.txt", FileSize: 8})
	scope, ok := errors.AsType[slackgo.SlackErrorResponse](err)
	if !ok || scope.Err != "missing_scope" || scope.Needed != "files:write" {
		t.Fatalf("native SDK permission error changed: %v", err)
	}
}

func TestSDKUploadGuardLeavesFileDownloadsUntouched(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer xoxp-synthetic-download" {
			t.Error("wrong download request")
		}
		if _, err := io.WriteString(w, "original file bytes"); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	client := api.NewSDKClient("xoxp-synthetic-download", api.SDKOptions{APIURL: server.URL, NoThrottle: true})
	var out bytes.Buffer
	if err := client.GetFileContext(t.Context(), server.URL+"/files-pri/F1/files.info", &out); err != nil || out.String() != "original file bytes" {
		t.Fatalf("Web API guard changed a file download: %v", err)
	}
}
