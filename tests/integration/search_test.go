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
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBinarySearchPreservesPagingAndRejectsIncompleteResults(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tc := range []struct {
		name, body, mode string
		args             []string
		exit             int
		requests         int32
		truncated        bool
	}{
		{name: "explicit page", mode: "json", args: []string{"--cursor", "2", "--max-items", "5"}, body: `{"ok":true,"messages":{"matches":[{"text":"Keep this match","ts":"1746284582.123456"}],"pagination":{"page":2,"page_count":2}}}`, requests: 1},
		{name: "page cap compact", mode: "compact", args: []string{"--cursor", "100"}, body: `{"ok":true,"messages":{"matches":[{"text":"Keep this match","ts":"1746284582.123456"}],"pagination":{"page":100,"page_count":200}}}`, requests: 1, truncated: true},
		{name: "incomplete response", mode: "json", body: `{"ok":true,"messages":{"matches":[]}}`, exit: 5, requests: 1},
		{name: "invalid input", mode: "json", args: []string{"--cursor", "0"}, exit: 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const token = "xoxp-synthetic-binary-search"
			var requests, authRequests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/auth.test":
					authRequests.Add(1)
					w.Header().Set("X-OAuth-Scopes", "search:read")
					writeJSON(w, `{"ok":true,"user_id":"U123"}`)
				case "/api/search.messages":
					requests.Add(1)
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.PostForm.Get("token") != token || r.PostForm.Get("query") != "release review" {
						t.Error("search request did not match the SDK contract")
					}
					if tc.name == "explicit page" && (r.PostForm.Get("page") != "2" || r.PostForm.Get("count") != "5") {
						t.Error("explicit page or count was lost")
					}
					writeJSON(w, tc.body)
				default:
					t.Errorf("unexpected request: %s", r.URL.Path)
					writeJSON(w, `{"ok":false,"error":"method_not_found"}`)
				}
			}))
			defer server.Close()
			isolated := t.TempDir()
			configPath := filepath.Join(isolated, "config.toml")
			config := "schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"T123\"\ntoken_type=\"user\"\ntoken=\"env:SLICK_SEARCH_FIXTURE_TOKEN\"\n"
			if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			args := append([]string{"lookup", "messages", "--query", "release review", "--output=" + tc.mode, "--no-throttle", "--timeout=8s"}, tc.args...)
			cmd := exec.CommandContext(ctx, binary, args...)
			cmd.Env = []string{"HOME=" + isolated, "SLICK_CONFIG=" + configPath, "SLICK_SEARCH_FIXTURE_TOKEN=" + token, "SLACK_CLI_BASE_URL=" + server.URL}
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			if ctx.Err() != nil || exitCode(err) != tc.exit || stderr.Len() != 0 || strings.Contains(stdout.String(), token) {
				t.Fatalf("wrong exit or output: err=%v stdout=%s stderr=%s", err, &stdout, &stderr)
			}
			if requests.Load() != tc.requests || (tc.requests == 0 && authRequests.Load() != 0) {
				t.Fatalf("unexpected API calls: search=%d auth=%d", requests.Load(), authRequests.Load())
			}
			var frame map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &frame); err != nil {
				t.Fatal(err)
			}
			if tc.exit != 0 {
				if len(frame) != 4 || frame["ok"] != false || frame["data"] != nil {
					t.Fatal("failure did not use the common machine envelope")
				}
				return
			}
			data := frame
			if tc.mode == "json" {
				if len(frame) != 4 || frame["ok"] != true {
					t.Fatal("success did not use the common machine envelope")
				}
				data = frame["data"].(map[string]any)
			}
			if data["matches"].([]any)[0].(map[string]any)["ts"] != "1746284582.123456" || (data["truncated"] == true) != tc.truncated {
				t.Fatal("search lost the timestamp or truncation state")
			}
			if tc.truncated && (data["pagination"].(map[string]any)["has_more"] != true || data["pagination"].(map[string]any)["next_cursor"] != nil) {
				t.Fatal("capped search suggested an invalid next page")
			}
		})
	}
}
