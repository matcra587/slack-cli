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
	"time"

	"github.com/matcra587/slack-cli/internal/agent"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	"github.com/matcra587/slack-cli/internal/config"
)

func TestGlobalOutputAndWorkspaceSelectorsBeforeAndAfterCommand(t *testing.T) {
	t.Setenv("SLICK_CONFIG", filepath.Join(t.TempDir(), "unused.toml"))
	for _, key := range agent.KnownEnvVars() {
		t.Setenv(key, "")
	}
	for _, tc := range []struct {
		name, mode, workspace string
		flags                 []string
		tty, prefix           bool
		code                  int
	}{
		{name: "default pipe", mode: "json", workspace: "Primary"},
		{name: "default terminal", mode: "human", workspace: "Primary", tty: true},
		{name: "explicit auto and canonical workspace", mode: "json", workspace: "Other", flags: []string{"--output=auto", "--workspace", " oThEr "}, prefix: true},
		{name: "short selectors", mode: "json", workspace: "Other", flags: []string{"-o", "json", "-w", "OTHER"}},
		{name: "long compact before command", mode: "compact", workspace: "Other", flags: []string{"--output=compact", "--workspace=Other"}, prefix: true},
		{name: "explicit human in pipe", mode: "human", workspace: "Primary", flags: []string{"-o", "human", "-w", "Primary", "-V", "never"}},
		{name: "empty output restores auto", mode: "json", workspace: "Primary", flags: []string{"-o", ""}},
		{name: "invalid output", mode: "json", workspace: "Primary", flags: []string{"--output=invalid"}, code: 4},
		{name: "remove selectors after error", mode: "json", workspace: "Primary"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{SchemaVersion: config.SchemaVersion, DefaultWorkspace: "Primary", Workspaces: map[string]config.WorkspaceProfile{
				"Primary": {Name: "Primary", TeamID: "T_PRIMARY", TokenType: config.TokenTypeBot, TokenRef: "env:SYNTHETIC_PRIMARY"},
				"Other":   {Name: "Other", TeamID: "T_OTHER", TokenType: config.TokenTypeUser, TokenRef: "env:SYNTHETIC_OTHER"},
			}}
			before, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			var out, diagnostic bytes.Buffer
			var reads atomic.Int32
			root := NewRootCommand(WithConfig(cfg), WithTTY(tc.tty), WithIO(strings.NewReader(""), &out, &diagnostic), WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
				reads.Add(1)
				return "", errors.New("synthetic-private-global-provider")
			})))
			args := []string{"workspace", "list"}
			if tc.prefix {
				args = append(append([]string{}, tc.flags...), args...)
			} else {
				args = append(args, tc.flags...)
			}
			root.SetArgs(args)
			err = root.ExecuteContext(t.Context())
			if tc.code == 0 && err != nil {
				t.Fatalf("valid global selectors failed: %v", err)
			}
			if tc.code != 0 {
				failure, ok := errors.AsType[clioutput.CommandError](err)
				if !ok || failure.CLIError.ExitCode != tc.code {
					t.Fatal("invalid output lost validation class")
				}
			}
			if reads.Load() != 0 || strings.Contains(out.String()+diagnostic.String(), "synthetic-private-global-provider") {
				t.Fatal("read-only global selection accessed or exposed credentials")
			}
			if tc.mode == "human" {
				if diagnostic.Len() != 0 || json.Valid(out.Bytes()) || !strings.Contains(out.String(), "Primary") || !strings.Contains(out.String(), "Other") {
					t.Fatal("human selection lost workspace rows or streams")
				}
			} else {
				var data map[string]any
				if diagnostic.Len() != 0 || json.Unmarshal(out.Bytes(), &data) != nil || strings.Contains(out.String(), "\x1b") {
					t.Fatal("global selection corrupted machine output")
				}
				if tc.mode == "json" {
					meta := data["meta"].(map[string]any)
					if data["ok"] != (tc.code == 0) || meta["command"] != "workspace.list" || meta["workspace"] != tc.workspace || meta["exit_code"] != float64(tc.code) {
						t.Fatal("global selectors lost canonical metadata")
					}
					if tc.code == 0 {
						data = data["data"].(map[string]any)
					} else if data["data"] != nil || data["errors"].([]any)[0].(map[string]any)["type"] != "validation_error" {
						t.Fatal("invalid output lost common failure envelope")
					}
				}
				if tc.code == 0 {
					rows, ok := data["workspaces"].([]any)
					if !ok || len(rows) != 2 {
						t.Fatal("global output selector lost workspace data")
					}
				}
			}
			after, err := json.Marshal(cfg)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("global selection changed prior config")
			}
		})
	}
}

func TestGlobalTimeoutPreservesCallerDeadlineAndFlagRemoval(t *testing.T) {
	t.Setenv("SLICK_CONFIG", filepath.Join(t.TempDir(), "unused.toml"))
	for _, tc := range []struct {
		name          string
		flags         []string
		cliTimeout    time.Duration
		inheritCaller bool
		shortCaller   bool
	}{
		{name: "default", cliTimeout: 30 * time.Second},
		{name: "long", flags: []string{"--timeout=2s"}, cliTimeout: 2 * time.Second},
		{name: "short", flags: []string{"-I", "2s"}, cliTimeout: 2 * time.Second},
		{name: "zero", flags: []string{"--timeout=0"}, inheritCaller: true},
		{name: "short zero", flags: []string{"-I=0"}, inheritCaller: true},
		{name: "negative", flags: []string{"--timeout=-1s"}, inheritCaller: true},
		{name: "caller earlier", flags: []string{"--timeout=2s"}, inheritCaller: true, shortCaller: true},
		{name: "remove override", cliTimeout: 30 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parentDeadline := time.Now().Add(2 * time.Minute)
			if tc.shortCaller {
				parentDeadline = time.Now().Add(time.Second)
			}
			parent, cancel := context.WithDeadline(t.Context(), parentDeadline)
			defer cancel()
			var out, diagnostic bytes.Buffer
			var reads atomic.Int32
			root, runtime := NewRootCommandWithRuntime(WithConfig(nil), WithTTY(false), WithIO(strings.NewReader(""), &out, &diagnostic), WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
				reads.Add(1)
				return "", errors.New("synthetic-private-deadline-provider")
			})))
			root.SetArgs(append([]string{"config", "path", "--output=json"}, tc.flags...))
			start := time.Now()
			command, err := root.ExecuteContextC(parent)
			end := time.Now()
			if runtime.CancelTimeout != nil {
				defer runtime.CancelTimeout()
			}
			if err != nil || diagnostic.Len() != 0 || reads.Load() != 0 {
				t.Fatalf("deadline flags failed or accessed credentials: %v", err)
			}
			deadline, ok := command.Context().Deadline()
			if !ok {
				t.Fatal("command lost its effective deadline")
			}
			if tc.inheritCaller {
				if !deadline.Equal(parentDeadline) {
					t.Fatal("nonpositive timeout or earlier caller deadline was replaced")
				}
			} else if deadline.Before(start.Add(tc.cliTimeout)) || deadline.After(end.Add(tc.cliTimeout)) {
				t.Fatal("timeout flag or removal selected the wrong deadline")
			}
			var frame map[string]any
			if json.Unmarshal(out.Bytes(), &frame) != nil || frame["ok"] != true || frame["meta"].(map[string]any)["command"] != "config.path" {
				t.Fatal("deadline flags corrupted ordinary command output")
			}
		})
	}
}
