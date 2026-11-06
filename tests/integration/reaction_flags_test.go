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
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	termansi "github.com/gechr/x/ansi"
)

func TestBinaryReactionFlagsKeepExactTargetsAndOrderedEmoji(t *testing.T) {
	binary := buildSlackBinary(t)
	const ts = "1746284582.000001"
	for _, action := range []string{"add", "remove", "list", "watch"} {
		mutation := action == "add" || action == "remove"
		previews := []bool{false}
		if mutation {
			previews = append(previews, true)
		}
		for _, tokenType := range []string{"bot", "user"} {
			for _, mode := range []string{"json", "compact", "human"} {
				for _, short := range []bool{false, true} {
					for _, dry := range previews {
						for _, target := range []struct{ name, channel, timestamp string }{
							{"id", "C_TARGET", ts},
							{"alias", "target", ts},
							{"padded argument", " target ", " " + ts + " "},
							{"padded stored alias", "spaced", "\t" + ts + "\t"},
							{"empty channel ignores default", "", ts},
							{"blank timestamp", "target", " \t "},
						} {
							t.Run(fmt.Sprintf("%s/%s/%s/short=%t/dry=%t/%s", action, tokenType, mode, short, dry, target.name), func(t *testing.T) {
								code := 0
								if strings.TrimSpace(target.channel) == "" || strings.TrimSpace(target.timestamp) == "" {
									code = 4
								}
								emojis := []string{"thumbsup"}
								if mutation && target.name == "id" {
									emojis = []string{"rocket", "heart", "white_check_mark"}
								}
								users := []string{"U_APPROVER"}
								if action == "watch" && (target.name == "id" || target.name == "padded stored alias") {
									users = nil // A positive count can outnumber Slack's reported users.
								}
								token := "xoxb-synthetic-reaction-flags"
								if tokenType == "user" {
									token = "xoxp-synthetic-reaction-flags"
								}
								var mu sync.Mutex
								authCalls, metadataCalls, reads := 0, 0, 0
								var applied []string
								state := map[string]bool{"unrelated": true}
								for _, emoji := range emojis {
									state[emoji] = action == "remove"
								}
								server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
									mu.Lock()
									defer mu.Unlock()
									if err := r.ParseForm(); err != nil {
										t.Error(err)
									}
									if r.Method != http.MethodPost || r.URL.RawQuery != "" || r.PostForm.Get("token") != token {
										t.Error("reaction changed POST form authentication")
									}
									switch r.URL.Path {
									case "/api/auth.test":
										authCalls++
										w.Header().Set("X-OAuth-Scopes", "reactions:read,reactions:write")
										writeJSON(w, `{"ok":true,"user_id":"U1"}`)
									case "/api/conversations.info":
										metadataCalls++
										if r.PostForm.Get("channel") != "C_TARGET" {
											t.Error("reaction metadata used the wrong channel")
										}
										writeJSON(w, `{"ok":false,"error":"missing_scope"}`)
									case "/api/reactions.add", "/api/reactions.remove":
										index := len(applied)
										name := r.PostForm.Get("name")
										if !mutation || r.URL.Path != "/api/reactions."+action || r.PostForm.Get("channel") != "C_TARGET" || r.PostForm.Get("timestamp") != ts || index >= len(emojis) || name != emojis[index] {
											t.Error("reaction lost its normalized target or ordered emoji values")
										}
										applied = append(applied, name)
										state[name] = action == "add"
										writeJSON(w, `{"ok":true}`)
									case "/api/reactions.get":
										reads++
										if mutation || r.PostForm.Get("channel") != "C_TARGET" || r.PostForm.Get("timestamp") != ts || r.PostForm.Get("full") != "true" || r.PostForm.Has("user") || r.PostForm.Has("name") {
											t.Error("reaction read changed target/full values or transmitted local filters")
										}
										body, err := json.Marshal(map[string]any{"ok": true, "type": "message", "message": map[string]any{"ts": ts, "reactions": []map[string]any{{"name": "+1", "count": 7, "users": users, "unreviewed_secret": "synthetic-private-reaction-field"}}}})
										if err != nil {
											t.Error(err)
										}
										writeJSON(w, string(body))
									default:
										t.Errorf("unexpected reaction request: %s", r.URL.Path)
										w.WriteHeader(http.StatusInternalServerError)
									}
								}))
								defer server.Close()
								root := t.TempDir()
								path := filepath.Join(root, "config.toml")
								prior := []byte(fmt.Sprintf("schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"T1\"\ntoken_type=%q\ntoken_ref=\"env:SLICK_REACTION_FLAGS_TOKEN\"\ndefault_channel=\"C_OTHER\"\n[workspaces.fixture.aliases]\ntarget=\"C_TARGET\"\nspaced=\" C_TARGET \"\nkeep=\"C_KEEP\"\n", tokenType))
								if err := os.WriteFile(path, prior, 0o600); err != nil {
									t.Fatal(err)
								}
								channelFlag, timeFlag, emojiFlag, userFlag, intervalFlag := "--channel", "--timestamp", "--emoji", "--user", "--interval"
								if short {
									channelFlag, timeFlag, emojiFlag, userFlag, intervalFlag = "-c", "-t", "-e", "-U", "-i"
								}
								args := []string{"react", action, channelFlag, target.channel, timeFlag, target.timestamp, "--output=" + mode, "--color=never", "--no-throttle", "--timeout=2s"}
								if mutation {
									if len(emojis) == 3 {
										args = append(args, emojiFlag, " :rocket: , , :heart: ", emojiFlag, ":white_check_mark:")
									} else {
										args = append(args, emojiFlag, " :thumbsup: ")
									}
								}
								if action == "watch" {
									args = append(args, emojiFlag, " :thumbsup: ", intervalFlag, "1s")
									switch target.name {
									case "id": // Omitted user filter must accept a positive count without users.
									case "padded stored alias":
										args = append(args, userFlag, "")
									default:
										args = append(args, userFlag, " U_APPROVER ")
									}
								}
								if dry {
									args = append(args, "--dry-run")
								}
								ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
								defer cancel()
								command := exec.CommandContext(ctx, binary, args...)
								command.Dir = root
								command.Env = append(isolatedRuntimeEnv(t), "SLICK_CONFIG="+path, "SLACK_CLI_CONFIG="+path, "SLACK_CLI_BASE_URL="+server.URL, "SLICK_REACTION_FLAGS_TOKEN="+token)
								var out, diagnostic strings.Builder
								command.Stdout, command.Stderr = &out, &diagnostic
								err := command.Run()
								if ctx.Err() != nil || exitCode(err) != code {
									t.Fatalf("reaction exit=%d want=%d: %s %s", exitCode(err), code, out.String(), diagnostic.String())
								}
								if strings.Contains(out.String()+diagnostic.String(), token) || strings.Contains(out.String()+diagnostic.String(), "synthetic-private-reaction-field") {
									t.Fatal("reaction disclosed credentials or unreviewed response data")
								}
								mu.Lock()
								if code != 0 || dry {
									if authCalls != 0 || metadataCalls != 0 || reads != 0 || len(applied) != 0 {
										t.Error("invalid target or preview reached Slack")
									}
								} else if authCalls != 1 || metadataCalls != 1 || (mutation && (!slices.Equal(applied, emojis) || reads != 0)) || (!mutation && (reads != 1 || len(applied) != 0)) {
									t.Error("reaction repeated/skipped an operation or watch mutated Slack")
								}
								for _, name := range emojis {
									want := action == "remove"
									if mutation && code == 0 && !dry {
										want = action == "add"
									}
									if state[name] != want || !state["unrelated"] {
										t.Error("reaction failed to preserve expected or unrelated state")
									}
								}
								mu.Unlock()
								assertReactionFlagResult(t, action, mode, code, dry, mutation, emojis, users, out.String(), diagnostic.String())
								after, readErr := os.ReadFile(path)
								if readErr != nil || string(after) != string(prior) {
									t.Fatal("reaction changed prior configuration or unrelated aliases")
								}
							})
						}
					}
				}
			}
		}
	}
}

func assertReactionFlagResult(t *testing.T, action, mode string, code int, dry, mutation bool, emojis, users []string, out, diagnostic string) {
	t.Helper()
	if mode == "human" {
		if code != 0 {
			if out != "" || diagnostic == "" {
				t.Fatal("reaction failure used wrong human streams")
			}
			return
		}
		if out == "" || diagnostic != "" {
			t.Fatal("reaction success lost human output or emitted diagnostics")
		}
		visible := termansi.Strip(out)
		if mutation {
			for _, emoji := range emojis {
				if !strings.Contains(visible, emoji) || !strings.Contains(visible, "1746284582.000001") || !strings.Contains(visible, "C_TARGET") {
					t.Fatal("human reaction lost emoji or exact target")
				}
			}
		} else if !strings.Contains(visible, "+1") || !strings.Contains(visible, "7") {
			t.Fatal("human reaction lost reported name/count")
		}
		return
	}
	if diagnostic != "" {
		t.Fatal("reaction machine diagnostics escaped stdout envelope")
	}
	data := json.RawMessage(out)
	if mode == "json" || code != 0 {
		var fields map[string]json.RawMessage
		var frame struct {
			OK     bool
			Data   json.RawMessage
			Errors []struct{ Type string }
			Meta   struct {
				Command, Workspace string
				Code               int `json:"exit_code"`
			}
		}
		if json.Unmarshal([]byte(out), &fields) != nil || len(fields) != 4 || json.Unmarshal([]byte(out), &frame) != nil || frame.OK != (code == 0) || frame.Meta.Command != "react."+action || frame.Meta.Workspace != "fixture" || frame.Meta.Code != code {
			t.Fatal("reaction lost common envelope or canonical metadata")
		}
		if code != 0 {
			if string(frame.Data) != "null" || len(frame.Errors) != 1 || frame.Errors[0].Type != "validation_error" {
				t.Fatal("reaction target validation lost its error contract")
			}
			return
		}
		if len(frame.Errors) != 0 {
			t.Fatal("successful reaction retained errors")
		}
		data = frame.Data
	}
	var result struct {
		Target    struct{ Channel, TS string }
		Mutations []struct {
			Channel, TS, Emoji string
			Dry                bool `json:"dry_run"`
		}
		Reactions []struct {
			Name  string
			Count int
			Users []string
		}
	}
	if json.Unmarshal(data, &result) != nil || result.Target.Channel != "C_TARGET" || result.Target.TS != "1746284582.000001" {
		t.Fatal("reaction result lost normalized target or timestamp precision")
	}
	if mutation {
		if len(result.Mutations) != len(emojis) {
			t.Fatal("reaction lost ordered mutation receipts")
		}
		for i, receipt := range result.Mutations {
			if receipt.Emoji != emojis[i] || receipt.Channel != "C_TARGET" || receipt.TS != "1746284582.000001" || receipt.Dry != dry {
				t.Fatal("reaction receipt changed order, target, or preview state")
			}
		}
	} else if len(result.Mutations) != 0 || len(result.Reactions) != 1 || result.Reactions[0].Name != "+1" || result.Reactions[0].Count != 7 || !slices.Equal(result.Reactions[0].Users, users) {
		t.Fatal("reaction read lost canonical emoji or inferred unreported users/counts")
	}
}
