package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	"github.com/matcra587/slack-cli/internal/config"
)

func TestReactionWatchInputGuardsBeforeCredentials(t *testing.T) {
	t.Setenv("SLICK_CONFIG", filepath.Join(t.TempDir(), "unused.toml"))
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, tc := range []struct{ name, flag, value string }{
		{"empty emoji", "emoji", ""},
		{"multiple emoji", "emoji", ":+1:,:heart:"},
		{"emoji with spaces", "emoji", "white check mark"},
		{"zero interval", "interval", "0s"},
		{"subsecond interval", "interval", "999ms"},
		{"negative interval", "interval", "-1s"},
	} {
		for _, short := range []bool{false, true} {
			for _, mode := range []string{"json", "compact", "human"} {
				// Both spellings keep every input guard. One invalid emoji and interval
				// cover each flag family's failure protocol in the other output modes.
				if mode != "json" && tc.name != "empty emoji" && tc.name != "subsecond interval" {
					continue
				}
				t.Run(fmt.Sprintf("%s/short=%t/%s", tc.name, short, mode), func(t *testing.T) {
					cfg := workspaceConfig(config.TokenTypeUser)
					before, err := json.Marshal(cfg)
					if err != nil {
						t.Fatal(err)
					}
					var out, diagnostic bytes.Buffer
					var reads atomic.Int32
					root := NewRootCommand(WithConfig(cfg), WithTTY(false), WithIO(strings.NewReader(""), &out, &diagnostic), WithSlackBaseURL("http://127.0.0.1:1"), WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
						reads.Add(1)
						return "", errors.New("synthetic-private-watch-provider")
					})))
					root.SetContext(t.Context())
					emojiFlag, intervalFlag := "--emoji", "--interval"
					if short {
						emojiFlag, intervalFlag = "-e", "-i"
					}
					emoji := ":+1:"
					if tc.flag == "emoji" {
						emoji = tc.value
					}
					args := []string{"react", "watch", "--channel=C_TARGET", "--timestamp=1746284582.000001", emojiFlag, emoji, "--color=never", "--output=" + mode}
					if tc.flag == "interval" {
						args = append(args, intervalFlag, tc.value)
					}
					root.SetArgs(args)
					err = root.Execute()
					failure, ok := errors.AsType[clioutput.CommandError](err)
					if !ok || failure.CLIError.ExitCode != 4 || reads.Load() != 0 {
						t.Fatalf("invalid watch reached credentials or changed its error class: reads=%d", reads.Load())
					}
					if strings.Contains(out.String()+diagnostic.String(), "synthetic-private-watch-provider") {
						t.Fatal("watch exposed a credential provider diagnostic")
					}
					if mode == "human" {
						if out.Len() != 0 || diagnostic.Len() == 0 {
							t.Fatal("watch validation used wrong human streams")
						}
					} else {
						var fields map[string]json.RawMessage
						var frame struct {
							Meta struct {
								Command string
								Code    int `json:"exit_code"`
							}
						}
						if diagnostic.Len() != 0 || json.Unmarshal(out.Bytes(), &fields) != nil || len(fields) != 4 || string(fields["ok"]) != "false" || string(fields["data"]) != "null" || json.Unmarshal(out.Bytes(), &frame) != nil || frame.Meta.Command != "react.watch" || frame.Meta.Code != 4 {
							t.Fatal("watch validation lost the common envelope or metadata")
						}
					}
					after, err := json.Marshal(cfg)
					if err != nil || !bytes.Equal(before, after) {
						t.Fatal("watch validation changed prior configuration")
					}
				})
			}
		}
	}
}
