package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	slackcache "github.com/matcra587/slack-cli/internal/cache"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	"github.com/matcra587/slack-cli/internal/config"
)

func TestCacheTTLOverflowFailsBeforeReadingCredentialsOrChangingState(t *testing.T) {
	t.Setenv("SLICK_CONFIG", filepath.Join(t.TempDir(), "unused.toml"))
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, resource := range []string{"users", "channels"} {
		for _, value := range []string{"153722868", "2147483647"} {
			for _, flag := range []string{"--ttl-minutes", "-T"} {
				for _, mode := range []string{"json", "compact", "human"} {
					// Both resources, overflow values and spellings retain JSON guards.
					// Each resource's first overflowing TTL covers other failure modes.
					if mode != "json" && (value != "153722868" || flag != "--ttl-minutes") {
						continue
					}
					t.Run(resource+"/"+value+"/"+flag+"/"+mode, func(t *testing.T) {
						cfg := workspaceConfig(config.TokenTypeUser)
						beforeConfig, err := json.Marshal(cfg)
						if err != nil {
							t.Fatal(err)
						}
						if _, err := slackcache.Write("default", resource, json.RawMessage(`{"`+resource+`":[]}`)); err != nil {
							t.Fatal(err)
						}
						path, err := slackcache.Path("default", resource)
						if err != nil {
							t.Fatal(err)
						}
						before, err := os.ReadFile(path)
						if err != nil {
							t.Fatal(err)
						}
						var out, diagnostic bytes.Buffer
						var reads atomic.Int32
						root := NewRootCommand(WithConfig(cfg), WithIO(strings.NewReader(""), &out, &diagnostic), WithTTY(false), WithSlackBaseURL("http://127.0.0.1:1"), WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
							reads.Add(1)
							return "", errors.New("synthetic-private-cache-provider")
						})))
						root.SetArgs([]string{"cache", resource, flag, value, "--output=" + mode, "--color=never"})
						err = root.Execute()
						e, ok := errors.AsType[clioutput.CommandError](err)
						if !ok || e.CLIError.ExitCode != 4 || reads.Load() != 0 {
							t.Fatalf("invalid TTL was accepted or reached credentials: %v reads=%d", err, reads.Load())
						}
						combined := out.String() + diagnostic.String()
						if !strings.Contains(combined, "--ttl-minutes") || strings.Contains(combined, "synthetic-private") {
							t.Fatal("TTL validation lost its flag or disclosed provider details")
						}
						if mode == "human" {
							if out.Len() != 0 || diagnostic.Len() == 0 {
								t.Fatal("TTL validation used the wrong stream")
							}
						} else {
							var frame map[string]json.RawMessage
							if err := json.Unmarshal(out.Bytes(), &frame); err != nil || len(frame) != 4 || string(frame["ok"]) != "false" || string(frame["data"]) != "null" || diagnostic.Len() != 0 {
								t.Fatal("TTL validation lost the common envelope")
							}
						}
						after, err := os.ReadFile(path)
						if err != nil || !bytes.Equal(before, after) {
							t.Fatal("invalid TTL changed the prior cache")
						}
						afterConfig, err := json.Marshal(cfg)
						if err != nil || !bytes.Equal(beforeConfig, afterConfig) {
							t.Fatal("invalid TTL changed workspace settings")
						}
					})
				}
			}
		}
	}
}
