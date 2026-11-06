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

func TestBinaryLookupListFlagsAndRemovalKeepRequestAndPaginationContracts(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, kind := range []string{"channel", "user"} {
		for _, mode := range []string{"json", "compact", "human"} {
			for _, short := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/short=%t", kind, mode, short), func(t *testing.T) {
					const token = "xoxp-synthetic-lookup-flags"
					var mu sync.Mutex
					calls, step := 0, 0
					requests := 0
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						mu.Lock()
						requests++
						mu.Unlock()
						if err := r.ParseForm(); err != nil {
							t.Error(err)
						}
						if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.PostForm.Get("token") != token {
							t.Error("lookup changed POST/authentication")
						}
						if r.URL.Path == "/api/auth.test" {
							w.Header().Set("X-OAuth-Scopes", "channels:read,groups:read,im:read,mpim:read,users:read")
							writeJSON(w, `{"ok":true,"user_id":"U1"}`)
							return
						}
						mu.Lock()
						calls++
						current := step
						mu.Unlock()
						wantMethod := "conversations.list"
						if kind == "user" {
							wantMethod = "users.list"
						}
						if r.URL.Path != "/api/"+wantMethod {
							t.Errorf("wrong lookup mode: %s", r.URL.Path)
						}
						limit, cursor := r.PostForm.Get("limit"), r.PostForm.Get("cursor")
						if current == 0 {
							if limit != "3" || cursor != "page-in" {
								t.Error("lookup lost explicit page limit/cursor")
							}
						} else {
							if cursor != "" {
								t.Error("cursor survived optional argument removal")
							}
							if kind == "channel" && r.PostForm.Has("limit") {
								t.Error("channel limit survived removal")
							}
							if kind == "user" && limit != "200" {
								t.Error("user list did not restore SDK default page size")
							}
						}
						if r.PostForm.Has("filter") || r.PostForm.Has("include_deleted") || r.PostForm.Has("presence") {
							t.Error("lookup sent local-only or retired fields")
						}
						if kind == "channel" {
							types := r.PostForm.Get("types")
							if current == 0 {
								if types != "im,mpim" {
									t.Error("channel aliases/types were not normalized")
								}
								writeJSON(w, `{"ok":true,"channels":[{"id":"D1","name":"keep","is_im":true,"user":"U1"},{"id":"G1","name":"other","is_mpim":true}],"response_metadata":{"next_cursor":"page-next"}}`)
							} else {
								if types != "public_channel,private_channel" {
									t.Error("channel types survived optional removal")
								}
								writeJSON(w, `{"ok":true,"channels":[{"id":"C1","name":"keep","is_channel":true},{"id":"G2","name":"other","is_group":true}],"response_metadata":{"next_cursor":"page-next"}}`)
							}
						} else {
							writeJSON(w, `{"ok":true,"members":[{"id":"U1","name":"keep"},{"id":"U2","name":"other"},{"id":"W1","name":"keep retired","deleted":true}],"response_metadata":{"next_cursor":"page-next"}}`)
						}
					}))
					defer server.Close()
					home := t.TempDir()
					cfgPath := filepath.Join(home, "config.toml")
					cfg := "schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"T123\"\ntoken_type=\"user\"\ntoken=\"env:SLICK_LOOKUP_FLAGS_TOKEN\"\n[workspaces.fixture.aliases]\nkeep=\"CKEEP\"\n"
					if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
						t.Fatal(err)
					}
					run := func(extra []string) (string, string, error) {
						ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
						defer cancel()
						args := append([]string{"lookup", kind, "--output=" + mode, "--color=never", "--no-throttle", "--timeout=3s"}, extra...)
						cmd := exec.CommandContext(ctx, binary, args...)
						cmd.Env = []string{"HOME=" + home, "XDG_CACHE_HOME=" + filepath.Join(home, "cache"), "SLICK_CONFIG=" + cfgPath, "SLACK_CLI_BASE_URL=" + server.URL, "SLICK_LOOKUP_FLAGS_TOKEN=" + token}
						var out, diagnostic bytes.Buffer
						cmd.Stdout, cmd.Stderr = &out, &diagnostic
						err := cmd.Run()
						if ctx.Err() != nil {
							t.Fatal("lookup flags exceeded fixture timeout")
						}
						if strings.Contains(out.String()+diagnostic.String(), token) {
							t.Fatal("lookup flags exposed credentials")
						}
						return out.String(), diagnostic.String(), err
					}
					flag := func(long, alias, value string) []string {
						if short {
							return []string{alias, value}
						}
						return []string{long + "=" + value}
					}
					args := append(flag("--max-items", "-M", "3"), flag("--cursor", "-C", "page-in")...)
					args = append(args, flag("--filter", "-f", "keep")...)
					selector, alias := "--channel", "-c"
					if kind == "user" {
						selector, alias = "--user", "-u"
					}
					args = append(args, flag(selector, alias, "")...)
					if kind == "channel" {
						args = append(args, flag("--types", "-t", "dm,group-dm,im")...)
					} else {
						if short {
							args = append(args, "-d")
						} else {
							args = append(args, "--include-deleted")
						}
					}
					for index, extra := range [][]string{args, nil, flag("--filter", "-f", "no-match")} {
						mu.Lock()
						step = index
						before := calls
						mu.Unlock()
						out, diagnostic, err := run(extra)
						if err != nil || diagnostic != "" || out == "" {
							t.Fatalf("valid lookup flags failed: %v %s", err, diagnostic)
						}
						wantCount := 1
						if kind == "user" && index == 0 {
							wantCount = 2
						}
						if index == 1 {
							wantCount = 2
						}
						if index == 2 {
							wantCount = 0
						}
						if mode == "human" {
							if !strings.Contains(out, "next_cursor=page-next") || !strings.Contains(out, "has_more=true") {
								t.Fatal("human filtered page lost pagination")
							}
						} else {
							data := []byte(out)
							var paging struct {
								Next  string `json:"next_cursor"`
								More  bool   `json:"has_more"`
								Count int    `json:"items_returned"`
							}
							if mode == "json" {
								var frame map[string]json.RawMessage
								if err := json.Unmarshal(data, &frame); err != nil || len(frame) != 4 || string(frame["ok"]) != "true" {
									t.Fatal("lookup success lost envelope")
								}
								var meta struct{ Pagination json.RawMessage }
								if err := json.Unmarshal(frame["meta"], &meta); err != nil {
									t.Fatal(err)
								}
								if err := json.Unmarshal(meta.Pagination, &paging); err != nil {
									t.Fatal(err)
								}
								data = frame["data"]
							}
							var result map[string]json.RawMessage
							if err := json.Unmarshal(data, &result); err != nil {
								t.Fatal(err)
							}
							if mode == "compact" {
								if err := json.Unmarshal(result["pagination"], &paging); err != nil {
									t.Fatal(err)
								}
							}
							key := "channels"
							if kind == "user" {
								key = "users"
							}
							var rows []struct{ ID string }
							if err := json.Unmarshal(result[key], &rows); err != nil || len(rows) != wantCount || paging.Count != wantCount || !paging.More || paging.Next != "page-next" {
								t.Fatal("lookup filter/removal lost rows or next cursor")
							}
						}
						mu.Lock()
						after := calls
						mu.Unlock()
						if after != before+1 {
							t.Fatal("lookup replayed or fetched unrequested pages")
						}
					}
					for _, invalid := range [][]string{{selector + "=U1", "--filter="}, {"--max-items=-1"}} {
						if kind == "channel" {
							invalid[0] = strings.ReplaceAll(invalid[0], "=U1", "=C1")
						}
						mu.Lock()
						before := requests
						mu.Unlock()
						out, diagnostic, err := run(invalid)
						if exitCode(err) != 4 {
							t.Fatalf("invalid lookup flags were accepted: %v", err)
						}
						mu.Lock()
						after := requests
						mu.Unlock()
						if after != before {
							t.Fatal("invalid lookup flags reached a Slack method")
						}
						if mode == "human" {
							if out != "" || !strings.Contains(diagnostic, "validation_error") {
								t.Fatal("human lookup rejection used wrong stream")
							}
						} else {
							var frame map[string]json.RawMessage
							if err := json.Unmarshal([]byte(out), &frame); err != nil || len(frame) != 4 || string(frame["ok"]) != "false" || diagnostic != "" {
								t.Fatal("lookup rejection lost machine envelope")
							}
						}
					}
					after, err := os.ReadFile(cfgPath)
					if err != nil || string(after) != cfg {
						t.Fatal("lookup flags changed saved settings")
					}
				})
			}
		}
	}
}
