//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Exercise slick's destination selection and flag adapter. The contents of the
// generated skills and Docent's filesystem containment have their own tests.
func TestBinaryAgentExportFlagsSelectDestinationAndFormat(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, tc := range []struct {
		name, destination, format string
		args, env                 []string
		code                      int
	}{
		{name: "explicit long", args: []string{"--dir=exports", "--format=agent-skill"}, destination: "exports", format: "agent-skill"},
		{name: "explicit short", args: []string{"-d", "exports", "-f", "claude-skill"}, destination: "exports", format: "claude-skill"},
		{name: "project codex long", args: []string{"--scope=project", "--harness=codex"}, destination: ".agents/skills", format: "agent-skill"},
		{name: "user codex short", args: []string{"-s", "user", "-H", "codex"}, destination: "home/.agents/skills", format: "agent-skill"},
		{name: "project claude short", args: []string{"-s", "project", "-H", "claude-code"}, destination: ".claude/skills", format: "claude-skill"},
		{name: "user claude long", args: []string{"--scope=user", "--harness=claude-code"}, destination: "home/.claude/skills", format: "claude-skill"},
		{name: "detect codex", args: []string{"--scope=project"}, env: []string{"CODEX_THREAD_ID=synthetic-thread"}, destination: ".agents/skills", format: "agent-skill"},
		{name: "detect claude", args: []string{"-s", "user"}, env: []string{"CLAUDECODE=1"}, destination: "home/.claude/skills", format: "claude-skill"},
		{name: "explicit harness overrides detection", args: []string{"--scope=project", "--harness=codex"}, env: []string{"CLAUDECODE=1"}, destination: ".agents/skills", format: "agent-skill"},
		{name: "explicit format overrides codex", args: []string{"-s", "project", "-H", "codex", "-f", "claude-skill"}, destination: ".agents/skills", format: "claude-skill"},
		{name: "explicit format overrides claude", args: []string{"--scope=user", "--harness=claude-code", "--format=agent-skill"}, destination: "home/.claude/skills", format: "agent-skill"},
		{name: "removed format uses default", args: []string{"--scope=project", "--harness=codex", "--format="}, destination: ".agents/skills", format: "agent-skill"},
		{name: "removed harness uses detection", args: []string{"--scope=project", "--harness="}, env: []string{"CLAUDECODE=1"}, destination: ".claude/skills", format: "claude-skill"},
		{name: "explicit directory ignores detection", args: []string{"-d", "exports", "-f", "agent-skill"}, env: []string{"CLAUDECODE=1"}, destination: "exports", format: "agent-skill"},
		{name: "missing destination", code: 4},
		{name: "missing format long", args: []string{"--dir=exports"}, code: 4},
		{name: "missing format short", args: []string{"-d", "exports"}, code: 4},
		{name: "missing detection", args: []string{"--scope=project"}, code: 4},
		{name: "unsupported detected agent", args: []string{"-s", "user"}, env: []string{"AI_AGENT=gemini-cli"}, code: 4},
		{name: "unsupported harness", args: []string{"-s", "project", "-H", "unknown"}, code: 4},
		{name: "unsupported scope", args: []string{"-s", "unknown", "-H", "codex"}, code: 4},
		{name: "harness without scope", args: []string{"-H", "codex"}, code: 4},
		{name: "directory and scope", args: []string{"-d", "exports", "-f", "agent-skill", "-s", "project", "-H", "codex"}, code: 4},
		{name: "unsupported scoped format", args: []string{"-s", "project", "-H", "codex", "-f", "unknown"}, code: 4},
		{name: "removed destination", args: []string{"--dir=", "--format=agent-skill"}, code: 4},
		{name: "removed scope", args: []string{"--scope=", "--harness=codex"}, code: 4},
		{name: "extra positional", args: []string{"-d", "exports", "-f", "agent-skill", "unexpected"}, code: 4},
		{name: "missing short value", args: []string{"-d"}, code: 4},
	} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				root := t.TempDir()
				home := filepath.Join(root, "home")
				if err := os.Mkdir(home, 0o700); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(root, "config.toml")
				const private = "synthetic-private-export-config"
				prior := []byte("[invalid " + private)
				if err := os.WriteFile(path, prior, 0o600); err != nil {
					t.Fatal(err)
				}
				var requests atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					requests.Add(1)
					w.WriteHeader(http.StatusInternalServerError)
				}))
				defer server.Close()
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				args := append([]string{"agent", "export", "--color=never", "--output=" + mode, "--workspace=does-not-exist"}, tc.args...)
				command := exec.CommandContext(ctx, binary, args...)
				command.Dir = root
				command.Env = append(isolatedRuntimeEnv(t), "HOME="+home, "USERPROFILE="+home, "SLICK_CONFIG="+path, "SLACK_CLI_CONFIG="+path, "SLACK_CLI_BASE_URL="+server.URL)
				command.Env = append(command.Env, tc.env...)
				var out, diagnostic strings.Builder
				command.Stdout = &out
				command.Stderr = &diagnostic
				err := command.Run()
				if ctx.Err() != nil || exitCode(err) != tc.code {
					t.Fatalf("export selection exit=%d want=%d: %s %s", exitCode(err), tc.code, out.String(), diagnostic.String())
				}
				if requests.Load() != 0 || strings.Contains(out.String()+diagnostic.String(), private) {
					t.Fatal("export accessed Slack or disclosed configuration")
				}
				after, readErr := os.ReadFile(path)
				if readErr != nil || string(after) != string(prior) {
					t.Fatal("export changed existing configuration")
				}
				assertExportFlagStreams(t, mode, tc.code, out.String(), diagnostic.String())
				if tc.code != 0 {
					for _, target := range []string{"exports", ".agents", ".claude", "home/.agents", "home/.claude"} {
						if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(target))); !os.IsNotExist(err) {
							t.Fatal("rejected export touched a skills destination")
						}
					}
					return
				}
				paths := binaryExportPaths(t, out.String(), mode)
				if len(paths) != 23 {
					t.Fatal("export selection lost the guide set")
				}
				for _, rel := range paths {
					if !filepath.IsLocal(rel) {
						t.Fatal("export returned a nonlocal artifact path")
					}
					content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(tc.destination), filepath.FromSlash(rel)))
					if err != nil {
						t.Fatalf("selected export destination missing: %v", err)
					}
					// Only identify which public format the adapter selected; do not
					// duplicate the upstream renderer's Markdown/frontmatter contract.
					if strings.Contains(string(content), "\nwhen_to_use:") != (tc.format == "claude-skill") {
						t.Fatal("export ignored the selected or default format")
					}
				}
			})
		}
	}
}

func assertExportFlagStreams(t *testing.T, mode string, code int, out, diagnostic string) {
	t.Helper()
	if mode == "human" {
		if code == 0 && (out == "" || diagnostic != "") {
			t.Fatal("successful export used wrong human streams")
		}
		if code != 0 && (out != "" || diagnostic == "") {
			t.Fatal("rejected export used wrong human streams")
		}
		return
	}
	if diagnostic != "" || !json.Valid([]byte(out)) {
		t.Fatal("export corrupted machine stdout protocol")
	}
	if mode == "compact" && code == 0 {
		return
	}
	var keys map[string]json.RawMessage
	var frame struct {
		OK     bool
		Data   json.RawMessage
		Errors []struct {
			Type string
			Code int `json:"exit_code"`
		}
		Meta struct {
			Command, Workspace string
			Code               int `json:"exit_code"`
		}
	}
	if json.Unmarshal([]byte(out), &keys) != nil || len(keys) != 4 || json.Unmarshal([]byte(out), &frame) != nil || frame.OK != (code == 0) || frame.Meta.Command != "agent.export" || frame.Meta.Workspace != "agent" || frame.Meta.Code != code {
		t.Fatal("export lost common envelope or canonical metadata")
	}
	if code != 0 && (string(frame.Data) != "null" || len(frame.Errors) != 1 || frame.Errors[0].Type != "validation_error" || frame.Errors[0].Code != 4) {
		t.Fatal("rejected export lost validation error contract")
	}
	if code == 0 && len(frame.Errors) != 0 {
		t.Fatal("successful export retained errors")
	}
}
