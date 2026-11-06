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

func assertConfigPathResult(t *testing.T, mode, path, out, diagnostic string, runErr error, exists bool, want int) {
	t.Helper()
	if exitCode(runErr) != want {
		t.Fatalf("config path exit=%d, want %d", exitCode(runErr), want)
	}
	if mode == "human" {
		if want == 0 {
			value := "exists=false"
			if exists {
				value = "exists=true"
			}
			if diagnostic != "" || !strings.Contains(out, value) {
				t.Fatalf("config path lost its human existence result: stdout=%q stderr=%q", out, diagnostic)
			}
		} else if out != "" || diagnostic == "" {
			t.Fatal("config path error used wrong human stream")
		}
		return
	}
	if diagnostic != "" {
		t.Fatal("config path machine result used stderr")
	}
	data := json.RawMessage(out)
	if mode == "json" || want != 0 {
		var frame struct {
			OK   bool            `json:"ok"`
			Data json.RawMessage `json:"data"`
			Meta struct {
				Command string `json:"command"`
				Exit    int    `json:"exit_code"`
			} `json:"meta"`
			Errors []struct {
				Type string `json:"type"`
				Exit int    `json:"exit_code"`
			} `json:"errors"`
		}
		if err := json.Unmarshal([]byte(out), &frame); err != nil || frame.OK != (want == 0) || frame.Meta.Command != "config.path" || frame.Meta.Exit != want {
			t.Fatal("config path lost common machine envelope")
		}
		if want != 0 {
			wantType := "server_error"
			if want == 4 {
				wantType = "validation_error"
			}
			if string(frame.Data) != "null" || len(frame.Errors) != 1 || frame.Errors[0].Exit != want || frame.Errors[0].Type != wantType {
				t.Fatal("config path filesystem error lost classification or returned success data")
			}
			return
		}
		if len(frame.Errors) != 0 {
			t.Fatal("successful config path returned errors")
		}
		data = frame.Data
	}
	var result struct {
		Path   string `json:"path"`
		Exists bool   `json:"exists"`
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &result); err != nil || result.Path != path || result.Exists != exists {
		t.Fatal("config path returned an incorrect path or existence value")
	}
	if err := json.Unmarshal(data, &fields); err != nil || len(fields) != 2 {
		t.Fatal("config path did not retain its two-field result")
	}
}

func TestBinaryConfigPathRejectsExtraArguments(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, mode := range []string{"json", "compact", "human"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			out, diagnostic, err := runSlackBinary(t, binary, path, "http://127.0.0.1:1", "", "--output="+mode, "--color=never", "config", "path", "unexpected")
			assertConfigPathResult(t, mode, path, out, diagnostic, err, false, 4)
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("rejected config path invocation created configuration")
			}
		})
	}
}

func TestBinaryConfigPathHonorsLocationPrecedenceAndExpansion(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, source := range []string{"home", "xdg", "legacy", "primary", "priority", "tilde", "environment", "relative"} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(source+"/"+mode, func(t *testing.T) {
				root := t.TempDir()
				home := filepath.Join(root, "home with spaces")
				xdg := ""
				legacy, primary := "", ""
				want := filepath.Join(home, ".config", "slick", "config.toml")
				switch source {
				case "xdg":
					xdg = filepath.Join(root, "config")
					want = filepath.Join(xdg, "slick", "config.toml")
				case "legacy":
					legacy = filepath.Join(root, "legacy.toml")
					want = legacy
				case "primary", "priority":
					primary = filepath.Join(root, "primary.toml")
					want = primary
					if source == "priority" {
						legacy = filepath.Join(root, "legacy.toml")
					}
				case "tilde":
					primary = "~/preferred.toml"
					want = filepath.Join(home, "preferred.toml")
				case "environment":
					primary = "$SLICK_PATH_FIXTURE/preferred.toml"
					want = filepath.Join(home, "preferred.toml")
				case "relative":
					primary, want = "relative.toml", "relative.toml"
				}
				actual := want
				if !filepath.IsAbs(actual) {
					actual = filepath.Join(home, actual)
				}
				if err := os.MkdirAll(filepath.Dir(actual), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(home, 0o700); err != nil {
					t.Fatal(err)
				}
				prior := []byte("synthetic path content [")
				if err := os.WriteFile(actual, prior, 0o600); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, binary, "--output="+mode, "--color=never", "config", "path")
				cmd.Dir = home
				cmd.Env = append(isolatedRuntimeEnv(t), "HOME="+home, "USERPROFILE="+home, "XDG_CONFIG_HOME="+xdg, "SLACK_CLI_CONFIG="+legacy, "SLICK_CONFIG="+primary, "SLICK_PATH_FIXTURE="+home, "SLACK_CLI_TOKEN=xoxb-synthetic-location")
				var out, diagnostic bytes.Buffer
				cmd.Stdout, cmd.Stderr = &out, &diagnostic
				err := cmd.Run()
				if ctx.Err() != nil {
					t.Fatal("config location command exceeded timeout")
				}
				assertConfigPathResult(t, mode, want, out.String(), diagnostic.String(), err, true, 0)
				if strings.Contains(out.String()+diagnostic.String(), "synthetic path content") || strings.Contains(out.String()+diagnostic.String(), "xoxb-synthetic-location") {
					t.Fatal("config path disclosed file contents or credentials")
				}
				after, err := os.ReadFile(actual)
				if err != nil || !bytes.Equal(after, prior) {
					t.Fatal("config path modified selected configuration")
				}
			})
		}
	}
}

func TestBinaryConfigPathReportsFilesystemErrorsAndRecovers(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires Unix permission enforcement without root")
	}
	binary := buildSlackBinary(t)
	for _, mode := range []string{"json", "compact", "human"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.toml")
			prior := []byte("schema_version=\"1\"\ndefault_workspace=\"default\"\n[workspaces.default]\nname=\"default\"\ntoken_ref=\"synthetic-unreviewed-reference\"\n")
			if err := os.WriteFile(path, prior, 0o600); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // Restore a private fixture directory after permission tests.
			if err := os.Chmod(dir, 0); err != nil {
				t.Fatal(err)
			}
			out, diagnostic, err := runSlackBinary(t, binary, path, "http://127.0.0.1:1", "", "--output="+mode, "--color=never", "config", "path")
			assertConfigPathResult(t, mode, path, out, diagnostic, err, false, 5)
			if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // Restore private fixture directory access.
				t.Fatal(err)
			}
			out, diagnostic, err = runSlackBinary(t, binary, path, "http://127.0.0.1:1", "", "--output="+mode, "--color=never", "config", "path")
			assertConfigPathResult(t, mode, path, out, diagnostic, err, true, 0)
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, prior) || strings.Contains(out+diagnostic, "synthetic-unreviewed-reference") {
				t.Fatal("config path modified or exposed config contents")
			}
		})
	}
}

func TestBinaryConfigPathTracksPresenceWithoutValidatingContents(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, mode := range []string{"json", "compact", "human"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.toml")
			run := func(exists bool, want int) {
				t.Helper()
				out, diagnostic, err := runSlackBinary(t, binary, path, "http://127.0.0.1:1", "", "--output="+mode, "--color=never", "config", "path")
				assertConfigPathResult(t, mode, path, out, diagnostic, err, exists, want)
				if strings.Contains(out+diagnostic, "synthetic malformed contents") {
					t.Fatal("config path disclosed config contents")
				}
			}
			run(false, 0)
			prior := []byte("synthetic malformed contents [")
			if err := os.WriteFile(path, prior, 0o600); err != nil {
				t.Fatal(err)
			}
			run(true, 0)
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, prior) {
				t.Fatal("config path rewrote malformed configuration")
			}
			if err := os.WriteFile(path, []byte("schema_version=\"1\"\ndefault_workspace=\"default\"\n[workspaces.default]\nname=\"default\"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			run(true, 0)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			run(false, 0)
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			run(true, 0)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("target.toml", path); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			run(false, 0)
			if err := os.WriteFile(filepath.Join(dir, "target.toml"), prior, 0o600); err != nil {
				t.Fatal(err)
			}
			run(true, 0)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("config.toml", path); err != nil {
				t.Fatal(err)
			}
			run(false, 5)
			info, err := os.Lstat(path)
			if err != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Fatal("config path replaced cyclic link")
			}
		})
	}
}

func TestBinaryConfigPathMissingParentsStayAbsent(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, obstacle := range []bool{false, true} {
		name := "missing parent"
		if obstacle {
			name = "file parent"
		}
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(name+"/"+mode, func(t *testing.T) {
				root := t.TempDir()
				parent := filepath.Join(root, "parent")
				if obstacle {
					if err := os.WriteFile(parent, []byte("keep"), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				path := filepath.Join(parent, "config.toml")
				out, diagnostic, err := runSlackBinary(t, binary, path, "http://127.0.0.1:1", "", "--output="+mode, "--color=never", "config", "path")
				assertConfigPathResult(t, mode, path, out, diagnostic, err, false, 0)
				if obstacle {
					body, err := os.ReadFile(parent)
					if err != nil || string(body) != "keep" {
						t.Fatal("config path changed a non-directory parent")
					}
				} else if _, err := os.Stat(parent); !os.IsNotExist(err) {
					t.Fatal("config path created missing directories")
				}
			})
		}
	}
}
