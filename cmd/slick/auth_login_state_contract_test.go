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

func TestAuthLoginPreservesStateOnPersistenceFailures(t *testing.T) {
	for _, tc := range []struct {
		name, mode   string
		old, blocked bool
		exit         int
	}{
		{"new profile success", "compact", false, false, 0},
		{"forced success", "human", true, false, 0},
		{"new save failure", "json", false, true, 5},
		{"replace save failure", "json", true, true, 5},
		{"snapshot failure", "json", true, false, 1},
		{"write failure", "json", true, false, 1},
		{"partial write failure", "json", true, false, 1},
		{"new partial write failure", "json", false, false, 1},
		{"restore failure", "json", true, true, 5},
		{"cleanup failure", "json", false, true, 5},
		{"canceled", "json", true, false, 6},
		{"deadline", "json", true, false, 7},
		{"malformed config", "json", false, false, 4},
		{"missing credential", "json", false, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const private = "synthetic-private-login-data"
			cfg := authTestConfig()
			profile := cfg.Workspaces["other"]
			profile.Aliases = map[string]string{"release": "C_KEEP"}
			cfg.Workspaces["other"] = profile
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := config.SaveFile(path, cfg); err != nil {
				t.Fatal(err)
			}
			if tc.name == "malformed config" {
				if err := os.WriteFile(path, []byte("broken = [\""+private+"\"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			prior, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			target, argument := "New", "New"
			if tc.old || tc.name == "missing credential" {
				target, argument = "other", " OTHER "
			}
			store := &authLoginStore{CredentialStore: config.NewMemoryCredentialStore(), fault: tc.name}
			oldSecret := "{\n  \"access_token\": \"xoxb-synthetic-old\"\n}"
			if tc.old {
				if err := store.CredentialStore.Set("slack-cli", target, oldSecret); err != nil {
					t.Fatal(err)
				}
			}
			savePath := path
			if tc.blocked {
				savePath = t.TempDir()
			}
			var calls atomic.Int32
			client := &http.Client{Transport: authStatusTransport(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Method != http.MethodPost || r.URL.Path != "/api/auth.test" || r.URL.RawQuery != "" || r.PostForm.Get("token") != "xoxp-synthetic-new" || len(r.PostForm) != 1 {
					t.Error("login changed validation request or authentication")
				}
				body := `{"ok":true,"team_id":"T_NEW","team":"New Team","user_id":"U_ACTOR","access_token":"` + private + `"}`
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
			options := []RootOption{WithConfig(cfg), WithConfigPath(savePath), WithCredentialStore(store), WithSlackBaseURL("http://127.0.0.1:1"), WithTTY(false), WithIO(strings.NewReader("xoxp-synthetic-new\n"), &out, &diagnostic), func(runtime *RootRuntime) { runtime.HTTPClient = client }}
			if tc.name == "malformed config" {
				options = options[1:]
			}
			root := NewRootCommand(options...)
			args := []string{"auth", "login", "--workspace-name=" + argument, "--token-stdin", "--output=" + tc.mode, "--color=never"}
			if tc.old {
				args = append(args, "--force")
			}
			root.SetArgs(args)
			err = root.ExecuteContext(ctx)
			failure, failed := errors.AsType[clioutput.CommandError](err)
			gotExit := 0
			if failed {
				gotExit = failure.CLIError.ExitCode
			} else if err != nil {
				t.Fatalf("login did not reach its handler: %v", err)
			}
			if gotExit != tc.exit {
				t.Fatalf("login exit=%d, want %d", gotExit, tc.exit)
			}
			wantCalls := int32(1)
			if tc.name == "canceled" || tc.name == "deadline" || tc.name == "malformed config" {
				wantCalls = 0
			}
			if calls.Load() != wantCalls {
				t.Error("login validated unexpectedly or repeated the API request")
			}
			wantSets := 1
			if tc.exit != 0 && tc.old {
				wantSets = 2 // one attempted write and one snapshot restoration
			}
			switch tc.name {
			case "snapshot failure", "canceled", "deadline", "malformed config":
				wantSets = 0
			}
			if store.sets != wantSets {
				t.Error("login performed premature or repeated credential writes")
			}
			incomplete := tc.name == "restore failure" || tc.name == "cleanup failure"
			stored, storedErr := store.CredentialStore.Get("slack-cli", target)
			if tc.exit == 0 || incomplete {
				payload, err := config.DecodeCredential(stored)
				if storedErr != nil || err != nil || payload.AccessToken != "xoxp-synthetic-new" || strings.Contains(stored, private) {
					t.Error("login lost the requested credential or persisted unreviewed Slack fields")
				}
			} else if tc.old {
				if storedErr != nil || stored != oldSecret {
					t.Error("failed login did not preserve the exact prior credential")
				}
			} else if !errors.Is(storedErr, config.ErrCredentialNotFound) {
				t.Error("failed login left a new orphan credential")
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if tc.exit != 0 {
				current, err := json.Marshal(cfg)
				if err != nil || !bytes.Equal(prior, current) || !bytes.Equal(before, after) {
					t.Error("failed login changed prior config")
				}
				if incomplete && failure.CLIError.Details["credential_restore_failed"] != true {
					t.Error("login falsely reported complete credential restoration")
				}
			} else {
				loaded, err := config.LoadFile(path)
				if err != nil || !reflect.DeepEqual(loaded, cfg) || loaded.DefaultWorkspace != "default" || loaded.Workspaces[target].TeamID != "T_NEW" || loaded.Workspaces[target].TokenType != config.TokenTypeUser || loaded.Workspaces[target].TokenRef != "keychain:slack-cli/"+target {
					t.Error("login did not publish the persisted canonical profile")
				}
				if !reflect.DeepEqual(loaded.Workspaces["default"], authTestConfig().Workspaces["default"]) || (target == "other" && loaded.Workspaces[target].Aliases["release"] != "C_KEEP") {
					t.Error("login changed unrelated settings")
				}
				if strings.Contains(string(after), "xoxp-synthetic-new") || strings.Contains(string(after), private) {
					t.Error("login persisted credential material in config")
				}
			}
			for _, secret := range []string{private, "xoxp-synthetic-new", "xoxb-synthetic-old"} {
				if strings.Contains(out.String()+diagnostic.String(), secret) {
					t.Error("login exposed credential or provider diagnostic data")
				}
			}
			if tc.mode == "human" {
				if out.Len() == 0 || diagnostic.Len() != 0 {
					t.Error("login lost human result streams")
				}
			} else {
				var frame map[string]any
				if json.Unmarshal(out.Bytes(), &frame) != nil || diagnostic.Len() != 0 {
					t.Fatal("login lost machine output or streams")
				}
				data := frame
				if tc.mode == "json" || tc.exit != 0 {
					if frame["ok"] != (tc.exit == 0) || frame["meta"].(map[string]any)["command"] != "auth.login" || frame["meta"].(map[string]any)["exit_code"] != float64(tc.exit) {
						t.Error("login lost canonical envelope")
					}
					if tc.exit == 0 {
						if frame["meta"].(map[string]any)["workspace"] != target {
							t.Error("login metadata names the old workspace")
						}
						data = frame["data"].(map[string]any)
					} else if frame["data"] != nil {
						t.Error("failed login invented success data")
					}
				}
				if tc.exit == 0 && (data["workspace"] != target || data["authenticated"] != true) {
					t.Error("login lost canonical success result")
				}
			}
			if tc.name == "new save failure" || tc.name == "replace save failure" {
				_, _, err := executeAuthRootWithOptions(t, cfg, path, store, "http://127.0.0.1:1", strings.NewReader("xoxp-synthetic-new\n"), false, args, func(runtime *RootRuntime) { runtime.HTTPClient = client })
				if err != nil || calls.Load() != wantCalls+1 || store.sets != wantSets+1 || cfg.Workspaces[target].TeamID != "T_NEW" {
					t.Error("explicit login recovery failed or replayed a write")
				}
			}
		})
	}
}

type authLoginStore struct {
	config.CredentialStore
	fault string
	sets  int
}

func (s *authLoginStore) Get(service, user string) (string, error) {
	if s.fault == "snapshot failure" {
		return "", errors.New("synthetic-private-login-data")
	}
	return s.CredentialStore.Get(service, user)
}

func (s *authLoginStore) Set(service, user, secret string) error {
	s.sets++
	if (s.fault == "write failure" && s.sets == 1) || (s.fault == "restore failure" && s.sets == 2) {
		return errors.New("synthetic-private-login-data")
	}
	if err := s.CredentialStore.Set(service, user, secret); err != nil {
		return err
	}
	if (s.fault == "partial write failure" || s.fault == "new partial write failure") && s.sets == 1 {
		return errors.New("synthetic-private-login-data")
	}
	return nil
}

func (s *authLoginStore) Delete(service, user string) error {
	if s.fault == "cleanup failure" {
		return errors.New("synthetic-private-login-data")
	}
	return s.CredentialStore.Delete(service, user)
}
