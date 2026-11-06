//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBinaryParseFailuresNameTheSelectedCanonicalCommand(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tc := range []struct {
		name, command string
		args          []string
	}{
		{"cache users", "cache.users", []string{"cache", "users", "--page-size"}},
		{"cache channels", "cache.channels", []string{"cache", "channels", "--max-pages"}},
		{"message send", "message.send", []string{"message", "send", "--text"}},
		{"history list", "history.list", []string{"history", "list", "--max-items"}},
		{"reaction add", "react.add", []string{"react", "add", "--emoji"}},
		{"lookup user", "lookup.user", []string{"lookup", "user", "--include-deleted=not-a-boolean"}},
		{"config list alias", "config.list", []string{"config", "ls", "--workspace"}},
		{"config rm alias", "config.unset", []string{"config", "rm", "aliases.release", "--workspace"}},
		{"config remove alias", "config.unset", []string{"config", "remove", "aliases.release", "--workspace"}},
		{"manifest alias", "manifest.template", []string{"manifest", "generate", "--name"}},
		{"auth status positional argument", "auth.status", []string{"auth", "status", "stray"}},
		{"cache positional argument", "cache.users", []string{"cache", "users", "stray"}},
		{"history positional argument", "history.list", []string{"history", "list", "stray"}},
		{"root flag", "slick", []string{"--workspace"}},
		{"invalid long color", "workspace.list", []string{"workspace", "list", "--color=invalid"}},
		{"invalid short color", "workspace.list", []string{"workspace", "list", "-V=invalid"}},
	} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				home := t.TempDir()
				configPath := filepath.Join(home, "config.toml")
				prior := []byte("schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\n[workspaces.fixture.aliases]\nrelease=\"CPRESERVE\"\n")
				if err := os.WriteFile(configPath, prior, 0o600); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				args := append([]string{"--output=" + mode, "--color=never"}, tc.args...)
				cmd := exec.CommandContext(ctx, binary, args...)
				cmd.Env = []string{"HOME=" + home, "XDG_CACHE_HOME=" + filepath.Join(home, "cache"), "SLICK_CONFIG=" + configPath, "SLACK_CLI_BASE_URL=http://127.0.0.1:1"}
				var out, diagnostic bytes.Buffer
				cmd.Stdout, cmd.Stderr = &out, &diagnostic
				err := cmd.Run()
				if ctx.Err() != nil || exitCode(err) != 4 {
					t.Fatalf("parse failure changed its exit: %v", err)
				}
				fragment, _, _ := strings.Cut(tc.args[len(tc.args)-1], "=")
				if !strings.Contains(out.String()+diagnostic.String(), fragment) {
					t.Fatal("parse failure did not identify the invalid argument")
				}
				if mode == "human" {
					if out.Len() != 0 || diagnostic.Len() == 0 {
						t.Fatal("human parse failure used the wrong stream")
					}
				} else {
					var frame map[string]json.RawMessage
					if err := json.Unmarshal(out.Bytes(), &frame); err != nil || len(frame) != 4 || string(frame["ok"]) != "false" || string(frame["data"]) != "null" || diagnostic.Len() != 0 {
						t.Fatal("parse failure lost the common envelope")
					}
					var meta struct {
						Command string
						Exit    int `json:"exit_code"`
					}
					if err := json.Unmarshal(frame["meta"], &meta); err != nil || meta.Command != tc.command || meta.Exit != 4 {
						t.Fatalf("parse failure command=%q exit=%d want command=%q exit=4", meta.Command, meta.Exit, tc.command)
					}
					var failures []struct {
						Type string
						Exit int `json:"exit_code"`
					}
					if err := json.Unmarshal(frame["errors"], &failures); err != nil || len(failures) != 1 || failures[0].Type != "validation_error" || failures[0].Exit != 4 {
						t.Fatal("parse failure changed error classification")
					}
				}
				after, err := os.ReadFile(configPath)
				if err != nil || !bytes.Equal(prior, after) {
					t.Fatal("parse failure changed prior configuration")
				}
				if _, err := os.Stat(filepath.Join(home, "cache")); !os.IsNotExist(err) {
					t.Fatal("parse failure created or touched cache state")
				}
			})
		}
	}
}
