//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Keep tree-wide executable reachability here. Mode, flag spelling and writer
// failure combinations belong to TestHelpWriteFailuresReturnIOWithoutReplay.
func TestBinaryHelpTreeReachability(t *testing.T) {
	binary := buildSlackBinary(t)
	type node struct {
		Path     string            `json:"path"`
		Children []json.RawMessage `json:"children"`
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "cmd", "slick", "testdata", "agent-schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var paths [][]string
	var walk func(json.RawMessage)
	walk = func(raw json.RawMessage) {
		var n node
		if err := json.Unmarshal(raw, &n); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, strings.Fields(strings.TrimPrefix(n.Path, "slick")))
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(data)
	slots := make(chan struct{}, 4)
	for _, path := range paths {
		name := strings.Join(path, " ")
		if name == "" {
			name = "root"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			slots <- struct{}{}
			defer func() { <-slots }()
			config := writePipeConfig(t)
			before, err := os.ReadFile(config)
			if err != nil {
				t.Fatal(err)
			}
			args := append([]string{"--color=never", "--output=json"}, path...)
			args = append(args, "--help")
			out, diagnostic, err := runSlackBinary(t, binary, config, "http://127.0.0.1:1", "", args...)
			if err != nil || diagnostic != "" || !strings.Contains(out, "Usage") || strings.HasPrefix(out, "{") {
				t.Fatalf("executable command did not return raw help: %v", err)
			}
			if name == "manifest template" && !strings.Contains(out, "Messaging Template User Scopes") {
				t.Fatal("custom manifest help lost its scope sections")
			}
			after, err := os.ReadFile(config)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("help changed config state")
			}
		})
	}
}

// These rows retain real OS write failures across the owned help entry points.
// The root tests retain every output mode and both help flag bindings.
func TestBinaryHelpWriteFailuresAndRecovery(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("/dev/full fixture requires Linux")
	}
	binary := buildSlackBinary(t)
	for _, tc := range []struct {
		name, mode string
		args       []string
		custom     bool
	}{
		{"root short human", "human", []string{"-h"}, false},
		{"leaf long json", "json", []string{"history", "list", "--help"}, false},
		{"custom manifest compact", "compact", []string{"manifest", "template", "--help"}, true},
		{"completion group short json", "json", []string{"completion", "-h"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := writePipeConfig(t)
			before, err := os.ReadFile(config)
			if err != nil {
				t.Fatal(err)
			}
			args := append([]string{"--color=never", "--output=" + tc.mode}, tc.args...)
			run := func(full bool) (string, string, error) {
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, binary, args...)
				cmd.Env = append(isolatedRuntimeEnv(t), "SLICK_CONFIG="+config)
				var out, diagnostic bytes.Buffer
				cmd.Stdout, cmd.Stderr = &out, &diagnostic
				if full {
					dst, err := os.OpenFile("/dev/full", os.O_WRONLY, 0)
					if err != nil {
						t.Fatal(err)
					}
					defer func() {
						if err := dst.Close(); err != nil {
							t.Error(err)
						}
					}()
					cmd.Stdout = dst
				}
				err := cmd.Run()
				if ctx.Err() != nil {
					t.Fatal("help fixture timed out")
				}
				return out.String(), diagnostic.String(), err
			}
			_, diagnostic, err := run(true)
			if exitCode(err) != 8 || strings.Count(diagnostic, "output_write_failed") != 1 || !strings.Contains(diagnostic, "exit_code=8") {
				t.Fatalf("help destination failure lost IO8: %v; %s", err, diagnostic)
			}
			healthy, diagnostic, err := run(false)
			if err != nil || diagnostic != "" || !strings.Contains(healthy, "Usage") || strings.HasPrefix(healthy, "{") {
				t.Fatalf("healthy invocation did not return help: %v", err)
			}
			if tc.custom && !strings.Contains(healthy, "Messaging Template User Scopes") {
				t.Fatal("custom manifest help lost its scope sections")
			}
			after, err := os.ReadFile(config)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("help changed config state")
			}
		})
	}
}
