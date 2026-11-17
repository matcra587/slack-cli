package api_test

import (
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

	cliapi "github.com/matcra587/slack-cli/internal/cli/api"
	"github.com/matcra587/slack-cli/internal/cli/runtime"
	"github.com/matcra587/slack-cli/internal/cli/runtime/runtimetest"
	"github.com/matcra587/slack-cli/internal/config"
)

func run(t *testing.T, serverURL, input string, customize func(*runtime.RootRuntime), args ...string) (map[string]any, string, error) {
	t.Helper()
	rt, stdout, stderr := runtimetest.NewRuntime(t, runtimetest.Options{SlackBaseURL: serverURL, Token: "xoxb-fixture-only", Stdin: strings.NewReader(input)})
	if customize != nil {
		customize(rt)
	}
	root := runtimetest.NewRoot(rt, stdout, stderr)
	root.SetContext(t.Context())
	root.AddCommand(cliapi.NewCommand(rt))
	args = append(args, "--output=json", "--no-throttle")
	_, _, err := runtimetest.Run(t, root, args, stdout, stderr)
	var result map[string]any
	if decodeErr := json.Unmarshal(stdout.Bytes(), &result); decodeErr != nil {
		t.Fatalf("invalid JSON output: %v; stdout=%s stderr=%s command-error=%v", decodeErr, stdout, stderr, err)
	}
	return result, stderr.String(), err
}

func noCredentials(t *testing.T) func(*runtime.RootRuntime) {
	t.Helper()
	return func(rt *runtime.RootRuntime) {
		rt.ConfigLoadError = errors.New("fixture config unavailable")
		rt.TokenResolver = runtime.TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
			t.Error("resolved credentials unexpectedly")
			return "", errors.New("forbidden")
		})
	}
}

func TestDiscoveryDoesNotNeedCredentials(t *testing.T) {
	for _, args := range [][]string{{"api", "methods", "users."}, {"api", "describe", "users.info"}, {"api", "methods", "absent-prefix"}} {
		result, stderr, err := run(t, "", "", noCredentials(t), args...)
		if err != nil || result["ok"] != true || stderr != "" {
			t.Fatalf("discovery failed: %#v %q %v", result, stderr, err)
		}
	}
}

func TestUnknownAndMutationMethodsNeverRunWithoutAuthorization(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); _, _ = io.WriteString(w, `{"ok":true}`) }))
	defer server.Close()
	for _, args := range [][]string{
		{"api", "call", "totally.unknown"},
		{"api", "call", "chat.postMessage", "--field", "channel=C1"},
		{"api", "call", "auth.revoke"},
		{"api", "call", "chat.postMessage", "--paginate", "--force", "--field", "channel=C1"},
		{"api", "call", "oauth.v2.access", "--force"},
		{"api", "call", "apps.manifest.export", "--force", "--output-file", filepath.Join(t.TempDir(), "private.json")},
	} {
		result, _, err := run(t, server.URL+"/", "", nil, args...)
		if err == nil || result["ok"] != false {
			t.Fatalf("unsafe call succeeded: %v %#v", args, result)
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("unsafe calls sent %d requests", requests.Load())
	}
}

func TestDryRunDoesNotResolveCredentialsOrCallAPI(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1); _, _ = io.WriteString(w, `{"ok":true}`) }))
	defer server.Close()
	result, _, err := run(t, server.URL+"/", `{"channel":"C1","text":"fixture","unfurl_links":false,"metadata":null}`, func(rt *runtime.RootRuntime) {
		rt.TokenResolver = runtime.TokenResolverFunc(func(context.Context, config.WorkspaceProfile) (string, error) {
			t.Error("dry-run resolved credentials")
			return "xoxb-fixture-only", nil
		})
	}, "api", "call", "chat.postMessage", "--input=-", "--dry-run", "--field", "thread_ts=1746284582.123456")
	if err != nil || result["ok"] != true || requests.Load() != 0 {
		t.Fatalf("dry-run called API or failed: %#v %v requests=%d", result, err, requests.Load())
	}
	data := result["data"].(map[string]any)
	params := data["parameters"].(map[string]any)
	if params["thread_ts"] != "1746284582.123456" || params["unfurl_links"] != false || data["dry_run"] != true {
		t.Fatalf("parameters changed: %#v", data)
	}
}

func TestCallsPreserveHTTPContractAndJSONValues(t *testing.T) {
	for _, method := range []string{"users.info", "chat.postMessage"} {
		t.Run(method, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				requests.Add(1)
				if req.URL.Path != "/api/"+method || req.Header.Get("Authorization") != "Bearer xoxb-fixture-only" {
					t.Errorf("wrong request: %s %s auth=%q", req.Method, req.URL.Path, req.Header.Get("Authorization"))
				}
				if method == "users.info" {
					if req.Method != "GET" || req.URL.Query().Get("user") != "U1" {
						t.Errorf("wrong GET params: %s %v", req.Method, req.URL.Query())
					}
				} else {
					if req.Method != "POST" || !strings.HasPrefix(req.Header.Get("Content-Type"), "application/json") {
						t.Errorf("wrong POST encoding: %s %s", req.Method, req.Header.Get("Content-Type"))
					}
					var body map[string]any
					if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if body["unfurl_links"] != false || body["zero"] != float64(0) || body["metadata"] != nil || len(body["blocks"].([]any)) != 0 {
						t.Errorf("JSON values lost: %#v", body)
					}
					if body["thread_ts"] != "1746284582.123456" {
						t.Errorf("timestamp changed: %#v", body)
					}
				}
				_, _ = io.WriteString(w, `{"ok":true,"result":[]}`)
			}))
			defer server.Close()
			args := []string{"api", "call", method}
			input := ""
			if method == "users.info" {
				args = append(args, "--field", "user=U1")
			} else {
				args = append(args, "--force", "--input=-", "--field", "thread_ts=1746284582.123456")
				input = `{"channel":"C1","text":"fixture","blocks":[],"unfurl_links":false,"zero":0,"metadata":null}`
			}
			result, stderr, err := run(t, server.URL+"/", input, nil, args...)
			if err != nil || result["ok"] != true || requests.Load() != 1 || stderr != "" {
				t.Fatalf("call failed: %#v %v %q requests=%d", result, err, stderr, requests.Load())
			}
			if _, ok := result["data"].(map[string]any)["response"].(map[string]any); !ok {
				t.Fatalf("response is not a native record: %#v", result)
			}
		})
	}
}

func TestFailuresNeverRetryOrPrintRemoteSecrets(t *testing.T) {
	for _, fixture := range []struct {
		name, body string
		status     int
		exit       float64
	}{
		{"permission", `{"ok":false,"error":"missing_scope","needed":"users:read"}`, 200, 1},
		{"rate", `fixture-remote-secret`, 429, 3},
		{"malformed", `{"ok":`, 200, 5},
		{"empty", ``, 200, 5},
		{"hostile", `{"ok":false,"error":"fixture-remote-secret\ncredential"}`, 200, 5},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.Header().Set("Retry-After", "2")
				w.WriteHeader(fixture.status)
				_, _ = io.WriteString(w, fixture.body)
			}))
			defer server.Close()
			result, stderr, err := run(t, server.URL+"/", "", nil, "api", "call", "chat.postMessage", "--force", "--field", "channel=C1")
			encoded, _ := json.Marshal(result)
			if err == nil || result["ok"] != false || result["meta"].(map[string]any)["exit_code"] != fixture.exit || requests.Load() != 1 {
				t.Fatalf("wrong failure: %#v %v requests=%d", result, err, requests.Load())
			}
			if strings.Contains(string(encoded)+stderr+err.Error(), "fixture-remote-secret") {
				t.Fatal("remote sensitive body leaked")
			}
		})
	}
}

func TestDeadlineDoesNotRetry(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) { requests.Add(1); <-req.Context().Done() }))
	defer server.Close()
	result, _, err := run(t, server.URL+"/", "", func(rt *runtime.RootRuntime) { rt.HTTPClient = &http.Client{Timeout: 20 * time.Millisecond} }, "api", "call", "users.info", "--field", "user=U1")
	if err == nil || result["ok"] != false || requests.Load() != 1 {
		t.Fatalf("deadline failure: %#v %v", result, err)
	}
}

func TestWorkspaceFailureUsesFailureEnvelope(t *testing.T) {
	result, _, err := run(t, "", "", noCredentials(t), "api", "call", "users.info", "--field", "user=U1")
	if err == nil || result["ok"] != false {
		t.Fatalf("workspace error lost envelope: %#v %v", result, err)
	}
}

func TestSensitiveResponseRequiresPrivateNewFile(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = io.WriteString(w, `{"ok":true,"upload_url":"fixture-secret-url"}`)
	}))
	defer server.Close()
	directory := t.TempDir()
	path := filepath.Join(directory, "response.json")
	args := []string{"api", "call", "files.getUploadURLExternal", "--force", "--field", "filename=fixture.txt", "--field", "length=1"}
	result, _, err := run(t, server.URL+"/", "", nil, args...)
	if err == nil || result["ok"] != false || requests.Load() != 0 {
		t.Fatal("sensitive response did not require file")
	}
	result, stderr, err := run(t, server.URL+"/", "", nil, append(args, "--output-file", path)...)
	encoded, _ := json.Marshal(result)
	if err != nil || result["ok"] != true || strings.Contains(string(encoded)+stderr, "fixture-secret-url") {
		t.Fatalf("unsafe receipt: %#v %s %v", result, stderr, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("file not protected: %v %v", info, err)
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "fixture-secret-url") {
		t.Fatal("missing protected body")
	}
	_, _, err = run(t, server.URL+"/", "", nil, append(args, "--output-file", path)...)
	after, _ := os.ReadFile(path)
	if err == nil || string(after) != string(body) || requests.Load() != 1 {
		t.Fatal("existing file overwritten or API called")
	}
	link := filepath.Join(directory, "symlink.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	_, _, err = run(t, server.URL+"/", "", nil, append(args, "--output-file", link)...)
	if err == nil || requests.Load() != 1 {
		t.Fatal("symlink followed")
	}
}
