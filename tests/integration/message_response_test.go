//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestBinaryMessageAcknowledgementsFailSafelyAndRecover(t *testing.T) {
	binary := buildSlackBinary(t)
	operations := []struct {
		name, command, method string
		args                  []string
	}{
		{"send", "message.send", "chat.postMessage", []string{"message", "send", "--channel=C123", "--message=PR review", "--no-attribution"}},
		{"reply", "reply", "chat.postMessage", []string{"reply", "--channel=C123", "--parent=1746284582.000000", "--message=PR review", "--no-attribution"}},
		{"edit", "message.edit", "chat.update", []string{"message", "edit", "--channel=C123", "--timestamp=1746284582.000001", "--message=PR review", "--no-attribution"}},
		{"delete", "message.delete", "chat.delete", []string{"message", "delete", "--channel=C123", "--timestamp=1746284582.000001", "--force"}},
		{"schedule", "message.send", "chat.scheduleMessage", []string{"message", "send", "--channel=C123", "--message=PR review", "--schedule=1h", "--no-attribution"}},
		{"scheduled list", "message.scheduled.list", "chat.scheduledMessages.list", []string{"message", "scheduled", "list"}},
		{"scheduled delete", "message.scheduled.delete", "chat.deleteScheduledMessage", []string{"message", "scheduled", "delete", "--channel=C123", "--scheduled-id=Q1"}},
	}
	for _, tc := range []struct {
		name, body string
		exit       int
	}{
		{"missing", `{}`, 5},
		{"null", `null`, 5},
		{"null ack", `{"ok":null}`, 5},
		{"false without error", `{"ok":false}`, 5},
		{"blank error", `{"ok":false,"error":" "}`, 5},
		{"contradictory", `{"ok":true,"error":"internal_error"}`, 5},
		{"wrong ack", `{"ok":"true"}`, 5},
		{"empty", ``, 5},
		{"truncated", `{"private_field":"synthetic-message-response-canary`, 5},
		{"trailing", `{"ok":true} {"private_field":"synthetic-message-response-canary"}`, 5},
		{"permission", `{"ok":false,"error":"missing_scope","needed":"chat:write"}`, 1},
	} {
		for _, op := range operations {
			for _, mode := range []string{"json", "compact", "human"} {
				t.Run(tc.name+"/"+op.name+"/"+mode, func(t *testing.T) {
					path := writeStatusUserConfig(t)
					prior, err := os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
					var repaired atomic.Bool
					var calls, acceptedOriginal, acceptedRecovery atomic.Int32
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if err := r.ParseForm(); err != nil {
							t.Error(err)
						}
						method := strings.TrimPrefix(r.URL.Path, "/api/")
						if method == op.method {
							calls.Add(1)
							if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.PostForm.Get("token") != "xoxb-test" {
								t.Error("chat acknowledgement changed method/path/auth")
							}
							if !repaired.Load() {
								if tc.exit != 1 && method != "chat.scheduledMessages.list" {
									acceptedOriginal.Add(1)
								}
								writeJSON(w, tc.body)
								return
							}
							if method != "chat.scheduledMessages.list" {
								acceptedRecovery.Add(1)
							}
							switch method {
							case "chat.postMessage":
								writeJSON(w, `{"ok":true,"channel":"C123","ts":"1746284582.000002","message":{"type":"message","ts":"1746284582.000002","thread_ts":"1746284582.000000","text":"Server normalized","unreviewed_secret":"synthetic-message-response-canary"}}`)
							case "chat.update":
								writeJSON(w, `{"ok":true,"channel":"C123","ts":"`+r.PostForm.Get("ts")+`","text":"Server normalized"}`)
							case "chat.delete":
								writeJSON(w, `{"ok":true,"channel":"C123","ts":"`+r.PostForm.Get("ts")+`"}`)
							case "chat.scheduleMessage":
								writeJSON(w, `{"ok":true,"channel":"C123","scheduled_message_id":"Q2"}`)
							case "chat.scheduledMessages.list":
								writeJSON(w, `{"ok":true,"scheduled_messages":[{"id":"Q2","channel_id":"C123","post_at":1780000000,"text":"Server normalized"}],"response_metadata":{"next_cursor":"NEXT"}}`)
							default:
								writeJSON(w, `{"ok":true}`)
							}
							return
						}
						switch method {
						case "auth.test", "conversations.info", "chat.getPermalink", "users.info", "users.getPresence":
							writeSlackHTTPRecovery(t, w, r, method)
						default:
							t.Errorf("unexpected follow-up or cleanup mutation: %s", method)
							w.WriteHeader(404)
						}
					}))
					defer server.Close()
					args := append([]string{}, op.args...)
					args = append(args, "--output="+mode, "--color=never", "--no-throttle", "--timeout=2s")
					out, diagnostic, err := runSlackBinary(t, binary, path, server.URL, "", args...)
					if exitCode(err) != tc.exit || calls.Load() != 1 {
						t.Fatalf("invalid message acknowledgement changed exit or replayed: exit=%d want=%d calls=%d", exitCode(err), tc.exit, calls.Load())
					}
					wantApplied := int32(1)
					if tc.exit == 1 || op.method == "chat.scheduledMessages.list" {
						wantApplied = 0
					}
					if acceptedOriginal.Load() != wantApplied || acceptedRecovery.Load() != 0 {
						t.Fatal("message acknowledgement replayed, rolled back or invented remote state")
					}
					assertMessageFailure(t, out, diagnostic, mode, op.command, tc.exit)
					repaired.Store(true)
					recoveryArgs := append([]string{}, args...)
					// A fresh target keeps this control from pretending that deletion is idempotent.
					for i, value := range recoveryArgs {
						if value == "--timestamp=1746284582.000001" {
							recoveryArgs[i] = "--timestamp=1746284582.000002"
						}
						if value == "--scheduled-id=Q1" {
							recoveryArgs[i] = "--scheduled-id=Q2"
						}
					}
					out, diagnostic, err = runSlackBinary(t, binary, path, server.URL, "", recoveryArgs...)
					expectedRecovery := int32(1)
					if op.method == "chat.scheduledMessages.list" {
						expectedRecovery = 0
					}
					if err != nil || diagnostic != "" || out == "" || calls.Load() != 2 || acceptedOriginal.Load() != wantApplied || acceptedRecovery.Load() != expectedRecovery {
						t.Fatalf("explicit message recovery did not preserve prior outcome and make one request: %v", err)
					}
					if strings.Contains(out, "synthetic-message-response-canary") || strings.Contains(out, "xoxb-test") {
						t.Fatal("message recovery exposed unreviewed data")
					}
					if mode != "human" {
						raw := json.RawMessage(out)
						if mode == "json" {
							var frame struct {
								OK     bool
								Data   json.RawMessage
								Meta   struct{ Command string }
								Errors []json.RawMessage
							}
							if json.Unmarshal(raw, &frame) != nil || !frame.OK || frame.Meta.Command != op.command || frame.Errors == nil || len(frame.Errors) != 0 {
								t.Fatal("message recovery lost standard success envelope")
							}
							raw = frame.Data
						}
						var data struct {
							Message     struct{ TS, Text string }
							TS          string
							ScheduledID string                `json:"scheduled_message_id"`
							Scheduled   []struct{ ID string } `json:"scheduled_messages"`
						}
						if json.Unmarshal(raw, &data) != nil {
							t.Fatal("message recovery lost machine data")
						}
						switch op.name {
						case "send", "reply", "edit":
							if data.Message.TS != "1746284582.000002" || data.Message.Text != "Server normalized" {
								t.Fatal("message recovery lost precise timestamp or returned content")
							}
						case "delete":
							if data.TS != "1746284582.000002" {
								t.Fatal("delete recovery lost exact target")
							}
						case "schedule", "scheduled delete":
							if data.ScheduledID != "Q2" {
								t.Fatal("scheduled recovery lost assigned or targeted ID")
							}
						case "scheduled list":
							if len(data.Scheduled) != 1 || data.Scheduled[0].ID != "Q2" {
								t.Fatal("scheduled recovery lost rows")
							}
						}
					}
					after, err := os.ReadFile(path)
					if err != nil || !bytes.Equal(prior, after) {
						t.Fatal("message response failure or recovery changed configuration")
					}
				})
			}
		}
	}
}

func assertMessageFailure(t *testing.T, out, diagnostic, mode, command string, exit int) {
	t.Helper()
	if strings.Contains(out+diagnostic, "synthetic-message-response-canary") || strings.Contains(out+diagnostic, "xoxb-test") {
		t.Fatal("message response failure leaked private data")
	}
	if mode == "human" {
		if out != "" || diagnostic == "" {
			t.Fatal("message human failure used wrong streams")
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
		Errors []struct {
			Type string
			Exit int `json:"exit_code"`
		}
	}
	kind := "server_error"
	if exit == 1 {
		kind = "auth_failure"
	}
	if json.Unmarshal([]byte(out), &keys) != nil || len(keys) != 4 || json.Unmarshal([]byte(out), &frame) != nil || frame.OK || string(frame.Data) != "null" || frame.Meta.Command != command || frame.Meta.Exit != exit || len(frame.Errors) != 1 || frame.Errors[0].Type != kind || frame.Errors[0].Exit != exit || diagnostic != "" {
		t.Fatal("message response lost standard failure frame or streams")
	}
}

func TestBinaryMessagePayloadsRequireUsableResults(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tc := range []struct {
		name, command, method, body string
		args                        []string
	}{
		{"send missing timestamp", "message.send", "chat.postMessage", `{"ok":true,"channel":"C123"}`, []string{"message", "send", "--channel=C123", "--message=PR review", "--no-attribution"}},
		{"reply missing channel", "reply", "chat.postMessage", `{"ok":true,"ts":"1746284582.000001"}`, []string{"reply", "--channel=C123", "--parent=1746284582.000000", "--message=PR review", "--no-attribution"}},
		{"schedule missing ID", "message.send", "chat.scheduleMessage", `{"ok":true,"channel":"C123","ts":"1746284582.000001"}`, []string{"message", "send", "--channel=C123", "--message=PR review", "--schedule=1h", "--no-attribution"}},
		{"scheduled list missing", "message.scheduled.list", "chat.scheduledMessages.list", `{"ok":true}`, []string{"message", "scheduled", "list"}},
		{"scheduled list null", "message.scheduled.list", "chat.scheduledMessages.list", `{"ok":true,"scheduled_messages":null}`, []string{"message", "scheduled", "list"}},
		{"scheduled list partial", "message.scheduled.list", "chat.scheduledMessages.list", `{"ok":true,"scheduled_messages":[{"id":"Q1","channel_id":"C123","post_at":1780000000},{}]}`, []string{"message", "scheduled", "list"}},
		{"scheduled list missing channel", "message.scheduled.list", "chat.scheduledMessages.list", `{"ok":true,"scheduled_messages":[{"id":"Q1","post_at":1780000000}]}`, []string{"message", "scheduled", "list"}},
		{"scheduled list missing time", "message.scheduled.list", "chat.scheduledMessages.list", `{"ok":true,"scheduled_messages":[{"id":"Q1","channel_id":"C123"}]}`, []string{"message", "scheduled", "list"}},
	} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				path := writeStatusUserConfig(t)
				before, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					method := strings.TrimPrefix(r.URL.Path, "/api/")
					if method == tc.method {
						calls.Add(1)
						writeJSON(w, tc.body)
						return
					}
					if method != "auth.test" {
						t.Error("invalid result triggered enrichment or another request")
						w.WriteHeader(404)
						return
					}
					writeSlackHTTPRecovery(t, w, r, method)
				}))
				defer server.Close()
				args := append([]string{}, tc.args...)
				args = append(args, "--output="+mode, "--color=never", "--no-throttle", "--timeout=2s")
				out, diagnostic, err := runSlackBinary(t, binary, path, server.URL, "", args...)
				if exitCode(err) != 5 || calls.Load() != 1 {
					t.Fatalf("incomplete chat result reported success or replayed: exit=%d calls=%d", exitCode(err), calls.Load())
				}
				assertMessageFailure(t, out, diagnostic, mode, tc.command, 5)
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatal("incomplete chat result changed local configuration")
				}
			})
		}
	}
}

func TestBinaryEmptyScheduledPageKeepsContinuation(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, mode := range []string{"json", "compact", "human"} {
		t.Run(mode, func(t *testing.T) {
			path := writeStatusUserConfig(t)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				method := strings.TrimPrefix(r.URL.Path, "/api/")
				if method == "auth.test" {
					writeSlackHTTPRecovery(t, w, r, method)
					return
				}
				calls.Add(1)
				if method != "chat.scheduledMessages.list" || r.Method != http.MethodPost || r.PostForm.Get("token") != "xoxb-test" || r.PostForm.Get("cursor") != "CUR" || r.PostForm.Get("limit") != "1" || r.PostForm.Has("channel") {
					t.Error("scheduled page changed method/auth/optional arguments")
				}
				writeJSON(w, `{"ok":true,"scheduled_messages":[],"response_metadata":{"next_cursor":"NEXT"}}`)
			}))
			defer server.Close()
			out, diagnostic, err := runSlackBinary(t, binary, path, server.URL, "", "message", "scheduled", "list", "--cursor=CUR", "--limit=1", "--output="+mode, "--color=never", "--no-throttle", "--timeout=2s")
			if err != nil || diagnostic != "" || calls.Load() != 1 {
				t.Fatalf("explicit empty scheduled page failed or replayed: %v", err)
			}
			if mode == "human" {
				for _, part := range []string{"has_more=true", "items_returned=0", "next_cursor=NEXT"} {
					if !strings.Contains(out, part) {
						t.Fatal("empty scheduled page lost human continuation")
					}
				}
				return
			}
			data := json.RawMessage(out)
			if mode == "json" {
				var frame struct {
					OK   bool
					Data json.RawMessage
					Meta struct{ Pagination map[string]json.RawMessage }
				}
				if json.Unmarshal(data, &frame) != nil || !frame.OK || string(frame.Meta.Pagination["items_returned"]) != "0" || string(frame.Meta.Pagination["next_cursor"]) != `"NEXT"` {
					t.Fatal("empty scheduled page lost envelope continuation")
				}
				data = frame.Data
			}
			var result struct {
				Scheduled  []json.RawMessage `json:"scheduled_messages"`
				Pagination map[string]json.RawMessage
			}
			if json.Unmarshal(data, &result) != nil || result.Scheduled == nil || len(result.Scheduled) != 0 || string(result.Pagination["items_returned"]) != "0" || string(result.Pagination["has_more"]) != "true" || string(result.Pagination["next_cursor"]) != `"NEXT"` {
				t.Fatal("empty scheduled page became missing data or lost continuation")
			}
		})
	}
}
