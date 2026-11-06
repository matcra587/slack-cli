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
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBinaryCacheFlagsPreservePaginationRefreshAndFailureState(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, resource := range []string{"users", "channels"} {
		for _, mode := range []string{"json", "compact", "human"} {
			for _, short := range []bool{false, true} {
				tokenType := "user"
				if short && mode != "json" {
					tokenType = "bot"
				}
				t.Run(fmt.Sprintf("%s/%s/short=%t/%s", resource, mode, short, tokenType), func(t *testing.T) {
					token := "xoxp-synthetic-cache-lifecycle"
					if tokenType == "bot" {
						token = "xoxb-synthetic-cache-lifecycle"
					}
					var mu sync.Mutex
					phase, limit := "paged", "2"
					calls := []string{}
					cancelReady := make(chan struct{}, 1)
					method, key, prefix := "users.list", "members", "U"
					if resource == "channels" {
						method, key, prefix = "conversations.list", "channels", "C"
					}
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						mu.Lock()
						defer mu.Unlock()
						if err := r.ParseForm(); err != nil {
							t.Error(err)
						}
						if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.PostForm.Get("token") != token {
							t.Error("cache lifecycle changed method or authentication")
						}
						if r.URL.Path == "/api/auth.test" {
							calls = append(calls, "auth")
							scope := "users:read,channels:read,groups:read,im:read,mpim:read"
							if phase == "scope" {
								scope = "chat:write"
							}
							w.Header().Set("X-OAuth-Scopes", scope)
							writeJSON(w, `{"ok":true,"user_id":"UOWNER"}`)
							return
						}
						cursor := r.PostForm.Get("cursor")
						calls = append(calls, cursor)
						if r.URL.Path != "/api/"+method {
							t.Error("cache lifecycle changed method path")
						}
						if r.PostForm.Get("limit") != limit {
							t.Errorf("cache page size=%s want=%s", r.PostForm.Get("limit"), limit)
						}
						if r.PostForm.Has("presence") || r.PostForm.Has("include_deleted") {
							t.Error("cache sent retired or local-only fields")
						}
						if resource == "channels" {
							if r.PostForm.Get("types") != "public_channel,private_channel,im,mpim" || r.PostForm.Get("exclude_archived") != "true" {
								t.Error("cache lost active conversation selection")
							}
						} else if r.PostForm.Get("include_locale") != "true" {
							t.Error("cache changed SDK locale selection")
						}
						if (phase == "timeout" || phase == "cancel") && cursor != "" {
							if phase == "cancel" {
								select {
								case cancelReady <- struct{}{}:
								default:
									t.Error("cache repeated a canceled request")
								}
							}
							select {
							case <-r.Context().Done():
							case <-time.After(2 * time.Second):
								t.Error("cache did not cancel the blocked request")
							}
							return
						}
						if phase == "partial" && cursor != "" {
							writeJSON(w, `{"ok":false,"error":"no_permission"}`)
							return
						}
						items := []map[string]any{}
						next := ""
						item := func(id string) map[string]any {
							return map[string]any{"id": prefix + id, "name": "fixture-" + id, "unreviewed_secret": "synthetic-unreviewed-cache-field"}
						}
						switch phase {
						case "paged", "partial", "timeout", "cancel":
							switch cursor {
							case "":
								items = append(items, item("1"))
								next = "page+/=1"
								if resource == "users" && limit != "1" {
									deleted := item("DELETED")
									deleted["deleted"] = true
									items = append(items, deleted)
								}
							case "page+/=1":
								next = "page+/=2"
							case "page+/=2":
								items = append(items, item("2"))
							default:
								t.Error("cache changed the opaque cursor")
							}
						case "changed":
							items = append(items, item("NEW"))
						case "changed again":
							items = append(items, item("NEXT"))
						case "empty":
						default:
							t.Error("unexpected lifecycle phase")
						}
						body, err := json.Marshal(map[string]any{"ok": true, key: items, "response_metadata": map[string]string{"next_cursor": next}})
						if err != nil {
							t.Error(err)
						}
						writeJSON(w, string(body))
					}))
					defer server.Close()
					home := t.TempDir()
					configPath := filepath.Join(home, "config.toml")
					configBody := []byte(fmt.Sprintf("schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"T1\"\ntoken_type=%q\ntoken=\"env:SLICK_CACHE_LIFECYCLE_TOKEN\"\n[workspaces.fixture.aliases]\nrelease=\"CPRESERVE\"\n", tokenType))
					if err := os.WriteFile(configPath, configBody, 0o600); err != nil {
						t.Fatal(err)
					}
					cacheDir := filepath.Join(home, "cache", "slick", "fixture")
					cachePath := filepath.Join(cacheDir, resource+".json")
					flags := func(refresh bool, ttl, size, pages string) []string {
						out := []string{}
						for _, f := range []struct{ long, alias, value string }{{"refresh", "r", fmt.Sprint(refresh)}, {"ttl-minutes", "T", ttl}, {"page-size", "s", size}, {"max-pages", "N", pages}} {
							if f.value == "" {
								continue
							}
							name := "--" + f.long
							if short {
								name = "-" + f.alias
							}
							out = append(out, name+"="+f.value)
						}
						return out
					}
					setPhase := func(name, size string) { mu.Lock(); phase, limit, calls = name, size, []string{}; mu.Unlock() }
					assertCalls := func(want ...string) {
						t.Helper()
						mu.Lock()
						defer mu.Unlock()
						if !slices.Equal(calls, want) {
							t.Fatalf("cache skipped, repeated, or over-fetched pages: got %v want %v", calls, want)
						}
					}
					run := func(extra []string, code int, fromCache bool, ids []string, truncated bool) []byte {
						t.Helper()
						ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
						defer cancel()
						args := append([]string{"cache", resource, "--output=" + mode, "--color=never", "--no-throttle", "--timeout=3s"}, extra...)
						cmd := exec.CommandContext(ctx, binary, args...)
						cmd.Env = []string{"HOME=" + home, "XDG_CACHE_HOME=" + filepath.Join(home, "cache"), "SLICK_CONFIG=" + configPath, "SLACK_CLI_BASE_URL=" + server.URL, "SLICK_CACHE_LIFECYCLE_TOKEN=" + token}
						var out, diagnostic bytes.Buffer
						cmd.Stdout, cmd.Stderr = &out, &diagnostic
						var err error
						if code == 6 {
							if err := cmd.Start(); err != nil {
								t.Fatal(err)
							}
							signaled := make(chan error, 1)
							go func() {
								select {
								case <-cancelReady:
									signaled <- cmd.Process.Signal(os.Interrupt)
								case <-ctx.Done():
									signaled <- ctx.Err()
								}
							}()
							err = cmd.Wait()
							select {
							case signalErr := <-signaled:
								if signalErr != nil {
									t.Fatal(signalErr)
								}
							case <-ctx.Done():
								t.Fatal("cache never reached its cancellation barrier")
							}
						} else {
							err = cmd.Run()
						}
						if ctx.Err() != nil {
							t.Fatal("cache lifecycle exceeded timeout")
						}
						if exitCode(err) != code {
							t.Fatalf("cache lifecycle exit=%d want=%d: %v", exitCode(err), code, err)
						}
						if strings.Contains(out.String()+diagnostic.String(), token) || strings.Contains(out.String()+diagnostic.String(), "synthetic-unreviewed-cache-field") {
							t.Fatal("cache lifecycle exposed sensitive data")
						}
						if mode != "human" && (code != 0 || mode == "json") {
							var envelope struct {
								Meta struct {
									Exit    int `json:"exit_code"`
									Command string
								}
								Errors []struct {
									Type string
									Exit int `json:"exit_code"`
								}
							}
							if err := json.Unmarshal(out.Bytes(), &envelope); err != nil || envelope.Meta.Exit != code || envelope.Meta.Command != "cache."+resource {
								t.Fatalf("cache envelope mismatch for %v: command=%q exit=%d want command=%q exit=%d", extra, envelope.Meta.Command, envelope.Meta.Exit, "cache."+resource, code)
							}
							if code == 0 && len(envelope.Errors) != 0 || code != 0 && (len(envelope.Errors) != 1 || envelope.Errors[0].Exit != code) {
								t.Fatal("cache errors disagree with process exit")
							}
							if code != 0 && envelope.Errors[0].Type != map[int]string{1: "auth_failure", 4: "validation_error", 5: "server_error", 6: "canceled", 7: "timeout"}[code] {
								t.Fatal("cache error type disagrees with its exit code")
							}
						}
						if code != 0 {
							if mode == "human" {
								if out.Len() != 0 || diagnostic.Len() == 0 {
									t.Fatal("human cache failure used the wrong stream")
								}
							} else {
								var frame map[string]json.RawMessage
								if err := json.Unmarshal(out.Bytes(), &frame); err != nil || len(frame) != 4 || string(frame["ok"]) != "false" || string(frame["data"]) != "null" || diagnostic.Len() != 0 {
									t.Fatal("cache failure lost the common envelope")
								}
							}
							return nil
						}
						if diagnostic.Len() != 0 || out.Len() == 0 {
							t.Fatal("cache success lost stdout/stderr contract")
						}
						var resultTime time.Time
						if mode != "human" {
							var frame map[string]json.RawMessage
							if err := json.Unmarshal(out.Bytes(), &frame); err != nil {
								t.Fatal(err)
							}
							data := frame
							if mode == "json" {
								if len(frame) != 4 || string(frame["ok"]) != "true" {
									t.Fatal("cache success lost the common envelope")
								}
								data = nil
								if err := json.Unmarshal(frame["data"], &data); err != nil {
									t.Fatal(err)
								}
							}
							var got struct {
								Count     int
								FromCache bool `json:"from_cache"`
								Truncated bool
								FetchedAt time.Time `json:"fetched_at"`
							}
							body, err := json.Marshal(data)
							if err != nil {
								t.Fatal(err)
							}
							if err := json.Unmarshal(body, &got); err != nil {
								t.Fatal(err)
							}
							if got.FromCache != fromCache {
								t.Fatalf("cache source=%t want=%t for flags %v", got.FromCache, fromCache, extra)
							}
							if got.Count != len(ids) {
								t.Fatalf("cache count=%d want=%d for flags %v", got.Count, len(ids), extra)
							}
							if got.Truncated != truncated {
								t.Fatalf("cache truncation=%t want=%t for flags %v", got.Truncated, truncated, extra)
							}
							if got.FetchedAt.IsZero() || data["count"] == nil || data["from_cache"] == nil {
								t.Fatal("cache result lost required metadata")
							}
							resultTime = got.FetchedAt
							var rows []struct{ ID string }
							if err := json.Unmarshal(data[resource], &rows); err != nil || rows == nil {
								t.Fatal("cache output lost its resource array")
							}
							actual := make([]string, 0, len(rows))
							for _, row := range rows {
								actual = append(actual, row.ID)
							}
							if !reflect.DeepEqual(actual, ids) {
								t.Fatal("cache output lost or retained unexpected records")
							}

						}
						body, err := os.ReadFile(cachePath)
						if err != nil {
							t.Fatal(err)
						}
						if bytes.Contains(body, []byte(token)) || bytes.Contains(body, []byte("unreviewed_secret")) {
							t.Fatal("cache persisted unreviewed fields")
						}
						var entry struct {
							Profile, Resource string
							FetchedAt         time.Time `json:"fetched_at"`
							Data              map[string]json.RawMessage
						}
						if err := json.Unmarshal(body, &entry); err != nil || entry.Profile != "fixture" || entry.Resource != resource {
							t.Fatal("cache lost entry identity")
						}
						if mode != "human" && !resultTime.Equal(entry.FetchedAt) {
							t.Fatal("cache output changed stored timestamp precision")
						}
						var rows []struct{ ID string }
						if err := json.Unmarshal(entry.Data[resource], &rows); err != nil {
							t.Fatal(err)
						}
						actual := make([]string, 0, len(rows))
						for _, row := range rows {
							actual = append(actual, row.ID)
						}
						if !reflect.DeepEqual(actual, ids) {
							t.Fatalf("cache lost or retained unexpected records: got %v want %v", actual, ids)
						}
						if info, err := os.Stat(cachePath); err != nil || info.Mode().Perm() != 0o600 {
							t.Fatal("cache output was not protected")
						}
						return body
					}
					initial := flags(false, "5", "2", "3")
					prior := run(initial, 0, false, []string{prefix + "1", prefix + "2"}, false)
					assertCalls("auth", "", "page+/=1", "page+/=2")
					setPhase("paged", "2")
					hit := run(initial, 0, true, []string{prefix + "1", prefix + "2"}, false)
					assertCalls()
					if !bytes.Equal(prior, hit) {
						t.Fatal("cache hit rewrote prior state")
					}
					setPhase("paged", "1")
					run(flags(true, "5", "1", "1"), 0, false, []string{prefix + "1"}, true)
					assertCalls("auth", "")
					setPhase("paged", "2")
					run(initial, 0, true, []string{prefix + "1"}, true)
					assertCalls()
					setPhase("paged", "200")
					prior = run(flags(true, "", "", ""), 0, false, []string{prefix + "1", prefix + "2"}, false)
					assertCalls("auth", "", "page+/=1", "page+/=2")
					var aged map[string]json.RawMessage
					if err := json.Unmarshal(prior, &aged); err != nil {
						t.Fatal(err)
					}
					old, err := json.Marshal(time.Now().Add(-48 * time.Hour).UTC())
					if err != nil {
						t.Fatal(err)
					}
					aged["fetched_at"] = old
					prior, err = json.Marshal(aged)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(cachePath, prior, 0o600); err != nil {
						t.Fatal(err)
					}
					setPhase("changed", "200")
					hit = run(flags(false, "10080", "", ""), 0, true, []string{prefix + "1", prefix + "2"}, false)
					assertCalls()
					if !bytes.Equal(prior, hit) {
						t.Fatal("long TTL cache hit rewrote stored timestamp")
					}
					setPhase("changed", "200")
					run(flags(false, "153722867", "", ""), 0, true, []string{prefix + "1", prefix + "2"}, false)
					assertCalls()
					setPhase("changed again", "200")
					prior = run(nil, 0, false, []string{prefix + "NEXT"}, false)
					assertCalls("auth", "")
					for _, name := range []string{"partial", "scope", "timeout", "cancel"} {
						if name == "cancel" && runtime.GOOS == "windows" {
							continue
						}
						setPhase(name, "2")
						extra, code := flags(true, "5", "2", "3"), 1
						if name == "timeout" {
							extra = append(extra, "--timeout=200ms")
							code = 7
						}
						if name == "cancel" {
							code = 6
						}
						run(extra, code, false, nil, false)
						if name != "scope" {
							assertCalls("auth", "", "page+/=1")
						} else {
							assertCalls("auth")
						}
						body, err := os.ReadFile(cachePath)
						if err != nil || !bytes.Equal(prior, body) {
							t.Fatal("failed refresh replaced prior cache state")
						}
					}
					setPhase("empty", "200")
					prior = run(flags(true, "-1", "999", "-1"), 0, false, []string{}, false)
					assertCalls("auth", "")
					setPhase("empty", "200")
					hit = run(nil, 0, true, []string{}, false)
					assertCalls()
					if !bytes.Equal(prior, hit) {
						t.Fatal("empty cache hit rewrote prior state")
					}
					if err := os.Remove(cachePath); err != nil {
						t.Fatal(err)
					}
					setPhase("paged", "200")
					run(flags(false, "0", "0", "0"), 0, false, []string{prefix + "1", prefix + "2"}, false)
					assertCalls("auth", "", "page+/=1", "page+/=2")
					if err := os.Remove(cachePath); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(cachePath, 0o700); err != nil {
						t.Fatal(err)
					}
					preserve := filepath.Join(cachePath, "preserve.txt")
					if err := os.WriteFile(preserve, []byte("preserve-drift"), 0o600); err != nil {
						t.Fatal(err)
					}
					setPhase("changed", "200")
					run(flags(true, "", "", ""), 5, false, nil, false)
					assertCalls("auth", "")
					if body, err := os.ReadFile(preserve); err != nil || string(body) != "preserve-drift" {
						t.Fatal("failed atomic cache write changed prior filesystem state")
					}
					entries, err := os.ReadDir(cacheDir)
					if err != nil {
						t.Fatal(err)
					}
					for _, e := range entries {
						if strings.HasPrefix(e.Name(), "."+resource+".json.") {
							t.Fatal("failed cache write left a temporary file")
						}
					}
					if err := os.Remove(preserve); err != nil {
						t.Fatal(err)
					}
					if err := os.Remove(cachePath); err != nil {
						t.Fatal(err)
					}
					setPhase("changed", "200")
					run(nil, 0, false, []string{prefix + "NEW"}, false)
					assertCalls("auth", "")
					for _, bad := range [][]string{{"--ttl-minutes=153722868"}, {"-T=2147483647"}, {"--page-size"}, {"--max-pages"}, {"--ttl-minutes"}} {
						setPhase("changed", "200")
						run(bad, 4, false, nil, false)
						assertCalls()
					}
					body, err := os.ReadFile(configPath)
					if err != nil || !bytes.Equal(body, configBody) {
						t.Fatal("cache lifecycle changed unrelated configuration")
					}
				})
			}
		}
	}
}
