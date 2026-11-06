package completion_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gechr/clog"
	clicompletion "github.com/matcra587/slack-cli/internal/cli/completion"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
	"github.com/matcra587/slack-cli/internal/config"
	"github.com/matcra587/slack-cli/internal/testutil"
)

func TestRuntimeCompletionHandlerConstructionAndLocalCandidatesNeedNoCredential(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	var reads atomic.Int32
	runtime := &cliruntime.RootRuntime{Config: workspaceConfig(config.TokenTypeBot), SlackBaseURL: "http://127.0.0.1:1", TokenResolver: cliruntime.TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
		reads.Add(1)
		return "", errors.New("synthetic-provider-private")
	})}
	handler := clicompletion.HandlerForRuntime(runtime)
	if reads.Load() != 0 {
		t.Fatal("handler construction read credentials")
	}
	if got := captureSlackCompletion(t, handler, "zsh", "workspace", nil); !slices.Equal(got, []string{"default"}) || reads.Load() != 0 {
		t.Fatal("local candidates read credentials or changed")
	}
	if got := captureSlackCompletion(t, handler, "zsh", "channel", nil); len(got) != 0 || reads.Load() != 1 {
		t.Fatal("uncached live candidate did not resolve exactly once or exposed failure")
	}
}

func TestRuntimeCompletionEnvironmentTokenFallback(t *testing.T) {
	for _, withConfig := range []bool{false, true} {
		t.Run(map[bool]string{false: "without config", true: "with config"}[withConfig], func(t *testing.T) {
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			t.Setenv("SLACK_CLI_TOKEN", "xoxb-synthetic-global-completion")
			server := testutil.NewSlackServer(t, map[string]testutil.SlackHandler{"users.list": func(req testutil.SlackRequest) testutil.SlackResponse {
				if req.Form.Get("token") != "xoxb-synthetic-global-completion" {
					t.Error("completion did not use explicit environment fallback")
				}
				return testutil.JSONResponse(`{"ok":true,"members":[{"id":"UENV","name":"environment"}]}`)
			}})
			var cfg *config.Config
			if withConfig {
				cfg = workspaceConfig(config.TokenTypeBot)
			}
			runtime := &cliruntime.RootRuntime{Config: cfg, SlackBaseURL: server.BaseURL(), TokenResolver: cliruntime.TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
				t.Error("environment token called credential provider")
				return "", errors.New("unexpected provider")
			})}
			got := captureSlackCompletion(t, clicompletion.HandlerForRuntime(runtime), "zsh", "user", nil)
			if !slices.Equal(got, []string{"UENV"}) || len(server.Requests("users.list")) != 1 {
				t.Fatal("explicit environment token fallback stopped working")
			}
		})
	}
}

func TestRuntimeCompletionWorkspaceSelectionCanBeRemoved(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	cfg := workspaceConfig(config.TokenTypeBot)
	other := cfg.Workspaces["default"]
	other.Name = "Other"
	cfg.Workspaces["Other"] = other
	var selected []string
	runtime := &cliruntime.RootRuntime{Config: cfg, WorkspaceName: "oThEr", SlackBaseURL: "http://127.0.0.1:1", TokenResolver: cliruntime.TokenResolverFunc(func(_ context.Context, profile config.WorkspaceProfile) (string, error) {
		selected = append(selected, profile.Name)
		return "", errors.New("synthetic provider failure")
	})}
	handler := clicompletion.HandlerForRuntime(runtime)
	captureSlackCompletion(t, handler, "zsh", "channel", nil)
	runtime.WorkspaceName = ""
	captureSlackCompletion(t, handler, "zsh", "channel", nil)
	if !slices.Equal(selected, []string{"Other", "default"}) {
		t.Fatal("completion kept old workspace after selector removal")
	}
}

type completionTransientOutput struct{ calls int }

func (w *completionTransientOutput) Write(p []byte) (int, error) {
	w.calls++
	return 0, errors.New("synthetic-private-writer")
}

func TestRuntimeCompletionHandlerOutputFailureCanBeRetriedExplicitly(t *testing.T) {
	var diagnostic, healthy bytes.Buffer
	writer := &completionTransientOutput{}
	runtime := &cliruntime.RootRuntime{Config: workspaceConfig(config.TokenTypeBot), Stdout: writer, Stderr: &diagnostic, ColorMode: clog.ColorNever}
	handler := clicompletion.HandlerForRuntime(runtime)
	handler("zsh", "cache_resource", nil)
	if writer.calls != 1 || strings.Count(diagnostic.String(), "output_write_failed") != 1 || strings.Contains(diagnostic.String(), "synthetic-private-writer") {
		t.Fatal("output failure was retried or leaked writer error")
	}
	runtime.Stdout = &healthy
	diagnostic.Reset()
	handler("zsh", "cache_resource", nil)
	if healthy.String() != "users\nchannels\n" || diagnostic.Len() != 0 {
		t.Fatal("explicit handler retry retained stale output failure")
	}
}
