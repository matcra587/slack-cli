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

func TestBinaryOrganizationFailureRecoveryPreservesConfig(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, row := range []struct {
		name, method, reply string
		args                []string
	}{
		{"dnd", "dnd.setSnooze", `{"ok":true,"snooze_enabled":true,"snooze_remaining":3600}`, []string{"dnd", "snooze", "--minutes=60"}},
		{"pin", "pins.add", `{"ok":true}`, []string{"pin", "add", "--channel=C123", "--timestamp=1746284500.000001"}},
		{"bookmark", "bookmarks.add", `{"ok":true,"bookmark":{"id":"Bk123","channel_id":"C123","title":"Runbook","link":"https://example.com"}}`, []string{"bookmark", "add", "--channel=C123", "--title=Runbook", "--link=https://example.com"}},
	} {
		t.Run(row.name, func(t *testing.T) {
			configPath := writeStatusUserConfig(t)
			before, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			var phase, calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				method := strings.TrimPrefix(r.URL.Path, "/api/")
				if r.Method != http.MethodPost || r.URL.RawQuery != "" {
					t.Error("unexpected SDK method or query")
				}
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.PostForm.Get("token") != "xoxb-test" {
					t.Error("synthetic credential not used")
				}
				if method == "auth.test" {
					w.Header().Set("X-OAuth-Scopes", "dnd:write,pins:write,bookmarks:write")
					writeJSON(w, `{"ok":true,"user_id":"U123","team_id":"T123"}`)
					return
				}
				if method != row.method {
					t.Errorf("unexpected API method: %s", method)
					w.WriteHeader(404)
					return
				}
				calls.Add(1)
				if phase.Load() == 0 {
					w.WriteHeader(503)
					writeJSON(w, `{"ok":false,"error":"internal_error"}`)
					return
				}
				writeJSON(w, row.reply)
			}))
			defer server.Close()
			for _, step := range []int32{0, 1} {
				phase.Store(step)
				args := append(row.args, "--output=json", "--no-throttle", "--timeout=2s")
				out, diagnostics, err := runSlackBinary(t, binary, configPath, server.URL, "", args...)
				var frame struct {
					OK   bool `json:"ok"`
					Meta struct {
						Command string `json:"command"`
					} `json:"meta"`
				}
				if json.Unmarshal([]byte(out), &frame) != nil || frame.OK != (step == 1) || frame.Meta.Command != row.args[0]+"."+row.args[1] || diagnostics != "" || (err == nil) != (step == 1) {
					t.Fatalf("unexpected process result: step=%d exit=%d stdout=%s stderr=%s", step, exitCode(err), out, diagnostics)
				}
				if calls.Load() != step+1 || strings.Contains(out, "xoxb-test") {
					t.Fatalf("expected one operation per invocation: step=%d calls=%d", step, calls.Load())
				}
				after, err := os.ReadFile(configPath)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatal("operation changed saved config")
				}
			}
		})
	}
}
