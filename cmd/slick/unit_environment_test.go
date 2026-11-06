package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	clicache "github.com/matcra587/slack-cli/internal/cli/cache"
	clitoken "github.com/matcra587/slack-cli/internal/cli/token"
)

func TestUnitRuntimeStartsWithoutAmbientConfigTokensOrCache(t *testing.T) {
	var out, diagnostic bytes.Buffer
	root := NewRootCommand(WithTTY(false), WithIO(strings.NewReader(""), &out, &diagnostic))
	root.SetArgs([]string{"config", "path", "--output=json", "--color=never"})
	if err := root.ExecuteContext(t.Context()); err != nil || diagnostic.Len() != 0 {
		t.Fatalf("isolated config path failed: %v", err)
	}
	var frame struct {
		OK   bool
		Data struct{ Exists bool }
	}
	if json.Unmarshal(out.Bytes(), &frame) != nil || !frame.OK || frame.Data.Exists {
		t.Fatal("unit runtime loaded ambient configuration")
	}
	if _, ok := clitoken.RuntimeEnvToken("default"); ok {
		t.Fatal("unit runtime inherited an ambient credential override")
	}
	if _, ok := clicache.LoadCachedChannels("default"); ok {
		t.Fatal("unit runtime inherited ambient channel metadata")
	}
	if _, ok := clicache.LoadCachedUsers("default"); ok {
		t.Fatal("unit runtime inherited ambient user metadata")
	}
}

func TestUnitRuntimeRejectsSyntheticAmbientState(t *testing.T) {
	ambient := t.TempDir()
	configPath := filepath.Join(ambient, "config.toml")
	configBody := []byte("schema_version='1'\ndefault_workspace='default'\n[workspaces.default]\nname='default'\n")
	if err := os.WriteFile(configPath, configBody, 0o600); err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(ambient, "cache", "slick", "default", "channels.json")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o700); err != nil {
		t.Fatal(err)
	}
	cacheBody, err := json.Marshal(map[string]any{"profile": "default", "resource": "channels", "fetched_at": time.Now().UTC(), "data": map[string]any{"channels": []map[string]string{{"id": "CCANARY", "name": "synthetic-ambient-channel"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, cacheBody, 0o600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, executable, "-test.run=^TestUnitRuntimeStartsWithoutAmbientConfigTokensOrCache$", "-test.count=1")
	child.Env = []string{
		"HOME=" + ambient, "USERPROFILE=" + ambient,
		"TMPDIR=" + ambient, "TMP=" + ambient, "TEMP=" + ambient,
		"XDG_CONFIG_HOME=" + filepath.Join(ambient, "config"), "XDG_CACHE_HOME=" + filepath.Join(ambient, "cache"),
		"SLICK_CONFIG=" + configPath, "SLACK_CLI_CONFIG=" + configPath,
		"SLACK_CLI_DEFAULT_WORKSPACE=default",
		"SLACK_CLI_TOKEN=xoxb-synthetic-unit-global", "SLACK_CLI_TOKEN_DEFAULT=xoxb-synthetic-unit-profile",
		"SLACK_CLI_BASE_URL=http://127.0.0.1:1",
	}
	for _, key := range []string{"SystemRoot", "WINDIR"} {
		if value, ok := os.LookupEnv(key); ok {
			child.Env = append(child.Env, key+"="+value)
		}
	}
	if _, err := child.CombinedOutput(); err != nil {
		t.Fatalf("synthetic ambient inputs influenced isolated child: %v", err)
	}
	for path, want := range map[string][]byte{configPath: configBody, cachePath: cacheBody} {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("unit child changed ambient synthetic state")
		}
	}
}
