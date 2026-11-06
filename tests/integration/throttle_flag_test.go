//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestBinaryNoThrottleFlagControlsProactivePacing(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tc := range []struct {
		name, flag, mode string
		disabled, prefix bool
	}{
		{"default", "", "json", false, false},
		{"short disable", "-Q", "compact", true, false},
		{"long disable before command", "--no-throttle", "json", true, true},
		{"explicit false", "--no-throttle=false", "human", false, false},
		{"short false before command", "-Q=false", "compact", false, true},
		{"removed flag", "", "human", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writePipeConfig(t)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var pages atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.PostForm.Get("token") != "xoxb-test" {
					t.Error("pacing flag changed request method or authentication")
				}
				switch r.URL.Path {
				case "/api/auth.test":
					w.Header().Set("X-OAuth-Scopes", "channels:read,groups:read,im:read,mpim:read")
					writeJSON(w, `{"ok":true,"user_id":"U_SELF"}`)
				case "/api/conversations.list":
					page := pages.Add(1)
					if r.PostForm.Get("limit") != "1" || page > 2 {
						t.Error("pacing flag changed page size or replayed a page")
					}
					if page == 1 {
						if r.PostForm.Get("cursor") != "" {
							t.Error("initial paced page had a cursor")
						}
						writeJSON(w, `{"ok":true,"channels":[{"id":"C_ONE","name":"one"}],"response_metadata":{"next_cursor":"page-2"}}`)
					} else {
						if r.PostForm.Get("cursor") != "page-2" {
							t.Error("unpaced page lost continuation")
						}
						writeJSON(w, `{"ok":true,"channels":[{"id":"C_TWO","name":"two"}],"response_metadata":{"next_cursor":""}}`)
					}
				default:
					t.Errorf("unexpected pacing request %s", r.URL.Path)
				}
			}))
			defer server.Close()
			args := []string{"cache", "channels", "--refresh", "--page-size=1", "--max-pages=2", "--timeout=1s", "--output=" + tc.mode, "--color=never"}
			if tc.flag != "" {
				if tc.prefix {
					args = append([]string{tc.flag}, args...)
				} else {
					args = append(args, tc.flag)
				}
			}
			out, diagnostic, err := runSlackBinary(t, binary, path, server.URL, "", args...)
			wantExit, wantPages := 7, int32(1)
			if tc.disabled {
				wantExit, wantPages = 0, 2
			}
			if exitCode(err) != wantExit || pages.Load() != wantPages {
				t.Fatalf("proactive pacing exit=%d pages=%d, want exit=%d pages=%d", exitCode(err), pages.Load(), wantExit, wantPages)
			}
			if tc.mode == "human" {
				if out != "" || !strings.Contains(diagnostic, "timeout") {
					t.Error("paced deadline changed human failure streams")
				}
			} else {
				if diagnostic != "" {
					t.Error("pacing diagnostic escaped machine stdout")
				}
				var frame map[string]any
				if err := json.Unmarshal([]byte(out), &frame); err != nil {
					t.Fatal(err)
				}
				if wantExit != 0 && (frame["ok"] != false || frame["data"] != nil || frame["meta"].(map[string]any)["exit_code"] != float64(7) || frame["errors"].([]any)[0].(map[string]any)["type"] != "timeout") {
					t.Error("paced deadline lost structured timeout failure")
				}
				if wantExit == 0 && tc.mode == "json" && frame["ok"] != true {
					t.Error("unpaced fetch lost success envelope")
				}
			}
			if strings.Contains(out+diagnostic, "xoxb-test") {
				t.Error("pacing output exposed credentials")
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != string(before) {
				t.Error("pacing selector changed stored config")
			}
		})
	}
}
