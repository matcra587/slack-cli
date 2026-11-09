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
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBinaryAgentExportLifecycle(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, mode := range []string{"json", "compact", "human"} {
		for _, format := range []string{"agent-skill", "claude-skill"} {
			t.Run(mode+"/"+format, func(t *testing.T) {
				project := t.TempDir()
				home := t.TempDir()
				target := filepath.Join(home, "skills")
				var requests atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); w.WriteHeader(500) }))
				defer server.Close()
				configPath := filepath.Join(project, "config.toml")
				priorConfig := []byte("# synthetic-private-export-config\n")
				if err := os.WriteFile(configPath, priorConfig, 0o600); err != nil {
					t.Fatal(err)
				}
				run := func(want int, args ...string) string {
					t.Helper()
					ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
					defer cancel()
					argv := append([]string{"agent", "export", "--color=never", "--output=" + mode}, args...)
					command := exec.CommandContext(ctx, binary, argv...)
					command.Dir = project
					command.Env = append(isolatedRuntimeEnv(t), "HOME="+home, "USERPROFILE="+home, "SLICK_CONFIG="+configPath, "SLACK_CLI_BASE_URL="+server.URL, "SLICK_EXPORT_TARGET="+target)
					var stdout, stderr strings.Builder
					command.Stdout = &stdout
					command.Stderr = &stderr
					err := command.Run()
					if ctx.Err() != nil || exitCode(err) != want {
						t.Fatalf("export exit: got %d want %d: %s %s", exitCode(err), want, stdout.String(), stderr.String())
					}
					if requests.Load() != 0 {
						t.Fatal("export made a Slack request")
					}
					if mode != "human" && stderr.Len() != 0 {
						t.Fatal("machine diagnostics escaped stdout envelope")
					}
					if mode == "human" && want != 0 && (stdout.Len() != 0 || stderr.Len() == 0) {
						t.Fatal("wrong human error streams")
					}
					if strings.Contains(stdout.String()+stderr.String(), "synthetic-private-export-config") {
						t.Fatal("config leaked")
					}
					after, readErr := os.ReadFile(configPath)
					if readErr != nil || string(after) != string(priorConfig) {
						t.Fatal("config changed")
					}
					return stdout.String()
				}
				base := []string{"--dir", target, "--format", format}
				preview := run(0, append(slices.Clone(base), "--dry-run")...)
				if _, err := os.Stat(target); !os.IsNotExist(err) {
					t.Fatal("dry run touched destination")
				}
				if mode == "human" {
					if !strings.Contains(preview, "dry run") {
						t.Fatal("human preview unmarked")
					}
				} else {
					data := decodeAgentExportData(t, preview, mode)
					if !data.DryRun || len(data.Paths) != 23 {
						t.Fatal("incomplete preview")
					}
				}
				out := run(0, base...)
				paths := binaryExportPaths(t, out, mode)
				if len(paths) != 23 || paths[0] != "slick-auth-setup/SKILL.md" || paths[1] != "slick-cache-metadata/SKILL.md" {
					t.Fatal("export missing known artifacts")
				}
				for _, rel := range paths {
					if !filepath.IsLocal(rel) {
						t.Fatal("nonlocal reported path")
					}
					content, err := os.ReadFile(filepath.Join(target, rel))
					if err != nil || !strings.Contains(string(content), "slick") {
						t.Fatal("reported artifact absent")
					}
				}
				first := filepath.Join(target, paths[0])
				if err := os.WriteFile(first, []byte("external edit"), 0o600); err != nil {
					t.Fatal(err)
				}
				unrelated := filepath.Join(target, "other.txt")
				if err := os.WriteFile(unrelated, []byte("keep"), 0o600); err != nil {
					t.Fatal(err)
				}
				run(0, append(slices.Clone(base), "--dry-run")...)
				data, err := os.ReadFile(first)
				if err != nil || string(data) != "external edit" {
					t.Fatal("preview changed external edit")
				}
				run(0, append(slices.Clone(base), "--dry-run=false")...)
				data, err = os.ReadFile(first)
				if err != nil || string(data) == "external edit" {
					t.Fatal("normal export did not update artifact")
				}
				blocker := filepath.Join(target, "slick-cache-metadata")
				if err := os.RemoveAll(blocker); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(blocker, []byte("keep blocker"), 0o600); err != nil {
					t.Fatal(err)
				}
				failed := run(5, base...)
				if mode != "human" {
					var frame struct {
						OK   bool
						Data json.RawMessage
						Meta struct {
							Code int `json:"exit_code"`
						}
						Errors []struct {
							Details struct {
								Completed []string `json:"completed_paths"`
							} `json:"details"`
						}
					}
					if json.Unmarshal([]byte(failed), &frame) != nil || frame.OK || string(frame.Data) != "null" || frame.Meta.Code != 5 || len(frame.Errors) != 1 || !slices.Equal(frame.Errors[0].Details.Completed, paths[:1]) {
						t.Fatal("partial failure contract lost")
					}
				}
				data, err = os.ReadFile(blocker)
				if err != nil || string(data) != "keep blocker" {
					t.Fatal("partial failure changed blocker")
				}
				if err := os.Remove(blocker); err != nil {
					t.Fatal(err)
				}
				run(0, base...)
				for _, rel := range paths {
					if _, err := os.Stat(filepath.Join(target, rel)); err != nil {
						t.Fatal("recovery incomplete")
					}
				}
				for _, raw := range []string{"~/skills", "$SLICK_EXPORT_TARGET"} {
					run(0, "--dir", raw, "--format", format)
				}
				for _, args := range [][]string{
					{"--dir", target}, {"--dir", target, "--format=unknown"}, {"--dir", target, "--format", format, "--scope=user"}, {"--dir", target, "--format", format, "--harness=codex"}, {"--dir="}, {"--scope=unknown"}, {"--scope=project", "--harness=unknown"}, {"--dir=$UNSET_EXPORT_PATH", "--format", format},
				} {
					run(4, args...)
				}
				data, err = os.ReadFile(unrelated)
				if err != nil || string(data) != "keep" {
					t.Fatal("unrelated state lost")
				}
			})
		}
	}
}

type binaryAgentExportData struct {
	Paths  []string `json:"paths"`
	DryRun bool     `json:"dry_run"`
}

func decodeAgentExportData(t *testing.T, out, mode string) binaryAgentExportData {
	t.Helper()
	raw := []byte(out)
	if mode == "json" {
		var frame struct {
			OK     bool
			Data   json.RawMessage
			Errors []json.RawMessage
			Meta   struct{ Command string }
		}
		if json.Unmarshal(raw, &frame) != nil || !frame.OK || frame.Meta.Command != "agent.export" || len(frame.Errors) != 0 {
			t.Fatal("invalid export success envelope")
		}
		raw = frame.Data
	}
	var data binaryAgentExportData
	if json.Unmarshal(raw, &data) != nil {
		t.Fatal("invalid export payload")
	}
	return data
}

func binaryExportPaths(t *testing.T, out, mode string) []string {
	t.Helper()
	if mode != "human" {
		return decodeAgentExportData(t, out, mode).Paths
	}
	var paths []string
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if !strings.HasPrefix(line, "#") {
			paths = append(paths, line)
		}
	}
	return paths
}

func TestBinaryAgentExportResultFailurePreservesArtifacts(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, mode := range []string{"json", "compact", "human"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, "skills")
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := reader.Close(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = writer.Close() }()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, binary, "agent", "export", "--dir", target, "--format=agent-skill", "--output="+mode, "--color=never")
			command.Env = append(isolatedRuntimeEnv(t), "SLICK_CONFIG="+filepath.Join(root, "missing.toml"))
			command.Dir = root
			command.Stdout = writer
			var diagnostic strings.Builder
			command.Stderr = &diagnostic
			err = command.Run()
			if ctx.Err() != nil || exitCode(err) != 8 || strings.Count(diagnostic.String(), "output_write_failed") != 1 {
				t.Fatalf("result failure lost exit8: %v %s", err, diagnostic.String())
			}
			count := 0
			if err := filepath.WalkDir(target, func(path string, entry os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.Name() == "SKILL.md" {
					count++
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if count != 23 {
				t.Fatalf("result failure lost exported artifacts: %d", count)
			}
		})
	}
}
