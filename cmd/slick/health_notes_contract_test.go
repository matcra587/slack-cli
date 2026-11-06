package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func TestHealthCommandsPreserveIncidentNotesAndState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("SLICK_CONFIG", path)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("SLACK_CLI_DEFAULT_WORKSPACE", "")
	cfg := &config.Config{SchemaVersion: "1", DefaultWorkspace: "fixture", Workspaces: map[string]config.WorkspaceProfile{"fixture": {Name: "fixture", DefaultChannel: "C_KEEP", Aliases: map[string]string{"alerts": "C_KEEP"}}}}
	if err := config.SaveFile(path, cfg); err != nil {
		t.Fatal(err)
	}
	configBefore, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	notes := []map[string]string{{"body": "  Investigating *delivery*.\r\nNext update shortly.  ", "date_created": "2026-11-06T13:00:00.123456-05:00"}, {"body": "Delivery has recovered. ✅", "date_created": "2026-11-06T13:10:00.654321-05:00"}}
	wireNotes := []map[string]string{{"body": notes[0]["body"], "date_created": notes[0]["date_created"], "unreviewed_secret": "synthetic-unreviewed-note-field"}, notes[1]}
	incidents := []map[string]any{
		{"id": json.Number("9007199254740993"), "title": "Messaging delay", "type": "incident", "status": "active", "services": []string{"Messaging"}, "notes": wireNotes},
		{"id": "fixture-empty", "title": "Huddles delay", "type": "incident", "status": "active", "services": []string{"Huddles"}, "notes": []any{}},
		{"id": "fixture-missing", "title": "Files notice", "type": "notice", "status": "active", "services": []string{"Files"}},
	}
	for _, command := range []string{"current", "history", "check"} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(command+"/"+mode, func(t *testing.T) {
				var calls, providers atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					body, err := io.ReadAll(r.Body)
					if err != nil || len(body) != 0 || r.URL.RawQuery != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
						t.Error("health command sent credentials or arguments")
					}
					w.Header().Set("Content-Type", "application/json")
					var result any
					switch r.URL.Path {
					case "/api/api.test":
						if r.Method != http.MethodPost {
							t.Error("wrong API probe method")
						}
						result = map[string]any{"ok": true}
					case "/current":
						if r.Method != http.MethodGet {
							t.Error("wrong current method")
						}
						result = map[string]any{"status": "active", "active_incidents": incidents}
					case "/history":
						if r.Method != http.MethodGet {
							t.Error("wrong history method")
						}
						result = incidents
					default:
						t.Error("unexpected health path")
						w.WriteHeader(500)
						return
					}
					if err := json.NewEncoder(w).Encode(result); err != nil {
						t.Error(err)
					}
				}))
				defer server.Close()
				var stdout, stderr bytes.Buffer
				root := NewRootCommand(WithConfig(cfg), WithConfigPath(path), WithSlackBaseURL(server.URL), WithSlackStatusBaseURL(server.URL), WithTTY(false), WithIO(strings.NewReader(""), &stdout, &stderr), WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
					providers.Add(1)
					return "", fmt.Errorf("unexpected credential access")
				})))
				root.SetArgs([]string{"health", command, "--output=" + mode, "--color=never"})
				if err := root.ExecuteContext(t.Context()); err != nil || stderr.Len() != 0 {
					t.Fatalf("health command failed: %v", err)
				}
				wantCalls := int32(1)
				if command == "check" {
					wantCalls = 2
				}
				if providers.Load() != 0 || calls.Load() != wantCalls {
					t.Fatal("health command accessed credentials or replayed requests")
				}
				if strings.Contains(stdout.String()+stderr.String(), "synthetic-unreviewed-note-field") {
					t.Fatal("unreviewed note field escaped the output allowlist")
				}
				if mode != "human" {
					data := stdout.Bytes()
					if mode == "json" {
						var frame struct {
							OK   bool
							Data json.RawMessage
							Meta struct{ Command string }
						}
						if json.Unmarshal(data, &frame) != nil || !frame.OK || frame.Meta.Command != "health."+command {
							t.Fatal("wrong common success envelope")
						}
						data = frame.Data
					}
					var result map[string]json.RawMessage
					if json.Unmarshal(data, &result) != nil {
						t.Fatal("invalid result")
					}
					key := "active_incidents"
					if command == "history" {
						key = "incidents"
					}
					var rows []map[string]json.RawMessage
					if json.Unmarshal(result[key], &rows) != nil || len(rows) != 3 {
						t.Fatal("incident rows lost")
					}
					var got []map[string]string
					if json.Unmarshal(rows[0]["notes"], &got) != nil || !reflect.DeepEqual(got, notes) {
						t.Fatal("incident note content, precision or order lost")
					}
					if string(rows[0]["id"]) != `"9007199254740993"` || string(rows[0]["note_count"]) != "2" {
						t.Fatal("incident ID precision or note count lost")
					}
					for _, row := range rows[1:] {
						if string(row["notes"]) != "[]" || string(row["note_count"]) != "0" {
							t.Fatal("empty incident notes must be [] with explicit zero count")
						}
					}
				} else if command != "check" && !strings.Contains(stdout.String(), "NOTES") {
					t.Fatal("human incident table lost note counts")
				}
				configAfter, err := json.Marshal(cfg)
				if err != nil || !bytes.Equal(configBefore, configAfter) {
					t.Fatal("health command changed runtime preferences")
				}
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(prior, after) {
					t.Fatal("health command changed saved preferences")
				}
			})
		}
	}
}
