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

func TestHistoryFlagGuardsBeforeCredentials(t *testing.T) {
	t.Setenv("SLICK_CONFIG", filepath.Join(t.TempDir(), "unused.toml"))
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"negative page", []string{"--max-items=-1"}},
		{"channel page too large", []string{"--max-items=1000"}},
		{"thread page too large", []string{"--thread=1746284500.000001", "--max-items=1001"}},
		{"blank thread cannot bypass channel limit", []string{"--thread= \t ", "--max-items=1000"}},
		{"negative reply count", []string{"--include-replies", "--reply-limit=-1"}},
		{"reply count too large", []string{"--include-replies", "--reply-limit=1000"}},
		{"huge reply count", []string{"--include-replies", "--reply-limit=2147483647"}},
		{"reply count without inclusion", []string{"--reply-limit=2"}},
		{"zero reply count without inclusion", []string{"--reply-limit=0"}},
		{"false inclusion with reply count", []string{"--include-replies=false", "--reply-limit=0"}},
		{"nested replies on thread", []string{"--thread=1746284500.000001", "--include-replies"}},
	} {
		for _, short := range []bool{false, true} {
			for _, mode := range []string{"json", "compact", "human"} {
				// Keep every bound/flag combination with both spellings in JSON.
				// Negative page retains each spelling's validation protocol in other modes.
				if mode != "json" && tc.name != "negative page" {
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
						return "", errors.New("synthetic-private-history-provider")
					})))
					root.SetContext(t.Context())
					args := []string{"history", "list", "--channel=C_TARGET", "--output=" + mode, "--color=never"}
					for _, arg := range tc.args {
						if short {
							name, value, hasValue := strings.Cut(arg, "=")
							name = map[string]string{"--max-items": "-M", "--thread": "-t", "--include-replies": "-R", "--reply-limit": "-L"}[name]
							arg = name
							if hasValue {
								arg += "=" + value
							}
						}
						args = append(args, arg)
					}
					root.SetArgs(args)
					err = root.Execute()
					failure, ok := errors.AsType[clioutput.CommandError](err)
					if !ok || failure.CLIError.ExitCode != 4 || reads.Load() != 0 {
						t.Fatalf("invalid history reached credentials or changed error class: reads=%d", reads.Load())
					}
					if strings.Contains(out.String()+diagnostic.String(), "synthetic-private-history-provider") {
						t.Fatal("history exposed a credential provider diagnostic")
					}
					if mode == "human" {
						if out.Len() != 0 || diagnostic.Len() == 0 {
							t.Fatal("history validation used wrong human streams")
						}
					} else {
						var fields map[string]json.RawMessage
						var frame struct {
							Meta struct {
								Command string
								Code    int `json:"exit_code"`
							}
						}
						if diagnostic.Len() != 0 || json.Unmarshal(out.Bytes(), &fields) != nil || len(fields) != 4 || string(fields["ok"]) != "false" || string(fields["data"]) != "null" || json.Unmarshal(out.Bytes(), &frame) != nil || frame.Meta.Command != "history.list" || frame.Meta.Code != 4 {
							t.Fatal("history validation lost common envelope or metadata")
						}
					}
					after, err := json.Marshal(cfg)
					if err != nil || !bytes.Equal(before, after) {
						t.Fatal("history validation changed prior config")
					}
				})
			}
		}
	}
}
