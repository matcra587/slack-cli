package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	slackcache "github.com/matcra587/slack-cli/internal/cache"
	clioutput "github.com/matcra587/slack-cli/internal/cli/output"
	"github.com/matcra587/slack-cli/internal/config"
)

func TestCachePreviewsPreserveStateWithoutCredentialsOrHTTP(t *testing.T) {
	for _, tc := range []struct {
		name, mode, resource string
		args                 []string
		missing, applies     bool
		exit                 int
	}{
		{"users refresh", "json", "users", []string{"cache", "users", "--refresh", "--dry-run"}, false, false, 0},
		{"channels missing", "compact", "channels", []string{"-n", "cache", "channels"}, true, false, 0},
		{"clear resource", "human", "users", []string{"cache", "clear", "users", "-n"}, false, false, 0},
		{"clear profile", "json", "all", []string{"--dry-run", "cache", "clear"}, false, false, 0},
		{"clear empty", "compact", "all", []string{"cache", "clear", "--dry-run"}, true, false, 0},
		{"invalid TTL", "json", "users", []string{"cache", "users", "--ttl-minutes=153722868", "--dry-run"}, false, false, 4},
		{"false applies", "json", "users", []string{"cache", "clear", "users", "--dry-run=false"}, false, true, 0},
		{"removed flag applies", "compact", "users", []string{"cache", "clear", "users"}, false, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			cacheRoot, err := slackcache.Root()
			if err != nil {
				t.Fatal(err)
			}
			if !tc.missing {
				for _, item := range []struct{ profile, resource, body string }{
					{"default", "users", `{"users":[]}`},
					{"default", "channels", `{"channels":[]}`},
					{"default", "custom", `{"note":"keep"}`},
					{"other", "users", `{"users":[]}`},
				} {
					if _, err := slackcache.Write(item.profile, item.resource, json.RawMessage(item.body)); err != nil {
						t.Fatal(err)
					}
				}
			}
			before := cachePreviewFiles(t, cacheRoot)
			cfg := authTestConfig()
			prior, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			var out, diagnostic bytes.Buffer
			resolutions, calls := 0, 0
			client := &http.Client{Transport: authStatusTransport(func(*http.Request) (*http.Response, error) {
				calls++
				return nil, errors.New("synthetic-HTTP-must-not-run")
			})}
			root, runtime := NewRootCommandWithRuntime(WithConfig(cfg), WithTTY(false), WithIO(strings.NewReader(""), &out, &diagnostic),
				func(runtime *RootRuntime) { runtime.HTTPClient = client }, WithTokenResolver(TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
					resolutions++
					return "", errors.New("synthetic-provider-must-not-run")
				})))
			root.SetArgs(append(tc.args, "--output="+tc.mode, "--color=never"))
			runErr := root.ExecuteContext(t.Context())
			if runtime.CancelTimeout != nil {
				runtime.CancelTimeout()
			}
			gotExit := 0
			if failure, ok := errors.AsType[clioutput.CommandError](runErr); ok {
				gotExit = failure.CLIError.ExitCode
			} else if runErr != nil {
				t.Fatal("cache preview returned unstructured failure")
			}
			if gotExit != tc.exit || calls != 0 || resolutions != 0 {
				t.Fatalf("cache exit/HTTP/provider=%d/%d/%d, want %d/0/0", gotExit, calls, resolutions, tc.exit)
			}
			after := cachePreviewFiles(t, cacheRoot)
			if tc.applies {
				users, err := slackcache.Path("default", "users")
				if err != nil {
					t.Fatal(err)
				}
				delete(before, users)
			}
			if !reflect.DeepEqual(before, after) {
				t.Error("cache preview changed files, or actual clear changed unrelated resources")
			}
			if tc.missing {
				if _, err := os.Stat(cacheRoot); !errors.Is(err, os.ErrNotExist) {
					t.Error("cache preview created directories")
				}
			}
			current, err := json.Marshal(cfg)
			if err != nil || !bytes.Equal(prior, current) {
				t.Error("cache preview changed workspace settings")
			}
			if tc.mode == "human" {
				if diagnostic.Len() != 0 || !strings.Contains(out.String(), "dry_run=true") {
					t.Error("human cache preview lost marker or streams")
				}
			} else {
				var data map[string]any
				if json.Unmarshal(out.Bytes(), &data) != nil || diagnostic.Len() != 0 {
					t.Fatal("cache preview is not clean JSON")
				}
				if tc.mode == "json" || tc.exit != 0 {
					if data["ok"] != (tc.exit == 0) {
						t.Fatal("cache preview lost envelope status")
					}
					if tc.exit == 0 {
						data = data["data"].(map[string]any)
					}
				}
				if tc.exit == 0 && !tc.applies && (data["dry_run"] != true || data["resource"] != tc.resource || data["profile"] != "default") {
					t.Error("cache preview lost planned profile/resource")
				}
				if tc.applies && (data["dry_run"] != nil || data["cleared"] != true) {
					t.Error("false/removed flag did not perform actual clear")
				}
			}
		})
	}
}

func cachePreviewFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files[path] = string(body)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
