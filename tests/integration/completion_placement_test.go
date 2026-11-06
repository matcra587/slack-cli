//go:build integration

package integration_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// The real-root TestCompletionActionsCannotRunMutatingSubcommands retains all
// mutation routes and mode combinations. These rows preserve executable exit,
// stream and zero-HTTP behavior for both status and file-input entry points.
func TestBinaryMisplacedCompletionActionsCannotMutateSlack(t *testing.T) {
	binary := buildSlackBinary(t)
	file := filepath.Join(t.TempDir(), "fixture.txt")
	if err := os.WriteFile(file, []byte("Release notes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	status := []string{"status", "set", "--text=PR review"}
	upload := []string{"file", "upload", "--channel=C123", "--file=" + file, "--no-attribution"}
	for _, tc := range []struct {
		name, mode, flag, command string
		args                      []string
	}{
		{"status print rejects in json", "json", "--print-completion", "status.set", status},
		{"status install rejects in compact", "compact", "--install-completion", "status.set", status},
		{"status uninstall rejects in human", "human", "--uninstall-completion", "status.set", status},
		{"status dynamic rejects in json", "json", "--@complete=workspace", "status.set", status},
		{"upload print rejects in human", "human", "--print-completion", "file.upload", upload},
		{"upload install rejects in json", "json", "--install-completion", "file.upload", upload},
		{"upload uninstall rejects in compact", "compact", "--uninstall-completion", "file.upload", upload},
		{"upload dynamic rejects in json", "json", "--@complete=workspace", "file.upload", upload},
		{"status inactive flags preserve preview", "compact", "", "status.set", status},
		{"upload inactive flags preserve preview", "json", "", "file.upload", upload},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeStatusUserConfig(t)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				writeJSON(w, `{"ok":false,"error":"access_denied"}`)
			}))
			defer server.Close()
			args := append([]string{}, tc.args...)
			if tc.flag != "" {
				args = append(args, tc.flag)
			} else {
				args = append(args, "--print-completion=false", "--install-completion=false", "--uninstall-completion=false", "--@complete=", "--dry-run")
			}
			args = append(args, "--@shell=fish", "--output="+tc.mode, "--color=never", "--no-throttle", "--timeout=250ms")
			out, diagnostic, err := runSlackBinary(t, binary, path, server.URL, "", args...)
			if calls.Load() != 0 {
				t.Fatal("completion placement reached Slack")
			}
			if strings.Contains(out+diagnostic, "xoxb-test") {
				t.Fatal("completion placement exposed credentials")
			}
			if tc.flag == "" {
				if err != nil || diagnostic != "" {
					t.Fatalf("inactive completion flags blocked normal preview: %v", err)
				}
				data := json.RawMessage(out)
				if tc.mode == "json" {
					var frame struct {
						OK   bool
						Data json.RawMessage
						Meta struct{ Command string }
					}
					if json.Unmarshal(data, &frame) != nil || !frame.OK || frame.Meta.Command != tc.command {
						t.Fatal("inactive flags lost preview envelope")
					}
					data = frame.Data
				}
				var preview struct {
					DryRun bool `json:"dry_run"`
				}
				if json.Unmarshal(data, &preview) != nil || !preview.DryRun {
					t.Fatal("inactive flags did not execute the intended preview")
				}
			} else {
				if exitCode(err) != 4 {
					t.Fatalf("completion action lost validation exit: got %d", exitCode(err))
				}
				if tc.mode == "human" {
					if out != "" || diagnostic == "" {
						t.Fatal("completion placement lost human failure streams")
					}
				} else {
					var fields map[string]json.RawMessage
					var frame struct {
						OK   bool
						Data json.RawMessage
						Meta struct {
							Exit    int `json:"exit_code"`
							Command string
						}
						Errors []struct{ Type string }
					}
					if json.Unmarshal([]byte(out), &fields) != nil || len(fields) != 4 || json.Unmarshal([]byte(out), &frame) != nil || frame.OK || string(frame.Data) != "null" || frame.Meta.Exit != 4 || frame.Meta.Command != tc.command || len(frame.Errors) != 1 || frame.Errors[0].Type != "validation_error" || diagnostic != "" {
						t.Fatal("completion placement lost standard failure envelope")
					}
				}
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("completion placement changed saved configuration")
			}
			body, err := os.ReadFile(file)
			if err != nil || string(body) != "Release notes\n" {
				t.Fatal("completion placement changed input file")
			}
		})
	}
}
