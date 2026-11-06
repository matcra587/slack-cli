//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBinaryCacheRejectsInvalidEntriesAndPreservesThemOnFailedRecovery(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, resource := range []string{"users", "channels"} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(resource+"/"+mode, func(t *testing.T) {
				const token = "xoxp-synthetic-cache-entry"
				const private = "synthetic-unreviewed-cache-secret"
				var mu sync.Mutex
				requests, lists := 0, 0
				failure := false
				method, scope, responseKey := "users.list", "users:read", "members"
				if resource == "channels" {
					method, scope, responseKey = "conversations.list", "channels:read,groups:read,im:read,mpim:read", "channels"
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					defer mu.Unlock()
					requests++
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.PostForm.Get("token") != token {
						t.Error("cache recovery changed method or authentication")
					}
					if r.URL.Path == "/api/auth.test" {
						w.Header().Set("X-OAuth-Scopes", scope)
						writeJSON(w, `{"ok":true,"user_id":"U1"}`)
						return
					}
					lists++
					if r.URL.Path != "/api/"+method || r.PostForm.Get("limit") != "200" || r.PostForm.Get("cursor") != "" {
						t.Error("cache recovery changed the default request")
					}
					if resource == "channels" && (r.PostForm.Get("types") != "public_channel,private_channel,im,mpim" || r.PostForm.Get("exclude_archived") != "true") {
						t.Error("cache recovery lost conversation selection")
					}
					if failure {
						writeJSON(w, `{"ok":false,"error":"no_permission"}`)
						return
					}
					writeJSON(w, fmt.Sprintf(`{"ok":true,%q:[],"unreviewed_secret":%q}`, responseKey, private))
				}))
				defer server.Close()
				home := t.TempDir()
				configPath := filepath.Join(home, "config.toml")
				configBody := []byte("schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"T1\"\ntoken_type=\"user\"\ntoken=\"env:SLICK_CACHE_ENTRY_TOKEN\"\n[workspaces.fixture.aliases]\nrelease=\"CPRESERVE\"\n")
				if err := os.WriteFile(configPath, configBody, 0o600); err != nil {
					t.Fatal(err)
				}
				cacheDir := filepath.Join(home, "cache", "slick", "fixture")
				if err := os.MkdirAll(cacheDir, 0o700); err != nil {
					t.Fatal(err)
				}
				cachePath := filepath.Join(cacheDir, resource+".json")
				unrelated := filepath.Join(cacheDir, "preserve.toml")
				if err := os.WriteFile(unrelated, []byte("keep=true\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				run := func(t *testing.T) (string, string, error) {
					t.Helper()
					ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
					defer cancel()
					cmd := exec.CommandContext(ctx, binary, "cache", resource, "--output="+mode, "--color=never", "--no-throttle", "--timeout=3s")
					cmd.Env = []string{"HOME=" + home, "XDG_CACHE_HOME=" + filepath.Join(home, "cache"), "SLICK_CONFIG=" + configPath, "SLACK_CLI_BASE_URL=" + server.URL, "SLICK_CACHE_ENTRY_TOKEN=" + token}
					var out, diagnostic bytes.Buffer
					cmd.Stdout, cmd.Stderr = &out, &diagnostic
					err := cmd.Run()
					if ctx.Err() != nil {
						t.Fatal("cache entry fixture exceeded its timeout")
					}
					if strings.Contains(out.String()+diagnostic.String(), token) || strings.Contains(out.String()+diagnostic.String(), private) {
						t.Fatal("cache recovery exposed sensitive data")
					}
					return out.String(), diagnostic.String(), err
				}
				for _, defect := range []string{"wrong profile", "wrong resource", "missing list", "null list", "missing ID", "blank ID", "wrong list type", "wrong ID type", "null payload", "malformed JSON", "wrong name type", "wrong optional boolean", "wrong truncation type"} {
					t.Run(defect, func(t *testing.T) {
						data := map[string]any{resource: []any{map[string]any{"id": "OLD", "name": "prior"}}}
						entry := map[string]any{"profile": "fixture", "resource": resource, "fetched_at": time.Now().UTC(), "data": data}
						switch defect {
						case "wrong profile":
							entry["profile"] = "another-profile"
						case "wrong resource":
							entry["resource"] = "another-resource"
						case "missing list":
							delete(data, resource)
						case "null list":
							data[resource] = nil
						case "missing ID":
							data[resource] = []any{map[string]any{"name": "prior"}}
						case "blank ID":
							data[resource] = []any{map[string]any{"id": "  "}}
						case "wrong list type":
							data[resource] = private
						case "wrong ID type":
							data[resource] = []any{map[string]any{"id": 12}}
						case "null payload":
							entry["data"] = nil
						case "wrong name type":
							data[resource] = []any{map[string]any{"id": "OLD", "name": 12}}
						case "wrong optional boolean":
							field := "deleted"
							if resource == "channels" {
								field = "is_member"
							}
							data[resource] = []any{map[string]any{"id": "OLD", field: "not-a-boolean"}}
						case "wrong truncation type":
							data["truncated"] = "not-a-boolean"
						}
						prior, err := json.Marshal(entry)
						if err != nil {
							t.Fatal(err)
						}
						if defect == "malformed JSON" {
							prior = []byte(`{"data":"` + private)
						}
						if err := os.WriteFile(cachePath, prior, 0o600); err != nil {
							t.Fatal(err)
						}
						mu.Lock()
						failure = true
						before := lists
						mu.Unlock()
						out, diagnostic, err := run(t)
						if exitCode(err) != 1 {
							t.Fatalf("invalid cache bypassed recovery or changed failure class: %v", err)
						}
						if mode == "human" {
							if out != "" || diagnostic == "" {
								t.Fatal("cache recovery error used the wrong stream")
							}
						} else {
							var frame map[string]json.RawMessage
							if err := json.Unmarshal([]byte(out), &frame); err != nil || len(frame) != 4 || string(frame["ok"]) != "false" || string(frame["data"]) != "null" || diagnostic != "" {
								t.Fatal("cache recovery error lost the common envelope")
							}
						}
						after, err := os.ReadFile(cachePath)
						if err != nil || !bytes.Equal(prior, after) {
							t.Fatal("failed recovery changed the prior cache file")
						}
						mu.Lock()
						if lists != before+1 {
							t.Error("failed cache recovery was skipped or replayed")
						}
						failure = false
						mu.Unlock()
						out, diagnostic, err = run(t)
						if err != nil || diagnostic != "" || out == "" || strings.Contains(out, "OLD") {
							t.Fatal("explicit cache recovery failed or returned the invalid entry")
						}
						body, err := os.ReadFile(cachePath)
						if err != nil || bytes.Contains(body, []byte(private)) || bytes.Contains(body, []byte(token)) || bytes.Contains(body, []byte("unreviewed_secret")) {
							t.Fatal("cache recovery persisted sensitive or unreviewed fields")
						}
						var saved struct {
							Profile, Resource string
							Data              map[string]json.RawMessage
						}
						if err := json.Unmarshal(body, &saved); err != nil || saved.Profile != "fixture" || saved.Resource != resource || string(saved.Data[resource]) != "[]" {
							t.Fatal("cache recovery lost identity or the valid empty list")
						}
						info, err := os.Stat(cachePath)
						if err != nil || info.Mode().Perm() != 0o600 {
							t.Fatal("cache recovery did not protect the output file")
						}
						mu.Lock()
						beforeRequests := requests
						if lists != before+2 {
							t.Error("successful recovery was skipped or replayed")
						}
						mu.Unlock()
						out, diagnostic, err = run(t)
						if err != nil || diagnostic != "" || out == "" {
							t.Fatal("valid empty cache could not be reused")
						}
						mu.Lock()
						if requests != beforeRequests {
							t.Error("valid empty cache unexpectedly accessed Slack")
						}
						mu.Unlock()
					})
				}
				for path, want := range map[string][]byte{configPath: configBody, unrelated: []byte("keep=true\n")} {
					body, err := os.ReadFile(path)
					if err != nil || !bytes.Equal(body, want) {
						t.Fatal("cache recovery changed unrelated state")
					}
				}
			})
		}
	}
}
