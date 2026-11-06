//go:build integration

package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

type scheduledFixtureRow struct {
	ID, Channel, Text string
	PostAt            int64
}

func TestBinaryScheduledCancellationLifecycle(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, kind := range []string{"bot", "user"} {
		for _, mode := range []string{"json", "compact", "human"} {
			for _, short := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/short=%t", kind, mode, short), func(t *testing.T) {
					const private = "synthetic-scheduled-private-canary"
					path := filepath.Join(t.TempDir(), "config.toml")
					config := fmt.Sprintf("schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"T123\"\ntoken_type=%q\ntoken_ref=\"env:SLACK_TEST_TOKEN\"\ndefault_channel=\"C_UNRELATED\"\n[workspaces.fixture.aliases]\nrelease=\"C_TARGET\"\n", kind)
					if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
						t.Fatal(err)
					}
					var mu sync.Mutex
					unrelated := scheduledFixtureRow{"Q_UNRELATED", "C_UNRELATED", "Unrelated release note", 2_000_000_000}
					pending := map[string]scheduledFixtureRow{
						unrelated.ID: unrelated,
						"Q_EXTERNAL": {"Q_EXTERNAL", "C_TARGET", "Scheduled outside this process", 2_000_000_000},
					}
					scope, behavior := "chat:write", "normal"
					calls, deletes, creates, nextID := 0, 0, 0, 0
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						mu.Lock()
						defer mu.Unlock()
						calls++
						if err := r.ParseForm(); err != nil {
							t.Error(err)
						}
						if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.PostForm.Get("token") != "xoxb-test" || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
							t.Error("scheduled lifecycle changed request method/path/encoding/authentication")
						}
						switch r.URL.Path {
						case "/api/auth.test":
							w.Header().Set("X-OAuth-Scopes", scope)
							writeJSON(w, `{"ok":true,"user_id":"U1"}`)
						case "/api/chat.scheduleMessage":
							creates++
							nextID++
							id := fmt.Sprintf("Q%d", nextID)
							at, err := strconv.ParseInt(r.PostForm.Get("post_at"), 10, 64)
							if err != nil || at <= 0 || r.PostForm.Get("channel") != "C_TARGET" || r.PostForm.Get("text") != "Release review follow-up" {
								t.Error("scheduled creation parameters differ")
							}
							pending[id] = scheduledFixtureRow{id, "C_TARGET", "Release review follow-up", at}
							writeJSON(w, `{"ok":true,"channel":"C_TARGET","scheduled_message_id":"`+id+`","unreviewed_secret":"`+private+`"}`)
						case "/api/chat.scheduledMessages.list":
							var ids []string
							for id := range pending {
								ids = append(ids, id)
							}
							slices.Sort(ids)
							rows := []map[string]any{}
							for _, id := range ids {
								row := pending[id]
								if channel := r.PostForm.Get("channel"); channel != "" && row.Channel != channel {
									continue
								}
								rows = append(rows, map[string]any{"id": row.ID, "channel_id": row.Channel, "post_at": row.PostAt, "text": row.Text, "unreviewed_secret": private})
							}
							_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "scheduled_messages": rows, "response_metadata": map[string]string{"next_cursor": ""}})
						case "/api/chat.deleteScheduledMessage":
							deletes++
							id, channel := r.PostForm.Get("scheduled_message_id"), r.PostForm.Get("channel")
							if r.PostForm.Get("as_user") != "true" || len(r.PostForm) != 4 {
								t.Error("scheduled cancellation parameters differ")
							}
							if behavior == "denied" {
								writeJSON(w, `{"ok":false,"error":"no_permission"}`)
								return
							}
							row, exists := pending[id]
							if !exists {
								writeJSON(w, `{"ok":false,"error":"invalid_scheduled_message_id"}`)
								return
							}
							if row.Channel != channel {
								writeJSON(w, `{"ok":false,"error":"channel_not_found"}`)
								return
							}
							delete(pending, id)
							if behavior == "lost acknowledgement" {
								writeJSON(w, `{"unreviewed_secret":"`+private+`"}`)
								return
							}
							writeJSON(w, `{"ok":true}`)
						case "/api/conversations.info":
							writeJSON(w, `{"ok":false,"error":"missing_scope"}`)
						default:
							t.Errorf("unexpected lifecycle request: %s", r.URL.Path)
							w.WriteHeader(500)
						}
					}))
					defer server.Close()
					call := func(label string, code int, args ...string) string {
						t.Helper()
						args = append(args, "--output="+mode, "--color=never", "--no-throttle")
						out, diagnostic, err := runSlackBinary(t, binary, path, server.URL, "", args...)
						mu.Lock()
						got := pending[unrelated.ID]
						mu.Unlock()
						if got != unrelated {
							t.Fatalf("%s changed unrelated scheduled message", label)
						}
						if exitCode(err) != code {
							t.Fatalf("%s: exit=%d want=%d: %s %s", label, exitCode(err), code, out, diagnostic)
						}
						if strings.Contains(out+diagnostic, private) || strings.Contains(out+diagnostic, "xoxb-test") {
							t.Fatalf("%s disclosed unreviewed data or credentials", label)
						}
						if mode == "human" {
							if code == 0 && diagnostic != "" {
								t.Fatalf("%s wrote diagnostics", label)
							}
							if code != 0 && (out != "" || diagnostic == "") {
								t.Fatalf("%s used wrong human error streams", label)
							}
						} else {
							if diagnostic != "" {
								t.Fatalf("%s escaped machine envelope", label)
							}
							if !json.Valid([]byte(out)) {
								t.Fatalf("%s returned malformed machine output", label)
							}
							if mode == "json" || code != 0 {
								wantCommand := "message." + args[1]
								if args[1] == "scheduled" {
									wantCommand += "." + args[2]
								}
								var fields map[string]json.RawMessage
								if json.Unmarshal([]byte(out), &fields) != nil || len(fields) != 4 {
									t.Fatalf("%s changed the common envelope shape", label)
								}
								var frame struct {
									OK     bool
									Data   json.RawMessage
									Errors []json.RawMessage
									Meta   struct {
										Code      int `json:"exit_code"`
										Command   string
										Workspace string
									}
								}
								if json.Unmarshal([]byte(out), &frame) != nil || frame.OK != (code == 0) || frame.Meta.Code != code || frame.Meta.Command != wantCommand || frame.Meta.Workspace != "fixture" {
									t.Fatalf("%s lost common envelope", label)
								}
								if code != 0 && (string(frame.Data) != "null" || len(frame.Errors) != 1) {
									t.Fatalf("%s published a failed operation as success", label)
								}
							}
						}
						after, err := os.ReadFile(path)
						if err != nil || string(after) != config {
							t.Fatalf("%s changed prior config", label)
						}

						return out
					}
					channelFlag := "--channel"
					if short {
						channelFlag = "-c"
					}
					cancelArgs := func(id string) []string {
						return []string{"message", "scheduled", "delete", channelFlag, " release ", "--scheduled-id", " " + id + " "}
					}
					create := func(id string) {
						t.Helper()
						out := call("create", 0, "message", "send", "--channel=release", "--message=Release review follow-up", "--schedule=90m", "--no-attribution")
						if mode == "human" {
							if !slices.Contains(strings.Fields(out), "scheduled_message_id="+id) {
								t.Fatal("creation receipt lost assigned scheduling ID")
							}
						} else {
							raw := []byte(out)
							if mode == "json" {
								var frame struct{ Data json.RawMessage }
								if json.Unmarshal(raw, &frame) != nil {
									t.Fatal("invalid creation envelope")
								}
								raw = frame.Data
							}
							var data struct {
								ID string `json:"scheduled_message_id"`
							}
							if json.Unmarshal(raw, &data) != nil || data.ID != id {
								t.Fatal("creation receipt lost assigned scheduling ID")
							}
						}
					}
					snapshot := func(id string) (scheduledFixtureRow, bool) {
						mu.Lock()
						defer mu.Unlock()
						row, ok := pending[id]
						return row, ok
					}
					count := func() (int, int) { mu.Lock(); defer mu.Unlock(); return calls, deletes }
					attempt := func(label string, code, mutationCount int, args ...string) string {
						t.Helper()
						_, before := count()
						out := call(label, code, args...)
						_, after := count()
						if after-before != mutationCount {
							t.Fatalf("%s omitted or retried cancellation", label)
						}
						return out
					}
					assertPresent := func(id string, want bool) {
						t.Helper()
						_, ok := snapshot(id)
						if ok != want {
							t.Fatalf("unexpected scheduled state for %s: present=%t want=%t", id, ok, want)
						}
					}
					create("Q1")
					assertPresent("Q1", true)
					listed := call("list created and existing", 0, "message", "scheduled", "list")
					for _, id := range []string{"Q1", "Q_EXTERNAL", "Q_UNRELATED"} {
						if !strings.Contains(listed, id) {
							t.Fatal("listing lost created or external resources")
						}
					}
					before, _ := count()
					preview := attempt("preview", 0, 0, append(cancelArgs("Q1"), "--dry-run")...)
					after, _ := count()
					if before != after {
						t.Fatal("cancellation preview reached credentials/Slack")
					}
					assertPresent("Q1", true)
					if mode != "human" {
						raw := []byte(preview)
						if mode == "json" {
							var frame struct{ Data json.RawMessage }
							if json.Unmarshal(raw, &frame) != nil {
								t.Fatal("invalid preview")
							}
							raw = frame.Data
						}
						var data struct {
							Channel string
							ID      string `json:"scheduled_message_id"`
							Dry     bool   `json:"dry_run"`
						}
						if json.Unmarshal(raw, &data) != nil || data.Channel != "C_TARGET" || data.ID != "Q1" || !data.Dry {
							t.Fatal("preview lost exact normalized target")
						}
					}
					for _, args := range [][]string{
						{"message", "scheduled", "delete", "--scheduled-id=Q1"},
						{"message", "scheduled", "delete", channelFlag, "release"},
						{"message", "scheduled", "delete", channelFlag, "release", "--scheduled-id= "},
						append(cancelArgs("Q1"), "--force"),
					} {
						before, _ := count()
						attempt("invalid arguments", 4, 0, args...)
						after, _ := count()
						if before != after {
							t.Fatal("invalid cancellation reached Slack")
						}
						assertPresent("Q1", true)
					}
					mu.Lock()
					behavior = "denied"
					mu.Unlock()
					attempt("permission failure", 1, 1, cancelArgs("Q1")...)
					assertPresent("Q1", true)
					mu.Lock()
					behavior = "normal"
					mu.Unlock()
					attempt("wrong channel", 2, 1, "message", "scheduled", "delete", channelFlag, "C_WRONG", "--scheduled-id=Q1")
					assertPresent("Q1", true)
					mu.Lock()
					changed := pending["Q1"]
					changed.Text = "Updated outside slick"
					changed.PostAt++
					pending["Q1"] = changed
					mu.Unlock()
					listed = call("observe drift", 0, "message", "scheduled", "list")
					if !strings.Contains(listed, "Updated outside slick") {
						t.Fatal("listing concealed external changes")
					}
					attempt("cancel without force", 0, 1, cancelArgs("Q1")...)
					assertPresent("Q1", false)
					attempt("already canceled", 2, 1, cancelArgs("Q1")...)
					mu.Lock()
					delete(pending, "Q_EXTERNAL")
					mu.Unlock()
					attempt("external disappearance", 2, 1, cancelArgs("Q_EXTERNAL")...)
					create("Q2")
					mu.Lock()
					behavior = "lost acknowledgement"
					mu.Unlock()
					attempt("accepted cancellation lost acknowledgement", 5, 1, cancelArgs("Q2")...)
					assertPresent("Q2", false)
					mu.Lock()
					behavior = "normal"
					mu.Unlock()
					listed = call("inspect after ambiguous result", 0, "message", "scheduled", "list")
					if strings.Contains(listed, "Q2") {
						t.Fatal("failed receipt caused rollback or recreation")
					}
					create("Q3")
					mu.Lock()
					scope = "reactions:read"
					mu.Unlock()
					attempt("scope failure", 1, 0, cancelArgs("Q3")...)
					assertPresent("Q3", true)
					mu.Lock()
					scope = "chat:write"
					mu.Unlock()
					attempt("scope recovery and removed preview", 0, 1, append(cancelArgs("Q3"), "--dry-run=false")...)
					assertPresent("Q3", false)
					mu.Lock()
					created := creates
					mu.Unlock()
					if created != 3 {
						t.Fatal("creation unexpectedly retried")
					}
				})
			}
		}
	}
}
