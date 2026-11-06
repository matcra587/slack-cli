package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	"github.com/matcra587/slack-cli/internal/config"
)

func TestAuthLogoutPreservesStateAndPreferences(t *testing.T) {
	for _, tc := range []struct {
		name, mode, flag string
		exit             int
	}{
		{"canonical and disabled preference", "json", "", 0},
		{"keep long", "human", "--keep-token", 0},
		{"keep short", "compact", "-K", 0},
		{"explicit false", "json", "--keep-token=false", 0},
		{"last profile", "json", "", 0},
		{"fallback", "json", "", 0},
		{"preview", "compact", "--dry-run", 0},
		{"save failure", "json", "", 5},
		{"canceled", "json", "", 6},
		{"deadline", "compact", "", 7},
		{"revoke failure", "json", "", 0},
		{"delete failure", "json", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const private = "synthetic-private-logout-data"
			cfg := authTestConfig()
			profile := cfg.Workspaces["default"]
			enabled := strings.HasPrefix(tc.name, "keep")
			profile.Attribution.Enabled = &enabled
			cfg.Workspaces["default"] = profile
			if tc.name == "last profile" || tc.name == "fallback" {
				profile.Attribution.Enabled = nil
				cfg.Workspaces["default"] = profile
			}
			switch tc.name {
			case "last profile":
				delete(cfg.Workspaces, "other")
			case "fallback":
				cfg.Workspaces["Alpha"] = config.WorkspaceProfile{Name: "Alpha", DefaultChannel: "C_KEEP"}
			}
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := config.SaveFile(path, cfg); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			prior, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			savePath := path
			if tc.name == "save failure" {
				savePath = t.TempDir()
			}
			store := &authLogoutStore{CredentialStore: config.NewMemoryCredentialStore()}
			if err := store.Set("slack-cli", "default", "synthetic-stored-payload"); err != nil {
				t.Fatal(err)
			}
			if tc.name == "delete failure" {
				store.failure = errors.New(private)
			}
			var providers, calls atomic.Int32
			client := &http.Client{Transport: authStatusTransport(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Method != http.MethodPost || r.URL.Path != "/api/auth.revoke" || r.URL.RawQuery != "" || r.PostForm.Get("token") != "xoxb-synthetic-logout" || len(r.PostForm) != 1 {
					t.Error("logout changed revoke method, path, parameters or authentication")
				}
				body := `{"ok":true,"revoked":true}`
				if tc.name == "revoke failure" {
					body = `{"ok":false,"error":"` + private + `"}`
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}
			ctx := t.Context()
			switch tc.name {
			case "canceled":
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			case "deadline":
				expired, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Minute))
				defer cancel()
				ctx = expired
			}
			var out, diagnostic bytes.Buffer
			root := NewRootCommand(WithConfig(cfg), WithConfigPath(savePath), WithCredentialStore(store), WithTTY(false), WithIO(strings.NewReader(""), &out, &diagnostic), WithSlackBaseURL("http://127.0.0.1:1"), func(runtime *RootRuntime) { runtime.HTTPClient = client }, WithTokenResolver(TokenResolverFunc(func(_ context.Context, target config.WorkspaceProfile) (string, error) {
				providers.Add(1)
				if target.Name != "default" || target.TokenRef != "keychain:slack-cli/default" {
					t.Error("revoke lost the original canonical profile snapshot")
				}
				return "xoxb-synthetic-logout", nil
			})))
			target := "default"
			if tc.name == "canonical and disabled preference" {
				target = " DEFAULT "
			}
			args := []string{"auth", "logout", target, "--output=" + tc.mode, "--color=never"}
			if tc.flag != "" {
				args = append(args, tc.flag)
			}
			root.SetArgs(args)
			err = root.ExecuteContext(ctx)
			gotExit := 0
			if failure, ok := errors.AsType[clioutput.CommandError](err); ok {
				gotExit = failure.CLIError.ExitCode
			} else if err != nil {
				t.Fatalf("logout failed before its handler: %v", err)
			}
			if gotExit != tc.exit {
				t.Fatalf("logout exit=%d, want %d", gotExit, tc.exit)
			}
			preserve := tc.exit != 0 || tc.name == "preview"
			keep := strings.HasPrefix(tc.name, "keep")
			wantCalls := int32(1)
			if preserve || keep {
				wantCalls = 0
			}
			if providers.Load() != wantCalls || calls.Load() != wantCalls || store.deletes.Load() != wantCalls {
				t.Error("logout performed premature or repeated credential/API mutations")
			}
			_, storedErr := store.Get("slack-cli", "default")
			if (preserve || keep || tc.name == "delete failure") != (storedErr == nil) {
				t.Error("logout lost the expected prior credential state")
			}
			if preserve {
				current, err := json.Marshal(cfg)
				after, readErr := os.ReadFile(path)
				if err != nil || readErr != nil || !bytes.Equal(prior, current) || !bytes.Equal(before, after) {
					t.Error("failed or preview logout changed prior state")
				}
			} else if tc.name == "last profile" {
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) || len(cfg.Workspaces) != 0 || cfg.DefaultWorkspace != "" {
					t.Error("last-profile logout left stale state")
				}
			} else {
				loaded, err := config.LoadFile(path)
				if err != nil || !reflect.DeepEqual(loaded, cfg) {
					t.Fatal("logout did not publish the persisted config")
				}
				if !reflect.DeepEqual(loaded.Workspaces["other"], authTestConfig().Workspaces["other"]) {
					t.Error("logout changed an unrelated profile")
				}
				if tc.name == "fallback" {
					if _, exists := loaded.Workspaces["default"]; exists || loaded.DefaultWorkspace != "Alpha" {
						t.Error("logout lost deterministic fallback selection")
					}
				} else {
					remaining, exists := loaded.Workspaces["default"]
					if !exists || remaining.Attribution.Enabled == nil || *remaining.Attribution.Enabled != enabled || remaining.TokenRef != "" || remaining.TeamID != "" || remaining.TokenType != "" {
						t.Error("logout discarded explicit attribution preference or retained auth fields")
					}
				}
			}
			for _, secret := range []string{private, "xoxb-synthetic-logout", "synthetic-stored-payload"} {
				if strings.Contains(out.String()+diagnostic.String(), secret) {
					t.Error("logout exposed private credential or diagnostic data")
				}
			}
			if tc.mode == "human" {
				if out.Len() == 0 {
					t.Error("logout omitted human result")
				}
			} else {
				var frame map[string]any
				if json.Unmarshal(out.Bytes(), &frame) != nil {
					t.Fatal("logout lost machine JSON")
				}
				data := frame
				if tc.mode == "json" || tc.exit != 0 {
					if frame["ok"] != (tc.exit == 0) || frame["meta"].(map[string]any)["command"] != "auth.logout" || frame["meta"].(map[string]any)["exit_code"] != float64(tc.exit) {
						t.Error("logout lost canonical envelope")
					}
					if tc.exit == 0 {
						data = frame["data"].(map[string]any)
					} else if frame["data"] != nil {
						t.Error("failed logout invented success data")
					}
				}
				if tc.exit == 0 && (data["workspace"] != "default" || (tc.name == "preview" && data["dry_run"] != true)) {
					t.Error("logout lost canonical target or preview marker")
				}
			}
			if tc.name == "save failure" {
				_, _, err := executeAuthRootWithOptions(t, cfg, path, store, "http://127.0.0.1:1", strings.NewReader(""), false, []string{"auth", "logout", "default", "--output=json"}, func(runtime *RootRuntime) { runtime.HTTPClient = client }, WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
					providers.Add(1)
					return "xoxb-synthetic-logout", nil
				})))
				if err != nil || providers.Load() != 1 || calls.Load() != 1 || store.deletes.Load() != 1 || cfg.Workspaces["default"].TokenRef != "" {
					t.Error("explicit logout recovery failed or replayed cleanup")
				}
			}
		})
	}
}

type authLogoutStore struct {
	config.CredentialStore
	deletes atomic.Int32
	failure error
}

func (s *authLogoutStore) Delete(service, user string) error {
	s.deletes.Add(1)
	if service != "slack-cli" || user != "default" {
		return errors.New("unexpected synthetic credential slot")
	}
	if s.failure != nil {
		return s.failure
	}
	return s.CredentialStore.Delete(service, user)
}
