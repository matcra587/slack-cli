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

	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	"github.com/matcra587/slack-cli/internal/config"
)

type authResultFailingWriter struct {
	kind   string
	calls  int
	prefix bytes.Buffer
}

func (w *authResultFailingWriter) Write(p []byte) (int, error) {
	w.calls++
	switch w.kind {
	case "short":
		return 0, nil
	case "partial":
		n := len(p) / 2
		_, _ = w.prefix.Write(p[:n])
		return n, nil
	default:
		return 0, errors.New("synthetic-private-auth-output")
	}
}

func TestAuthResultWriteFailuresKeepAppliedStateWithoutReplay(t *testing.T) {
	for _, tc := range []struct {
		name, mode, failure string
		args                []string
		apiCalls, sets      int
	}{
		{"login", "json", "error", []string{"auth", "login", "--workspace-name=other", "--token-stdin", "--force"}, 1, 1},
		{"logout", "compact", "short", []string{"auth", "logout", "other", "--keep-token"}, 0, 0},
		{"switch", "human", "partial", []string{"auth", "switch", "other"}, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := authTestConfig()
			profile := cfg.Workspaces["other"]
			profile.DefaultChannel = "C_KEEP"
			cfg.Workspaces["other"] = profile
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := config.SaveFile(path, cfg); err != nil {
				t.Fatal(err)
			}
			store := &authLoginStore{CredentialStore: config.NewMemoryCredentialStore()}
			const previous = `{"access_token":"xoxp-synthetic-prior-output"}`
			if err := store.CredentialStore.Set("slack-cli", "other", previous); err != nil {
				t.Fatal(err)
			}
			var calls, resolutions atomic.Int32
			client := &http.Client{Transport: authStatusTransport(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Method != "POST" || r.URL.Path != "/api/auth.test" || r.PostForm.Get("token") != "xoxp-synthetic-new-output" || len(r.PostForm) != 1 {
					t.Error("output failure changed validation request or authentication")
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"ok":true,"team_id":"T_NEW","team":"New","user_id":"U_NEW"}`)), Request: r}, nil
			})}
			writer := &authResultFailingWriter{kind: tc.failure}
			var diagnostic bytes.Buffer
			root, runtime := NewRootCommandWithRuntime(WithConfig(cfg), WithConfigPath(path), WithCredentialStore(store), WithSlackBaseURL("http://127.0.0.1:1"), WithTTY(false), WithIO(strings.NewReader("xoxp-synthetic-new-output\n"), writer, &diagnostic),
				func(runtime *RootRuntime) { runtime.HTTPClient = client },
				WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
					resolutions.Add(1)
					return "", errors.New("synthetic-provider-must-not-run")
				})))
			root.SetArgs(append(tc.args, "--output="+tc.mode, "--color=never"))
			runErr := root.ExecuteContext(t.Context())
			if runtime.CancelTimeout != nil {
				runtime.CancelTimeout()
			}
			failure, ok := errors.AsType[clioutput.CommandError](runErr)
			if !ok || failure.CLIError.ExitCode != 8 || writer.calls != 1 || store.sets != tc.sets || calls.Load() != int32(tc.apiCalls) || resolutions.Load() != 0 {
				t.Fatal("auth output failure lost exit 8, repeated output or repeated the operation")
			}
			if strings.Count(diagnostic.String(), "output_write_failed") != 1 || !strings.Contains(diagnostic.String(), "exit_code=8") {
				t.Error("auth output failure lost safe diagnostic")
			}
			for _, secret := range []string{"synthetic-private-auth-output", "xoxp-synthetic-new-output", "xoxp-synthetic-prior-output"} {
				if strings.Contains(diagnostic.String()+writer.prefix.String()+runErr.Error(), secret) {
					t.Error("auth output failure exposed sensitive data")
				}
			}
			loaded, err := config.LoadFile(path)
			if err != nil || loaded.Workspaces["other"].DefaultChannel != "C_KEEP" {
				t.Fatal("auth output failure lost saved config or unrelated preference")
			}
			stored, err := store.Get("slack-cli", "other")
			if err != nil {
				t.Fatal("auth output failure removed saved credentials")
			}
			wantDefault := "default"
			switch tc.name {
			case "login":
				payload, err := config.DecodeCredential(stored)
				if err != nil || payload.AccessToken != "xoxp-synthetic-new-output" || loaded.Workspaces["other"].TeamID != "T_NEW" {
					t.Error("output failure rolled back completed login")
				}
			case "logout":
				if loaded.Workspaces["other"].TokenRef != "" || stored != previous {
					t.Error("output failure undid logout or ignored keep-token")
				}
			case "switch":
				wantDefault = "other"
				if stored != previous {
					t.Error("workspace switch changed credentials")
				}
			}
			if loaded.DefaultWorkspace != wantDefault {
				t.Error("output failure lost the applied default workspace")
			}
			var recovered, healthyDiagnostic bytes.Buffer
			reader := NewRootCommand(WithConfigPath(path), WithCredentialStore(store), WithIO(strings.NewReader(""), &recovered, &healthyDiagnostic), WithTTY(false))
			reader.SetArgs([]string{"config", "get", "default_workspace", "--output=json", "--color=never"})
			var frame struct {
				OK   bool
				Data struct{ Value string }
			}
			if err := reader.ExecuteContext(t.Context()); err != nil || healthyDiagnostic.Len() != 0 || json.Unmarshal(recovered.Bytes(), &frame) != nil || !frame.OK || frame.Data.Value != wantDefault {
				t.Error("healthy CLI could not read back state after failed output")
			}
		})
	}
}
