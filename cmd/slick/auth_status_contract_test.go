package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	"github.com/matcra587/slack-cli/internal/config"
)

func TestAuthStatusRedactsFailuresAndPreservesCancellation(t *testing.T) {
	t.Setenv("SLICK_CONFIG", filepath.Join(t.TempDir(), "unused.toml"))
	for _, mode := range []string{"json", "compact", "human"} {
		for _, behavior := range []string{"valid", "provider", "missing", "private Slack error", "invalid auth", "transport", "empty", "canceled", "deadline"} {
			t.Run(mode+"/"+behavior, func(t *testing.T) {
				const private = "synthetic-private-auth-data"
				cfg := &config.Config{SchemaVersion: config.SchemaVersion, DefaultWorkspace: "Beta", Workspaces: map[string]config.WorkspaceProfile{
					"Beta":  {Name: "Beta", TeamID: "T_BETA", TokenType: config.TokenTypeBot, TokenRef: "env:SYNTHETIC_BETA"},
					"Alpha": {Name: "Alpha", TeamID: "T_ALPHA", TokenType: config.TokenTypeBot, TokenRef: "env:SYNTHETIC_ALPHA"},
				}}
				if behavior == "empty" {
					cfg = nil
				}
				before, err := json.Marshal(cfg)
				if err != nil {
					t.Fatal(err)
				}
				var providers, calls atomic.Int32
				client := &http.Client{Transport: authStatusTransport(func(r *http.Request) (*http.Response, error) {
					calls.Add(1)
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					if r.Method != http.MethodPost || r.URL.Path != "/api/auth.test" || r.URL.RawQuery != "" {
						t.Error("status changed auth.test method, path or form encoding")
					}
					alpha := r.PostForm.Get("token") == "xoxb-synthetic-alpha"
					if !alpha && r.PostForm.Get("token") != "xoxb-synthetic-beta" {
						t.Error("status resolved the wrong profile credential")
					}
					if alpha && behavior == "transport" {
						return nil, errors.New(private)
					}
					body := `{"ok":true,"user_id":"U_ACTOR","team_id":"T_FIXTURE"}`
					if alpha && behavior == "private Slack error" {
						body = `{"ok":false,"error":"` + private + `"}`
					} else if alpha && behavior == "invalid auth" {
						body = `{"ok":false,"error":"invalid_auth"}`
					}
					return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
				})}
				var out, diagnostic bytes.Buffer
				root := NewRootCommand(WithConfig(cfg), WithTTY(false), WithIO(strings.NewReader(""), &out, &diagnostic), WithSlackBaseURL("http://127.0.0.1:1"), func(runtime *RootRuntime) { runtime.HTTPClient = client }, WithTokenResolver(TokenResolverFunc(func(_ context.Context, profile config.WorkspaceProfile) (string, error) {
					providers.Add(1)
					if profile.Name == "Alpha" {
						if behavior == "provider" {
							return "", errors.New(private)
						}
						if behavior == "missing" {
							return "", config.ErrCredentialNotFound
						}
						return "xoxb-synthetic-alpha", nil
					}
					return "xoxb-synthetic-beta", nil
				})))
				ctx := t.Context()
				wantExit := 0
				switch behavior {
				case "canceled":
					canceled, cancel := context.WithCancel(ctx)
					cancel()
					ctx, wantExit = canceled, 6
				case "deadline":
					expired, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Minute))
					defer cancel()
					ctx, wantExit = expired, 7
				}
				root.SetArgs([]string{"auth", "status", "--output=" + mode, "--color=never", "--no-throttle"})
				err = root.ExecuteContext(ctx)
				if wantExit == 0 && err != nil {
					t.Fatalf("status inspection failed: %v", err)
				}
				if wantExit != 0 {
					failure, ok := errors.AsType[clioutput.CommandError](err)
					if !ok || failure.CLIError.ExitCode != wantExit || calls.Load() != 0 || providers.Load() != 0 {
						t.Fatal("auth status swallowed cancellation/deadline or accessed credentials afterward")
					}
				} else {
					wantProviders, wantCalls := int32(2), int32(2)
					if behavior == "provider" || behavior == "missing" {
						wantCalls = 1
					}
					if behavior == "empty" {
						wantProviders, wantCalls = 0, 0
					}
					if providers.Load() != wantProviders || calls.Load() != wantCalls {
						t.Error("auth status omitted or replayed profile validation")
					}
				}
				combined := out.String() + diagnostic.String()
				for _, secret := range []string{private, "xoxb-synthetic-alpha", "xoxb-synthetic-beta"} {
					if strings.Contains(combined, secret) {
						t.Error("auth status exposed provider, transport or Slack private data")
					}
				}
				if mode == "human" {
					if (wantExit == 0 && (out.Len() == 0 || diagnostic.Len() != 0)) || (wantExit != 0 && (out.Len() != 0 || diagnostic.Len() == 0)) {
						t.Error("auth status changed human output streams or lost empty-state output")
					}
				} else {
					var data map[string]any
					if diagnostic.Len() != 0 || json.Unmarshal(out.Bytes(), &data) != nil {
						t.Fatal("auth status lost machine JSON or streams")
					}
					if mode == "json" || wantExit != 0 {
						if data["ok"] != (wantExit == 0) || data["meta"].(map[string]any)["command"] != "auth.status" || data["meta"].(map[string]any)["exit_code"] != float64(wantExit) {
							t.Error("auth status lost canonical envelope metadata")
						}
						if wantExit == 0 {
							data = data["data"].(map[string]any)
						} else if data["data"] != nil {
							t.Error("auth status invented result data after cancellation")
						}
					}
					if wantExit == 0 {
						rows, ok := data["workspaces"].([]any)
						if !ok {
							t.Fatal("auth status returned null instead of a workspace list")
						}
						if behavior == "empty" {
							if len(rows) != 0 {
								t.Error("empty auth status invented workspaces")
							}
						} else {
							if len(rows) != 2 || rows[0].(map[string]any)["workspace"] != "Alpha" || rows[1].(map[string]any)["workspace"] != "Beta" {
								t.Fatal("auth status lost stable workspace order")
							}
							alpha, beta := rows[0].(map[string]any), rows[1].(map[string]any)
							wantState := "invalid"
							switch behavior {
							case "valid":
								wantState = "valid"
							case "missing":
								wantState = "missing"
							}
							if alpha["validation_state"] != wantState || alpha["authenticated"] != (behavior == "valid") || beta["authenticated"] != true {
								t.Error("auth status lost per-profile validation outcomes")
							}
							if behavior == "invalid auth" && alpha["validation_error"] != "invalid_auth" {
								t.Error("auth status dropped a useful safe Slack error code")
							}
						}
					}
				}
				after, err := json.Marshal(cfg)
				if err != nil || !bytes.Equal(before, after) {
					t.Error("auth status changed prior config")
				}
			})
		}
	}
}

func TestAuthStatusStopsAfterCancellationDuringValidation(t *testing.T) {
	t.Setenv("SLICK_CONFIG", filepath.Join(t.TempDir(), "unused.toml"))
	for _, phase := range []string{"provider", "API"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			cfg := &config.Config{SchemaVersion: config.SchemaVersion, DefaultWorkspace: "Alpha", Workspaces: map[string]config.WorkspaceProfile{
				"Alpha": {Name: "Alpha", TeamID: "T_ALPHA", TokenType: config.TokenTypeBot, TokenRef: "env:SYNTHETIC_ALPHA"},
				"Beta":  {Name: "Beta", TeamID: "T_BETA", TokenType: config.TokenTypeBot, TokenRef: "env:SYNTHETIC_BETA"},
			}}
			var providers, calls atomic.Int32
			client := &http.Client{Transport: authStatusTransport(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				cancel()
				return nil, r.Context().Err()
			})}
			var out, diagnostic bytes.Buffer
			root := NewRootCommand(WithConfig(cfg), WithTTY(false), WithIO(strings.NewReader(""), &out, &diagnostic), WithSlackBaseURL("http://127.0.0.1:1"), func(runtime *RootRuntime) { runtime.HTTPClient = client }, WithTokenResolver(TokenResolverFunc(func(ctx context.Context, _ config.WorkspaceProfile) (string, error) {
				providers.Add(1)
				if phase == "provider" {
					cancel()
					return "", ctx.Err()
				}
				return "xoxb-synthetic-alpha", nil
			})))
			root.SetArgs([]string{"auth", "status", "--output=json", "--no-throttle"})
			failure, ok := errors.AsType[clioutput.CommandError](root.ExecuteContext(ctx))
			wantCalls := int32(0)
			if phase == "API" {
				wantCalls = 1
			}
			if !ok || failure.CLIError.ExitCode != 6 || providers.Load() != 1 || calls.Load() != wantCalls {
				t.Fatalf("auth status continued validation after cancellation: error=%v providers=%d calls=%d output=%s diagnostics=%s", failure, providers.Load(), calls.Load(), out.String(), diagnostic.String())
			}
			var frame map[string]any
			if err := json.Unmarshal(out.Bytes(), &frame); err != nil || frame["ok"] != false || frame["data"] != nil || diagnostic.Len() != 0 {
				t.Fatal("canceled status published partial success or changed streams")
			}
		})
	}
}

type authStatusTransport func(*http.Request) (*http.Response, error)

func (f authStatusTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
