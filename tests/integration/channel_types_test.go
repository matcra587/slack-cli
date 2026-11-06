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

func TestBinaryChannelTypesValidateEveryValueAndPreserveAliases(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tokenType := range []string{"bot", "user"} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(tokenType+"/"+mode, func(t *testing.T) {
				const token = "xox-synthetic-channel-types"
				const all = "public_channel,private_channel,im,mpim"
				var mu sync.Mutex
				requests, lists := 0, 0
				wantTypes, wantLimit := "", ""
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					defer mu.Unlock()
					requests++
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.PostForm.Get("token") != token {
						t.Error("channel lookup changed method or authentication")
					}
					switch r.URL.Path {
					case "/api/auth.test":
						w.Header().Set("X-OAuth-Scopes", "channels:read,groups:read,im:read,mpim:read")
						writeJSON(w, `{"ok":true,"user_id":"U1"}`)
					case "/api/conversations.list":
						lists++
						if r.PostForm.Get("types") != wantTypes || r.PostForm.Get("limit") != wantLimit || r.PostForm.Get("cursor") != "opaque:page+/=" {
							t.Errorf("channel values changed: types=%q limit=%q cursor=%q", r.PostForm.Get("types"), r.PostForm.Get("limit"), r.PostForm.Get("cursor"))
						}
						if r.PostForm.Has("exclude_archived") || r.PostForm.Has("team_id") {
							t.Error("channel lookup invented optional arguments")
						}
						writeJSON(w, `{"ok":true,"channels":[{"id":"C1","name":"fixture","is_channel":true}],"response_metadata":{"next_cursor":"next:opaque+/="}}`)
					default:
						t.Errorf("unexpected channel request: %s", r.URL.Path)
						writeJSON(w, `{"ok":false,"error":"method_not_found"}`)
					}
				}))
				defer server.Close()
				home := t.TempDir()
				configPath := filepath.Join(home, "config.toml")
				prior := []byte(fmt.Sprintf("schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"T1\"\ntoken_type=%q\ntoken=\"env:SLICK_TYPES_TOKEN\"\n[workspaces.fixture.aliases]\nrelease=\"COTHER\"\n", tokenType))
				if err := os.WriteFile(configPath, prior, 0o600); err != nil {
					t.Fatal(err)
				}
				run := func(t *testing.T, extra ...string) (string, string, error) {
					t.Helper()
					ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
					defer cancel()
					args := append([]string{"lookup", "channel", "--output=" + mode, "--color=never", "--no-throttle", "--timeout=3s"}, extra...)
					cmd := exec.CommandContext(ctx, binary, args...)
					cmd.Env = []string{"HOME=" + home, "XDG_CACHE_HOME=" + filepath.Join(home, "cache"), "SLICK_CONFIG=" + configPath, "SLACK_CLI_BASE_URL=" + server.URL, "SLICK_TYPES_TOKEN=" + token}
					var out, diagnostic bytes.Buffer
					cmd.Stdout, cmd.Stderr = &out, &diagnostic
					err := cmd.Run()
					if ctx.Err() != nil {
						t.Fatal("channel type fixture exceeded timeout")
					}
					if strings.Contains(out.String()+diagnostic.String(), token) || strings.Contains(out.String()+diagnostic.String(), "synthetic-unreviewed-value") {
						t.Fatal("channel type diagnostic exposed sensitive input")
					}
					return out.String(), diagnostic.String(), err
				}
				for _, tc := range []struct {
					name, value, types string
				}{
					{"all", "all", all},
					{"all after aliases", "dm,ALL,channels", all},
					{"all before aliases", "all,group-dm,private", all},
					{"public aliases", " channel,channels,PUBLIC,public_channel ", "public_channel"},
					{"private aliases", "group,groups,PRIVATE,private_channel", "private_channel"},
					{"DM aliases", "dm,DMS,im", "im"},
					{"group DM aliases", "group_dm,group-dm,MPIM", "mpim"},
					{"mixed order", "dm,public,im,group-dm", "im,public_channel,mpim"},
					{"empty", "", "public_channel,private_channel"},
					{"empty segments", ",,  ,", "public_channel,private_channel"},
				} {
					t.Run(tc.name, func(t *testing.T) {
						for _, short := range []bool{false, true} {
							mu.Lock()
							wantTypes, wantLimit = tc.types, "999"
							before := lists
							mu.Unlock()
							flags := []string{"--types=" + tc.value, "--max-items=999", "--cursor=opaque:page+/="}
							if short {
								flags = []string{"-t", tc.value, "-M", "999", "-C", "opaque:page+/="}
							}
							out, diagnostic, err := run(t, flags...)
							if err != nil || diagnostic != "" || !strings.Contains(out, "C1") {
								t.Fatalf("valid channel type selection failed: %v", err)
							}
							mu.Lock()
							count := lists - before
							mu.Unlock()
							if count != 1 {
								t.Fatal("channel lookup replayed or fetched an unrequested page")
							}
							if !strings.Contains(out, "next:opaque+/=") {
								t.Fatal("channel lookup changed the opaque next cursor")
							}
						}
					})
				}
				for _, bad := range [][]string{
					{"--types=all,typo"},
					{"-t", "typo,all"},
					{"--types=im,typo,mpim"},
					{"-t", "all,xoxp-synthetic-unreviewed-value"},
					{"--max-items=1000"},
					{"-M", "2147483647"},
				} {
					mu.Lock()
					before := requests
					mu.Unlock()
					out, diagnostic, err := run(t, bad...)
					if code := exitCode(err); code != 4 {
						t.Fatalf("invalid channel values were accepted: %v", err)
					}
					mu.Lock()
					count := requests - before
					mu.Unlock()
					if count != 0 {
						t.Fatal("invalid channel values reached Slack")
					}
					if mode == "human" {
						if out != "" || diagnostic == "" {
							t.Fatal("channel validation used the wrong stream")
						}
					} else {
						var frame map[string]json.RawMessage
						if err := json.Unmarshal([]byte(out), &frame); err != nil || len(frame) != 4 || string(frame["ok"]) != "false" || string(frame["data"]) != "null" || diagnostic != "" {
							t.Fatal("channel validation lost the failure envelope")
						}
					}
					mu.Lock()
					wantTypes, wantLimit = "public_channel,private_channel", ""
					before = lists
					mu.Unlock()
					out, diagnostic, err = run(t, "--cursor=opaque:page+/=")
					if err != nil || diagnostic != "" || !strings.Contains(out, "C1") {
						t.Fatal("channel lookup did not recover after invalid values were removed")
					}
					mu.Lock()
					count = lists - before
					mu.Unlock()
					if count != 1 {
						t.Fatal("channel recovery did not make exactly one list request")
					}
				}
				after, err := os.ReadFile(configPath)
				if err != nil || !bytes.Equal(prior, after) {
					t.Fatal("channel type selection changed prior settings")
				}
			})
		}
	}
}
