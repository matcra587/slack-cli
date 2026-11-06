package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	"github.com/matcra587/slack-cli/internal/config"
)

func TestUploadInvalidInputsFailBeforeCredentialsInRealAndDryRun(t *testing.T) {
	t.Setenv("SLICK_CONFIG", filepath.Join(t.TempDir(), "unused.toml"))
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.txt")
	artifact := filepath.Join(dir, "artifact.txt")
	for file, content := range map[string]string{empty: "", artifact: "artifact"} {
		if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name   string
		args   []string
		reader func() io.Reader
	}{
		{"missing file", nil, nil},
		{"empty path", []string{"--file=" + empty}, nil},
		{"missing path", []string{"--file=" + filepath.Join(dir, "missing.txt")}, nil},
		{"directory", []string{"--file=" + dir}, nil},
		{"empty stdin", []string{"--file=-", "--filename=artifact.dat"}, nil},
		{"missing stdin filename", []string{"--file=-"}, func() io.Reader { return strings.NewReader("artifact") }},
		{"blank stdin filename", []string{"--file=-", "--filename= \t "}, func() io.Reader { return strings.NewReader("artifact") }},
		{"blank override", []string{"--file=" + artifact, "--filename= \t "}, nil},
		{"newline filename", []string{"--file=" + artifact, "--filename=artifact\nsynthetic-unreviewed-name"}, nil},
		{"carriage filename", []string{"--file=" + artifact, "--filename=artifact\rsynthetic-unreviewed-name"}, nil},
		{"NUL filename", []string{"--file=" + artifact, "--filename=artifact\x00synthetic-unreviewed-name"}, nil},
		{"failed stdin", []string{"--file=-", "--filename=artifact.dat"}, func() io.Reader { return &uploadReadFailure{} }},
	} {
		for _, mode := range []string{"json", "compact", "human"} {
			// Every input keeps both real and preview guards. A failed reader covers
			// failure streams and private input/provider diagnostics in other modes.
			if mode != "json" && tc.name != "failed stdin" {
				continue
			}
			for _, dryRun := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/dry=%t", tc.name, mode, dryRun), func(t *testing.T) {
					var out, diagnostic bytes.Buffer
					var resolutions atomic.Int32
					input := io.Reader(strings.NewReader(""))
					if tc.reader != nil {
						input = tc.reader()
					}
					cfg := workspaceConfig(config.TokenTypeUser)
					before, err := json.Marshal(cfg)
					if err != nil {
						t.Fatal(err)
					}
					root := NewRootCommand(WithConfig(cfg), WithIO(input, &out, &diagnostic), WithTTY(false), WithSlackBaseURL("http://127.0.0.1:1"), WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
						resolutions.Add(1)
						return "", errors.New("synthetic-private-provider")
					})))
					args := append([]string{"file", "upload", "--channel=C123", "--no-attribution", "--output=" + mode}, tc.args...)
					if dryRun {
						args = append(args, "--dry-run")
					}
					root.SetArgs(args)
					err = root.Execute()
					commandErr, ok := errors.AsType[clioutput.CommandError](err)
					if !ok || commandErr.CLIError.ExitCode != 4 || resolutions.Load() != 0 {
						t.Fatalf("invalid upload reached credentials or changed exit: err=%v resolutions=%d", err, resolutions.Load())
					}
					if mode == "human" {
						if out.Len() != 0 || diagnostic.Len() == 0 {
							t.Fatal("human upload failure used wrong streams")
						}
					} else {
						var frame map[string]json.RawMessage
						if err := json.Unmarshal(out.Bytes(), &frame); err != nil {
							t.Fatal(err)
						}
						if len(frame) != 4 || string(frame["ok"]) != "false" || string(frame["data"]) != "null" || diagnostic.Len() != 0 {
							t.Fatal("upload validation changed common machine failure frame")
						}
					}
					if strings.Contains(out.String()+diagnostic.String(), "synthetic-private-provider") || strings.Contains(out.String()+diagnostic.String(), "synthetic-private-input-error") || strings.Contains(out.String()+diagnostic.String(), "synthetic-unreviewed-name") {
						t.Fatal("input or provider details leaked into upload diagnostics")
					}
					after, err := json.Marshal(cfg)
					if err != nil || !bytes.Equal(before, after) {
						t.Fatal("invalid upload changed workspace settings")
					}
					content, err := os.ReadFile(artifact)
					if err != nil || string(content) != "artifact" {
						t.Fatal("invalid upload changed the source file")
					}
				})
			}
		}
	}
}

type uploadReadFailure struct{ sent bool }

func (r *uploadReadFailure) Read(p []byte) (int, error) {
	if !r.sent {
		r.sent = true
		return copy(p, "partial data"), nil
	}
	return 0, errors.New("synthetic-private-input-error")
}
