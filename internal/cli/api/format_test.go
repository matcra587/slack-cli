package api_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	cliapi "github.com/matcra587/slack-cli/internal/cli/api"
	"github.com/matcra587/slack-cli/internal/cli/output"
	"github.com/matcra587/slack-cli/internal/cli/runtime/runtimetest"
)

func TestAPIOutputModesUseSharedContracts(t *testing.T) {
	for _, mode := range []string{"json", "compact", "human"} {
		for _, command := range [][]string{{"api", "methods", "users.info"}, {"api", "describe", "users.info"}, {"api", "call", "users.info", "--field", "user=U1", "--dry-run"}} {
			t.Run(mode+"/"+strings.Join(command, "-"), func(t *testing.T) {
				rt, stdout, stderr := runtimetest.NewRuntime(t, runtimetest.Options{Token: "xoxb-fixture-only"})
				root := runtimetest.NewRoot(rt, stdout, stderr)
				root.AddCommand(cliapi.NewCommand(rt))
				_, _, err := runtimetest.Run(t, root, append(command, "--output="+mode), stdout, stderr)
				if err != nil || stderr.Len() != 0 || stdout.Len() == 0 {
					t.Fatalf("bad output: %s %s %v", stdout, stderr, err)
				}
				if mode == "human" {
					return
				}
				var object map[string]any
				if err := json.Unmarshal(stdout.Bytes(), &object); err != nil {
					t.Fatal(err)
				}
				if mode == "json" {
					if len(object) != 4 || object["ok"] != true || object["meta"] == nil || object["data"] == nil || object["errors"] == nil {
						t.Fatalf("wrong envelope: %s", stdout)
					}
				} else if _, exists := object["ok"]; exists {
					t.Fatalf("compact output contained envelope: %s", stdout)
				}
			})
		}
	}
}

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, errors.New("fixture pipe closed") }

func TestOutputFailureAfterMutationUsesExitEightWithoutReplay(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = io.WriteString(w, `{"ok":true,"ts":"1746284582.123456"}`)
	}))
	defer server.Close()
	rt, stdout, stderr := runtimetest.NewRuntime(t, runtimetest.Options{SlackBaseURL: server.URL + "/", Token: "xoxb-fixture-only"})
	rt.Stdout = failedWriter{}
	root := runtimetest.NewRoot(rt, stdout, stderr)
	root.AddCommand(cliapi.NewCommand(rt))
	_, _, err := runtimetest.Run(t, root, []string{"api", "call", "chat.postMessage", "--force", "--field", "channel=C1", "--output=json", "--no-throttle"}, stdout, stderr)
	commandErr, ok := errors.AsType[output.CommandError](err)
	if !ok || commandErr.CLIError.ExitCode != 8 || requests.Load() != 1 {
		t.Fatalf("wrong output failure or replay: %v requests=%d", err, requests.Load())
	}
}
