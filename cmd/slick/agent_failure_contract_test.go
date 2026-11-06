package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func TestAgentEarlyFailuresKeepAgentModeAndWorkspace(t *testing.T) {
	for _, tty := range []bool{false, true} {
		for _, mode := range []string{"auto", "json", "compact", "human"} {
			for _, missing := range []bool{false, true} {
				for _, args := range [][]string{
					{"agent", "schema", "--compact", "--shapes"},
					{"agent", "guide", "send-msg", "--all", "--section=Run"},
					{"agent", "schema", "--path=unknown command"},
					{"agent", "export", "--dir=unused", "--format=unknown"},
				} {
					t.Run(fmt.Sprintf("tty=%t/%s/missing=%t/%s", tty, mode, missing, strings.Join(args[1:], " ")), func(t *testing.T) {
						cfg := workspaceConfig(config.TokenTypeBot)
						var out, diagnostic bytes.Buffer
						lookups := 0
						root, runtime := NewRootCommandWithRuntime(WithConfig(cfg), WithTTY(tty), WithIO(strings.NewReader(""), &out, &diagnostic), WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
							lookups++
							return "", errors.New("synthetic-agent-failure-secret")
						})))
						if missing {
							runtime.Config = nil
							runtime.ConfigLoadError = errors.New("synthetic-agent-failure-private-config")
						}
						commandArgs := append([]string{}, args...)
						if args[1] == "export" {
							commandArgs[2] = "--dir=" + filepath.Join(t.TempDir(), "skills")
						}
						root.SetArgs(append(commandArgs, "--output="+mode, "--color=never", "--workspace=not-configured"))
						err := root.ExecuteContext(t.Context())
						if runtime.CancelTimeout != nil {
							runtime.CancelTimeout()
						}
						var failure CommandError
						if !errors.As(err, &failure) || failure.CLIError.ExitCode != 4 {
							t.Fatalf("agent validation exit differs: %v", err)
						}
						if lookups != 0 || strings.Contains(out.String()+diagnostic.String(), "synthetic-agent-failure-") {
							t.Fatal("agent error handling resolved credentials or disclosed private context")
						}
						if mode == "human" {
							if out.Len() != 0 || diagnostic.Len() == 0 {
								t.Fatal("explicit human agent failure used wrong streams")
							}
							return
						}
						var frame struct {
							OK   bool
							Data json.RawMessage
							Meta struct {
								Command, Workspace string
								Code               int `json:"exit_code"`
							}
						}
						if diagnostic.Len() != 0 || json.Unmarshal(out.Bytes(), &frame) != nil || frame.OK || frame.Meta.Workspace != "agent" || frame.Meta.Command != "agent."+args[1] || frame.Meta.Code != 4 || string(frame.Data) != "null" {
							t.Fatal("agent failure lost forced machine mode or agent workspace label")
						}
					})
				}
			}
		}
	}
}
