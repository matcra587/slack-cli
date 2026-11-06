//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBinaryStatusFlagsUpdateResetAndDryRun(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, mode := range []string{"json", "compact", "human"} {
		t.Run(mode, func(t *testing.T) {
			path := writeStatusUserConfig(t)
			prior, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			calls, authCalls := 0, 0
			state := map[string]any{"status_text": "Previous status", "status_emoji": ":calendar:", "status_expiration": float64(2_000_000_000), "display_name": "Fixture user", "unreviewed_secret": "synthetic-status-canary"}
			snapshot := func() map[string]any {
				mu.Lock()
				defer mu.Unlock()
				return maps.Clone(state)
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.PostForm.Get("token") != "xoxb-test" || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || r.Header.Get("Cookie") != "" {
					t.Error("wrong status method, auth or encoding")
				}
				switch r.URL.Path {
				case "/api/auth.test":
					mu.Lock()
					authCalls++
					mu.Unlock()
					if len(r.PostForm) != 1 {
						t.Error("unexpected scope-check parameters")
					}
					w.Header().Set("X-Oauth-Scopes", "users.profile:write")
					writeJSON(w, `{"ok":true,"user_id":"UFIXTURE"}`)
				case "/api/users.profile.set":
					var fields map[string]any
					if len(r.PostForm) != 2 || json.Unmarshal([]byte(r.PostForm.Get("profile")), &fields) != nil || len(fields) != 3 {
						t.Error("status update added unrelated parameters or omitted reset fields")
					}
					for _, key := range []string{"status_text", "status_emoji", "status_expiration"} {
						if _, ok := fields[key]; !ok {
							t.Error("status update omitted required reset field")
						}
					}
					mu.Lock()
					calls++
					maps.Copy(state, fields)
					body, err := json.Marshal(map[string]any{"ok": true, "profile": state})
					mu.Unlock()
					if err != nil {
						t.Error(err)
						return
					}
					writeJSON(w, string(body))
				default:
					t.Error("unexpected Slack operation")
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			steps := []struct {
				name        string
				args        []string
				text, emoji string
				expiration  int64
				relative    bool
			}{
				{name: "long flags", args: []string{"set", "--text= PR review ", "--emoji=::eyes::"}, text: "PR review", emoji: ":eyes:"},
				{name: "short flags", args: []string{"set", "-t", "Heads down", "-e", "headphones", "-x", "2h"}, text: "Heads down", emoji: ":headphones:", relative: true},
				{name: "remove emoji and expiration", args: []string{"set", "--text=Reading docs"}, text: "Reading docs"},
				{name: "emoji only", args: []string{"set", "-e", "eyes"}, emoji: ":eyes:"},
				{name: "positional", args: []string{"set", "In a meeting", ":calendar:"}, text: "In a meeting", emoji: ":calendar:"},
				{name: "nonempty flag precedence", args: []string{"set", "positional text", "calendar", "--text=Release review", "--emoji=eyes"}, text: "Release review", emoji: ":eyes:"},
				{name: "empty flag fallback", args: []string{"set", "PR review", "eyes", "--text=", "--emoji="}, text: "PR review", emoji: ":eyes:"},
				{name: "long expiry", args: []string{"set", "--text=Heads down", "--expires-in=2h"}, text: "Heads down", relative: true},
				{name: "explicit zero duration", args: []string{"set", "--text=Reading docs", "-x", "0"}, text: "Reading docs"},
				{name: "absolute RFC3339", args: []string{"set", "--text=Vacation", "--until=2033-05-18T03:33:20Z"}, text: "Vacation", expiration: 2_000_000_000},
				{name: "absolute Unix short flag", args: []string{"set", "--text=Vacation", "-U", "2000000000"}, text: "Vacation", expiration: 2_000_000_000},
				{name: "relative until", args: []string{"set", "--text=Heads down", "--until=+2h"}, text: "Heads down", relative: true},
				{name: "zero until", args: []string{"set", "--text=Reading docs", "--until=0"}, text: "Reading docs"},
				{name: "zero duration with absolute until", args: []string{"set", "--text=Vacation", "--expires-in=0", "--until=2000000000"}, text: "Vacation", expiration: 2_000_000_000},
				{name: "empty until with positive duration", args: []string{"set", "--text=Heads down", "--expires-in=2h", "--until="}, text: "Heads down", relative: true},
				{name: "plain text preserved", args: []string{"set", "--text=*PR review* <@UFIXTURE>"}, text: "*PR review* <@UFIXTURE>"},
				{name: "clear", args: []string{"clear"}},
			}
			for _, step := range steps {
				for _, dryFlag := range []string{"--dry-run", "-n", ""} {
					t.Run(step.name+"/"+dryFlag, func(t *testing.T) {
						before := snapshot()
						mu.Lock()
						oldCalls, oldAuth := calls, authCalls
						mu.Unlock()
						args := append([]string{"status"}, step.args...)
						args = append(args, "--output="+mode, "--color=never", "--timeout=2s", "--no-throttle")
						if dryFlag != "" {
							args = append(args, dryFlag)
						}
						start := time.Now()
						out, diagnostic, err := runSlackBinary(t, binary, path, server.URL, "", args...)
						end := time.Now()
						if err != nil || diagnostic != "" {
							t.Fatalf("status flag invocation failed: %v", err)
						}
						after := snapshot()
						mu.Lock()
						gotCalls, gotAuth := calls-oldCalls, authCalls-oldAuth
						mu.Unlock()
						expected := 1
						if dryFlag != "" {
							expected = 0
							if !reflect.DeepEqual(before, after) {
								t.Fatal("status dry run changed prior profile state")
							}
						}
						if gotCalls != expected || gotAuth != expected {
							t.Fatal("status flag execution retried or dry run accessed Slack")
						}
						exp := step.expiration
						if dryFlag == "" {
							if after["status_text"] != step.text || after["status_emoji"] != step.emoji || after["display_name"] != "Fixture user" || after["unreviewed_secret"] != "synthetic-status-canary" || len(after) != 5 {
								t.Fatal("status update lost requested fields, reset, or unrelated state")
							}
							if step.relative {
								got, ok := after["status_expiration"].(float64)
								if !ok || int64(got) < start.Add(2*time.Hour).Unix() || int64(got) > end.Add(2*time.Hour).Unix() {
									t.Fatal("relative expiry did not use elapsed seconds")
								}
								exp = int64(got)
							} else if after["status_expiration"] != float64(exp) {
								t.Fatal("status omitted expiry reset or changed absolute seconds")
							}
						}
						assertStatusFlagSuccess(t, out, mode, step.args[0], step.text, step.emoji, exp, step.relative && dryFlag != "", dryFlag != "", start, end)
						saved, err := os.ReadFile(path)
						if err != nil || !bytes.Equal(prior, saved) {
							t.Fatal("status flag changed local configuration")
						}
					})
				}
			}
		})
	}
}

func writeStatusUserConfig(t *testing.T) string {
	t.Helper()
	path := writePipeConfig(t)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body = bytes.Replace(body, []byte(`token_type = "bot"`), []byte(`token_type = "user"`), 1)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertStatusFlagSuccess(t *testing.T, out, mode, action, text, emoji string, expiration int64, relative, dry bool, start, end time.Time) {
	t.Helper()
	if strings.Contains(out, "synthetic-status-canary") || strings.Contains(out, "xoxb-test") {
		t.Fatal("status output exposed private profile field or credentials")
	}
	if mode == "human" {
		if !strings.Contains(out, "Status ") {
			t.Fatal("status lost human action output")
		}
		if expiration != 0 && !strings.Contains(out, fmt.Sprintf("expiration=%d", expiration)) {
			t.Fatal("status lost human expiry")
		}
		return
	}
	assertStatusMetadata(t, out, mode, action, 0)
	raw := json.RawMessage(out)
	if mode == "json" {
		var keys map[string]json.RawMessage
		var frame struct {
			OK     bool
			Data   json.RawMessage
			Meta   struct{ Command string }
			Errors []json.RawMessage
		}
		if json.Unmarshal(raw, &keys) != nil || len(keys) != 4 || json.Unmarshal(raw, &frame) != nil || !frame.OK || frame.Meta.Command != "status."+action || frame.Errors == nil || len(frame.Errors) != 0 {
			t.Fatal("status flags lost standard success envelope")
		}
		raw = frame.Data
	}
	var data map[string]any
	if json.Unmarshal(raw, &data) != nil {
		t.Fatal("status flags lost machine data")
	}
	want := map[string]any{}
	if text != "" {
		want["text"] = text
	}
	if emoji != "" {
		want["emoji"] = emoji
	}
	if dry {
		want["dry_run"] = true
	}
	if relative {
		got, ok := data["expiration"].(float64)
		if !ok || int64(got) < start.Add(2*time.Hour).Unix() || int64(got) > end.Add(2*time.Hour).Unix() {
			t.Fatal("dry-run relative expiry did not use elapsed seconds")
		}
		expiration = int64(got)
	}
	if expiration != 0 {
		want["expiration"] = float64(expiration)
	}
	if !reflect.DeepEqual(data, want) {
		t.Fatal("status flag output lost requested values or stable omitted fields")
	}
}

func TestBinaryStatusValidationPreservesStateAndRecovers(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"empty set", []string{"set"}},
		{"blank text", []string{"set", "--text=   "}},
		{"empty emoji", []string{"set", "--emoji=::::"}},
		{"bad duration", []string{"set", "PR review", "--expires-in=tomorrow"}},
		{"negative duration", []string{"set", "PR review", "-x=-1s"}},
		{"bad absolute time", []string{"set", "PR review", "--until=tomorrow"}},
		{"bad relative time", []string{"set", "PR review", "--until=+tomorrow"}},
		{"bad clock", []string{"set", "PR review", "-U=25:00"}},
		{"conflicting expiry", []string{"set", "PR review", "--expires-in=1h", "--until=2000000000"}},
		{"extra set argument", []string{"set", "PR review", "eyes", "extra"}},
		{"extra clear argument", []string{"clear", "extra"}},
		{"set-only flag on clear", []string{"clear", "--emoji=eyes"}},
	} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				path := writeStatusUserConfig(t)
				prior, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var mu sync.Mutex
				calls := 0
				state := "prior status"
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					defer mu.Unlock()
					calls++
					switch r.URL.Path {
					case "/api/auth.test":
						w.Header().Set("X-Oauth-Scopes", "users.profile:write")
						writeJSON(w, `{"ok":true}`)
					case "/api/users.profile.set":
						state = "recovered status"
						writeJSON(w, `{"ok":true}`)
					default:
						t.Error("unexpected status validation request")
						w.WriteHeader(404)
					}
				}))
				defer server.Close()
				args := append([]string{"status"}, tc.args...)
				args = append(args, "--output="+mode, "--color=never", "--no-throttle", "--timeout=2s")
				out, diagnostic, err := runSlackBinary(t, binary, path, server.URL, "", args...)
				if exitCode(err) != 4 {
					t.Fatalf("invalid status arguments returned exit=%d want=4", exitCode(err))
				}
				assertStatusFailureFrame(t, out, diagnostic, tc.args[0], mode, 4)
				assertStatusMetadata(t, out, mode, tc.args[0], 4)
				mu.Lock()
				untouched := calls == 0 && state == "prior status"
				mu.Unlock()
				if !untouched {
					t.Fatal("invalid status arguments accessed Slack or changed prior state")
				}
				out, diagnostic, err = runSlackBinary(t, binary, path, server.URL, "", "status", "set", "recovered status", "--output="+mode, "--color=never", "--no-throttle", "--timeout=2s")
				mu.Lock()
				recovered := calls == 2 && state == "recovered status"
				mu.Unlock()
				if err != nil || diagnostic != "" || out == "" || !recovered {
					t.Fatalf("status validation did not recover on explicit valid invocation: %v", err)
				}
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(prior, after) {
					t.Fatal("status validation changed local configuration")
				}
			})
		}
	}
}

func TestBinaryStatusTokenScopesAndServiceFailuresPreserveState(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tc := range []struct {
		name, scopes, body            string
		status, exit, auth, mutations int
		bot                           bool
	}{
		{name: "bot token", scopes: "users.profile:write", body: `{"ok":false,"error":"missing_scope","needed":"users.profile:write"}`, exit: 1, bot: true},
		{name: "wrong scopes", scopes: "users:read,chat:write", body: `{"ok":false,"error":"missing_scope","needed":"users.profile:write"}`, exit: 1, auth: 1},
		{name: "Slack scope denial", scopes: "users.profile:write", body: `{"ok":false,"error":"missing_scope","needed":"users.profile:write"}`, exit: 1, auth: 1, mutations: 1},
		{name: "Slack permission denial", scopes: "users.profile:write", body: `{"ok":false,"error":"no_permission"}`, exit: 1, auth: 1, mutations: 1},
		{name: "service unavailable", scopes: "users.profile:write", body: `synthetic-status-canary`, status: 503, exit: 5, auth: 1, mutations: 1},
		{name: "missing scope header", body: `{"ok":false,"error":"missing_scope","needed":"users.profile:write"}`, exit: 1, auth: 1, mutations: 1},
	} {
		for _, action := range []string{"set", "clear"} {
			for _, mode := range []string{"json", "compact", "human"} {
				t.Run(tc.name+"/"+action+"/"+mode, func(t *testing.T) {
					path := writeStatusUserConfig(t)
					if tc.bot {
						body, err := os.ReadFile(path)
						if err != nil {
							t.Fatal(err)
						}
						body = bytes.Replace(body, []byte(`token_type = "user"`), []byte(`token_type = "bot"`), 1)
						if err := os.WriteFile(path, body, 0o600); err != nil {
							t.Fatal(err)
						}
					}
					prior, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					var mu sync.Mutex
					recovered := false
					authCalls, mutations := 0, 0
					state := "prior status"
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if err := r.ParseForm(); err != nil {
							t.Error(err)
						}
						if r.Method != http.MethodPost || r.PostForm.Get("token") != "xoxb-test" || r.URL.RawQuery != "" {
							t.Error("wrong status authentication or method")
						}
						mu.Lock()
						defer mu.Unlock()
						switch r.URL.Path {
						case "/api/auth.test":
							authCalls++
							scopes := tc.scopes
							if recovered {
								scopes = "users.profile:write"
							}
							if scopes != "" {
								w.Header().Set("X-Oauth-Scopes", scopes)
							}
							writeJSON(w, `{"ok":true,"user_id":"UFIXTURE"}`)
						case "/api/users.profile.set":
							mutations++
							if recovered {
								var profile struct {
									Text string `json:"status_text"`
								}
								if json.Unmarshal([]byte(r.PostForm.Get("profile")), &profile) != nil {
									t.Error("invalid recovery profile")
								}
								state = profile.Text
								writeJSON(w, `{"ok":true}`)
								return
							}
							if tc.status != 0 {
								w.WriteHeader(tc.status)
							}
							writeJSON(w, tc.body)
						default:
							t.Error("unexpected status operation")
							w.WriteHeader(404)
						}
					}))
					defer server.Close()
					args := []string{"status", action, "--output=" + mode, "--color=never", "--no-throttle", "--timeout=2s"}
					if action == "set" {
						args = append(args, "--text=PR review")
					}
					planArgs := append(append([]string{}, args...), "--dry-run")
					planOut, planDiagnostic, planErr := runSlackBinary(t, binary, path, server.URL, "", planArgs...)
					if planErr != nil || planDiagnostic != "" {
						t.Fatalf("status preview required permissions or a working service: %v", planErr)
					}
					planText := "PR review"
					if action == "clear" {
						planText = ""
					}
					assertStatusFlagSuccess(t, planOut, mode, action, planText, "", 0, false, true, time.Time{}, time.Time{})
					mu.Lock()
					planned := authCalls == 0 && mutations == 0 && state == "prior status"
					mu.Unlock()
					if !planned {
						t.Fatal("status preview accessed Slack or changed prior state")
					}
					out, diagnostic, err := runSlackBinary(t, binary, path, server.URL, "", args...)
					if exitCode(err) != tc.exit {
						t.Fatalf("status guard returned exit=%d want=%d", exitCode(err), tc.exit)
					}
					assertStatusFailureFrame(t, out, diagnostic, action, mode, tc.exit)
					assertStatusMetadata(t, out, mode, action, tc.exit)
					mu.Lock()
					preserved := authCalls == tc.auth && mutations == tc.mutations && state == "prior status"
					mu.Unlock()
					if !preserved {
						t.Fatal("status guard bypassed preflight, replayed a mutation or changed prior state")
					}
					if mode != "human" && (tc.name == "wrong scopes" || tc.name == "Slack scope denial" || tc.name == "missing scope header") {
						var frame struct {
							Errors []struct {
								Details struct {
									Needed []string `json:"needed"`
								}
							}
						}
						if json.Unmarshal([]byte(out), &frame) != nil || len(frame.Errors) != 1 || !reflect.DeepEqual(frame.Errors[0].Details.Needed, []string{"users.profile:write"}) {
							t.Fatal("status scope error lost structured requirement")
						}
					}
					after, err := os.ReadFile(path)
					if err != nil || !bytes.Equal(prior, after) {
						t.Fatal("failed status mutation changed local configuration")
					}
					if tc.bot {
						prior = bytes.Replace(prior, []byte(`token_type = "bot"`), []byte(`token_type = "user"`), 1)
						if err := os.WriteFile(path, prior, 0o600); err != nil {
							t.Fatal(err)
						}
					}
					mu.Lock()
					recovered = true
					mu.Unlock()
					out, diagnostic, err = runSlackBinary(t, binary, path, server.URL, "", args...)
					mu.Lock()
					healed := authCalls == tc.auth+1 && mutations == tc.mutations+1 && state == planText
					mu.Unlock()
					if err != nil || diagnostic != "" || out == "" || !healed {
						t.Fatalf("status guard did not recover once after permission/service repair: %v", err)
					}
					after, err = os.ReadFile(path)
					if err != nil || !bytes.Equal(prior, after) {
						t.Fatal("recovered status changed local configuration")
					}
				})
			}
		}
	}
}

func assertStatusMetadata(t *testing.T, out, mode, action string, exit int) {
	t.Helper()
	if mode == "human" || (mode == "compact" && exit == 0) {
		return
	}
	var frame struct{ Meta map[string]json.RawMessage }
	if json.Unmarshal([]byte(out), &frame) != nil || len(frame.Meta) != 5 {
		t.Fatal("status result lost common metadata fields")
	}
	var meta struct {
		Command, Workspace, Timestamp string
		RequestID                     string `json:"request_id"`
		Exit                          int    `json:"exit_code"`
	}
	body, err := json.Marshal(frame.Meta)
	if err != nil || json.Unmarshal(body, &meta) != nil || meta.Command != "status."+action || meta.Workspace != "default" || meta.Exit != exit || strings.TrimSpace(meta.RequestID) == "" {
		t.Fatal("status result metadata inconsistent")
	}
	if _, err := time.Parse(time.RFC3339, meta.Timestamp); err != nil {
		t.Fatal("status timestamp is not RFC3339")
	}
}
