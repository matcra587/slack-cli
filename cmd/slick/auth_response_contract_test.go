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
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	"github.com/matcra587/slack-cli/internal/config"
)

func TestAuthLoginResponseFailuresPreserveState(t *testing.T) {
	const valid = `{"ok":true,"team_id":"T_FIXTURE","team":"Fixture","user_id":"U_FIXTURE","unknown_secret":"synthetic-private-auth-response"}`
	for _, tc := range []struct {
		name, body, mode string
		status, exit     int
	}{
		{"success", valid, "json", 200, 0},
		{"missing acknowledgement", `{"team_id":"T_FIXTURE","user_id":"U_FIXTURE"}`, "json", 200, 5},
		{"null acknowledgement", `{"ok":null,"team_id":"T_FIXTURE","user_id":"U_FIXTURE"}`, "compact", 200, 5},
		{"contradictory acknowledgement", `{"ok":true,"error":"invalid_auth","team_id":"T_FIXTURE","user_id":"U_FIXTURE"}`, "json", 200, 5},
		{"missing error", `{"ok":false,"team_id":"T_FIXTURE","user_id":"U_FIXTURE"}`, "json", 200, 5},
		{"malformed JSON", `{"ok":`, "compact", 200, 5},
		{"wrong JSON type", `{"ok":"yes"}`, "json", 200, 5},
		{"empty response", "", "json", 200, 5},
		{"invalid auth", `{"ok":false,"error":"invalid_auth"}`, "json", 200, 1},
		{"permission denied", `{"ok":false,"error":"no_permission"}`, "compact", 200, 1},
		{"private error", `{"ok":false,"error":"synthetic-private-auth-response"}`, "human", 200, 5},
		{"transport failure", "", "json", 0, 5},
		{"rate without timing", "synthetic-private-auth-response", "json", 429, clioutput.ExitCodeRateLimit},
		{"rate with timing", "synthetic-private-auth-response", "compact", 429, 7},
		{"server failure", "synthetic-private-auth-response", "json", 500, 5},
		{"transport deadline", "", "json", 0, 7},
		{"cancel after validation", valid, "json", 200, 6},
		{"provider cancel", valid, "json", 200, 6},
		{"store cancel", valid, "json", 200, 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := authTestConfig()
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
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			store := &authResponseStore{CredentialStore: config.NewMemoryCredentialStore(), phase: tc.name, cancel: cancel}
			const old = `{"access_token":"xoxb-synthetic-prior"}`
			if err := store.CredentialStore.Set("slack-cli", "other", old); err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			var recovered atomic.Bool
			client := &http.Client{Transport: authStatusTransport(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Method != http.MethodPost || r.URL.Path != "/api/auth.test" || r.URL.RawQuery != "" || len(r.PostForm) != 1 || r.PostForm.Get("token") != "xoxp-synthetic-response" {
					t.Error("auth response handling changed validation request or replayed credentials")
				}
				if tc.name == "transport failure" {
					return nil, errors.New("synthetic-private-auth-response")
				}
				if tc.name == "transport deadline" {
					return nil, context.DeadlineExceeded
				}
				if tc.name == "cancel after validation" {
					cancel()
				}
				body, status := tc.body, tc.status
				if recovered.Load() {
					body, status = valid, 200
				}
				header := make(http.Header)
				if tc.name == "rate with timing" {
					header.Set("Retry-After", "0")
				}
				return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}
			var out, diagnostic bytes.Buffer
			root := NewRootCommand(WithConfig(cfg), WithConfigPath(path), WithCredentialStore(store), WithSlackBaseURL("http://127.0.0.1:1"), WithIO(strings.NewReader("xoxp-synthetic-response\n"), &out, &diagnostic), WithTTY(false), func(runtime *RootRuntime) { runtime.HTTPClient = client })
			budget := "2s"
			if tc.name == "rate with timing" {
				budget = "150ms"
			}
			args := []string{"auth", "login", "-w", "other", "-s", "-F", "--output=" + tc.mode, "--timeout=" + budget, "--color=never"}
			root.SetArgs(args)
			err = root.ExecuteContext(ctx)
			gotExit := 0
			if failure, ok := errors.AsType[clioutput.CommandError](err); ok {
				gotExit = failure.CLIError.ExitCode
			} else if err != nil {
				t.Fatalf("auth response failed outside handler: %v", err)
			}
			if gotExit != tc.exit {
				t.Fatalf("auth response exit=%d, want %d", gotExit, tc.exit)
			}
			wantReads, wantWrites := 0, 0
			switch tc.name {
			case "success":
				wantReads, wantWrites = 1, 1
			case "provider cancel":
				wantReads = 1
			case "store cancel":
				wantReads, wantWrites = 1, 2
			}
			if calls.Load() != 1 || store.reads != wantReads || store.writes != wantWrites {
				t.Error("auth response performed premature or repeated credential writes/requests")
			}
			stored, err := store.CredentialStore.Get("slack-cli", "other")
			if err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if tc.exit != 0 {
				current, err := json.Marshal(cfg)
				if err != nil || !bytes.Equal(prior, current) || !bytes.Equal(before, after) || stored != old {
					t.Error("failed auth validation changed prior credential/config")
				}
			} else {
				payload, err := config.DecodeCredential(stored)
				if err != nil || payload.AccessToken != "xoxp-synthetic-response" || strings.Contains(stored, "synthetic-private-auth-response") || cfg.Workspaces["other"].TeamID != "T_FIXTURE" {
					t.Error("auth success lost reviewed data or persisted an unknown secret")
				}
			}
			for _, secret := range []string{"synthetic-private-auth-response", "xoxp-synthetic-response", "xoxb-synthetic-prior"} {
				if strings.Contains(out.String()+diagnostic.String(), secret) {
					t.Error("auth response exposed credential or untrusted error data")
				}
			}
			if tc.mode == "human" {
				if out.Len() != 0 || diagnostic.Len() == 0 {
					t.Error("auth failure changed human streams")
				}
			} else {
				var frame map[string]any
				if json.Unmarshal(out.Bytes(), &frame) != nil || diagnostic.Len() != 0 {
					t.Fatal("auth response lost machine JSON or streams")
				}
				if frame["ok"] != (tc.exit == 0) || frame["meta"].(map[string]any)["command"] != "auth.login" || frame["meta"].(map[string]any)["exit_code"] != float64(tc.exit) || (tc.exit != 0 && frame["data"] != nil) {
					t.Error("auth response lost canonical failure/success metadata")
				}
			}
			if tc.name == "private error" {
				recovered.Store(true)
				_, _, err := executeAuthRootWithOptions(t, cfg, path, store, "http://127.0.0.1:1", strings.NewReader("xoxp-synthetic-response\n"), false, args, func(runtime *RootRuntime) { runtime.HTTPClient = client })
				if err != nil || calls.Load() != 2 || store.writes != 1 || cfg.Workspaces["other"].TeamID != "T_FIXTURE" {
					t.Error("explicit auth recovery failed or replayed validation")
				}
			}
		})
	}
}

func TestAuthLoginRedirectDoesNotForwardCredential(t *testing.T) {
	var originCalls, forwarded atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		forwarded.Add(1)
		_, _ = io.WriteString(w, `{"ok":true,"team_id":"T_WRONG","user_id":"U_WRONG"}`)
	}))
	defer destination.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originCalls.Add(1)
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api/auth.test" || r.PostForm.Get("token") != "xoxp-synthetic-origin" {
			t.Error("auth redirect test lost its real credential-bearing POST")
		}
		w.Header().Set("Location", destination.URL+"/api/auth.test?synthetic-private-auth-response")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	cfg := authTestConfig()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := config.SaveFile(path, cfg); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	store := config.NewMemoryCredentialStore()
	const old = `{"access_token":"xoxb-synthetic-prior"}`
	if err := store.Set("slack-cli", "other", old); err != nil {
		t.Fatal(err)
	}
	out, diagnostic, err := executeAuthRootWithInput(t, cfg, path, store, origin.URL, strings.NewReader("xoxp-synthetic-origin\n"), false, []string{"auth", "login", "-w", "other", "-s", "-F", "--output=json", "--color=never", "--timeout=2s"})
	failure, ok := errors.AsType[clioutput.CommandError](err)
	if !ok || failure.CLIError.ExitCode != 5 || originCalls.Load() != 1 || forwarded.Load() != 0 {
		t.Fatal("auth redirect forwarded or replayed the credential-bearing POST")
	}
	after, err := os.ReadFile(path)
	stored, storeErr := store.Get("slack-cli", "other")
	if err != nil || !bytes.Equal(before, after) || storeErr != nil || stored != old || cfg.Workspaces["other"].TeamID != "T999" {
		t.Error("refused auth redirect changed prior state")
	}
	var frame map[string]any
	if json.Unmarshal([]byte(out), &frame) != nil || frame["ok"] != false || frame["data"] != nil || diagnostic != "" || strings.Contains(out, "synthetic-private-auth-response") || strings.Contains(out, "xoxp-synthetic-origin") {
		t.Error("refused auth redirect lost safe machine failure output")
	}
}

func TestAuthLogoutReportsUnconfirmedRevocationAndKeepsLocalCleanup(t *testing.T) {
	for _, tc := range []struct{ name, body, warning string }{
		{"revoked", `{"ok":true,"revoked":true}`, ""},
		{"not revoked", `{"ok":true,"revoked":false}`, "did not confirm token revocation"},
		{"missing revoked", `{"ok":true}`, "token revocation failed"},
		{"missing acknowledgement", `{"revoked":true}`, "token revocation failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := authTestConfig()
			profile := cfg.Workspaces["default"]
			enabled := false
			profile.Attribution.Enabled = &enabled
			cfg.Workspaces["default"] = profile
			path := filepath.Join(t.TempDir(), "config.toml")
			store := config.NewMemoryCredentialStore()
			if err := store.Set("slack-cli", "default", `{"access_token":"xoxb-synthetic-revoke"}`); err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			client := &http.Client{Transport: authStatusTransport(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Method != http.MethodPost || r.URL.Path != "/api/auth.revoke" || r.URL.RawQuery != "" || len(r.PostForm) != 1 || r.PostForm.Get("token") != "xoxb-synthetic-revoke" {
					t.Error("revocation acknowledgement handling changed request or enabled test mode")
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body)), Request: r}, nil
			})}
			out, diagnostic, err := executeAuthRootWithOptions(t, cfg, path, store, "http://127.0.0.1:1", strings.NewReader(""), false, []string{"auth", "logout", "default", "--output=json", "--color=never"}, func(runtime *RootRuntime) { runtime.HTTPClient = client })
			if err != nil || calls.Load() != 1 || (tc.warning == "" && diagnostic != "") || (tc.warning != "" && !strings.Contains(diagnostic, tc.warning)) {
				t.Fatalf("logout lost explicit remote outcome or replayed cleanup: %v", err)
			}
			if _, err := store.Get("slack-cli", "default"); !errors.Is(err, config.ErrCredentialNotFound) {
				t.Error("unconfirmed remote revocation prevented local credential cleanup")
			}
			loaded, err := config.LoadFile(path)
			if err != nil || !reflect.DeepEqual(loaded, cfg) || loaded.Workspaces["default"].Attribution.Enabled == nil || loaded.Workspaces["default"].TokenRef != "" || !reflect.DeepEqual(loaded.Workspaces["other"], authTestConfig().Workspaces["other"]) {
				t.Error("logout response handling lost local settings or persistence")
			}
			var frame map[string]any
			if json.Unmarshal([]byte(out), &frame) != nil || frame["ok"] != true || frame["meta"].(map[string]any)["command"] != "auth.logout" || strings.Contains(out+diagnostic, "xoxb-synthetic-revoke") {
				t.Error("logout acknowledgement lost safe success output")
			}
		})
	}
}

type authResponseStore struct {
	config.CredentialStore
	phase         string
	cancel        context.CancelFunc
	reads, writes int
}

func (s *authResponseStore) Get(service, user string) (string, error) {
	s.reads++
	value, err := s.CredentialStore.Get(service, user)
	if s.phase == "provider cancel" {
		s.cancel()
	}
	return value, err
}

func (s *authResponseStore) Set(service, user, value string) error {
	s.writes++
	err := s.CredentialStore.Set(service, user, value)
	if s.phase == "store cancel" && s.writes == 1 {
		s.cancel()
	}
	return err
}
