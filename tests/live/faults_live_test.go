//go:build live

package live_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	slack "github.com/slack-go/slack"
)

func TestLiveFaultControls(t *testing.T) {
	var runner *liveRunner
	var observer *slackObserver
	var committed liveReceipt
	// Registered first so the normal independent cleanup executes before this
	// genuine Slack missing-message check. It does not use the canceled phase.
	t.Cleanup(func() {
		if runner == nil || committed.TS == "" {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		present, err := observer.Inspect(ctx, committed)
		if err != nil || present {
			t.Error("owned interrupted-write message cleanup is unverified")
			return
		}
		args := []string{"react", "list", "--channel", committed.Channel, "--timestamp", committed.TS, "--workspace", runner.plan.Workspace, "--output=json", "--color=never", "--timeout=10s"}
		env := append(slices.Clone(runner.env), "SLICK_LIVE_CASE=real-missing-message")
		out, err := runner.execute(ctx, args, env, "")
		assertFaultFrame(t, out, err, 2, "not_found", runner.plan.Workspace)
		t.Log("FAULT_CHECK real_missing_message source=Slack exit=2 cleanup=verified")
	})
	r, s := setupLive(t)
	runner, observer = r, s
	r.exec = executeFaultChild
	token := ""
	for _, value := range r.env {
		if after, ok := strings.CutPrefix(value, "SLICK_LIVE_ACCESS_TOKEN="); ok {
			token = after
		}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	upstream := &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	t.Run("real permission denial", func(t *testing.T) {
		if slices.Contains(s.scopes, "users.profile:read") {
			t.Skip("current token grants the selected read scope; permission control needs a restricted token")
		}
		_, err := s.client.GetUserProfileContext(r.ctx, &slack.GetUserProfileParameters{UserID: r.plan.ActorID})
		var response slack.SlackErrorResponse
		if !errors.As(err, &response) || response.Err != "missing_scope" {
			t.Fatal("real Slack did not return the expected missing-scope denial")
		}
		relay, e := newFaultRelay(token, "permission", "https://slack.com/api/", r.plan, upstream)
		if e != nil {
			t.Fatal(e)
		}
		defer relay.server.Close()
		out, commandErr := runFaultLookup(t, r, relay, "permission", "10s", false)
		assertFaultFrame(t, out, commandErr, 1, "auth_failure", r.plan.Workspace)
		if !bytes.Contains(out, []byte("users.profile:read")) {
			t.Fatal("real needed scope was lost by CLI error mapping")
		}
		counts := relay.snapshot()
		if counts.Attempts != 1 || counts.Forwarded != 1 || counts.Rejected != 0 {
			t.Fatal("permission error was retried or relay guard rejected the request")
		}
		t.Log("FAULT_CHECK real_permission source=Slack_users.profile.get relay=explicit_endpoint_substitution exit=1 attempts=1")
	})
	for _, row := range []struct {
		name, mode               string
		exit, attempts, forwards int
		kind                     string
		debug                    bool
	}{
		{name: "429 recovery", mode: "429-once", attempts: 2, forwards: 1},
		{name: "429 exhausted", mode: "429-exhausted", exit: 3, kind: "rate_limit", attempts: 3},
		{name: "malformed response privacy", mode: "malformed", exit: 5, kind: "server_error", attempts: 1, debug: true},
		{name: "read connection lost", mode: "drop-read", exit: 5, kind: "server_error", attempts: 1, forwards: 1},
		{name: "read deadline", mode: "deadline", exit: 7, kind: "timeout", attempts: 1},
	} {
		t.Run(row.name, func(t *testing.T) {
			relay, err := newFaultRelay(token, row.mode, "https://slack.com/api/", r.plan, upstream)
			if err != nil {
				t.Fatal(err)
			}
			defer relay.server.Close()
			budget := "10s"
			if row.mode == "deadline" {
				budget = "1s"
			}
			start := time.Now()
			out, commandErr := runFaultLookup(t, r, relay, row.mode, budget, row.debug)
			assertFaultFrame(t, out, commandErr, row.exit, row.kind, r.plan.Workspace)
			counts := relay.snapshot()
			if counts.Attempts != row.attempts || counts.Forwarded != row.forwards || counts.Rejected != 0 {
				t.Fatalf("fault request counts changed: attempts=%d forwarded=%d rejected=%d", counts.Attempts, counts.Forwarded, counts.Rejected)
			}
			if strings.HasPrefix(row.mode, "429") && time.Since(start) < time.Second {
				t.Fatal("Retry-After wait was not observed")
			}
			if row.mode == "429-exhausted" {
				var frame struct {
					Errors []struct {
						Retry *int `json:"retry_after_seconds"`
					}
				}
				_ = json.Unmarshal(out, &frame)
				if len(frame.Errors) != 1 || frame.Errors[0].Retry == nil || *frame.Errors[0].Retry != 1 {
					t.Fatal("rate-limit retry timing lost")
				}
			}
			data, err := r.read(r.ctx, "lookup", "user", "--user", r.plan.ActorID)
			if err != nil {
				t.Fatal("read did not recover on a separate canonical Slack request")
			}
			var user struct{ User struct{ ID string } }
			if json.Unmarshal(data, &user) != nil || user.User.ID != r.plan.ActorID {
				t.Fatal("read recovery changed the actor")
			}
			t.Logf("FAULT_CHECK %s source=controlled_relay actual_Slack_forwards=%d exit=%d attempts=%d recovery=verified", row.mode, counts.Forwarded, row.exit, counts.Attempts)
		})
	}
	t.Run("partial refresh failure preserves cache", func(t *testing.T) {
		liveRead(t, r, "cache", "users", "--refresh", "--page-size", "100")
		cacheRoot := ""
		for _, value := range r.env {
			if after, ok := strings.CutPrefix(value, "XDG_CACHE_HOME="); ok {
				cacheRoot = after
			}
		}
		cacheFile := filepath.Join(cacheRoot, "slick", r.plan.Workspace, "users.json")
		before, err := os.ReadFile(cacheFile)
		if err != nil {
			t.Fatal("baseline cache snapshot missing")
		}
		relay, err := newFaultRelay(token, "cache-503", "https://slack.com/api/", r.plan, upstream)
		if err != nil {
			t.Fatal(err)
		}
		defer relay.server.Close()
		args := []string{"cache", "users", "--refresh", "--page-size", "1", "--max-pages", "100", "--workspace", r.plan.Workspace, "--output=json", "--color=never", "--timeout=10s"}
		env := append(slices.Clone(r.env), "SLACK_CLI_BASE_URL="+relay.server.URL, "SLICK_LIVE_CASE=cache-mid-page-503")
		out, commandErr := r.execute(r.ctx, args, env, "")
		assertFaultFrame(t, out, commandErr, 5, "server_error", r.plan.Workspace)
		counts := relay.snapshot()
		after, readErr := os.ReadFile(cacheFile)
		if counts.Attempts != 2 || counts.Forwarded != 1 || counts.Rejected != 0 || readErr != nil || !bytes.Equal(before, after) {
			t.Fatal("failed partial refresh retried or changed the prior cache")
		}
		data := liveRead(t, r, "cache", "users")
		var warm struct {
			FromCache bool `json:"from_cache"`
		}
		if json.Unmarshal(data, &warm) != nil || !warm.FromCache {
			t.Fatal("prior complete cache did not remain usable")
		}
		current, err := os.ReadFile(r.plan.ConfigPath)
		if err != nil || !bytes.Equal(current, r.prior) {
			t.Fatal("failure changed saved configuration")
		}
		t.Log("FAULT_CHECK mid_page_503 source=controlled_relay actual_Slack_pages=1 attempts=2 prior_cache=byte_identical config=unchanged recovery=warm_hit")
	})
	t.Run("committed write acknowledgement lost", func(t *testing.T) {
		relay, err := newFaultRelay(token, "lost-write", "https://slack.com/api/", r.plan, upstream)
		if err != nil {
			t.Fatal(err)
		}
		defer relay.server.Close()
		beforeEnv := r.env
		r.env = append(slices.Clone(r.env), "SLACK_CLI_BASE_URL="+relay.server.URL, "SLICK_LIVE_CASE=lost-write-ack")
		defer func() { r.env = beforeEnv }()
		previousExec := r.exec
		var processErr error
		var processOutput []byte
		r.exec = func(ctx context.Context, binary string, args, env []string, stdin string) ([]byte, error) {
			out, err := previousExec(ctx, binary, args, env, stdin)
			processErr = err
			processOutput = out
			return out, err
		}
		defer func() { r.exec = previousExec }()
		_, commandErr := r.command("send", r.plan.PublicChannel, "", "Release review "+r.plan.RunID+": interrupted acknowledgement recovery check.")
		counts := relay.snapshot()
		committed = counts.Committed
		if commandErr == nil {
			t.Fatal("lost mutation acknowledgement was not reported as uncertain")
		}
		assertFaultFrame(t, processOutput, processErr, 5, "server_error", r.plan.Workspace)
		if counts.Attempts != 1 || counts.Forwarded != 1 || counts.ControlError || counts.Rejected != 0 || committed.TS == "" || !r.ambiguous {
			t.Fatal("write was replayed or exact commit evidence is incomplete")
		}
		pending := lastReceipt(t, r)
		if pending.Kind != "uncertain-send" {
			t.Fatal("lost acknowledgement did not retain an uncertain receipt")
		}
		found, err := s.Recover(r.ctx, pending)
		if err != nil || len(found) != 1 || found[0].Channel != committed.Channel || found[0].TS != committed.TS {
			t.Fatal("independent recovery did not find exactly the committed message")
		}
		t.Log("FAULT_CHECK lost_write source=real_Slack_commit_controlled_ack_loss attempts=1 forwards=1 recovered_exactly=1 no_mutation_replay=true cleanup=required")
	})
}

func runFaultLookup(t *testing.T, r *liveRunner, relay *faultRelay, mode, budget string, debug bool) ([]byte, error) {
	t.Helper()
	args := []string{"lookup", "user", "--user", r.plan.ActorID, "--workspace", r.plan.Workspace, "--output=json", "--color=never", "--timeout=" + budget}
	if debug {
		args = append(args, "--debug")
	}
	env := append(slices.Clone(r.env), "SLACK_CLI_BASE_URL="+relay.server.URL, "SLICK_LIVE_CASE="+mode)
	return r.execute(r.ctx, args, env, "")
}

func assertFaultFrame(t *testing.T, out []byte, err error, exit int, kind, workspace string) {
	t.Helper()
	actual := 0
	if err != nil {
		actual = -1
		if status, ok := errors.AsType[*exec.ExitError](err); ok {
			actual = status.ExitCode()
		}
	}
	var keys map[string]json.RawMessage
	var frame struct {
		OK   bool
		Meta struct {
			Exit               int `json:"exit_code"`
			Command, Workspace string
		}
		Errors []struct{ Type string }
	}
	if actual != exit || json.Unmarshal(out, &keys) != nil || len(keys) != 4 || json.Unmarshal(out, &frame) != nil || frame.OK != (exit == 0) || frame.Meta.Exit != exit || frame.Meta.Workspace != workspace || frame.Meta.Command == "" || frame.Errors == nil {
		t.Fatalf("CLI outcome/envelope mismatch: actual exit=%d expected=%d", actual, exit)
	}
	for _, key := range []string{"ok", "meta", "data", "errors"} {
		if keys[key] == nil {
			t.Fatal("CLI envelope lost a required field")
		}
	}
	if exit == 0 {
		if len(frame.Errors) != 0 {
			t.Fatal("successful recovery contains errors")
		}
	} else if len(frame.Errors) != 1 || frame.Errors[0].Type != kind || string(keys["data"]) != "null" {
		t.Fatal("structured CLI failure classification or data is inconsistent")
	}
}
