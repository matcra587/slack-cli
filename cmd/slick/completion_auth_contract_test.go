package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	slackcache "github.com/matcra587/slack-cli/internal/cache"
	"github.com/matcra587/slack-cli/internal/config"
)

func runRuntimeCompletion(t *testing.T, ctx context.Context, cfg *config.Config, baseURL string, resolver TokenResolver, args ...string) (string, string, error) {
	t.Helper()
	out, err := os.CreateTemp(t.TempDir(), "completion-stdout-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = out.Close() }()
	original := os.Stdout
	os.Stdout = out
	defer func() { os.Stdout = original }()
	var diagnostic bytes.Buffer
	//nolint:contextcheck // ExecuteContext below attaches ctx; construction only registers Cobra handlers.
	root, runtime := NewRootCommandWithRuntime(WithConfig(cfg), WithSlackBaseURL(baseURL), WithTokenResolver(resolver), WithTTY(false), WithIO(strings.NewReader(""), out, &diagnostic))
	root.SetArgs(append([]string{"--color=never"}, args...))
	err = root.ExecuteContext(ctx)
	if runtime.CancelTimeout != nil {
		runtime.CancelTimeout()
	}
	if _, seekErr := out.Seek(0, 0); seekErr != nil {
		t.Fatal(seekErr)
	}
	data, readErr := io.ReadAll(out)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(data)+diagnostic.String(), "xoxb-synthetic-") || strings.Contains(string(data)+diagnostic.String(), "synthetic-provider-private") {
		t.Fatal("completion leaked synthetic credential/provider data")
	}
	return string(data), diagnostic.String(), err
}

func completionProfileConfig() *config.Config {
	cfg := &config.Config{SchemaVersion: "1", DefaultWorkspace: "default", Workspaces: map[string]config.WorkspaceProfile{}}
	for _, name := range []string{"default", "Other", "a.b"} {
		cfg.Workspaces[name] = config.WorkspaceProfile{Name: name, TeamID: "T_SYNTHETIC", TokenType: config.TokenTypeBot, TokenRef: "env:SLICK_UNUSED_COMPLETION_TOKEN"}
	}
	return cfg
}

func TestRuntimeCompletionLocalPathsNeverResolveCredentials(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, tc := range []struct {
		name     string
		args     []string
		contains string
		empty    bool
	}{
		{"discovery", nil, `"commands"`, false},
		{"public script", []string{"completion", "fish"}, "complete -c slick", false},
		{"hidden script", []string{"--print-completion", "--@shell=fish"}, "complete -c slick", false},
		{"workspace", []string{"--@complete=workspace"}, "Other", false},
		{"config key", []string{"--@complete=config_key"}, "workspaces.Other.default_channel", false},
		{"config bool", []string{"--@complete=config_value", "--", "workspaces.default.attribution.enabled"}, "true", false},
		{"guides", []string{"--@complete=guide_workflow"}, "core-contract", false},
		{"cache resource", []string{"--@complete=cache_resource"}, "channels", false},
		{"unknown kind", []string{"--@complete=unknown"}, "", true},
		{"missing key", []string{"--@complete=config_value"}, "", true},
		{"unknown key", []string{"--@complete=config_value", "--", "unknown.default_channel"}, "", true},
		{"unknown key profile", []string{"--@complete=config_value", "--", "workspaces.Unknown.default_channel"}, "", true},
		{"unknown workspace", []string{"--workspace=Unknown", "--@complete=channel"}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls, providers atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(500) }))
			defer server.Close()
			resolver := TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
				providers.Add(1)
				return "", errors.New("synthetic-provider-private")
			})
			out, diagnostic, err := runRuntimeCompletion(t, t.Context(), completionProfileConfig(), server.URL, resolver, tc.args...)
			if err != nil || diagnostic != "" || providers.Load() != 0 || calls.Load() != 0 {
				t.Fatalf("local completion consulted credentials/Slack: providers=%d requests=%d err=%v", providers.Load(), calls.Load(), err)
			}
			if tc.empty {
				if out != "" {
					t.Fatal("unknown/local empty path emitted candidates")
				}
			} else if !strings.Contains(out, tc.contains) {
				t.Fatal("local completion did not produce advertised output")
			}
		})
	}
}

func TestRuntimeCompletionFreshCachesNeverResolveCredentials(t *testing.T) {
	for _, tc := range []struct {
		name, profile, resource, payload, kind, contains string
		args                                             []string
	}{
		{"channels", "default", "channels", `{"channels":[{"id":"CCACHED","name":"cached"}]}`, "channel", "CCACHED", nil},
		{"empty channels", "default", "channels", `{"channels":[]}`, "channel", "", nil},
		{"users", "default", "users", `{"users":[{"id":"UCACHED","name":"cached","deleted":false}]}`, "user", "UCACHED", nil},
		{"empty users", "default", "users", `{"users":[]}`, "user", "", nil},
		{"deleted users", "default", "users", `{"users":[{"id":"UGONE","name":"gone","deleted":true}]}`, "user", "", nil},
		{"selected workspace", "Other", "channels", `{"channels":[{"id":"COTHER","name":"other"}]}`, "channel", "COTHER", []string{"--workspace=oThEr"}},
		{"key target", "Other", "channels", `{"channels":[{"id":"COTHER","name":"other"}]}`, "config_value", "COTHER", []string{"--", "workspaces.Other.default_channel"}},
		{"dotted key", "a.b", "channels", `{"channels":[{"id":"CDOTTED","name":"dotted"}]}`, "config_value", "CDOTTED", []string{"--", "workspaces.a.b.default_channel"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			_, err := slackcache.Write(tc.profile, tc.resource, json.RawMessage(tc.payload))
			if err != nil {
				t.Fatal(err)
			}
			path, err := slackcache.Path(tc.profile, tc.resource)
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var providers atomic.Int32
			resolver := TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
				providers.Add(1)
				return "", errors.New("synthetic-provider-private")
			})
			args := append([]string{"--@complete=" + tc.kind}, tc.args...)
			out, diagnostic, err := runRuntimeCompletion(t, t.Context(), completionProfileConfig(), "http://127.0.0.1:1", resolver, args...)
			if err != nil || diagnostic != "" || providers.Load() != 0 {
				t.Fatalf("fresh cache consulted credentials: %v", err)
			}
			if tc.contains == "" {
				if out != "" {
					t.Fatal("empty/deleted cache emitted candidates")
				}
			} else if !strings.Contains(out, tc.contains) {
				t.Fatal("completion selected the wrong profile cache")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("completion changed cache state")
			}
		})
	}
}

func TestRuntimeCompletionLiveFallbackUsesSelectedProfileAndDeadline(t *testing.T) {
	for _, tc := range []struct {
		name, profile, kind, method string
		args                        []string
	}{
		{"default", "default", "channel", "conversations.list", nil},
		{"selected workspace", "Other", "channel", "conversations.list", []string{"--workspace=oThEr"}},
		{"selected user workspace", "Other", "user", "users.list", []string{"--workspace=Other"}},
		{"key target", "Other", "config_value", "conversations.list", []string{"--workspace=default", "--", "workspaces.Other.default_channel"}},
		{"dotted key", "a.b", "config_value", "conversations.list", []string{"--", "workspaces.a.b.default_channel"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			var providers, requests atomic.Int32
			resolver := TokenResolverFunc(func(ctx context.Context, profile config.WorkspaceProfile) (string, error) {
				providers.Add(1)
				if profile.Name != tc.profile {
					t.Error("provider selected the wrong profile")
				}
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > 5*time.Second {
					t.Error("completion provider has no bounded deadline")
				}
				return "xoxb-synthetic-" + tc.profile, nil
			})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Method != http.MethodPost || r.URL.Path != "/api/"+tc.method || r.URL.RawQuery != "" || r.Form.Get("token") != "xoxb-synthetic-"+tc.profile || r.Form.Get("limit") != "100" {
					t.Error("live completion changed method/path/limit/profile authentication")
				}
				w.Header().Set("Content-Type", "application/json")
				if tc.kind == "user" {
					_, _ = io.WriteString(w, `{"ok":true,"members":[{"id":"UREMOTE","name":"remote"}]}`)
				} else {
					_, _ = io.WriteString(w, `{"ok":true,"channels":[{"id":"CREMOTE","name":"remote"}]}`)
				}
			}))
			defer server.Close()
			args := append([]string{"--@complete=" + tc.kind}, tc.args...)
			out, diagnostic, err := runRuntimeCompletion(t, t.Context(), completionProfileConfig(), server.URL, resolver, args...)
			if err != nil || diagnostic != "" || providers.Load() != 1 || requests.Load() != 1 || !strings.Contains(out, "REMOTE") {
				t.Fatalf("live fallback failed or replayed: providers=%d requests=%d err=%v", providers.Load(), requests.Load(), err)
			}
		})
	}
}

func TestRuntimeCompletionProviderFailuresAndCancellationStayQuiet(t *testing.T) {
	for _, mode := range []string{"error", "empty", "blank", "canceled caller", "deadline", "cancel before return"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "canceled caller" {
				cancel()
			}
			var providers, requests atomic.Int32
			var observedDeadline atomic.Bool
			resolver := TokenResolverFunc(func(ctx context.Context, _ config.WorkspaceProfile) (string, error) {
				providers.Add(1)
				switch mode {
				case "error":
					return "", errors.New("synthetic-provider-private")
				case "empty":
					return "", nil
				case "blank":
					return " \t ", nil
				case "deadline":
					select {
					case <-ctx.Done():
						observedDeadline.Store(true)
						return "", ctx.Err()
					case <-time.After(time.Second):
						return "", errors.New("provider did not receive deadline")
					}
				case "cancel before return":
					cancel()
					return "xoxb-synthetic-late", nil
				default:
					return "xoxb-synthetic-unexpected", nil
				}
			})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); w.WriteHeader(500) }))
			defer server.Close()
			out, diagnostic, err := runRuntimeCompletion(t, ctx, completionProfileConfig(), server.URL, resolver, "--@complete=channel", "--timeout=100ms")
			wantProviders := int32(1)
			if mode == "canceled caller" {
				wantProviders = 0
			}
			if err != nil || out != "" || diagnostic != "" || providers.Load() != wantProviders || requests.Load() != 0 {
				t.Fatal("failed/canceled provider emitted data, reached Slack or retried")
			}
			if mode == "deadline" && !observedDeadline.Load() {
				t.Fatal("provider ignored completion command deadline")
			}
		})
	}
}

func TestRuntimeCompletionAPICallsHonorCommandDeadline(t *testing.T) {
	for _, kind := range []string{"channel", "user", "config_value"} {
		t.Run(kind, func(t *testing.T) {
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			var requests atomic.Int32
			canceled := make(chan struct{}, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if _, err := io.Copy(io.Discard, r.Body); err != nil {
					t.Error("completion request body could not be read")
				}
				select {
				case <-r.Context().Done():
					select {
					case canceled <- struct{}{}:
					default:
					}
				case <-time.After(time.Second):
					w.WriteHeader(500)
				}
			}))
			defer server.Close()
			resolver := TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) { return "xoxb-synthetic-deadline", nil })
			args := []string{"--@complete=" + kind, "--timeout=100ms"}
			if kind == "config_value" {
				args = append(args, "--", "workspaces.default.default_channel")
			}
			out, diagnostic, err := runRuntimeCompletion(t, t.Context(), completionProfileConfig(), server.URL, resolver, args...)
			if err != nil || out != "" || diagnostic != "" || requests.Load() != 1 {
				t.Fatal("timed out API completion emitted data or retried")
			}
			select {
			case <-canceled:
			case <-time.After(time.Second):
				t.Fatal("completion detached the API call from the command deadline")
			}
		})
	}
}

func TestRuntimeCompletionBadCacheFailuresPreserveStateAndRecover(t *testing.T) {
	for _, resource := range []string{"channels", "users"} {
		for _, defect := range []string{"malformed", "stale", "wrong profile"} {
			t.Run(resource+"/"+defect, func(t *testing.T) {
				t.Setenv("XDG_CACHE_HOME", t.TempDir())
				path, err := slackcache.Path("default", resource)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				entry := map[string]any{"profile": "default", "resource": resource, "fetched_at": time.Now().UTC(), "data": map[string]any{resource: []map[string]any{{"id": "COLD", "name": "old"}}}}
				if defect == "stale" {
					entry["fetched_at"] = time.Now().Add(-25 * time.Hour)
				}
				if defect == "wrong profile" {
					entry["profile"] = "Other"
				}
				prior, err := json.Marshal(entry)
				if err != nil {
					t.Fatal(err)
				}
				if defect == "malformed" {
					prior = []byte("{")
				}
				if err := os.WriteFile(path, prior, 0o600); err != nil {
					t.Fatal(err)
				}
				var recovered atomic.Bool
				var providers, requests atomic.Int32
				resolver := TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
					providers.Add(1)
					return "xoxb-synthetic-recovery", nil
				})
				kind, method := "channel", "conversations.list"
				if resource == "users" {
					kind, method = "user", "users.list"
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests.Add(1)
					if r.Method != http.MethodPost || r.URL.Path != "/api/"+method || r.FormValue("token") != "xoxb-synthetic-recovery" {
						t.Error("cache fallback used wrong request/authentication")
					}
					w.Header().Set("Content-Type", "application/json")
					if !recovered.Load() {
						_, _ = io.WriteString(w, `{"ok":false,"error":"missing_scope"}`)
					} else if resource == "users" {
						_, _ = io.WriteString(w, `{"ok":true,"members":[{"id":"URECOVERY","name":"recovered"}]}`)
					} else {
						_, _ = io.WriteString(w, `{"ok":true,"channels":[{"id":"CRECOVERY","name":"recovered"}]}`)
					}
				}))
				defer server.Close()
				out, diagnostic, err := runRuntimeCompletion(t, t.Context(), completionProfileConfig(), server.URL, resolver, "--@complete="+kind)
				if err != nil || out != "" || diagnostic != "" || providers.Load() != 1 || requests.Load() != 1 {
					t.Fatal("bad cache completion failed noisily, reused bad data or retried")
				}
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(prior, after) {
					t.Fatal("failed completion replaced cache state")
				}
				recovered.Store(true)
				out, diagnostic, err = runRuntimeCompletion(t, t.Context(), completionProfileConfig(), server.URL, resolver, "--@complete="+kind)
				if err != nil || diagnostic != "" || !strings.Contains(out, "RECOVERY") || providers.Load() != 2 || requests.Load() != 2 {
					t.Fatal("explicit completion recovery failed or replayed")
				}
				after, err = os.ReadFile(path)
				if err != nil || !bytes.Equal(prior, after) {
					t.Fatal("successful completion unexpectedly rewrote cache")
				}
			})
		}
	}
}
