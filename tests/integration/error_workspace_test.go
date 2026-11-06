//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestBinaryEarlyErrorsPreserveWorkspaceWithoutCredentialReads(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tc := range []struct {
		name, workspace, command, config string
		args                             []string
	}{
		{"configured default", "Primary", "cache.users", "valid", []string{"cache", "users", "--page-size=bad"}},
		{"long selector", "Secondary", "cache.users", "valid", []string{"--workspace=secondary", "cache", "users", "--page-size=bad"}},
		{"selector after parse failure", "Secondary", "cache.users", "valid", []string{"cache", "users", "--page-size=bad", "--workspace=SECONDARY"}},
		{"short separate", "Secondary", "cache.users", "valid", []string{"cache", "users", "-w", " secondary ", "--page-size=bad"}},
		{"short attached", "Secondary", "cache.users", "valid", []string{"cache", "users", "-wSecondary", "--page-size=bad"}},
		{"short empty value", "Primary", "cache.users", "valid", []string{"cache", "users", "-w=", "--page-size=bad"}},
		{"short cluster", "Secondary", "cache.users", "valid", []string{"cache", "users", "-QwSecondary", "--page-size=bad"}},
		{"last selector wins", "Secondary", "cache.users", "valid", []string{"--workspace=Primary", "cache", "users", "--page-size=bad", "--workspace=Secondary"}},
		{"missing later value", "Secondary", "cache.users", "valid", []string{"cache", "users", "--workspace=Secondary", "--workspace"}},
		{"blank means default", "Primary", "cache.users", "valid", []string{"cache", "users", "--workspace=  ", "--page-size=bad"}},
		{"long text is not a selector", "Primary", "message.send", "valid", []string{"message", "send", "--message", "--workspace=Other", "--color=bad"}},
		{"short text is not a selector", "Primary", "message.send", "valid", []string{"message", "send", "--message", "-wOther", "--color=bad"}},
		{"literal terminator", "Primary", "cache.users", "valid", []string{"cache", "users", "--", "--workspace=Other"}},
		{"unknown requested profile", "Unknown", "cache.users", "valid", []string{"cache", "users", "--workspace=Unknown"}},
		{"missing config selected target", "Requested", "cache.users", "missing", []string{"cache", "users", "--workspace=Requested"}},
		{"malformed config selected target", "Requested", "cache.users", "malformed", []string{"cache", "users", "--workspace=Requested"}},
	} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				home := t.TempDir()
				configPath := filepath.Join(home, "config.toml")
				prior := []byte("schema_version=\"1\"\ndefault_workspace=\"Primary\"\n[workspaces.Primary]\nname=\"Primary\"\nteam_id=\"T1\"\ntoken_type=\"user\"\ntoken_ref=\"op://synthetic/item/token\"\n[workspaces.Secondary]\nname=\"Secondary\"\nteam_id=\"T2\"\ntoken_type=\"bot\"\ntoken_ref=\"op://synthetic/other/token\"\n")
				if tc.config == "malformed" {
					prior = []byte("schema_version = [ broken")
				}
				if tc.config != "missing" {
					if err := os.WriteFile(configPath, prior, 0o600); err != nil {
						t.Fatal(err)
					}
				}
				bin := filepath.Join(home, "bin")
				if err := os.Mkdir(bin, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(bin, "op"), []byte("#!/bin/sh\nprintf called >> \"$SLICK_PROVIDER_TRACE\"\nexit 91\n"), 0o700); err != nil {
					t.Fatal(err)
				}
				trace := filepath.Join(home, "provider-called")
				var requests atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests.Add(1)
					writeJSON(w, `{"ok":false,"error":"no_permission"}`)
				}))
				defer server.Close()
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				args := append([]string{"--output=" + mode, "--color=never"}, tc.args...)
				cmd := exec.CommandContext(ctx, binary, args...)
				cmd.Env = []string{"HOME=" + home, "USERPROFILE=" + home, "XDG_CACHE_HOME=" + filepath.Join(home, "cache"), "SLICK_CONFIG=" + configPath, "SLACK_CLI_BASE_URL=" + server.URL, "PATH=" + bin, "SLICK_PROVIDER_TRACE=" + trace}
				var out, diagnostic bytes.Buffer
				cmd.Stdout, cmd.Stderr = &out, &diagnostic
				err := cmd.Run()
				if ctx.Err() != nil || exitCode(err) != 4 {
					t.Fatalf("early workspace failure changed exit: %v", err)
				}
				if mode == "human" {
					if out.Len() != 0 || diagnostic.Len() == 0 {
						t.Fatal("human early error used wrong stream")
					}
				} else {
					var frame map[string]json.RawMessage
					if err := json.Unmarshal(out.Bytes(), &frame); err != nil || len(frame) != 4 || string(frame["ok"]) != "false" || string(frame["data"]) != "null" || diagnostic.Len() != 0 {
						t.Fatal("early workspace error lost common envelope")
					}
					var meta struct {
						Workspace, Command string
						Exit               int `json:"exit_code"`
					}
					if err := json.Unmarshal(frame["meta"], &meta); err != nil || meta.Workspace != tc.workspace || meta.Command != tc.command || meta.Exit != 4 {
						t.Fatalf("early error workspace=%q command=%q exit=%d want workspace=%q command=%q exit=4", meta.Workspace, meta.Command, meta.Exit, tc.workspace, tc.command)
					}
				}
				if requests.Load() != 0 {
					t.Fatal("formatting early errors called Slack")
				}
				if _, err := os.Stat(trace); !os.IsNotExist(err) {
					t.Fatal("formatting early errors read credentials")
				}
				if tc.config != "missing" {
					after, err := os.ReadFile(configPath)
					if err != nil || !bytes.Equal(prior, after) {
						t.Fatal("early failure changed prior config")
					}
				} else if _, err := os.Stat(configPath); !os.IsNotExist(err) {
					t.Fatal("early failure created missing config")
				}
				if _, err := os.Stat(filepath.Join(home, "cache")); !os.IsNotExist(err) {
					t.Fatal("early error touched cache state")
				}
			})
		}
	}
}
