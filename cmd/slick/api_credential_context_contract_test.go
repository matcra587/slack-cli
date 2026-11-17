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
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func TestAPICredentialFailuresPreserveCancellationWithoutRequests(t *testing.T) {
	for _, row := range []struct {
		name  string
		cause error
		exit  int
	}{
		{"provider canceled", context.Canceled, 6},
		{"provider deadline", context.DeadlineExceeded, 7},
		{"constructor canceled", nil, 6},
		{"provider failure", errors.New("synthetic-private-credential-error"), 1},
	} {
		t.Run(row.name, func(t *testing.T) {
			caller, cancel := context.WithCancel(t.Context())
			defer cancel()
			var out, diagnostic bytes.Buffer
			requests, resolutions := 0, 0
			root, runtime := NewRootCommandWithRuntime(WithConfig(authTestConfig()), WithTTY(false),
				WithIO(strings.NewReader(""), &out, &diagnostic),
				WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
					resolutions++
					if row.cause == nil {
						cancel()
						return "xoxb-synthetic-status", nil
					}
					return "", fmt.Errorf("synthetic-private-provider: %w", row.cause)
				})), func(runtime *RootRuntime) {
					runtime.HTTPClient = &http.Client{Transport: authStatusTransport(func(*http.Request) (*http.Response, error) {
						requests++
						return nil, errors.New("synthetic-private-unexpected-request")
					})}
				})
			root.SetArgs([]string{"api", "call", "conversations.list", "--paginate", "--output=json"})
			_, err := executeRoot(caller, root, runtime)
			if runtime.CancelTimeout != nil {
				runtime.CancelTimeout()
			}
			typed, ok := errors.AsType[CommandError](err)
			if !ok || typed.CLIError.ExitCode != row.exit {
				t.Fatalf("credential failure exit=%v want=%d", err, row.exit)
			}
			if resolutions != 1 || requests != 0 || diagnostic.Len() != 0 {
				t.Fatal("credential failure sent a request, retried resolution, or emitted human diagnostics")
			}
			if strings.Contains(out.String()+err.Error(), "synthetic-private") {
				t.Fatal("credential failure exposed provider details")
			}
			if !strings.Contains(out.String(), fmt.Sprintf(`"exit_code":%d`, row.exit)) {
				t.Fatal("credential failure envelope lost its exit code")
			}
		})
	}
}

func TestAPIUnknownWorkspaceFailsWithoutPanicOrCredentials(t *testing.T) {
	for _, mode := range []string{"human", "json", "compact"} {
		t.Run(mode, func(t *testing.T) {
			defer func() {
				if value := recover(); value != nil {
					t.Errorf("unknown workspace panicked: %v", value)
				}
			}()
			var out, diagnostic bytes.Buffer
			providers := 0
			root, runtime := NewRootCommandWithRuntime(WithConfig(authTestConfig()), WithTTY(false),
				WithIO(strings.NewReader(""), &out, &diagnostic),
				WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
					providers++
					return "", errors.New("synthetic-private-provider")
				})))
			root.SetArgs([]string{"api", "call", "users.info", "--field=user=U123", "--workspace=synthetic-missing", "--output=" + mode})
			_, err := executeRoot(t.Context(), root, runtime)
			if runtime.CancelTimeout != nil {
				runtime.CancelTimeout()
			}
			typed, ok := errors.AsType[CommandError](err)
			if !ok || typed.CLIError.ExitCode != 4 || providers != 0 {
				t.Fatalf("unknown workspace lost its validation error or accessed credentials: %v", err)
			}
			if mode == "human" {
				if out.Len() != 0 || diagnostic.Len() == 0 {
					t.Fatal("human validation error used the wrong streams")
				}
			} else if !json.Valid(out.Bytes()) || !strings.Contains(out.String(), `"ok":false`) || diagnostic.Len() != 0 {
				t.Fatal("machine validation error lost its failure envelope")
			}
		})
	}
}

func TestAPIEmptySelectedCredentialNeverFallsBackToSavedProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	fallback := &config.Config{
		SchemaVersion: config.SchemaVersion, DefaultWorkspace: "fallback",
		Workspaces: map[string]config.WorkspaceProfile{
			"fallback": {Name: "fallback", TeamID: "T_FALLBACK", TokenType: config.TokenTypeBot, TokenRef: "keychain:slack-cli/fallback"},
		},
	}
	if err := config.SaveFile(path, fallback); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLICK_CONFIG", path)
	t.Setenv("SLACK_CLI_DEFAULT_WORKSPACE", "")
	t.Setenv("SLACK_CLI_TOKEN", "xoxb-synthetic-fallback")
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = io.WriteString(w, `{"ok":true,"user":{"id":"U123"}}`)
	}))
	t.Cleanup(server.Close)
	for _, token := range []string{"", " \t\n"} {
		t.Run(fmt.Sprintf("length=%d", len(token)), func(t *testing.T) {
			var out, diagnostic bytes.Buffer
			resolutions := 0
			root, runtime := NewRootCommandWithRuntime(WithConfig(authTestConfig()), WithTTY(false), WithSlackBaseURL(server.URL),
				WithIO(strings.NewReader(""), &out, &diagnostic),
				WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
					resolutions++
					return token, nil
				})))
			root.SetArgs([]string{"api", "call", "users.info", "--field=user=U123", "--output=json"})
			_, err := executeRoot(t.Context(), root, runtime)
			if runtime.CancelTimeout != nil {
				runtime.CancelTimeout()
			}
			typed, ok := errors.AsType[CommandError](err)
			if !ok || typed.CLIError.ExitCode != 1 || resolutions != 1 || requests.Load() != 0 {
				t.Fatalf("empty selected credential used a fallback or lost its auth error: err=%v requests=%d", err, requests.Load())
			}
			if !json.Valid(out.Bytes()) || diagnostic.Len() != 0 || strings.Contains(out.String(), "xoxb-") {
				t.Fatal("credential rejection lost its envelope or exposed credentials")
			}
		})
	}
}
