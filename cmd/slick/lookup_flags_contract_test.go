package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	"github.com/matcra587/slack-cli/internal/config"
)

func TestLookupUnsupportedFlagCombinationsFailBeforeCredentials(t *testing.T) {
	t.Setenv("SLICK_CONFIG", filepath.Join(t.TempDir(), "unused.toml"))
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, tc := range []struct {
		name string
		args []string
		flag string
	}{
		{"channel maximum", []string{"channel", "--channel=C1", "--max-items=2"}, "--max-items"},
		{"channel explicit zero", []string{"channel", "--channel=C1", "--max-items=0"}, "--max-items"},
		{"channel cursor", []string{"channel", "-c", "C1", "-C", ""}, "--cursor"},
		{"channel filter", []string{"channel", "--channel=C1", "--filter="}, "--filter"},
		{"channel types", []string{"channel", "-c", "C1", "-t", "public_channel,private_channel"}, "--types"},
		{"channel negative", []string{"channel", "-M", "-1"}, "--max-items"},
		{"channel upper limit", []string{"channel", "--max-items=1000"}, "--max-items"},
		{"channel huge limit", []string{"channel", "-M", "2147483647"}, "--max-items"},
		{"channel unknown type", []string{"channel", "--types=typo"}, "--types"},
		{"channel unknown after all", []string{"channel", "-t", "all,typo"}, "--types"},
		{"channel unknown before all", []string{"channel", "--types=typo,all"}, "--types"},
		{"channel unknown between types", []string{"channel", "-t", "im,typo,mpim"}, "--types"},
		{"channel private type diagnostic", []string{"channel", "--types=all,xoxp-synthetic-unreviewed-value"}, "--types"},
		{"channel blank stored alias", []string{"channel", "--channel=blank-alias"}, "channel is required"},
		{"user maximum", []string{"user", "--user=U1", "--max-items=2"}, "--max-items"},
		{"user explicit zero", []string{"user", "--user=U1", "--max-items=0"}, "--max-items"},
		{"user cursor", []string{"user", "-u", "U1", "-C", ""}, "--cursor"},
		{"user filter", []string{"user", "--user=U1", "--filter="}, "--filter"},
		{"user deleted", []string{"user", "--user=U1", "--include-deleted=false"}, "--include-deleted"},
		{"user negative", []string{"user", "-M", "-1"}, "--max-items"},
		{"user blank stored alias", []string{"user", "--user=blank-alias", "--presence"}, "user is required"},
	} {
		for _, mode := range []string{"json", "compact", "human"} {
			// Every argument combination retains its precredential/state checks.
			// Other modes keep private-type redaction and user-target validation.
			if mode != "json" && tc.name != "channel private type diagnostic" && tc.name != "user blank stored alias" {
				continue
			}
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				cfg := workspaceConfig(config.TokenTypeUser)
				profile := cfg.Workspaces[cfg.DefaultWorkspace]
				if profile.Aliases == nil {
					profile.Aliases = map[string]string{}
				}
				profile.Aliases["blank-alias"] = " \t "
				cfg.Workspaces[cfg.DefaultWorkspace] = profile
				before, err := json.Marshal(cfg)
				if err != nil {
					t.Fatal(err)
				}
				var out, diagnostic bytes.Buffer
				var reads atomic.Int32
				root := NewRootCommand(WithConfig(cfg), WithIO(strings.NewReader(""), &out, &diagnostic), WithTTY(false), WithSlackBaseURL("http://127.0.0.1:1"), WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
					reads.Add(1)
					return "", errors.New("synthetic-private-lookup-provider")
				})))
				root.SetArgs(append(append([]string{"lookup"}, tc.args...), "--output="+mode, "--color=never"))
				err = root.Execute()
				e, ok := errors.AsType[clioutput.CommandError](err)
				if !ok || e.CLIError.ExitCode != 4 || reads.Load() != 0 {
					t.Fatalf("invalid lookup reached credentials or changed exit: %v reads=%d", err, reads.Load())
				}
				combined := out.String() + diagnostic.String()
				if !strings.Contains(combined, tc.flag) || strings.Contains(combined, "synthetic-private") || strings.Contains(combined, "xoxp-synthetic-unreviewed-value") {
					t.Fatal("lookup diagnostic lost flag or exposed provider details")
				}
				if mode == "human" {
					if out.Len() != 0 || diagnostic.Len() == 0 {
						t.Fatal("human validation used wrong stream")
					}
				} else {
					var frame map[string]json.RawMessage
					if err := json.Unmarshal(out.Bytes(), &frame); err != nil || len(frame) != 4 || string(frame["ok"]) != "false" || string(frame["data"]) != "null" || diagnostic.Len() != 0 {
						t.Fatal("lookup validation lost common envelope")
					}
				}
				after, err := json.Marshal(cfg)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatal("invalid lookup changed prior workspace state")
				}
			})
		}
	}
}
