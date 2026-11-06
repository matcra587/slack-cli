//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestBinaryStatusRequiresAcknowledgementAndRecovers(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tc := range []struct {
		name, body string
		exit       int
	}{
		{"missing", `{}`, 5},
		{"null", `null`, 5},
		{"false without error", `{"ok":false}`, 5},
		{"blank error", `{"ok":false,"error":" "}`, 5},
		{"contradictory", `{"ok":true,"error":"internal_error"}`, 5},
		{"wrong acknowledgement", `{"ok":"true"}`, 5},
		{"trailing document", `{"ok":true} {"secret":"synthetic-status-canary"}`, 5},
		{"truncated", `{"private_field":"synthetic-status-canary`, 5},
		{"empty", ``, 5},
		{"permission", `{"ok":false,"error":"missing_scope","needed":"users.profile:write"}`, 1},
	} {
		for _, action := range []string{"set", "clear"} {
			for _, mode := range []string{"json", "compact", "human"} {
				t.Run(tc.name+"/"+action+"/"+mode, func(t *testing.T) {
					path := writePipeConfig(t)
					prior, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					prior = bytes.Replace(prior, []byte(`token_type = "bot"`), []byte(`token_type = "user"`), 1)
					if err := os.WriteFile(path, prior, 0o600); err != nil {
						t.Fatal(err)
					}
					var recovered atomic.Bool
					var calls, authCalls atomic.Int32
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if err := r.ParseForm(); err != nil {
							t.Error(err)
						}
						if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.PostForm.Get("token") != "xoxb-test" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
							t.Error("wrong method, parameters or synthetic authentication")
						}
						w.Header().Set("Content-Type", "application/json")
						switch r.URL.Path {
						case "/api/auth.test":
							authCalls.Add(1)
							w.Header().Set("X-Oauth-Scopes", "users.profile:write")
							writeJSON(w, `{"ok":true,"user_id":"UFIXTURE"}`)
						case "/api/users.profile.set":
							calls.Add(1)
							text, emoji := "PR review", ":eyes:"
							if action == "clear" {
								text, emoji = "", ""
							}
							var profile map[string]any
							if json.Unmarshal([]byte(r.PostForm.Get("profile")), &profile) != nil || !reflect.DeepEqual(profile, map[string]any{"status_text": text, "status_emoji": emoji, "status_expiration": float64(0)}) || len(r.PostForm) != 2 {
								t.Error("status changed unrelated profile fields or omitted reset values")
							}
							if recovered.Load() {
								writeJSON(w, `{"ok":true,"profile":{"status_text":"PR review","private_field":"synthetic-status-canary"}}`)
							} else {
								writeJSON(w, tc.body)
							}
						default:
							t.Error("unexpected Slack request")
							w.WriteHeader(404)
						}
					}))
					defer server.Close()
					args := []string{"status", action, "--output=" + mode, "--color=never", "--no-throttle", "--timeout=2s"}
					if action == "set" {
						args = append(args, "--text=PR review", "--emoji=eyes")
					}
					out, diagnostic, err := runSlackBinary(t, binary, path, server.URL, "", args...)
					if exitCode(err) != tc.exit {
						t.Fatalf("status returned exit=%d want=%d", exitCode(err), tc.exit)
					}
					if calls.Load() != 1 || authCalls.Load() != 1 {
						t.Fatal("status mutation was replayed or bypassed scope preflight")
					}
					assertStatusFailureFrame(t, out, diagnostic, action, mode, tc.exit)
					recovered.Store(true)
					out, diagnostic, err = runSlackBinary(t, binary, path, server.URL, "", args...)
					if err != nil || diagnostic != "" || out == "" || calls.Load() != 2 || authCalls.Load() != 2 {
						t.Fatalf("status did not recover exactly once on new invocation: %v", err)
					}
					if strings.Contains(out, "synthetic-status-canary") || strings.Contains(out, "xoxb-test") {
						t.Fatal("recovery output exposed unreviewed fields or credentials")
					}
					data := json.RawMessage(out)
					if mode == "json" {
						var frame struct {
							OK     bool
							Data   json.RawMessage
							Meta   struct{ Command string }
							Errors []json.RawMessage
						}
						if json.Unmarshal(data, &frame) != nil || !frame.OK || frame.Meta.Command != "status."+action || frame.Errors == nil || len(frame.Errors) != 0 {
							t.Fatal("status recovery lost success envelope")
						}
						data = frame.Data
					}
					if mode != "human" {
						want := map[string]any{}
						if action == "set" {
							want = map[string]any{"text": "PR review", "emoji": ":eyes:"}
						}
						var got map[string]any
						if json.Unmarshal(data, &got) != nil || !reflect.DeepEqual(got, want) {
							t.Fatal("status recovery changed requested-result contract")
						}
					}
					after, err := os.ReadFile(path)
					if err != nil || !bytes.Equal(prior, after) {
						t.Fatal("status changed local configuration")
					}
				})
			}
		}
	}
}

func assertStatusFailureFrame(t *testing.T, out, diagnostic, action, mode string, exit int) {
	t.Helper()
	if strings.Contains(out+diagnostic, "synthetic-status-canary") || strings.Contains(out+diagnostic, "xoxb-test") {
		t.Fatal("status failure exposed credentials or unreviewed response")
	}
	if mode == "human" {
		if out != "" || diagnostic == "" {
			t.Fatal("wrong human failure streams")
		}
		return
	}
	var keys map[string]json.RawMessage
	var frame struct {
		OK   bool
		Data json.RawMessage
		Meta struct {
			Command string
			Exit    int `json:"exit_code"`
		}
		Errors []json.RawMessage
	}
	if json.Unmarshal([]byte(out), &keys) != nil || len(keys) != 4 || json.Unmarshal([]byte(out), &frame) != nil || frame.OK || string(frame.Data) != "null" || frame.Meta.Command != "status."+action || frame.Meta.Exit != exit || len(frame.Errors) != 1 || diagnostic != "" {
		t.Fatal("status failure lost standard envelope or streams")
	}
}
