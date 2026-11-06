package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	slackcache "github.com/matcra587/slack-cli/internal/cache"
	"github.com/matcra587/slack-cli/internal/config"
)

type completionFailingWriter struct {
	mode   string
	calls  int
	prefix bytes.Buffer
}

func (w *completionFailingWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.mode == "transient" && w.calls > 1 {
		return w.prefix.Write(p)
	}
	switch w.mode {
	case "short":
		return 0, nil
	case "partial":
		n := len(p) / 2
		_, _ = w.prefix.Write(p[:n])
		return n, nil
	default:
		return 0, errors.New("synthetic-private-completion-writer")
	}
}

func TestDynamicCompletionWriteFailuresReturnIOWithoutReplay(t *testing.T) {
	for _, path := range []string{"workspace", "config key", "config bool", "guide", "cache names", "cached channels", "cached users", "live channels", "live users"} {
		for _, mode := range []string{"json", "compact", "human"} {
			for _, failure := range []string{"error", "short", "partial", "transient"} {
				// Keep every candidate source. Local and live representatives cover
				// all writer failures and mode overrides on the shared shell protocol.
				if path != "cache names" && path != "live users" && (mode != "json" || failure != "error") {
					continue
				}
				t.Run(path+"/"+mode+"/"+failure, func(t *testing.T) {
					t.Setenv("XDG_CACHE_HOME", t.TempDir())
					cfg := completionProfileConfig()
					cfgBefore, err := json.Marshal(cfg)
					if err != nil {
						t.Fatal(err)
					}
					args := []string{"--output=" + mode, "--color=never", "--@shell=fish"}
					kind := "workspace"
					var positional []string
					resource, payload := "", ""
					live := strings.HasPrefix(path, "live ")
					switch path {
					case "config key":
						kind = "config_key"
					case "config bool":
						kind = "config_value"
						positional = []string{"--", "workspaces.default.attribution.enabled"}
					case "guide":
						kind = "guide_workflow"
					case "cache names":
						kind = "cache_resource"
					case "cached channels":
						kind = "channel"
						resource = "channels"
						payload = `{"channels":[{"id":"CCACHED1","name":"first"},{"id":"CCACHED2","name":"second"}]}`
					case "cached users":
						kind = "user"
						resource = "users"
						payload = `{"users":[{"id":"UCACHED1","name":"first"},{"id":"UCACHED2","name":"second"}]}`
					case "live channels":
						kind = "channel"
					case "live users":
						kind = "user"
					}
					args = append(args, "--@complete="+kind)
					args = append(args, positional...)
					var cachePath string
					var cacheBefore []byte
					if resource != "" {
						if _, err := slackcache.Write("default", resource, json.RawMessage(payload)); err != nil {
							t.Fatal(err)
						}
						cachePath, err = slackcache.Path("default", resource)
						if err != nil {
							t.Fatal(err)
						}
						cacheBefore, err = os.ReadFile(cachePath)
						if err != nil {
							t.Fatal(err)
						}
					}
					var requests, providers atomic.Int32
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						requests.Add(1)
						if r.Method != http.MethodPost || r.FormValue("token") != "xoxb-synthetic-output" {
							t.Error("completion output failure changed API request/auth")
						}
						w.Header().Set("Content-Type", "application/json")
						switch r.URL.Path {
						case "/api/conversations.list":
							_, _ = io.WriteString(w, `{"ok":true,"channels":[{"id":"CREMOTE1","name":"first"},{"id":"CREMOTE2","name":"second"}]}`)
						case "/api/users.list":
							_, _ = io.WriteString(w, `{"ok":true,"members":[{"id":"UREMOTE1","name":"first"},{"id":"UREMOTE2","name":"second"}]}`)
						default:
							t.Error("unexpected completion request")
							w.WriteHeader(500)
						}
					}))
					defer server.Close()
					resolver := TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
						providers.Add(1)
						return "xoxb-synthetic-output", nil
					})
					writer := &completionFailingWriter{mode: failure}
					var diagnostic bytes.Buffer
					root, runtime := NewRootCommandWithRuntime(WithConfig(cfg), WithSlackBaseURL(server.URL), WithTokenResolver(resolver), WithTTY(false), WithIO(strings.NewReader(""), writer, &diagnostic))
					root.SetArgs(args)
					err = root.ExecuteContext(t.Context())
					if runtime.CancelTimeout != nil {
						runtime.CancelTimeout()
					}
					commandErr, ok := errors.AsType[CommandError](err)
					if !ok || commandErr.CLIError.ExitCode != 8 || commandErr.CLIError.Type != "io" || commandErr.CLIError.Message != "output_write_failed" {
						t.Fatalf("completion did not return IO8: %v", err)
					}
					if writer.calls != 1 || strings.Count(diagnostic.String(), "output_write_failed") != 1 || !strings.Contains(diagnostic.String(), "exit_code=8") {
						t.Fatal("completion retried output or duplicated/lost its safe diagnostic")
					}
					if strings.Contains(fmt.Sprint(err)+diagnostic.String(), "synthetic-private-completion-writer") || strings.Contains(diagnostic.String(), "xoxb-synthetic-output") {
						t.Fatal("completion diagnostic exposed private writer/token data")
					}
					want := int32(0)
					if live {
						want = 1
					}
					if requests.Load() != want || providers.Load() != want {
						t.Fatal("output failure retried API/provider or local path accessed credentials")
					}
					var recovered, secondDiagnostic bytes.Buffer
					next, nextRuntime := NewRootCommandWithRuntime(WithConfig(cfg), WithSlackBaseURL(server.URL), WithTokenResolver(resolver), WithTTY(false), WithIO(strings.NewReader(""), &recovered, &secondDiagnostic))
					next.SetArgs(args)
					if err := next.ExecuteContext(t.Context()); err != nil {
						t.Fatalf("explicit completion recovery failed: %v", err)
					}
					if nextRuntime.CancelTimeout != nil {
						nextRuntime.CancelTimeout()
					}
					if secondDiagnostic.Len() != 0 || strings.Count(recovered.String(), "\n") < 2 || strings.HasPrefix(recovered.String(), "{") || requests.Load() != want*2 || providers.Load() != want*2 {
						t.Fatal("recovery lost shell protocol or repeated operations")
					}
					cfgAfter, err := json.Marshal(cfg)
					if err != nil || !bytes.Equal(cfgBefore, cfgAfter) {
						t.Fatal("completion output failure changed runtime preferences")
					}
					if cachePath != "" {
						after, err := os.ReadFile(cachePath)
						if err != nil || !bytes.Equal(cacheBefore, after) {
							t.Fatal("completion output failure changed cached state")
						}
					}
				})
			}
		}
	}
}

func TestEmptyDynamicCompletionDoesNotAttemptOutput(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	writer := &completionFailingWriter{mode: "error"}
	var diagnostic bytes.Buffer
	root, runtime := NewRootCommandWithRuntime(WithConfig(completionProfileConfig()), WithTTY(false), WithIO(strings.NewReader(""), writer, &diagnostic), WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
		t.Error("empty local completion consulted provider")
		return "", errors.New("unexpected provider")
	})))
	root.SetArgs([]string{"--@complete=unknown", "--color=never"})
	if err := root.ExecuteContext(t.Context()); err != nil || writer.calls != 0 || diagnostic.Len() != 0 {
		t.Fatal("empty completion attempted output or returned a spurious failure")
	}
	if runtime.CancelTimeout != nil {
		runtime.CancelTimeout()
	}
}

func TestDynamicCompletionBothOutputStreamsCanFailWithoutReplay(t *testing.T) {
	stdout, stderr := &completionFailingWriter{mode: "error"}, &completionFailingWriter{mode: "error"}
	root, runtime := NewRootCommandWithRuntime(WithConfig(completionProfileConfig()), WithTTY(false), WithIO(strings.NewReader(""), stdout, stderr))
	root.SetArgs([]string{"--@complete=cache_resource", "--color=never"})
	err := root.ExecuteContext(t.Context())
	if runtime.CancelTimeout != nil {
		runtime.CancelTimeout()
	}
	typed, ok := errors.AsType[CommandError](err)
	if !ok || typed.CLIError.ExitCode != 8 || stdout.calls != 1 || stderr.calls != 1 || strings.Contains(fmt.Sprint(err), "synthetic-private-completion-writer") {
		t.Fatal("failed diagnostic retried output, lost IO8, or exposed writer error")
	}
}
