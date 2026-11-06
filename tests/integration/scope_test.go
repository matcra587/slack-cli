//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBinaryScopeFailuresUseOneNeededShape(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tc := range []struct {
		name, header, body, match string
		needed                    []string
		calls                     int32
	}{
		{name: "preflight", header: "chat:write", match: "all", needed: []string{"reactions:write"}},
		{name: "structured", header: "reactions:write", body: `{"ok":false,"error":"missing_scope","needed":"reactions:write,chat:write"}`, match: "reported", needed: []string{"reactions:write", "chat:write"}, calls: 1},
		{name: "metadata", header: "reactions:write", body: `{"ok":false,"error":"missing_scope","response_metadata":{"messages":["missing required scope: reactions:write"]}}`, match: "reported", needed: []string{"reactions:write"}, calls: 1},
	} {
		for _, mode := range []string{"json", "compact"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				const token = "xoxp-synthetic-binary-scope"
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.PostForm.Get("token") != token {
						t.Error("wrong method or synthetic authentication")
					}
					switch r.URL.Path {
					case "/api/auth.test":
						w.Header().Set("X-OAuth-Scopes", tc.header)
						writeJSON(w, `{"ok":true,"user_id":"U1"}`)
					case "/api/conversations.info":
						writeJSON(w, `{"ok":true,"channel":{"id":"C123","name":"fixture"}}`)
					case "/api/reactions.add":
						calls.Add(1)
						writeJSON(w, tc.body)
					default:
						t.Errorf("unexpected method: %s", r.URL.Path)
						writeJSON(w, `{"ok":false,"error":"method_not_found"}`)
					}
				}))
				defer server.Close()
				isolated := t.TempDir()
				configPath := filepath.Join(isolated, "config.toml")
				cfg := "schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"T123\"\ntoken_type=\"user\"\ntoken=\"env:SLICK_SCOPE_FIXTURE_TOKEN\"\n"
				if err := os.WriteFile(configPath, []byte(cfg), 0o600); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, binary, "react", "add", "--channel=C123", "--timestamp=1746284582.123456", "--emoji=rocket", "--output="+mode, "--no-throttle", "--timeout=8s")
				cmd.Env = []string{"HOME=" + isolated, "SLICK_CONFIG=" + configPath, "SLICK_SCOPE_FIXTURE_TOKEN=" + token, "SLACK_CLI_BASE_URL=" + server.URL}
				var stdout, stderr bytes.Buffer
				cmd.Stdout, cmd.Stderr = &stdout, &stderr
				err := cmd.Run()
				if ctx.Err() != nil || exitCode(err) != 1 || stderr.Len() != 0 || strings.Contains(stdout.String(), token) || calls.Load() != tc.calls {
					t.Fatalf("wrong scope failure: err=%v stdout=%s stderr=%s", err, &stdout, &stderr)
				}
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(stdout.Bytes(), &fields); err != nil {
					t.Fatal(err)
				}
				if len(fields) != 4 || string(fields["ok"]) != "false" || string(fields["data"]) != "null" {
					t.Fatal("scope failure lost the common envelope")
				}
				var errors []struct {
					Type    string `json:"type"`
					Exit    int    `json:"exit_code"`
					Details struct {
						Needed []string `json:"needed"`
						Match  string   `json:"needed_match"`
					} `json:"details"`
				}
				if err := json.Unmarshal(fields["errors"], &errors); err != nil {
					t.Fatal(err)
				}
				if len(errors) != 1 || errors[0].Type != "auth_failure" || errors[0].Exit != 1 || !reflect.DeepEqual(errors[0].Details.Needed, tc.needed) || errors[0].Details.Match != tc.match {
					t.Fatal("scope names, array type or requirement meaning changed")
				}
			})
		}
	}
}
