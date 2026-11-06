//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/matcra587/slack-cli/internal/config"
)

func TestBinaryCredentialProviderFailuresAreBoundedAndRecoverable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses an isolated POSIX provider executable")
	}
	binary := buildSlackBinary(t)
	for _, tc := range []struct {
		name string
		exit int
	}{{"fail", 1}, {"wait", 7}, {"empty", 1}} {
		t.Run(tc.name, func(t *testing.T) {
			isolated := t.TempDir()
			bin := filepath.Join(isolated, "bin")
			if err := os.Mkdir(bin, 0o700); err != nil {
				t.Fatal(err)
			}
			script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$SLICK_OP_FIXTURE_CALLS\"\ncase \"$SLICK_OP_FIXTURE_MODE\" in\nfail) printf '%s\\n' 'synthetic-private-provider-error' >&2; exit 1;;\nwait) exec /bin/sleep 30;;\nempty) exit 0;;\nsuccess) exec /bin/cat \"$SLICK_OP_FIXTURE_RESULT\";;\nesac\n"
			if err := os.WriteFile(filepath.Join(bin, "op"), []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			callsFile := filepath.Join(isolated, "provider-calls.txt")
			resultFile := filepath.Join(isolated, "provider-result.json")
			const token = "xoxp-synthetic-recovered-provider"
			encoded, err := config.EncodeCredential(config.CredentialPayload{AccessToken: token})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(resultFile, []byte(encoded), 0o600); err != nil {
				t.Fatal(err)
			}
			configPath := filepath.Join(isolated, "config.toml")
			cfg := "schema_version=\"1\"\ndefault_workspace=\"fixture\"\n[workspaces.fixture]\nname=\"fixture\"\nteam_id=\"T123\"\ntoken_type=\"user\"\ntoken=\"op://fixture/item/token\"\n"
			if err := os.WriteFile(configPath, []byte(cfg), 0o600); err != nil {
				t.Fatal(err)
			}
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Method != http.MethodPost || r.PostForm.Get("token") != token || r.URL.RawQuery != "" {
					t.Error("wrong method or synthetic authentication after recovery")
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/api/auth.test":
					w.Header().Set("X-OAuth-Scopes", "reactions:write")
					writeJSON(w, `{"ok":true,"user_id":"U1"}`)
				case "/api/conversations.info":
					writeJSON(w, `{"ok":true,"channel":{"id":"C123","name":"fixture"}}`)
				case "/api/reactions.add":
					writeJSON(w, `{"ok":true}`)
				default:
					t.Errorf("unexpected path: %s", r.URL.Path)
					writeJSON(w, `{"ok":false,"error":"method_not_found"}`)
				}
			}))
			defer server.Close()
			run := func(mode string) (string, string, error) {
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				timeout := "2s"
				if mode == "wait" {
					timeout = "100ms"
				}
				cmd := exec.CommandContext(ctx, binary, "react", "add", "--channel=C123", "--timestamp=1746284582.123456", "--emoji=rocket", "--output=json", "--no-throttle", "--timeout="+timeout)
				cmd.Env = []string{"HOME=" + isolated, "PATH=" + bin, "SLICK_CONFIG=" + configPath, "SLACK_CLI_BASE_URL=" + server.URL, "SLICK_OP_FIXTURE_MODE=" + mode, "SLICK_OP_FIXTURE_CALLS=" + callsFile, "SLICK_OP_FIXTURE_RESULT=" + resultFile}
				var out, diagnostic bytes.Buffer
				cmd.Stdout, cmd.Stderr = &out, &diagnostic
				err := cmd.Run()
				if ctx.Err() != nil {
					t.Fatal("provider command exceeded the outer test timeout")
				}
				return out.String(), diagnostic.String(), err
			}
			stdout, stderr, err := run(tc.name)
			if exitCode(err) != tc.exit || stderr != "" || strings.Contains(stdout, token) || strings.Contains(stdout, "synthetic-private-provider-error") || requests.Load() != 0 {
				t.Fatalf("unsafe provider failure or wrong exit: err=%v stdout=%s stderr=%s", err, stdout, stderr)
			}
			var frame map[string]json.RawMessage
			if err := json.Unmarshal([]byte(stdout), &frame); err != nil {
				t.Fatal(err)
			}
			if len(frame) != 4 || string(frame["ok"]) != "false" || string(frame["data"]) != "null" {
				t.Fatal("provider failure changed the common machine envelope")
			}
			after, err := os.ReadFile(configPath)
			if err != nil || string(after) != cfg {
				t.Fatal("provider failure rewrote existing configuration")
			}
			calls, err := os.ReadFile(callsFile)
			if err != nil || string(calls) != "read op://fixture/item/token\n" {
				t.Fatal("wrong provider method/reference or unexpected retry")
			}
			stdout, stderr, err = run("success")
			if err != nil || stderr != "" || !strings.Contains(stdout, `"ok":true`) || strings.Contains(stdout, token) || requests.Load() != 3 {
				t.Fatalf("explicit provider recovery failed: err=%v stdout=%s stderr=%s", err, stdout, stderr)
			}
			calls, err = os.ReadFile(callsFile)
			if err != nil || string(calls) != "read op://fixture/item/token\nread op://fixture/item/token\n" {
				t.Fatal("recovery retried provider access unexpectedly")
			}
		})
	}
}
