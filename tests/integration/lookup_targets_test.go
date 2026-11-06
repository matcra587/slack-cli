//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	termansi "github.com/gechr/x/ansi"
)

func TestBinaryLookupTargetsAndPresenceFlags(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, kind := range []string{"channel", "user"} {
		for _, tokenType := range []string{"bot", "user"} {
			for _, mode := range []string{"json", "compact", "human"} {
				for _, short := range []bool{false, true} {
					// JSON retains every target/profile/presence combination with both
					// spellings. Other modes keep success and both failure categories.
					if mode != "json" && short {
						continue
					}
					targetID, scope := "U_TARGET", "users:read"
					if kind == "channel" {
						targetID, scope = "C_TARGET", "channels:read"
						if tokenType == "bot" {
							targetID, scope = "G_TARGET", "groups:read"
						}
					}
					targets := []struct{ name, value string }{
						{"id", targetID},
						{"alias", "target"},
						{"padded argument", " target "},
						{"padded stored alias", "spaced"},
						{"empty stored alias", "empty"},
						{"blank stored alias", "blank"},
					}
					if kind == "channel" {
						targets = append(targets, struct{ name, value string }{"unconfigured name", "release-name"})
					}
					presenceModes := []string{"omitted"}
					if kind == "user" {
						presenceModes = append(presenceModes, "enabled", "explicit false")
					}
					for _, target := range targets {
						if mode != "json" {
							switch target.name {
							case "id", "blank stored alias", "unconfigured name":
							default:
								continue
							}
						}
						for _, presenceMode := range presenceModes {
							t.Run(fmt.Sprintf("%s/%s/%s/short=%t/%s/%s", kind, tokenType, mode, short, target.name, presenceMode), func(t *testing.T) {
								presence := presenceMode == "enabled"
								wantCode := 0
								switch target.name {
								case "unconfigured name":
									wantCode = 2
								case "empty stored alias", "blank stored alias":
									wantCode = 4
								}
								token := "xoxp-synthetic-lookup-target"
								if tokenType == "bot" {
									token = "xoxb-synthetic-lookup-target"
								}
								var authCalls, infoCalls, presenceCalls atomic.Int32
								server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
									if err := r.ParseForm(); err != nil {
										t.Error(err)
									}
									if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.PostForm.Get("token") != token {
										t.Error("lookup changed POST form authentication")
									}
									switch r.URL.Path {
									case "/api/auth.test":
										authCalls.Add(1)
										w.Header().Set("X-OAuth-Scopes", scope)
										writeJSON(w, `{"ok":true,"user_id":"U1"}`)
									case "/api/conversations.info":
										infoCalls.Add(1)
										want := targetID
										if wantCode == 2 {
											want = "release-name"
										}
										if kind != "channel" || r.PostForm.Get("channel") != want || r.PostForm.Get("include_num_members") != "true" || r.PostForm.Get("include_locale") != "false" {
											t.Errorf("lookup changed exact conversation target or options: channel=%q", r.PostForm.Get("channel"))
										}
										if wantCode == 2 {
											writeJSON(w, `{"ok":false,"error":"channel_not_found"}`)
											return
										}
										writeJSON(w, `{"ok":true,"channel":{"id":"`+targetID+`","name":"release","is_member":false,"num_members":3,"unreviewed_secret":"synthetic-private-lookup-response"}}`)
									case "/api/users.info":
										infoCalls.Add(1)
										if kind != "user" || r.PostForm.Get("user") != targetID || r.PostForm.Get("include_locale") != "true" {
											t.Errorf("lookup changed exact user target or locale: user=%q", r.PostForm.Get("user"))
										}
										writeJSON(w, `{"ok":true,"user":{"id":"U_TARGET","name":"reviewer","tz":"Europe/London","profile":{"status_text":"Review complete"},"unreviewed_secret":"synthetic-private-lookup-response"}}`)
									case "/api/users.getPresence":
										presenceCalls.Add(1)
										if !presence || r.PostForm.Get("user") != targetID {
											t.Error("presence was fetched without the flag or for the wrong user")
										}
										writeJSON(w, `{"ok":true,"presence":"away","unreviewed_secret":"synthetic-private-lookup-response"}`)
									default:
										t.Errorf("single lookup made an unexpected request: %s", r.URL.Path)
										w.WriteHeader(http.StatusInternalServerError)
									}
								}))
								defer server.Close()
								root := t.TempDir()
								path := filepath.Join(root, "config.toml")
								prior := []byte(fmt.Sprintf("schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"T1\"\ntoken_type=%q\ntoken_ref=\"env:SLICK_LOOKUP_TARGET_TOKEN\"\n[workspaces.fixture.aliases]\ntarget=%q\nspaced=%q\nempty=\"\"\nblank=\" \\t \"\nkeep=\"C_UNRELATED\"\n", tokenType, targetID, " "+targetID+" "))
								if err := os.WriteFile(path, prior, 0o600); err != nil {
									t.Fatal(err)
								}
								selector, presenceFlag := "--"+kind, "--presence"
								if short {
									selector = "-c"
									if kind == "user" {
										selector = "-u"
									}
									presenceFlag = "-p"
								}
								args := []string{"lookup", kind, selector, target.value, "--output=" + mode, "--color=never", "--no-throttle"}
								if presence {
									args = append(args, presenceFlag)
								} else if presenceMode == "explicit false" {
									args = append(args, presenceFlag+"=false")
								}
								ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
								defer cancel()
								command := exec.CommandContext(ctx, binary, args...)
								command.Dir = root
								command.Env = append(isolatedRuntimeEnv(t), "SLICK_CONFIG="+path, "SLACK_CLI_CONFIG="+path, "SLACK_CLI_BASE_URL="+server.URL, "SLICK_LOOKUP_TARGET_TOKEN="+token)
								var out, diagnostic strings.Builder
								command.Stdout, command.Stderr = &out, &diagnostic
								err := command.Run()
								if ctx.Err() != nil || exitCode(err) != wantCode {
									t.Fatalf("lookup exit=%d want=%d: %s %s", exitCode(err), wantCode, out.String(), diagnostic.String())
								}
								wantPresenceCalls := int32(0)
								if presence {
									wantPresenceCalls = 1
								}
								wantInfoCalls := int32(1)
								if wantCode == 4 {
									wantInfoCalls, wantPresenceCalls = 0, 0
								}
								if authCalls.Load() != wantInfoCalls || infoCalls.Load() != wantInfoCalls || presenceCalls.Load() != wantPresenceCalls {
									t.Fatal("single lookup retried, browsed, or fetched unrequested presence")
								}
								if strings.Contains(out.String()+diagnostic.String(), token) || strings.Contains(out.String()+diagnostic.String(), "synthetic-private-lookup-response") {
									t.Fatal("lookup disclosed credentials or an unreviewed response field")
								}
								assertLookupTargetResult(t, kind, mode, targetID, presence, wantCode, out.String(), diagnostic.String())
								after, readErr := os.ReadFile(path)
								if readErr != nil || string(after) != string(prior) {
									t.Fatal("lookup changed prior configuration or unrelated aliases")
								}
							})
						}
					}
				}
			}
		}
	}
}

func TestBinaryLookupUserTimezoneColorModes(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tc := range []struct{ name, flag, color string }{
		{"auto", "--color", "auto"},
		{"never", "--color", "never"},
		{"always", "--color", "always"},
		{"short auto", "-V", "auto"},
		{"short never", "-V", "never"},
		{"short always", "-V", "always"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Method != http.MethodPost || r.PostForm.Get("token") != "xoxb-test" {
					t.Error("color selection changed request authentication or method")
				}
				switch r.URL.Path {
				case "/api/auth.test":
					w.Header().Set("X-OAuth-Scopes", "users:read")
					writeJSON(w, `{"ok":true,"user_id":"U1"}`)
				case "/api/users.info":
					writeJSON(w, `{"ok":true,"user":{"id":"U123","name":"reviewer","tz":"Europe/London"}}`)
				default:
					t.Errorf("unexpected color-fixture API request: %s", r.URL.Path)
					w.WriteHeader(http.StatusInternalServerError)
				}
			}))
			defer server.Close()
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte("schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"T1\"\ntoken_type=\"bot\"\ntoken_ref=\"env:SLACK_TEST_TOKEN\"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			out, diagnostic, err := runSlackBinary(t, binary, path, server.URL, "", "lookup", "user", "--user=U123", "--output=human", tc.flag+"="+tc.color, "--no-throttle")
			if err != nil || diagnostic != "" || !strings.Contains(termansi.Strip(out), "Europe/London") {
				t.Fatalf("color fixture lost the returned timezone: %v", err)
			}
			if strings.Contains(out, "\x1b") != (tc.color == "always") {
				t.Fatal("timezone output ignored explicit color mode or automatic nonterminal detection")
			}
			if tc.color == "always" && strings.Contains(out, "Europe/London") {
				t.Fatal("explicit color lost the separate region and city styles")
			}
		})
	}
}

func assertLookupTargetResult(t *testing.T, kind, mode, id string, presence bool, code int, out, diagnostic string) {
	t.Helper()
	if mode == "human" {
		if strings.Contains(out+diagnostic, "\x1b") {
			t.Fatal("lookup emitted terminal escapes with --color=never")
		}
		visible := termansi.Strip(out)
		if code != 0 {
			if out != "" || diagnostic == "" {
				t.Fatal("lookup failure used wrong human streams")
			}
			return
		}
		if diagnostic != "" || !strings.Contains(visible, id) || strings.Contains(visible, "presence=away") != presence {
			t.Fatal("human lookup lost the identity or presence flag")
		}
		if kind == "user" && (!strings.Contains(visible, "Europe/London") || !strings.Contains(visible, "Review complete")) {
			t.Fatalf("human lookup lost returned timezone or status text: %q", out)
		}
		return
	}
	if diagnostic != "" {
		t.Fatal("lookup wrote machine diagnostics outside stdout")
	}
	data := json.RawMessage(out)
	if mode == "json" || code != 0 {
		var keys map[string]json.RawMessage
		var frame struct {
			OK     bool
			Data   json.RawMessage
			Errors []struct{ Type string }
			Meta   struct {
				Command, Workspace string
				Code               int `json:"exit_code"`
			}
		}
		if json.Unmarshal([]byte(out), &keys) != nil || len(keys) != 4 || json.Unmarshal([]byte(out), &frame) != nil || frame.OK != (code == 0) || frame.Meta.Command != "lookup."+kind || frame.Meta.Workspace != "fixture" || frame.Meta.Code != code {
			t.Fatal("lookup lost common envelope or canonical metadata")
		}
		if code != 0 {
			kind := "not_found"
			if code == 4 {
				kind = "validation_error"
			}
			if string(frame.Data) != "null" || len(frame.Errors) != 1 || frame.Errors[0].Type != kind {
				t.Fatal("lookup lost its error contract")
			}
			return
		}
		if len(frame.Errors) != 0 {
			t.Fatal("successful lookup retained errors")
		}
		data = frame.Data
	}
	var fields map[string]json.RawMessage
	var entity struct {
		ID, Presence string
		Timezone     string `json:"tz"`
		StatusText   string `json:"status_text"`
	}
	if json.Unmarshal(data, &fields) != nil || json.Unmarshal(fields[kind], &entity) != nil || entity.ID != id {
		t.Fatal("lookup lost the exact returned identity")
	}
	var values map[string]json.RawMessage
	if json.Unmarshal(fields[kind], &values) != nil {
		t.Fatal("lookup returned an invalid entity")
	}
	if _, exists := values["presence"]; exists != presence || (presence && entity.Presence != "away") {
		t.Fatal("lookup ignored presence selection or optional removal")
	}
	if kind == "channel" && (string(values["is_member"]) != "false" || string(values["num_members"]) != "3") {
		t.Fatal("lookup lost explicit false membership or member count")
	}
	if kind == "user" && (entity.Timezone != "Europe/London" || entity.StatusText != "Review complete") {
		t.Fatal("lookup lost returned timezone or status text")
	}
}
