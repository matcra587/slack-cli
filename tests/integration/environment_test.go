//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestIntegrationRuntimeHomeCannotReadAmbientUpload(t *testing.T) {
	binary := buildSlackBinary(t)
	ambient := t.TempDir()
	t.Setenv("HOME", ambient)
	t.Setenv("USERPROFILE", ambient)
	if err := os.WriteFile(filepath.Join(ambient, "ambient.txt"), []byte("synthetic ambient data"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "file", "upload", "--channel=C123", "--file=~/ambient.txt", "--dry-run", "--no-attribution", "--output=json", "--color=never")
	cmd.Env = append(isolatedRuntimeEnv(t), "SLACK_CLI_CONFIG="+writePipeConfig(t), "SLACK_TEST_TOKEN=xoxb-test")
	output, err := cmd.Output()
	if ctx.Err() != nil || exitCode(err) != 4 {
		t.Fatalf("isolated HOME accessed an ambient upload: %v", err)
	}
	if strings.Contains(string(output), "synthetic ambient data") {
		t.Fatal("isolated runtime exposed an ambient file")
	}
}

func TestIntegrationRunnersIgnoreAmbientConfigTokensAndCaches(t *testing.T) {
	binary := buildSlackBinary(t)
	for _, runner := range []string{"agent", "manual"} {
		for _, canary := range []string{"config", "token", "cache"} {
			t.Run(runner+"/"+canary, func(t *testing.T) {
				ambient := t.TempDir()
				for _, key := range []string{"SLICK_CONFIG", "SLACK_CLI_CONFIG", "SLACK_CLI_TOKEN", "SLACK_CLI_TOKEN_DEFAULT"} {
					t.Setenv(key, "")
				}
				t.Setenv("HOME", ambient)
				t.Setenv("USERPROFILE", ambient)
				t.Setenv("XDG_CONFIG_HOME", filepath.Join(ambient, "config"))
				t.Setenv("XDG_CACHE_HOME", filepath.Join(ambient, "cache"))
				t.Setenv("SLICK_AMBIENT_CANARY_TOKEN", "xoxb-synthetic-ambient-config")
				configPath := writePipeConfig(t)
				ambientConfig := filepath.Join(ambient, "ambient.toml")
				prior := []byte("schema_version=\"1\"\ndefault_workspace=\"ambient\"\n[workspaces.ambient]\nname=\"ambient\"\nteam_id=\"TCANARY\"\ntoken_type=\"bot\"\ntoken=\"env:SLICK_AMBIENT_CANARY_TOKEN\"\n")
				if err := os.WriteFile(ambientConfig, prior, 0o600); err != nil {
					t.Fatal(err)
				}
				switch canary {
				case "config":
					t.Setenv("SLICK_CONFIG", ambientConfig)
				case "token":
					t.Setenv("SLACK_CLI_TOKEN_DEFAULT", "xoxb-synthetic-ambient-override")
					t.Setenv("SLACK_CLI_TOKEN", "xoxb-synthetic-ambient-global")
				}
				cachePath := filepath.Join(ambient, "cache", "slick", "default", "channels.json")
				if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
					t.Fatal(err)
				}
				cacheBody, err := json.Marshal(map[string]any{"profile": "default", "resource": "channels", "fetched_at": time.Now().UTC(), "data": map[string]any{"channels": []map[string]any{{"id": "C123", "name": "ambient-cache-canary", "type": "channel"}}}})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(cachePath, cacheBody, 0o600); err != nil {
					t.Fatal(err)
				}
				var badToken, metadataCalls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					gotToken := r.FormValue("token")
					if gotToken == "" {
						gotToken = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
					}
					if gotToken != "xoxb-test" {
						badToken.Add(1)
					}
					switch r.URL.Path {
					case "/api/auth.test":
						w.Header().Set("X-OAuth-Scopes", "channels:history,channels:read")
						writeJSON(w, `{"ok":true,"user_id":"U123"}`)
					case "/api/conversations.history":
						writeJSON(w, `{"ok":true,"messages":[{"type":"message","user":"U123","text":"fixture","ts":"1746284582.123456"}]}`)
					case "/api/chat.getPermalink":
						writeJSON(w, `{"ok":true,"permalink":"https://fixture.invalid/message"}`)
					case "/api/conversations.info":
						metadataCalls.Add(1)
						writeJSON(w, `{"ok":true,"channel":{"id":"C123","name":"fixture-channel"}}`)
					default:
						t.Errorf("unexpected isolation request: %s", r.URL.Path)
						writeJSON(w, `{"ok":false,"error":"method_not_found"}`)
					}
				}))
				defer server.Close()
				args := []string{"history", "list", "--channel=C123", "--output=json", "--color=never", "--no-throttle", "--timeout=2s"}
				var stdout, stderr string
				if runner == "agent" {
					stdout, stderr, err = runSlackBinary(t, binary, configPath, server.URL, "", args...)
				} else {
					stdout, stderr, err = runSlackBinaryWithoutAgentEnv(t, binary, configPath, server.URL, args...)
				}
				if err != nil || stderr != "" {
					t.Fatalf("isolated runner failed: %v", err)
				}
				if badToken.Load() != 0 {
					t.Error("runner used ambient credentials instead of its synthetic fixture token")
				}
				var frame struct {
					Meta struct{ Workspace string }
					Data struct {
						Messages []struct {
							ChannelName string `json:"channel_name"`
						}
					}
				}
				if err := json.Unmarshal([]byte(stdout), &frame); err != nil {
					t.Fatal(err)
				}
				if frame.Meta.Workspace != "default" {
					t.Error("runner selected ambient account configuration")
				}
				if strings.Contains(stdout, "ambient-cache-canary") || metadataCalls.Load() != 1 || len(frame.Data.Messages) != 1 || frame.Data.Messages[0].ChannelName != "fixture-channel" {
					t.Error("runner reused ambient metadata cache")
				}
				for path, want := range map[string][]byte{ambientConfig: prior, cachePath: cacheBody} {
					body, err := os.ReadFile(path)
					if err != nil || string(body) != string(want) {
						t.Fatal("runner changed ambient account state")
					}
				}
			})
		}
	}
}

// isolatedRuntimeEnv is for the application under test. The Go compiler keeps
// its separately configured build environment in buildSlackBinary.
func isolatedRuntimeEnv(t *testing.T) []string {
	t.Helper()
	home := t.TempDir()
	tmp := filepath.Join(home, "tmp")
	if err := os.Mkdir(tmp, 0o700); err != nil {
		t.Fatal(err)
	}
	env := []string{
		"HOME=" + home,
		"USERPROFILE=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, "config"),
		"XDG_CACHE_HOME=" + filepath.Join(home, "cache"),
		"TMPDIR=" + tmp,
		"TMP=" + tmp,
		"TEMP=" + tmp,
		"SLACK_CLI_BASE_URL=http://127.0.0.1:1",
	}
	if os.Getenv("SLICK_TEST_RACE_BINARY") == "1" {
		// Keep race-built children practical without inheriting ambient GORACE.
		env = append(env, "GORACE=atexit_sleep_ms=0 halt_on_error=1")
	}
	for _, key := range []string{"SystemRoot", "WINDIR"} {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	return env
}
