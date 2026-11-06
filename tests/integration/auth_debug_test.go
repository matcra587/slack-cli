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

func TestBinaryAuthDebugProtectsResponseAndInput(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tc := range []struct {
		name, flag, mode string
		prefix, debug    bool
	}{
		{"long before command", "--debug", "json", true, true},
		{"short", "-D", "compact", false, true},
		{"explicit false", "--debug=false", "json", false, false},
		{"short false before command", "-D=false", "json", true, false},
		{"removed flag", "", "json", false, false},
		{"human", "-D", "human", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writePipeConfig(t)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Method != http.MethodPost || r.URL.Path != "/api/auth.test" || r.URL.RawQuery != "" || len(r.PostForm) != 1 || r.PostForm.Get("token") != "xoxp-synthetic-native-auth" {
					t.Error("debug flag changed authentication request or token source")
				}
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Synthetic-Secret", "synthetic-private-auth-response")
				w.WriteHeader(http.StatusBadGateway)
				writeJSON(w, `{"ok":false,"error":"synthetic-private-auth-response","access_token":"xoxp-unreviewed-native-token"}`)
			}))
			defer server.Close()
			args := []string{"auth", "login", "--workspace-name=default", "--token-stdin", "--output=" + tc.mode, "--color=never"}
			if tc.flag != "" {
				if tc.prefix {
					args = append([]string{tc.flag}, args...)
				} else {
					args = append(args, tc.flag)
				}
			}
			out, diagnostic, err := runSlackBinary(t, binary, path, server.URL, "xoxp-synthetic-native-auth\n", args...)
			if exitCode(err) != 5 || calls.Load() != 1 {
				t.Fatalf("debug auth failure exit=%d calls=%d, want exit=5 calls=1", exitCode(err), calls.Load())
			}
			if strings.Contains(diagnostic, "Slack SDK") != tc.debug {
				t.Error("debug flag or its removal lost diagnostic selection")
			}
			for _, secret := range []string{"xoxp-synthetic-native-auth", "xoxp-unreviewed-native-token", "synthetic-private-auth-response"} {
				if strings.Contains(out+diagnostic, secret) {
					t.Error("debug auth output exposed token, response or header data")
				}
			}
			if tc.mode == "human" {
				if out != "" || !strings.Contains(diagnostic, "server_error") {
					t.Error("debug changed human failure streams")
				}
			} else {
				var frame map[string]any
				if json.Unmarshal([]byte(out), &frame) != nil || len(frame) != 4 || frame["ok"] != false || frame["data"] != nil || frame["meta"].(map[string]any)["command"] != "auth.login" || frame["meta"].(map[string]any)["exit_code"] != float64(5) || (!tc.debug && diagnostic != "") {
					t.Error("debug changed machine failure envelope or streams")
				}
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Error("failed debug login changed prior config")
			}
		})
	}
}
