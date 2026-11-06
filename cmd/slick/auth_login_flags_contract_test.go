package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	"github.com/matcra587/slack-cli/internal/config"
)

func TestAuthLoginFlagValidationAndPreviewAvoidSideEffects(t *testing.T) {
	t.Setenv("SYNTHETIC_PREVIEW_TOKEN", "")
	t.Setenv("xoxp-synthetic-hidden", "")
	for _, tc := range []struct {
		name, mode  string
		args        []string
		exit, reads int
	}{
		{"stdin preview", "json", []string{"--token-stdin", "--dry-run"}, 0, 0},
		{"file preview", "compact", []string{"-f", "synthetic-missing-token-file", "-n"}, 0, 0},
		{"env preview", "human", []string{"-e", "SYNTHETIC_PREVIEW_TOKEN", "--dry-run"}, 0, 0},
		{"OAuth preview", "json", []string{"--method=oauth", "--oauth-client-id=synthetic-client", "--oauth-callback-port=0", "--dry-run"}, 0, 0},
		{"OAuth short preview", "compact", []string{"-m", "oauth", "-C", "synthetic-client", "-r", "http://127.0.0.1:0/callback", "-n"}, 0, 0},
		{"missing source", "json", nil, 4, 0},
		{"missing workspace", "json", []string{"--workspace-name=", "-s"}, 4, 0},
		{"TTY preview needs workspace", "json", []string{"--workspace-name=", "--dry-run"}, 4, 0},
		{"stdin file conflict", "json", []string{"-s", "-f", "unused"}, 4, 0},
		{"file env conflict", "compact", []string{"--token-file=unused", "--token-env=SYNTHETIC_PREVIEW_TOKEN"}, 4, 0},
		{"token client flag", "json", []string{"-s", "-C", "synthetic-client"}, 4, 0},
		{"token redirect flag", "json", []string{"-s", "-r", "http://127.0.0.1:0/callback"}, 4, 0},
		{"token callback flag", "human", []string{"-s", "-p", "0"}, 4, 0},
		{"OAuth stdin flag", "json", []string{"-m", "oauth", "-C", "synthetic-client", "-p", "0", "-s"}, 4, 0},
		{"OAuth file flag", "compact", []string{"--method=oauth", "--oauth-client-id=synthetic-client", "--oauth-callback-port=0", "--token-file=unused"}, 4, 0},
		{"OAuth env flag", "json", []string{"--method=oauth", "--oauth-client-id=synthetic-client", "--oauth-callback-port=0", "--token-env=SYNTHETIC_PREVIEW_TOKEN"}, 4, 0},
		{"conflicting redirects", "json", []string{"-m", "oauth", "-C", "synthetic-client", "-r", "http://127.0.0.1:0/callback", "-p", "0"}, 4, 0},
		{"missing client", "json", []string{"-m", "oauth", "-p", "0"}, 4, 0},
		{"blank client", "compact", []string{"-m", "oauth", "-C", " ", "-p", "0"}, 4, 0},
		{"high port", "json", []string{"-m", "oauth", "-C", "synthetic-client", "-p", "65536"}, 4, 0},
		{"bad port", "json", []string{"-m", "oauth", "-C", "synthetic-client", "-p", "invalid"}, 4, 0},
		{"nonlocal redirect", "human", []string{"-m", "oauth", "-C", "synthetic-client", "-r", "http://example.invalid:1/callback"}, 4, 0},
		{"redirect credentials", "json", []string{"-m", "oauth", "-C", "synthetic-client", "-r", "http://user:synthetic-private-login-option-data@127.0.0.1:0/callback"}, 4, 0},
		{"redirect parse diagnostic", "json", []string{"-m", "oauth", "-C", "synthetic-client", "-r", "http://127.0.0.1:synthetic-private-login-option-data/callback"}, 4, 0},
		{"redirect fragment", "json", []string{"-m", "oauth", "-C", "synthetic-client", "-r", "http://127.0.0.1:0/callback#private"}, 4, 0},
		{"unknown method", "json", []string{"--method=invalid", "-s"}, 4, 0},
		{"stdin error", "json", []string{"--token-stdin"}, 4, 1},
		{"empty token", "compact", []string{"--token-stdin"}, 4, 1},
		{"malformed token", "json", []string{"--token-stdin"}, 4, 1},
		{"missing token file", "json", []string{"--token-file={missing-file}"}, 4, 0},
		{"empty env", "human", []string{"--token-env=SYNTHETIC_PREVIEW_TOKEN"}, 4, 0},
		{"env value instead of name", "json", []string{"--token-env=xoxp-synthetic-hidden"}, 4, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := authTestConfig()
			path := filepath.Join(t.TempDir(), "untouched.toml")
			prior := []byte("synthetic existing destination")
			if err := os.WriteFile(path, prior, 0o600); err != nil {
				t.Fatal(err)
			}
			store := &authLoginAccessProbe{}
			input := &authLoginReadProbe{}
			switch tc.name {
			case "empty token":
				input.value = strings.NewReader("")
			case "malformed token":
				input.value = strings.NewReader("xoxe-1-synthetic-refresh\n")
			}
			var out, diagnostic bytes.Buffer
			var calls, opened atomic.Int32
			root := NewRootCommand(WithConfig(cfg), WithConfigPath(path), WithCredentialStore(store), WithIO(input, &out, &diagnostic), WithTTY(tc.name == "TTY preview needs workspace"), WithSlackBaseURL("http://127.0.0.1:1"), WithOAuthTimeout(20*time.Millisecond), WithURLOpener(func(string) error {
				opened.Add(1)
				return errors.New("synthetic-private-login-option-data")
			}), func(runtime *RootRuntime) {
				runtime.HTTPClient = &http.Client{Transport: authStatusTransport(func(r *http.Request) (*http.Response, error) {
					calls.Add(1)
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":true,"user_id":"U_FIXTURE","team_id":"T_FIXTURE"}`)), Request: r}, nil
				})}
			})
			args := append([]string{"auth", "login", "--workspace-name=Other", "--output=" + tc.mode, "--color=never"}, tc.args...)
			for i := range args {
				args[i] = strings.ReplaceAll(args[i], "{missing-file}", filepath.Join(filepath.Dir(path), "synthetic-private-login-option-data"))
			}
			root.SetArgs(args)
			err := root.ExecuteContext(t.Context())
			gotExit := 0
			if failure, ok := errors.AsType[clioutput.CommandError](err); ok {
				gotExit = failure.CLIError.ExitCode
			} else if err != nil {
				t.Fatalf("login option validation failed outside the handler: %v", err)
			}
			if gotExit != tc.exit {
				t.Fatalf("login option exit=%d, want %d", gotExit, tc.exit)
			}
			if (input.reads != 0) != (tc.reads != 0) || store.calls.Load() != 0 || calls.Load() != 0 || opened.Load() != 0 {
				t.Error("invalid options or preview accessed credentials, Slack or browser")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(prior, after) || cfg.DefaultWorkspace != "default" || len(cfg.Workspaces) != 2 {
				t.Error("invalid options or preview changed prior state")
			}
			if strings.Contains(out.String()+diagnostic.String(), "synthetic-private-login-option-data") || strings.Contains(out.String()+diagnostic.String(), "xoxp-synthetic-hidden") {
				t.Error("login option failure exposed a credential or input diagnostic")
			}
			if tc.mode == "human" {
				if (tc.exit == 0 && (out.Len() == 0 || diagnostic.Len() != 0 || !strings.Contains(out.String(), "dry_run=true"))) || (tc.exit != 0 && (out.Len() != 0 || diagnostic.Len() == 0)) {
					t.Error("login options lost human preview/failure streams")
				}
			} else {
				var frame map[string]any
				if json.Unmarshal(out.Bytes(), &frame) != nil || diagnostic.Len() != 0 {
					t.Fatal("login options lost machine JSON or streams")
				}
				data := frame
				if tc.mode == "json" || tc.exit != 0 {
					if frame["ok"] != (tc.exit == 0) || frame["meta"].(map[string]any)["command"] != "auth.login" || frame["meta"].(map[string]any)["exit_code"] != float64(tc.exit) {
						t.Error("login options lost canonical envelope")
					}
					if tc.exit == 0 {
						data = frame["data"].(map[string]any)
					} else if frame["data"] != nil {
						t.Error("invalid login options invented success data")
					}
				}
				if tc.exit == 0 && (data["dry_run"] != true || data["workspace"] != "other" || data["authenticated"] != false) {
					t.Error("login preview lost canonical target or claimed authentication")
				}
			}
		})
	}
}

func TestAuthLoginTokenFlagsImportExactSourcesAndOverrides(t *testing.T) {
	for _, tc := range []struct {
		name, token   string
		args          []string
		old, override bool
		exit          int
	}{
		{"stdin long", "xoxp-synthetic-stdin", []string{"--workspace=default", "--workspace-name= OTHER ", "--token-stdin", "--token-file=", "--method=token", "--team-id=T_OVERRIDE", "--team-name=Override Team", "--force"}, true, true, 0},
		{"stdin short", "xoxb-synthetic-stdin", []string{"-w", "OTHER", "-s", "-m", "token", "-T", "T_OVERRIDE", "-N", "Override Team", "-F"}, true, true, 0},
		{"file long", "xoxe.xoxp-synthetic-file", []string{"-w", "other", "--token-file=$SYNTHETIC_FLAG_FILE", "--team-id=", "--team-name="}, false, false, 0},
		{"file short", "xoxb-synthetic-file", []string{"-w", "other", "-f", "~/token file"}, false, false, 0},
		{"env long", "xoxe.xoxb-synthetic-env", []string{"-w", "other", "--token-env= SYNTHETIC_FLAG_TOKEN ", "--token-stdin=false"}, false, false, 0},
		{"env short", "xoxp-synthetic-env", []string{"-w", "other", "-e", "SYNTHETIC_FLAG_TOKEN", "--auth-method=token", "--force=false"}, false, false, 0},
		{"force false", "xoxp-synthetic-stdin", []string{"-w", "other", "-s", "--force=false"}, true, false, 4},
		{"force removed", "xoxp-synthetic-stdin", []string{"-w", "other", "-s"}, true, false, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			file := filepath.Join(home, "token file")
			t.Setenv("HOME", home)
			t.Setenv("SYNTHETIC_FLAG_FILE", file)
			stdinToken, fileToken, envToken := "xoxb-unselected-stdin", "xoxb-unselected-file", "xoxp-unselected-env"
			switch {
			case strings.HasPrefix(tc.name, "file"):
				fileToken = tc.token
			case strings.HasPrefix(tc.name, "env"):
				envToken = tc.token
			default:
				stdinToken = tc.token
			}
			t.Setenv("SYNTHETIC_FLAG_TOKEN", " \t"+envToken+"\n")
			inputBytes, fileBytes := []byte(stdinToken+"\r\n"), []byte(fileToken+"\r\n")
			if err := os.WriteFile(file, fileBytes, 0o600); err != nil {
				t.Fatal(err)
			}
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
			const old = `{"access_token":"xoxb-synthetic-old"}`
			if tc.old {
				if err := store.Set("slack-cli", "other", old); err != nil {
					t.Fatal(err)
				}
			}
			var calls atomic.Int32
			client := &http.Client{Transport: authStatusTransport(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Method != http.MethodPost || r.URL.Path != "/api/auth.test" || r.URL.RawQuery != "" || len(r.PostForm) != 1 || r.PostForm.Get("token") != tc.token {
					t.Error("login did not validate the selected exact token source")
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":true,"user_id":"U_FIXTURE","team_id":"T_FIXTURE","team":"Fixture Team"}`)), Request: r}, nil
			})}
			args := append([]string{"auth", "login", "--output=json", "--color=never"}, tc.args...)
			out, diagnostic, err := executeAuthRootWithOptions(t, cfg, path, store, "http://127.0.0.1:1", strings.NewReader(string(inputBytes)), false, args, func(runtime *RootRuntime) { runtime.HTTPClient = client })
			gotExit := 0
			if failure, ok := errors.AsType[clioutput.CommandError](err); ok {
				gotExit = failure.CLIError.ExitCode
			} else if err != nil {
				t.Fatalf("login flags failed outside handler: %v", err)
			}
			if gotExit != tc.exit || calls.Load() != 1 || diagnostic != "" {
				t.Fatalf("login flags exit=%d calls=%d, want exit=%d calls=1", gotExit, calls.Load(), tc.exit)
			}
			stored, err := store.Get("slack-cli", "other")
			if err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if tc.exit != 0 {
				if stored != old || !bytes.Equal(before, after) || cfg.Workspaces["other"].TeamID != "T999" {
					t.Error("omitted or false force flag changed existing auth")
				}
			} else {
				credential, err := config.DecodeCredential(stored)
				wantID, wantName := "T_FIXTURE", "Fixture Team"
				if tc.override {
					wantID, wantName = "T_OVERRIDE", "Override Team"
				}
				wantType := config.TokenTypeBot
				if strings.Contains(tc.token, "xoxp-") {
					wantType = config.TokenTypeUser
				}
				profile := cfg.Workspaces["other"]
				if err != nil || credential.AccessToken != tc.token || profile.TeamID != wantID || profile.TeamName != wantName || profile.TokenType != wantType || profile.TokenRef != "keychain:slack-cli/other" || cfg.DefaultWorkspace != "default" || len(cfg.Workspaces) != 2 {
					t.Error("login flags lost token kind, override/removal, canonical identity or unrelated config")
				}
			}
			for _, token := range []string{stdinToken, fileToken, envToken} {
				if strings.Contains(out+diagnostic+string(after), token) {
					t.Error("login flag value escaped credential storage")
				}
			}
			fileAfter, err := os.ReadFile(file)
			if err != nil || !bytes.Equal(fileBytes, fileAfter) {
				t.Error("login modified its token input file")
			}
			var frame map[string]any
			if json.Unmarshal([]byte(out), &frame) != nil || frame["ok"] != (tc.exit == 0) || frame["meta"].(map[string]any)["command"] != "auth.login" || frame["meta"].(map[string]any)["workspace"] != "other" {
				t.Error("login flags lost canonical machine result metadata")
			}
		})
	}
}

type authLoginReadProbe struct {
	reads int
	value *strings.Reader
}

func (r *authLoginReadProbe) Read(p []byte) (int, error) {
	r.reads++
	if r.value != nil {
		return r.value.Read(p)
	}
	return 0, errors.New("synthetic-private-login-option-data")
}

type authLoginAccessProbe struct{ calls atomic.Int32 }

func (s *authLoginAccessProbe) Get(string, string) (string, error) {
	s.calls.Add(1)
	return "", errors.New("synthetic-private-login-option-data")
}

func (s *authLoginAccessProbe) Set(string, string, string) error {
	s.calls.Add(1)
	return errors.New("synthetic-private-login-option-data")
}

func (s *authLoginAccessProbe) Delete(string, string) error {
	s.calls.Add(1)
	return errors.New("synthetic-private-login-option-data")
}
