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
	"sync/atomic"
	"testing"
	"time"
)

func TestBinaryWorkspaceListStableAndCredentialFree(t *testing.T) {
	binary := buildSlackBinary(t)
	names := []string{"Zulu", "alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel", "india", "juliet", "kilo", "lima", "mike", "november", "oscar"}
	expected := slices.Clone(names)
	slices.Sort(expected)
	for _, mode := range []string{"json", "compact", "human"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "config.toml")
			var config strings.Builder
			config.WriteString("schema_version=\"1\"\ndefault_workspace=\"alpha\"\n")
			for _, name := range names {
				fmt.Fprintf(&config, "[workspaces.%s]\nname=%q\nteam_id=\"T_SYNTHETIC\"\ntoken_type=\"bot\"\ntoken_ref=\"op://synthetic/workspace/token\"\ndefault_channel=\"C_FIXTURE\"\n", name, name)
			}
			prior := config.String()
			if err := os.WriteFile(path, []byte(prior), 0o600); err != nil {
				t.Fatal(err)
			}
			bin := filepath.Join(root, "bin")
			if err := os.Mkdir(bin, 0o700); err != nil {
				t.Fatal(err)
			}
			trace := filepath.Join(root, "provider-called")
			if err := os.WriteFile(filepath.Join(bin, "op"), []byte("#!/bin/sh\nprintf called >> \"$SLICK_PROVIDER_TRACE\"\nprintf xoxb-synthetic-forbidden\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); w.WriteHeader(500) }))
			defer server.Close()
			for _, selector := range []string{"", "--workspace=alpha", "-w=Zulu"} {
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				args := []string{"workspace", "list", "--output=" + mode, "--color=never"}
				if selector != "" {
					args = append(args, selector)
				}
				command := exec.CommandContext(ctx, binary, args...)
				command.Dir = root
				command.Env = append(isolatedRuntimeEnv(t), "SLICK_CONFIG="+path, "SLACK_CLI_BASE_URL="+server.URL, "PATH="+bin, "SLICK_PROVIDER_TRACE="+trace)
				var out, diagnostic strings.Builder
				command.Stdout = &out
				command.Stderr = &diagnostic
				err := command.Run()
				cancel()
				if err != nil || diagnostic.Len() != 0 {
					t.Fatalf("workspace list failed: %v %s", err, diagnostic.String())
				}
				if strings.Contains(out.String(), "xoxb-synthetic-forbidden") {
					t.Fatal("credential disclosed")
				}
				var got []string
				if mode == "human" {
					lines := strings.Split(strings.TrimSpace(out.String()), "\n")
					for _, line := range lines[1:] {
						fields := strings.Fields(line)
						if len(fields) > 0 {
							got = append(got, fields[0])
						}
					}
				} else {
					raw := []byte(out.String())
					if mode == "json" {
						var frame struct {
							OK   bool
							Data json.RawMessage
							Meta struct{ Command string }
						}
						if json.Unmarshal(raw, &frame) != nil || !frame.OK || frame.Meta.Command != "workspace.list" {
							t.Fatal("invalid list envelope")
						}
						raw = frame.Data
					}
					var data struct {
						Workspaces []struct {
							Name           string
							TokenRef       string `json:"token_ref"`
							DefaultChannel string `json:"default_channel"`
						}
					}
					if json.Unmarshal(raw, &data) != nil {
						t.Fatal("invalid list payload")
					}
					for _, profile := range data.Workspaces {
						got = append(got, profile.Name)
						if profile.TokenRef != "op://synthetic/workspace/token" || profile.DefaultChannel != "C_FIXTURE" {
							t.Fatal("reviewed profile fields lost")
						}
					}
				}
				if !slices.Equal(got, expected) {
					t.Fatalf("workspace rows are not in stable name order: got %q want %q", got, expected)
				}
				if requests.Load() != 0 {
					t.Fatal("local listing accessed Slack")
				}
				if _, err := os.Stat(trace); !os.IsNotExist(err) {
					t.Fatal("local listing resolved credentials")
				}
				after, err := os.ReadFile(path)
				if err != nil || string(after) != prior {
					t.Fatal("local listing changed configuration")
				}
			}
		})
	}
}
