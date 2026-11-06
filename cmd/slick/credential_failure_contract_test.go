package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	slackcache "github.com/matcra587/slack-cli/internal/cache"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	"github.com/matcra587/slack-cli/internal/config"
)

func TestSlackCommandsProtectCredentialFailuresAndKeepContextExitCodes(t *testing.T) {
	t.Setenv("SLICK_CONFIG", filepath.Join(t.TempDir(), "unused.toml"))
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	file := filepath.Join(t.TempDir(), "review.txt")
	if err := os.WriteFile(file, []byte("Review complete"), 0o600); err != nil {
		t.Fatal(err)
	}
	caches := map[string][]byte{}
	for _, resource := range []string{"channels", "users", "unrelated"} {
		if _, err := slackcache.Write("default", resource, json.RawMessage(`{"channels":[],"users":[],"marker":"preserve"}`)); err != nil {
			t.Fatal(err)
		}
		path, err := slackcache.Path("default", resource)
		if err != nil {
			t.Fatal(err)
		}
		caches[path], err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
	}
	routes := [][]string{
		{"message", "send", "--channel=C123", "--message=Review complete"},
		{"message", "send", "--channel=C123", "--message=Review complete", "--schedule=90m"},
		{"message", "edit", "--channel=C123", "--timestamp=1746284582.123456", "--message=Review complete"},
		{"message", "delete", "--channel=C123", "--timestamp=1746284582.123456", "--force"},
		{"message", "scheduled", "list"},
		{"message", "scheduled", "delete", "--channel=C123", "--scheduled-id=Q1"},
		{"reply", "--channel=C123", "--parent=1746284582.123456", "--message=Review complete"},
		{"history", "list", "--channel=C123"},
		{"react", "add", "--channel=C123", "--timestamp=1746284582.123456", "--emoji=rocket"},
		{"react", "remove", "--channel=C123", "--timestamp=1746284582.123456", "--emoji=rocket"},
		{"react", "list", "--channel=C123", "--timestamp=1746284582.123456"},
		{"react", "watch", "--channel=C123", "--timestamp=1746284582.123456", "--emoji=rocket"},
		{"file", "upload", "--channel=C123", "--file=" + file},
		{"lookup", "channel"},
		{"lookup", "channel", "--channel=C123"},
		{"lookup", "user"},
		{"lookup", "user", "--user=U1"},
		{"lookup", "messages", "--query=release review"},
		{"status", "set", "--text=Reviewing changes"},
		{"status", "clear"},
		{"cache", "users", "--refresh"},
		{"cache", "channels", "--refresh"},
	}
	const secret = "synthetic-provider-private-value"
	for _, tc := range []struct {
		name  string
		cause error
		exit  int
	}{
		{"provider failure", errors.New("provider returned " + secret), 1},
		{"missing credential", fmt.Errorf("provider %s: %w", secret, config.ErrCredentialNotFound), 1},
		{"provider cancellation", fmt.Errorf("provider %s: %w", secret, context.Canceled), 6},
		{"provider deadline", fmt.Errorf("provider %s: %w", secret, context.DeadlineExceeded), 7},
		{"empty credential", nil, 1},
	} {
		for _, route := range routes {
			for _, mode := range []string{"json", "compact", "human"} {
				// Each handler keeps every cause and its exit/privacy/state assertions.
				// Immediate send covers shared failure rendering in the other modes.
				immediateSend := route[0] == "message" && route[1] == "send" && len(route) == 4
				if mode != "json" && !immediateSend {
					continue
				}
				t.Run(tc.name+"/"+strings.Join(route, " ")+"/"+mode, func(t *testing.T) {
					var out, diagnostic bytes.Buffer
					var requests, resolutions atomic.Int32
					cfg := workspaceConfig(config.TokenTypeUser)
					before, err := json.Marshal(cfg)
					if err != nil {
						t.Fatal(err)
					}
					root := NewRootCommand(WithConfig(cfg), WithIO(strings.NewReader(""), &out, &diagnostic), WithTTY(false), WithSlackBaseURL("http://127.0.0.1:1"),
						WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
							resolutions.Add(1)
							return "", tc.cause
						})),
						func(runtime *RootRuntime) {
							runtime.HTTPClient = &http.Client{Transport: credentialFailureTransport{calls: &requests}}
						},
					)
					args := append(append([]string{}, route...), "--output="+mode, "--no-throttle")
					root.SetArgs(args)
					err = root.Execute()
					if err == nil || strings.Contains(out.String()+diagnostic.String()+fmt.Sprintf("%v %+v %#v", err, err, err), secret) {
						t.Fatal("credential failure reported success or disclosed provider credentials")
					}
					if resolutions.Load() != 1 || requests.Load() != 0 {
						t.Fatal("credential failure was retried or reached Slack")
					}
					commandErr, ok := errors.AsType[clioutput.CommandError](err)
					if !ok || commandErr.CLIError.ExitCode != tc.exit {
						t.Fatal("credential failure lost its exit class")
					}
					if mode == "human" {
						if out.Len() != 0 || diagnostic.Len() == 0 {
							t.Fatal("human credential failure used stdout")
						}
					} else {
						var fields map[string]json.RawMessage
						if err := json.Unmarshal(out.Bytes(), &fields); err != nil {
							t.Fatal(err)
						}
						if len(fields) != 4 || string(fields["ok"]) != "false" || string(fields["data"]) != "null" || diagnostic.Len() != 0 {
							t.Fatal("credential failure changed the common envelope or streams")
						}
						var meta struct {
							Exit int `json:"exit_code"`
						}
						if err := json.Unmarshal(fields["meta"], &meta); err != nil {
							t.Fatal(err)
						}
						var failures []struct {
							Exit int `json:"exit_code"`
						}
						if err := json.Unmarshal(fields["errors"], &failures); err != nil {
							t.Fatal(err)
						}
						if meta.Exit != tc.exit || len(failures) != 1 || failures[0].Exit != tc.exit {
							t.Fatalf("credential context used wrong exit: got=%d want=%d", meta.Exit, tc.exit)
						}
					}
					after, err := json.Marshal(cfg)
					if err != nil || string(before) != string(after) {
						t.Fatal("credential failure changed workspace settings")
					}
					for path, before := range caches {
						after, err := os.ReadFile(path)
						if err != nil || !bytes.Equal(before, after) {
							t.Fatal("credential failure overwrote a prior or unrelated cache")
						}
					}
				})
			}
		}
	}
}

type credentialFailureTransport struct{ calls *atomic.Int32 }

func (transport credentialFailureTransport) RoundTrip(*http.Request) (*http.Response, error) {
	transport.calls.Add(1)
	return nil, errors.New("unexpected network access in credential failure fixture")
}

func TestExpiredCredentialContextStopsBeforeProviderAndSlack(t *testing.T) {
	t.Setenv("SLICK_CONFIG", filepath.Join(t.TempDir(), "unused.toml"))
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, timeout := range []bool{false, true} {
		name, exit := "cancel", 6
		if timeout {
			name, exit = "deadline", 7
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			if timeout {
				cancel()
				ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			} else {
				cancel()
			}
			defer cancel()
			var out, diagnostic bytes.Buffer
			var resolutions, requests atomic.Int32
			root := NewRootCommand(WithConfig(workspaceConfig(config.TokenTypeUser)), WithIO(strings.NewReader(""), &out, &diagnostic), WithTTY(false),
				WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
					resolutions.Add(1)
					return "xoxp-synthetic-unreached", nil
				})),
				func(runtime *RootRuntime) {
					runtime.HTTPClient = &http.Client{Transport: credentialFailureTransport{calls: &requests}}
				},
			)
			root.SetContext(ctx)
			root.SetArgs([]string{"react", "add", "--channel=C123", "--timestamp=1746284582.123456", "--emoji=rocket", "--output=json"})
			err := root.Execute()
			if err == nil || diagnostic.Len() != 0 || !strings.Contains(out.String(), fmt.Sprintf(`"exit_code":%d`, exit)) || resolutions.Load() != 0 || requests.Load() != 0 {
				t.Fatal("expired context reached credentials/network or changed its exit class")
			}
		})
	}
}

func TestSDKWrappedCancellationKeepsCanceledExitWithoutCanceledParent(t *testing.T) {
	t.Setenv("SLICK_CONFIG", filepath.Join(t.TempDir(), "unused.toml"))
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	var out, diagnostic bytes.Buffer
	root := NewRootCommand(WithConfig(workspaceConfig(config.TokenTypeUser)), WithIO(strings.NewReader(""), &out, &diagnostic), WithTTY(false),
		WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
			return "xoxp-synthetic-client-context", nil
		})),
		func(runtime *RootRuntime) {
			runtime.HTTPClient = &http.Client{Transport: credentialCanceledTransport{}}
		},
	)
	root.SetArgs([]string{"react", "add", "--channel=C123", "--timestamp=1746284582.123456", "--emoji=rocket", "--output=json", "--no-throttle"})
	err := root.Execute()
	commandErr, ok := errors.AsType[clioutput.CommandError](err)
	if !ok || commandErr.CLIError.ExitCode != 6 || diagnostic.Len() != 0 || !strings.Contains(out.String(), `"type":"canceled"`) {
		t.Fatalf("wrapped SDK cancellation changed class: err=%v stdout=%s", err, &out)
	}
}

type credentialCanceledTransport struct{}

func (credentialCanceledTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("request stopped: %w", context.Canceled)
}
