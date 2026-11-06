package api_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/api"
	slackgo "github.com/slack-go/slack"
)

func callLookup(ctx context.Context, client *slackgo.Client, method string) error {
	switch method {
	case "conversations.info":
		_, err := client.GetConversationInfoContext(ctx, &slackgo.GetConversationInfoInput{ChannelID: "C1"})
		return err
	case "conversations.list":
		_, _, err := client.GetConversationsContext(ctx, &slackgo.GetConversationsParameters{})
		return err
	case "users.info":
		_, err := client.GetUserInfoContext(ctx, "U1")
		return err
	case "users.lookupByEmail":
		_, err := client.GetUserByEmailContext(ctx, "person@example.test")
		return err
	case "users.getPresence":
		_, err := client.GetUserPresenceContext(ctx, "U1")
		return err
	case "users.list":
		_, err := client.GetUsersPaginated().Next(ctx)
		return err
	default:
		return fmt.Errorf("unknown fixture method %s", method)
	}
}

func TestSDKLookupResponsesRequireAcknowledgementAndUsableData(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		method, field, valid string
		list                 bool
	}{
		{"conversations.info", "channel", `{"id":"D1","user":"U1","is_im":true}`, false},
		{"users.info", "user", `{"id":"W1","deleted":true}`, false},
		{"users.lookupByEmail", "user", `{"id":"U1"}`, false},
		{"users.getPresence", "presence", `"away"`, false},
		{"conversations.list", "channels", `[{"id":"C1"}]`, true},
		{"users.list", "members", `[{"id":"U1"}]`, true},
	} {
		bodies := []struct {
			name, body string
			valid      bool
		}{
			{"valid", `{"ok":true,"` + tc.field + `":` + tc.valid + `}`, true},
			{"missing acknowledgement", `{"` + tc.field + `":` + tc.valid + `}`, false},
			{"missing data", `{"ok":true}`, false},
			{"null data", `{"ok":true,"` + tc.field + `":null}`, false},
			{"conflicting acknowledgement", `{"ok":true,"error":"no_permission","` + tc.field + `":` + tc.valid + `}`, false},
			{"empty error", `{"ok":false,"error":" "}`, false},
		}
		if tc.list {
			bodies = append(bodies, struct {
				name, body string
				valid      bool
			}{"empty list", `{"ok":true,"` + tc.field + `":[]}`, true}, struct {
				name, body string
				valid      bool
			}{"missing member ID", `{"ok":true,"` + tc.field + `":[{}]}`, false})
		} else if tc.field != "presence" {
			bodies = append(bodies, struct {
				name, body string
				valid      bool
			}{"empty object", `{"ok":true,"` + tc.field + `":{}}`, false})
		} else {
			bodies = append(bodies, struct {
				name, body string
				valid      bool
			}{"empty presence", `{"ok":true,"presence":""}`, false})
		}
		for _, body := range bodies {
			t.Run(tc.method+"/"+body.name, func(t *testing.T) {
				t.Parallel()
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodPost || r.URL.Path != "/api/"+tc.method {
						t.Error("wrong SDK method/path")
					}
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if r.PostForm.Get("token") != "xoxp-synthetic-lookup-response" {
						t.Error("missing synthetic authentication")
					}
					if _, err := io.WriteString(w, body.body); err != nil {
						t.Error(err)
					}
				}))
				defer server.Close()
				client := api.NewSDKClient("xoxp-synthetic-lookup-response", api.SDKOptions{APIURL: server.URL, NoThrottle: true})
				err := callLookup(t.Context(), client, tc.method)
				if (err == nil) != body.valid {
					t.Fatalf("lookup response valid=%t, expected %t: %v", err == nil, body.valid, err)
				}
			})
		}
	}
}

func TestSDKLookupGuardPreservesNativeMultiUserInfo(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api/users.info" || r.PostForm.Get("users") != "U1,W1" || r.PostForm.Has("user") {
			t.Error("native multi-user request was changed")
		}
		if _, err := io.WriteString(w, `{"ok":true,"users":[{"id":"U1"},{"id":"W1"}]}`); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	client := api.NewSDKClient("xoxp-synthetic-many", api.SDKOptions{APIURL: server.URL, NoThrottle: true})
	users, err := client.GetUsersInfoContext(t.Context(), "U1", "W1")
	if err != nil || users == nil || len(*users) != 2 || (*users)[0].ID != "U1" || (*users)[1].ID != "W1" {
		t.Fatalf("native multi-user lookup was lost: %v", err)
	}
}

func TestSDKLookupGuardClosesBodiesAndPreservesSafeCancellation(t *testing.T) {
	t.Parallel()
	for _, cancelNow := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelNow), func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			body := &lookupFailBody{}
			client := api.NewSDKClient("xoxp-synthetic-lookup-body", api.SDKOptions{HTTPClient: &http.Client{Transport: sdkTestRoundTripper(func(r *http.Request) (*http.Response, error) {
				if cancelNow {
					cancel()
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: body, Request: r}, nil
			})}, NoThrottle: true})
			_, err := client.GetUserInfoContext(ctx, "U1")
			if err == nil || strings.Contains(err.Error(), "synthetic-private") || body.closes != 1 {
				t.Fatal("lookup response cleanup or diagnostic protection failed")
			}
			if cancelNow && !errors.Is(err, context.Canceled) {
				t.Fatal("lookup read failure discarded cancellation")
			}
		})
	}
}

type lookupFailBody struct{ closes int }

func (*lookupFailBody) Read([]byte) (int, error) { return 0, errors.New("synthetic-private-read") }
func (b *lookupFailBody) Close() error           { b.closes++; return nil }

func TestSDKLookupGuardDoesNotInterceptBinaryDownloads(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Error("download method changed")
		}
		if _, err := io.WriteString(w, "artifact bytes"); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	client := api.NewSDKClient("xoxp-synthetic-lookup-download", api.SDKOptions{APIURL: server.URL, NoThrottle: true})
	var out bytes.Buffer
	if err := client.GetFileContext(t.Context(), server.URL+"/api/users.info", &out); err != nil || out.String() != "artifact bytes" {
		t.Fatal("lookup guard intercepted a file download")
	}
}

func TestSDKLookupGuardPreservesStructuredScopeFields(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := io.WriteString(w, `{"ok":false,"error":"missing_scope","needed":"users:read","provided":"synthetic-private-data"}`); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	client := api.NewSDKClient("xoxp-synthetic-lookup-scope", api.SDKOptions{APIURL: server.URL, NoThrottle: true})
	_, err := client.GetUserInfoContext(t.Context(), "U1")
	scope, ok := errors.AsType[slackgo.SlackErrorResponse](err)
	if !ok || scope.Needed != "users:read" {
		t.Fatal("lookup guard replaced structured SDK scope failure")
	}
}
