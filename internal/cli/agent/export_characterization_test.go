package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	cliagent "github.com/matcra587/slack-cli/internal/cli/agent"
	"github.com/matcra587/slack-cli/internal/cli/clitheme"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	"github.com/matcra587/slack-cli/internal/cli/runtime/runtimetest"
	"github.com/matcra587/slack-cli/internal/config"
)

func TestAgentExportCharacterization(t *testing.T) {
	routes := []struct {
		name, format, scope, harness, skillsDir string
	}{
		{name: "explicit-agent-skill", format: "agent-skill", skillsDir: "exports"},
		{name: "explicit-claude-skill", format: "claude-skill", skillsDir: "exports"},
		{name: "project-codex", scope: "project", harness: "codex", skillsDir: ".agents/skills"},
		{name: "user-codex", scope: "user", harness: "codex", skillsDir: ".agents/skills"},
		{name: "project-claude", scope: "project", harness: "claude-code", skillsDir: ".claude/skills"},
		{name: "user-claude", scope: "user", harness: "claude-code", skillsDir: ".claude/skills"},
	}
	for _, route := range routes {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(route.name+"/"+mode, func(t *testing.T) {
				isolated := t.TempDir()
				home := filepath.Join(isolated, "home")
				project := filepath.Join(isolated, "project")
				for _, dir := range []string{home, project} {
					if err := os.MkdirAll(dir, 0o700); err != nil {
						t.Fatal(err)
					}
				}
				for key, value := range map[string]string{
					"HOME": home, "USERPROFILE": home,
					"XDG_CONFIG_HOME":  filepath.Join(isolated, "config"),
					"XDG_CACHE_HOME":   filepath.Join(isolated, "cache"),
					"SLICK_CONFIG":     filepath.Join(isolated, "config", "slick.toml"),
					"SLACK_CLI_CONFIG": filepath.Join(isolated, "config", "slick.toml"),
				} {
					t.Setenv(key, value)
				}
				t.Chdir(project)
				root := project
				if route.scope == "user" {
					root = home
				}
				exportRoot := filepath.Join(root, filepath.FromSlash(route.skillsDir))
				preserved := map[string]string{
					filepath.Join(exportRoot, "unrelated.txt"):          "keep this file\n",
					filepath.Join(exportRoot, "other-tool", "SKILL.md"): "unrelated skill\n",
					filepath.Join(isolated, "config", "slick.toml"):     "# synthetic configuration fixture\n",
					filepath.Join(isolated, "cache", "unrelated.json"):  "{}\n",
				}
				for path, content := range preserved {
					if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				args := []string{"agent", "export", "--output=" + mode}
				if route.scope == "" {
					args = append(args, "--dir", exportRoot, "--format", route.format)
				} else {
					args = append(args, "--scope", route.scope, "--harness", route.harness)
				}
				stdout := executeIsolatedExport(t, args)
				paths := exportReportedPaths(t, mode, stdout)
				want := expectedSlickExportPaths()
				if !slices.Equal(paths, want) {
					t.Fatalf("reported paths = %q, want %q", paths, want)
				}
				for _, path := range paths {
					content, err := os.ReadFile(filepath.Join(exportRoot, filepath.FromSlash(path)))
					if err != nil {
						t.Fatalf("reported artifact %s: %v", path, err)
					}
					if !strings.Contains(string(content), "slick") {
						t.Errorf("artifact %s lacks the slick guide content", path)
					}
				}
				for path, want := range preserved {
					got, err := os.ReadFile(path)
					if err != nil || string(got) != want {
						t.Errorf("unrelated fixture changed: %s: %q, %v", path, got, err)
					}
				}
				if mode == "human" && route.scope != "" && !strings.HasPrefix(stdout, "#") {
					t.Fatalf("scoped human export omitted destination report: %q", stdout)
				}
			})
		}
	}
}

func executeIsolatedExport(t *testing.T, args []string) string {
	t.Helper()
	runtime, stdout, stderr := runtimetest.NewRuntime(t, runtimetest.Options{
		Token: "xoxb-synthetic-export-fixture", Theme: clitheme.Default(),
		SlackBaseURL: "https://fixture.invalid/",
	})
	runtime.TokenResolver = cliruntime.TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
		t.Error("agent export unexpectedly requested credentials")
		return "", errors.New("credentials unavailable in export fixture")
	})
	runtime.HTTPClient = &http.Client{Transport: exportRejectTransport{t: t}}
	root := runtimetest.NewRoot(runtime, stdout, stderr)
	root.AddCommand(cliagent.NewCommand(runtime))
	out, diagnostic, err := runtimetest.Run(t, root, args, stdout, stderr)
	if err != nil || diagnostic != "" {
		t.Fatalf("agent export: err=%v, stderr=%q", err, diagnostic)
	}
	return out
}

type exportRejectTransport struct{ t *testing.T }

func (transport exportRejectTransport) RoundTrip(*http.Request) (*http.Response, error) {
	transport.t.Error("agent export unexpectedly made an HTTP request")
	return nil, errors.New("network unavailable in export fixture")
}

func exportReportedPaths(t *testing.T, mode, stdout string) []string {
	t.Helper()
	if mode == "human" {
		var paths []string
		for line := range strings.SplitSeq(strings.TrimSpace(stdout), "\n") {
			if !strings.HasPrefix(line, "#") {
				paths = append(paths, line)
			}
		}
		return paths
	}
	var payload struct {
		Paths []string `json:"paths"`
	}
	data := []byte(stdout)
	if mode == "json" {
		var envelope struct {
			OK   bool `json:"ok"`
			Meta struct {
				Command  string `json:"command"`
				ExitCode int    `json:"exit_code"`
			} `json:"meta"`
			Data   json.RawMessage   `json:"data"`
			Errors []json.RawMessage `json:"errors"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			t.Fatalf("decode export envelope: %v", err)
		}
		if !envelope.OK || envelope.Meta.Command != "agent.export" || envelope.Meta.ExitCode != 0 || len(envelope.Errors) != 0 {
			t.Fatalf("unexpected export envelope: %s", stdout)
		}
		data = envelope.Data
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("decode export paths: %v", err)
	}
	return payload.Paths
}

func expectedSlickExportPaths() []string {
	slugs := []string{
		"auth-setup", "cache-metadata", "check-health", "cleanup-msgs", "config-prefs",
		"core-contract", "delete-msg", "developer-review", "discover-destination", "edit-msg",
		"inspect-schema", "lookup-user", "react", "read-history", "reply", "safe-mutation",
		"schedule-msg", "search-msgs", "send-dm", "send-msg", "set-status", "upload-file",
	}
	paths := make([]string, len(slugs))
	for i, slug := range slugs {
		paths[i] = "slick-" + slug + "/SKILL.md"
	}
	return paths
}
