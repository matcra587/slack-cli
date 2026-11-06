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

func TestBlankSendAliasesFailBeforeCredentials(t *testing.T) {
	t.Setenv("SLICK_CONFIG", filepath.Join(t.TempDir(), "unused.toml"))
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, scheduled := range []bool{false, true} {
		for _, targetFlag := range []string{"--channel", "-c", "--user", "-u"} {
			for _, mode := range []string{"json", "compact", "human"} {
				// Both send paths and all target spellings retain JSON guards. Keep
				// channel and user failure streams on distinct send paths in other modes.
				presentationCase := scheduled && targetFlag == "--channel" || !scheduled && targetFlag == "--user"
				if mode != "json" && !presentationCase {
					continue
				}
				t.Run(fmt.Sprintf("scheduled=%t/%s/%s", scheduled, targetFlag, mode), func(t *testing.T) {
					cfg := workspaceConfig(config.TokenTypeUser)
					profile := cfg.Workspaces["default"]
					profile.DefaultChannel = "C_UNRELATED"
					profile.Aliases = map[string]string{"blank": " \t "}
					cfg.Workspaces["default"] = profile
					before, err := json.Marshal(cfg)
					if err != nil {
						t.Fatal(err)
					}
					var reads atomic.Int32
					var out, diagnostic bytes.Buffer
					root := NewRootCommand(WithConfig(cfg), WithTTY(false), WithIO(strings.NewReader(""), &out, &diagnostic), WithSlackBaseURL("http://127.0.0.1:1"), WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
						reads.Add(1)
						return "", errors.New("synthetic-private-send-provider")
					})))
					args := []string{"message", "send", targetFlag, "blank", "--message", "Release review ready", "--output=" + mode, "--color=never"}
					if scheduled {
						args = append(args, "--schedule", "1h")
					}
					root.SetContext(t.Context())
					root.SetArgs(args)
					err = root.Execute()
					failure, ok := errors.AsType[clioutput.CommandError](err)
					if !ok || failure.CLIError.ExitCode != 4 || reads.Load() != 0 {
						t.Fatalf("blank alias reached credentials or changed validation class: reads=%d", reads.Load())
					}
					if strings.Contains(out.String()+diagnostic.String(), "synthetic-private-send-provider") {
						t.Fatal("blank alias exposed credential-provider diagnostics")
					}
					if mode == "human" {
						if out.Len() != 0 || diagnostic.Len() == 0 {
							t.Fatal("blank alias used wrong human streams")
						}
					} else {
						var frame struct {
							OK   bool
							Data any
							Meta struct {
								Command string
								Code    int `json:"exit_code"`
							}
						}
						if diagnostic.Len() != 0 || json.Unmarshal(out.Bytes(), &frame) != nil || frame.OK || frame.Data != nil || frame.Meta.Command != "message.send" || frame.Meta.Code != 4 {
							t.Fatal("blank alias lost failure envelope or metadata")
						}
					}
					after, err := json.Marshal(cfg)
					if err != nil || !bytes.Equal(before, after) {
						t.Fatal("blank alias validation changed saved settings")
					}
				})
			}
		}
	}
}
