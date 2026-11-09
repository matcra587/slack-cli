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

func TestBinaryAgentSchemaReadClassifications(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, view := range []string{"normal", "compact", "shapes"} {
		for _, mode := range []string{"json", "compact", "human"} {
			for _, configKind := range []string{"valid", "missing", "malformed"} {
				t.Run(view+"/"+mode+"/"+configKind, func(t *testing.T) {
					run := agentSchemaFixture(t, binary, configKind)
					args := []string{"--output=" + mode}
					if view != "normal" {
						args = append(args, "--"+view)
					}
					out, diagnostic, code := run(args...)
					if code != 0 || diagnostic != "" {
						t.Fatal("credential-free agent discovery failed")
					}
					schema := decodeAgentReadSchema(t, out, mode)
					observed := map[string]bool{}
					var visit func(agentReadSchemaNode)
					visit = func(node agentReadSchemaNode) {
						if _, exists := observed[node.Path]; exists {
							t.Fatal("agent schema duplicated a command path")
						}
						observed[node.Path] = node.ReadOnly
						for _, child := range node.Children {
							visit(child)
						}
					}
					visit(schema)
					want := expectedAgentReadClasses()
					if len(observed) != len(want) {
						t.Fatal("agent classification requires review for changed command paths")
					}
					for path, read := range want {
						got, exists := observed[path]
						if !exists || got != read {
							t.Fatalf("agent read classification differs for %s: got=%v exists=%v want=%v", path, got, exists, read)
						}
					}
				})
			}
		}
	}
}

func TestBinaryAgentSelectedReadClasses(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, path := range []string{"help", "lookup channel", "manifest template", "completion bash", "message send", "agent export", "cache clear"} {
		for _, mode := range []string{"json", "compact", "human"} {
			t.Run(path+"/"+mode, func(t *testing.T) {
				run := agentSchemaFixture(t, binary, "valid")
				out, diagnostic, code := run("--path="+path, "--output="+mode)
				if code != 0 || diagnostic != "" {
					t.Fatal("selected agent discovery failed")
				}
				got := decodeAgentReadSchema(t, out, mode)
				canonical := "slick " + path
				if got.Path != canonical || got.ReadOnly != expectedAgentReadClasses()[canonical] {
					t.Fatal("selected schema lost slick's safety classification")
				}
			})
		}
	}
}

type agentReadSchemaNode struct {
	Path     string
	ReadOnly bool `json:"read_only"`
	Children []agentReadSchemaNode
}

func decodeAgentReadSchema(t *testing.T, out, mode string) agentReadSchemaNode {
	t.Helper()
	data := json.RawMessage(out)
	if mode == "json" {
		var fields map[string]json.RawMessage
		var frame struct {
			OK   bool
			Data json.RawMessage
			Meta struct{ Command string }
		}
		if json.Unmarshal(data, &fields) != nil || len(fields) != 4 || json.Unmarshal(data, &frame) != nil || !frame.OK || frame.Meta.Command != "agent.schema" {
			t.Fatal("agent discovery lost the common success envelope")
		}
		data = frame.Data
	}
	if mode != "human" {
		var payload struct{ Schema json.RawMessage }
		if json.Unmarshal(data, &payload) != nil {
			t.Fatal("agent discovery payload is not JSON")
		}
		data = payload.Schema
	}
	var node agentReadSchemaNode
	if json.Unmarshal(data, &node) != nil || node.Path == "" {
		t.Fatal("agent discovery did not return its schema artifact")
	}
	return node
}

func expectedAgentReadClasses() map[string]bool {
	want := map[string]bool{}
	for _, path := range []string{
		"agent guide", "agent schema", "auth status", "dnd info", "dnd team", "pin list", "bookmark list",
		"completion bash", "completion elvish", "completion fish", "completion nu", "completion pwsh", "completion zsh",
		"config get", "config list", "config path",
		"health api-test", "health check", "health current", "health history", "help", "history list",
		"lookup channel", "lookup messages", "lookup user", "manifest template", "message scheduled list",
		"react list", "react watch", "version", "workspace list",
	} {
		want["slick "+path] = true
	}
	for _, path := range []string{
		"dnd snooze", "dnd end-snooze", "dnd end", "pin add", "pin remove", "bookmark add", "bookmark edit", "bookmark remove",
		"agent export", "auth login", "auth logout", "auth switch", "cache channels", "cache clear", "cache users",
		"config init", "config set", "config unset", "file upload",
		"message delete", "message edit", "message scheduled delete", "message send",
		"react add", "react remove", "reply", "status clear", "status set",
	} {
		want["slick "+path] = false
	}
	// Routing parents retain their conservative classification, including root
	// invocation where hidden completion actions can write local files.
	for path := range want {
		for parent := path; strings.Contains(parent, " "); {
			parent = parent[:strings.LastIndex(parent, " ")]
			want[parent] = false
		}
	}
	return want
}

func agentSchemaFixture(t *testing.T, binary, configKind string) func(...string) (string, string, int) {
	t.Helper()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); w.WriteHeader(503) }))
	t.Cleanup(server.Close)
	path := filepath.Join(t.TempDir(), "config.toml")
	prior := []byte("schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"TFIXTURE\"\ntoken_type=\"user\"\ntoken_ref=\"env:SLICK_SCHEMA_UNUSED_TOKEN\"\n[workspaces.fixture.aliases]\nops=\"C123\"\n")
	if configKind == "malformed" {
		prior = []byte("synthetic-schema-private-config = [\n")
	}
	if configKind != "missing" {
		if err := os.WriteFile(path, prior, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return func(args ...string) (string, string, int) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, binary, append([]string{"agent", "schema", "--color=never"}, args...)...)
		cmd.Env = append(isolatedRuntimeEnv(t), "SLICK_CONFIG="+path, "SLACK_CLI_BASE_URL="+server.URL, "SLACK_CLI_CALLBACK_PORT=53221")
		var out, diagnostic strings.Builder
		cmd.Stdout, cmd.Stderr = &out, &diagnostic
		err := cmd.Run()
		if ctx.Err() != nil || requests.Load() != 0 {
			t.Fatal("agent discovery accessed Slack or exceeded its deadline")
		}
		after, readErr := os.ReadFile(path)
		if configKind == "missing" {
			if !os.IsNotExist(readErr) {
				t.Fatal("agent discovery created configuration")
			}
		} else if readErr != nil || string(after) != string(prior) {
			t.Fatal("agent discovery changed prior configuration")
		}
		if strings.Contains(out.String()+diagnostic.String(), "synthetic-schema-private-config") || strings.Contains(out.String()+diagnostic.String(), "env:SLICK_SCHEMA_UNUSED_TOKEN") {
			t.Fatal("agent discovery exposed unrelated configuration")
		}
		return out.String(), diagnostic.String(), exitCode(err)
	}
}
