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
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBinaryAgentGuideAdapter(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, configKind := range []string{"valid", "missing", "malformed"} {
		for _, tc := range []struct {
			name, kind string
			args       []string
			code       int
			empty      bool
		}{
			{name: "index", kind: "index"},
			{name: "runbook", kind: "runbook", args: []string{"send-msg"}},
			{name: "legacy alias", kind: "runbook", args: []string{"send_msg"}},
			{name: "all long", kind: "all", args: []string{"--all"}},
			{name: "all short", kind: "all", args: []string{"-a"}},
			{name: "removed all", kind: "index", args: []string{"--all=false"}},
			{name: "section long", kind: "section", args: []string{"send-msg", "--section=Run"}},
			{name: "section short", kind: "section", args: []string{"send_msg", "-s", "Run"}},
			{name: "empty section content", kind: "section", args: []string{"inspect-schema", "--section=Preconditions"}, empty: true},
			{name: "removed section", kind: "runbook", args: []string{"send-msg", "--section="}},
			{name: "unknown guide", args: []string{"unknown-guide"}, code: 4},
			{name: "unknown section", args: []string{"send-msg", "--section=Unknown"}, code: 4},
			{name: "section without guide", args: []string{"--section=Run"}, code: 4},
			{name: "all with guide", args: []string{"send-msg", "--all"}, code: 4},
			{name: "all with section", args: []string{"send-msg", "--all", "--section=Run"}, code: 4},
			{name: "extra positional", args: []string{"send-msg", "reply"}, code: 4},
		} {
			t.Run(configKind+"/"+tc.name, func(t *testing.T) {
				run := agentAdapterFixture(t, binary, configKind, "guide")
				human := run("human", tc.code, tc.args...)
				for _, mode := range []string{"json", "compact"} {
					out := run(mode, tc.code, tc.args...)
					if tc.code != 0 {
						continue
					}
					raw := agentAdapterPayload(t, out, mode)
					var data struct {
						Version        string `json:"contract_version"`
						Kind, Document string
					}
					if json.Unmarshal(raw, &data) != nil || data.Version != "2" || data.Kind != tc.kind || data.Document != human {
						t.Fatal("guide adapter changed kind/version or artifact bytes across modes")
					}
					if (strings.TrimSpace(data.Document) == "") != tc.empty {
						t.Fatal("guide adapter lost empty/nonempty artifact distinction")
					}
				}
			})
		}
	}
}

func TestBinaryAgentSchemaAdapter(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, configKind := range []string{"valid", "missing", "malformed"} {
		for _, tc := range []struct {
			name            string
			args            []string
			path            string
			compact, shapes bool
			code            int
		}{
			{name: "default", path: "slick"},
			{name: "compact long", args: []string{"--compact"}, path: "slick", compact: true},
			{name: "compact short", args: []string{"-c"}, path: "slick", compact: true},
			{name: "removed compact", args: []string{"--compact=false"}, path: "slick"},
			{name: "shapes long", args: []string{"--shapes"}, path: "slick", shapes: true},
			{name: "shapes short", args: []string{"-s"}, path: "slick", shapes: true},
			{name: "removed shapes", args: []string{"--shapes=false"}, path: "slick"},
			{name: "path long", args: []string{"--path=message send"}, path: "slick message send", shapes: true},
			{name: "path short", args: []string{"-p", "message send"}, path: "slick message send", shapes: true},
			{name: "rooted path", args: []string{"--path=slick message send"}, path: "slick message send", shapes: true},
			{name: "removed path", args: []string{"--path="}, path: "slick"},
			{name: "unknown path", args: []string{"--path=unknown command"}, code: 4},
			{name: "compact with path", args: []string{"--compact", "--path=message send"}, code: 4},
			{name: "compact with shapes", args: []string{"--compact", "--shapes"}, code: 4},
			{name: "path with shapes", args: []string{"--path=message send", "--shapes"}, code: 4},
			{name: "extra positional", args: []string{"message"}, code: 4},
		} {
			t.Run(configKind+"/"+tc.name, func(t *testing.T) {
				run := agentAdapterFixture(t, binary, configKind, "schema")
				human := run("human", tc.code, tc.args...)
				for _, mode := range []string{"json", "compact"} {
					out := run(mode, tc.code, tc.args...)
					if tc.code != 0 {
						continue
					}
					raw := agentAdapterPayload(t, out, mode)
					var data struct{ Schema json.RawMessage }
					if json.Unmarshal(raw, &data) != nil {
						t.Fatal("schema adapter lost artifact wrapper")
					}
					var humanArtifact, machineArtifact any
					if json.Unmarshal([]byte(human), &humanArtifact) != nil || json.Unmarshal(data.Schema, &machineArtifact) != nil || !reflect.DeepEqual(humanArtifact, machineArtifact) {
						t.Fatal("schema artifact changed across output modes")
					}
					var schema struct {
						Path, Description string
						Output            json.RawMessage `json:"output_schema"`
						Extensions        struct {
							Version string `json:"contract_version"`
						}
					}
					if json.Unmarshal(data.Schema, &schema) != nil || schema.Path != tc.path || schema.Extensions.Version != "2" {
						t.Fatal("schema adapter lost selected path or contract version")
					}
					if (schema.Description == "") != tc.compact {
						t.Fatal("schema compact option did not control descriptive metadata")
					}
					if (len(schema.Output) > 0) != tc.shapes {
						t.Fatal("schema shapes/path options did not control embedded output shape")
					}
				}
			})
		}
	}
}

func agentAdapterPayload(t *testing.T, out, mode string) json.RawMessage {
	t.Helper()
	raw := json.RawMessage(out)
	if mode == "json" {
		var frame struct{ Data json.RawMessage }
		if json.Unmarshal(raw, &frame) != nil {
			t.Fatal("invalid adapter result")
		}
		raw = frame.Data
	}
	return raw
}

func agentAdapterFixture(t *testing.T, binary, configKind, operation string) func(string, int, ...string) string {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "config.toml")
	const canary = "synthetic-private-discovery-config"
	prior := []byte("schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"T123\"\ntoken_type=\"bot\"\ntoken_ref=\"op://synthetic/fixture/token\"\nagent_label=\"" + canary + "\"\n")
	if configKind == "malformed" {
		prior = []byte("[invalid config " + canary)
	}
	if configKind != "missing" {
		if err := os.WriteFile(path, prior, 0o600); err != nil {
			t.Fatal(err)
		}
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
	t.Cleanup(server.Close)
	return func(mode string, code int, args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		argv := append([]string{"agent", operation, "--output=" + mode, "--color=never"}, args...)
		command := exec.CommandContext(ctx, binary, argv...)
		command.Dir = root
		command.Env = append(isolatedRuntimeEnv(t), "SLICK_CONFIG="+path, "SLACK_CLI_CONFIG="+path, "SLACK_CLI_BASE_URL="+server.URL, "PATH="+bin, "SLICK_PROVIDER_TRACE="+trace)
		var out, diagnostic strings.Builder
		command.Stdout = &out
		command.Stderr = &diagnostic
		err := command.Run()
		if ctx.Err() != nil || exitCode(err) != code {
			t.Fatalf("adapter exit differs: got=%d want=%d %s %s", exitCode(err), code, out.String(), diagnostic.String())
		}
		if requests.Load() != 0 {
			t.Fatal("local discovery contacted Slack")
		}
		if _, err := os.Stat(trace); !os.IsNotExist(err) {
			t.Fatal("local discovery resolved credentials")
		}
		after, readErr := os.ReadFile(path)
		if configKind == "missing" {
			if !os.IsNotExist(readErr) {
				t.Fatal("discovery created configuration")
			}
		} else if readErr != nil || string(after) != string(prior) {
			t.Fatal("discovery changed prior configuration")
		}
		if strings.Contains(out.String()+diagnostic.String(), canary) || strings.Contains(out.String()+diagnostic.String(), "xoxb-synthetic-forbidden") {
			t.Fatal("discovery disclosed config or credentials")
		}
		if mode == "human" {
			if code == 0 && diagnostic.Len() != 0 {
				t.Fatal("successful human discovery wrote diagnostics")
			}
			if code != 0 && (out.Len() != 0 || diagnostic.Len() == 0) {
				t.Fatal("human discovery failure used wrong streams")
			}
		} else {
			if diagnostic.Len() != 0 || !json.Valid([]byte(out.String())) {
				t.Fatal("machine discovery corrupted its stdout protocol")
			}
			if mode == "json" || code != 0 {
				var fields map[string]json.RawMessage
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
				if json.Unmarshal([]byte(out.String()), &fields) != nil || len(fields) != 4 || json.Unmarshal([]byte(out.String()), &frame) != nil || frame.OK != (code == 0) || frame.Meta.Command != "agent."+operation || frame.Meta.Workspace != "agent" || frame.Meta.Code != code {
					t.Fatalf("discovery lost common envelope or canonical metadata: command=%q workspace=%q code=%d keys=%d ok=%t", frame.Meta.Command, frame.Meta.Workspace, frame.Meta.Code, len(fields), frame.OK)
				}
				if code != 0 && (string(frame.Data) != "null" || len(frame.Errors) != 1 || frame.Errors[0].Type != "validation_error" || frame.Errors[0].Code != 4) {
					t.Fatal("discovery failure lost validation contract")
				}
				if code == 0 && len(frame.Errors) != 0 {
					t.Fatal("successful discovery retained errors")
				}
			}
		}
		return out.String()
	}
}
