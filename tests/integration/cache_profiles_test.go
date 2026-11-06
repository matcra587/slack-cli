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

func TestBinaryCacheProfilesDoNotShareReadWriteOrClearState(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, pair := range []struct{ name, first, second string }{
		{"slash", "a/b", "a_b"},
		{"backslash", `a\b`, "a_b"},
		{"double dot", "a..b", "a_b"},
		{"prefix", "~reserved", "reserved"},
		{"trailing dot", "team.", "team"},
		{"long Unicode", strings.Repeat("界", 100), "unicode"},
	} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(pair.name+"/"+mode, func(t *testing.T) {
				tokens := map[string]string{"xoxp-synthetic-cache-first": "U1", "xoxp-synthetic-cache-second": "U2"}
				var mu sync.Mutex
				requests := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					requests++
					mu.Unlock()
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					id, ok := tokens[r.PostForm.Get("token")]
					if !ok || r.Method != http.MethodPost || r.URL.RawQuery != "" {
						t.Error("cache profile lost authentication or request method")
					}
					switch r.URL.Path {
					case "/api/auth.test":
						w.Header().Set("X-OAuth-Scopes", "users:read")
						writeJSON(w, `{"ok":true,"user_id":"UOWNER"}`)
					case "/api/users.list":
						if r.PostForm.Get("limit") != "200" || r.PostForm.Get("cursor") != "" {
							t.Error("profile cache changed pagination defaults")
						}
						writeJSON(w, fmt.Sprintf(`{"ok":true,"members":[{"id":%q,"name":"fixture"}]}`, id))
					default:
						t.Errorf("unexpected profile cache request: %s", r.URL.Path)
					}
				}))
				defer server.Close()
				home := t.TempDir()
				configPath := filepath.Join(home, "config.toml")
				configBody := []byte(fmt.Sprintf("schema_version=\"1\"\ndefault_workspace=%q\n[workspaces.%q]\nname=%q\nteam_id=\"T1\"\ntoken_type=\"user\"\ntoken=\"env:SLICK_CACHE_FIRST\"\n[workspaces.%q]\nname=%q\nteam_id=\"T2\"\ntoken_type=\"user\"\ntoken=\"env:SLICK_CACHE_SECOND\"\n", pair.first, pair.first, pair.first, pair.second, pair.second))
				if err := os.WriteFile(configPath, configBody, 0o600); err != nil {
					t.Fatal(err)
				}
				run := func(profile, expectedID string, clear ...string) {
					t.Helper()
					mu.Lock()
					beforeRequests := requests
					mu.Unlock()
					ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
					defer cancel()
					args := []string{"cache", "users", "--workspace=" + profile, "--output=" + mode, "--color=never", "--no-throttle", "--timeout=3s"}
					if clear != nil {
						args[1] = "clear"
						args = append(args, clear...)
					}
					cmd := exec.CommandContext(ctx, binary, args...)
					cmd.Env = []string{"HOME=" + home, "XDG_CACHE_HOME=" + filepath.Join(home, "cache"), "SLICK_CONFIG=" + configPath, "SLACK_CLI_BASE_URL=" + server.URL, "SLICK_CACHE_FIRST=xoxp-synthetic-cache-first", "SLICK_CACHE_SECOND=xoxp-synthetic-cache-second"}
					var out, diagnostic bytes.Buffer
					cmd.Stdout, cmd.Stderr = &out, &diagnostic
					if err := cmd.Run(); err != nil || diagnostic.Len() != 0 || out.Len() == 0 || ctx.Err() != nil {
						t.Fatalf("profile cache command failed: %v", err)
					}
					mu.Lock()
					afterRequests := requests
					mu.Unlock()
					if clear != nil && beforeRequests != afterRequests {
						t.Fatal("local cache clearing unexpectedly accessed Slack")
					}
					if strings.Contains(out.String()+diagnostic.String(), "xoxp-synthetic-cache-") {
						t.Fatal("profile cache exposed credentials")
					}
					if mode == "human" {
						return
					}
					var frame map[string]json.RawMessage
					if err := json.Unmarshal(out.Bytes(), &frame); err != nil {
						t.Fatal(err)
					}
					data := frame
					if mode == "json" {
						if len(frame) != 4 || string(frame["ok"]) != "true" {
							t.Fatal("profile cache lost success envelope")
						}
						data = nil
						if err := json.Unmarshal(frame["data"], &data); err != nil {
							t.Fatal(err)
						}
					}
					var actualProfile string
					if err := json.Unmarshal(data["profile"], &actualProfile); err != nil || actualProfile != profile {
						t.Fatal("cache command used another profile")
					}
					if expectedID != "" {
						var users []struct{ ID string }
						if err := json.Unmarshal(data["users"], &users); err != nil || len(users) != 1 || users[0].ID != expectedID {
							t.Fatal("profile cache returned another profile's data")
						}
					}
				}
				run(pair.first, "U1")
				run(pair.second, "U2")
				paths := map[string]string{}
				original := map[string][]byte{}
				if err := filepath.WalkDir(filepath.Join(home, "cache", "slick"), func(path string, d os.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if d.IsDir() {
						return nil
					}
					body, err := os.ReadFile(path)
					if err != nil {
						return err
					}
					var entry struct {
						Profile string
						Data    struct{ Users []struct{ ID string } }
					}
					if err := json.Unmarshal(body, &entry); err != nil {
						return err
					}
					wantID := map[string]string{pair.first: "U1", pair.second: "U2"}[entry.Profile]
					if len(entry.Data.Users) != 1 || entry.Data.Users[0].ID != wantID {
						t.Fatal("profile cache persisted another profile's data")
					}
					paths[entry.Profile] = path
					original[entry.Profile] = body
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				if len(paths) != 2 || paths[pair.first] == paths[pair.second] {
					t.Fatal("profile entries share state or one profile was overwritten")
				}
				mu.Lock()
				before := requests
				mu.Unlock()
				run(pair.first, "U1")
				run(pair.second, "U2")
				mu.Lock()
				after := requests
				mu.Unlock()
				if after != before {
					t.Fatal("independent fresh profile caches were not reused")
				}
				run(pair.first, "", "users")
				if _, err := os.Stat(paths[pair.first]); !os.IsNotExist(err) {
					t.Fatal("selected profile resource was not cleared")
				}
				body, err := os.ReadFile(paths[pair.second])
				if err != nil || !bytes.Equal(body, original[pair.second]) {
					t.Fatal("single clear changed another profile's state")
				}
				run(pair.second, "U2")
				run(pair.first, "U1")
				// A nonnil empty slice selects profile-wide clearing.
				run(pair.first, "", []string{}...)
				if _, err := os.Stat(paths[pair.first]); !os.IsNotExist(err) {
					t.Fatal("profile sweep left its cache behind")
				}
				body, err = os.ReadFile(paths[pair.second])
				if err != nil || !bytes.Equal(body, original[pair.second]) {
					t.Fatal("profile sweep changed another profile's state")
				}
				mu.Lock()
				before = requests
				mu.Unlock()
				run(pair.second, "U2")
				mu.Lock()
				after = requests
				mu.Unlock()
				if before != after {
					t.Fatal("clearing another profile invalidated the surviving cache")
				}
				body, err = os.ReadFile(configPath)
				if err != nil || !bytes.Equal(body, configBody) {
					t.Fatal("cache commands changed configured profile credentials")
				}
			})
		}
	}
}
