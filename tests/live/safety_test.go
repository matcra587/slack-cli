package live_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	slack "github.com/slack-go/slack"
)

type syntheticObserver struct {
	team, actor    string
	writes         int
	present        map[string]bool
	denied         bool
	healthyCleanup bool
	visited        []liveReceipt
	recovered      []liveReceipt
}

func (s *syntheticObserver) Recover(context.Context, liveReceipt) ([]liveReceipt, error) {
	return s.recovered, nil
}

func (s *syntheticObserver) Identity(context.Context) (string, string, error) {
	return s.team, s.actor, nil
}
func (s *syntheticObserver) Channel(context.Context, string) error { return nil }
func (s *syntheticObserver) Conversation(context.Context, []string) (string, error) {
	return "DSYNTHETIC", nil
}

func (s *syntheticObserver) Inspect(ctx context.Context, v liveReceipt) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	return s.present[v.TS], nil
}

func (s *syntheticObserver) Delete(ctx context.Context, v liveReceipt) error {
	s.writes++
	s.visited = append(s.visited, v)
	s.healthyCleanup = ctx.Err() == nil
	if s.denied && s.writes == 1 {
		return errors.New("synthetic private denial")
	}
	delete(s.present, v.TS)
	return nil
}

func syntheticPlan(t *testing.T) livePlan {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission gate requires native Windows ACL review before live use")
	}
	root, err := os.MkdirTemp(t.TempDir(), "private-run-")
	if err != nil {
		t.Fatal("private fixture root")
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal("canonical fixture root")
	}
	p := livePlan{RunRoot: root, ConfigPath: filepath.Join(root, "config.toml"), Binary: filepath.Join(root, "slick"), Workspace: liveWorkspace, TeamID: "TTEST", ActorID: "UTEST", PublicChannel: "CTEST", RunID: "synthetic-run-123"}
	cfg := "schema_version=\"1\"\ndefault_workspace=\"live-oauth-isolated\"\n[workspaces.live-oauth-isolated]\nname=\"live-oauth-isolated\"\nteam_id=\"TTEST\"\ntoken_type=\"user\"\ntoken_ref=\"env:SLICK_LIVE_ACCESS_TOKEN\"\n"
	if os.WriteFile(p.ConfigPath, []byte(cfg), 0o600) != nil || os.WriteFile(p.Binary, []byte("synthetic executable placeholder"), 0o700) != nil {
		t.Fatal("fixture write")
	}
	return p
}

func syntheticRunner(t *testing.T, p livePlan, observer *syntheticObserver, execute liveExec) *liveRunner {
	t.Helper()
	r, err := newLiveRunner(p, "xoxp-explicit-synthetic", observer, execute)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.stop)
	return r
}

func receiptOutput(ts string) []byte {
	return []byte(`{"ok":true,"data":{"message":{"channel":"CTEST","ts":"` + ts + `"}}}`)
}

func TestLiveSafetyGateAndIsolation(t *testing.T) {
	for _, row := range []string{"confirmation absent", "plan missing", "plan mismatch", "config provider rejected", "ambient environment excluded", "unapproved channel rejected", "recipient admission", "file scopes unavailable", "excluded operation rejected", "receipt before assertion and canceled cleanup", "lost receipt uniquely recovered", "ambiguous receipt no replay", "cleanup continues after denial"} {
		t.Run(row, func(t *testing.T) {
			calls := 0
			if row == "confirmation absent" || row == "plan missing" {
				_, err := loadLivePlan(func(key string) string {
					calls++
					if row == "confirmation absent" && key != "SLICK_LIVE_CONFIRM" {
						t.Fatal("gate read plan or credential before confirmation")
					}
					if row == "plan missing" && key == "SLICK_LIVE_CONFIRM" {
						return "designated-fixtures"
					}
					if key == "SLICK_LIVE_ACCESS_TOKEN" {
						t.Fatal("gate read credentials")
					}
					return ""
				})
				if err == nil || calls > 2 {
					t.Fatal("gate allowed discovery")
				}
				return
			}
			p := syntheticPlan(t)
			if row == "recipient admission" {
				p.Recipients = []string{"UPEER", "UOTHER"}
			}
			observer := &syntheticObserver{team: p.TeamID, actor: p.ActorID, present: map[string]bool{}}
			execute := func(context.Context, string, []string, []string, string) ([]byte, error) {
				calls++
				return receiptOutput("1746284582.000001"), nil
			}
			if row == "config provider rejected" {
				body, _ := os.ReadFile(p.ConfigPath)
				body = []byte(strings.ReplaceAll(string(body), "env:SLICK_LIVE_ACCESS_TOKEN", "op://synthetic/private/credential"))
				_ = os.WriteFile(p.ConfigPath, body, 0o600)
				if validateLivePlan(p) == nil {
					t.Fatal("provider reference accepted")
				}
				return
			}
			if row == "ambient environment excluded" {
				t.Setenv("SLACK_CLI_BASE_URL", "https://synthetic.invalid")
				t.Setenv("SLACK_CLI_DEFAULT_WORKSPACE", "ordinary")
				t.Setenv("HTTPS_PROXY", "https://synthetic.invalid")
			}
			r := syntheticRunner(t, p, observer, execute)
			if row == "plan mismatch" {
				observer.team = "TOTHER"
				if r.preflight() == nil {
					t.Fatal("wrong team accepted")
				}
				_, _ = r.command("send", p.PublicChannel, "", "review "+p.RunID)
				if calls != 0 || observer.writes != 0 {
					t.Fatal("mismatched identity mutated")
				}
				return
			}
			if r.preflight() != nil {
				t.Fatal("synthetic preflight")
			}
			switch row {
			case "file scopes unavailable":
				if _, err := r.command("upload", p.PublicChannel, p.Binary, "review "+p.RunID); err == nil || calls != 0 {
					t.Fatal("upload created without both file scopes")
				}
			case "recipient admission":
				if _, err := r.admitRecipients([]string{"UUNAPPROVED"}); err == nil {
					t.Fatal("unapproved peer admitted")
				}
				channel, err := r.admitRecipients([]string{"UPEER"})
				if err != nil {
					t.Fatal("approved peer admission failed")
				}
				r.exec = func(_ context.Context, _ string, args, _ []string, _ string) ([]byte, error) {
					calls++
					if slices.Contains(args, "--channel") || !slices.Contains(args, "--user") {
						t.Fatal("recipient flags changed")
					}
					return []byte(`{"data":{"message":{"channel":"DSYNTHETIC","ts":"1746284582.000001"}}}`), nil
				}
				if _, err := r.command("send-users", channel, "UPEER", "review "+p.RunID); err != nil || len(r.receipts) != 1 || r.receipts[0].ParentTS != "" || calls != 1 {
					t.Fatal("recipient receipt or request contract lost")
				}
			case "unapproved channel rejected":
				if _, err := r.command("send", "CUNAPPROVED", "", "review "+p.RunID); err == nil || calls != 0 {
					t.Fatal("unapproved target reached executor")
				}
			case "ambient environment excluded":
				t.Setenv("SLACK_CLI_BASE_URL", "https://synthetic.invalid")
				t.Setenv("SLACK_CLI_DEFAULT_WORKSPACE", "ordinary")
				t.Setenv("HTTPS_PROXY", "https://synthetic.invalid")
				for _, entry := range r.env {
					if strings.Contains(entry, "synthetic.invalid") || strings.Contains(entry, "ordinary") {
						t.Fatal("ambient environment inherited")
					}
				}
				if !slices.Contains(r.env, "SLICK_CONFIG="+p.ConfigPath) || !slices.Contains(r.env, "SLICK_LIVE_ACCESS_TOKEN=xoxp-explicit-synthetic") {
					t.Fatal("explicit fixture env missing")
				}
			case "excluded operation rejected":
				for _, op := range []string{"status", "oauth", "revoke"} {
					if _, err := r.command(op, p.PublicChannel, "", ""); err == nil {
						t.Fatal("excluded operation allowed")
					}
				}
				if calls != 0 {
					t.Fatal("excluded operation executed")
				}
			case "receipt before assertion and canceled cleanup":
				r.exec = func(context.Context, string, []string, []string, string) ([]byte, error) {
					calls++
					return receiptOutput("1746284582.000001"), errors.New("synthetic private token diagnostic")
				}
				_, err := r.command("send", p.PublicChannel, "", "review "+p.RunID)
				if err == nil || strings.Contains(err.Error(), "private") || len(r.receipts) != 1 || calls != 1 {
					t.Fatal("receipt/redaction/no-replay guard lost")
				}
				observer.present[r.receipts[0].TS] = true
				r.stop()
				if r.cleanup() != nil || observer.writes != 1 || !observer.healthyCleanup {
					t.Fatal("cleanup reused canceled context")
				}
				if body, err := os.ReadFile(r.ledger); err != nil || !strings.Contains(string(body), "1746284582.000001") || strings.Contains(string(body), "xoxp-") {
					t.Fatal("private exact ledger invalid")
				}
			case "ambiguous receipt no replay":
				r.exec = func(context.Context, string, []string, []string, string) ([]byte, error) {
					calls++
					return []byte(`{"ok":false}`), nil
				}
				if _, err := r.command("send", p.PublicChannel, "", "review "+p.RunID); err == nil || calls != 1 || !r.ambiguous {
					t.Fatal("uncertain mutation accepted or replayed")
				}
				observer.recovered = []liveReceipt{{Kind: "message", Channel: p.PublicChannel, TS: "1746284582.000001"}, {Kind: "message", Channel: p.PublicChannel, TS: "1746284582.000002"}}
				if r.cleanup() == nil {
					t.Fatal("unverified cleanup reported success")
				}
				if observer.writes != 0 {
					t.Fatal("ambiguous matches were broadly deleted")
				}
			case "lost receipt uniquely recovered":
				r.exec = func(context.Context, string, []string, []string, string) ([]byte, error) {
					calls++
					return []byte(`{"ok":true}`), nil
				}
				_, err := r.command("send", p.PublicChannel, "", "review "+p.RunID)
				if err == nil || calls != 1 || !r.ambiguous {
					t.Fatal("lost receipt did not preserve functional failure")
				}
				observer.recovered = []liveReceipt{{Kind: "message", Channel: p.PublicChannel, TS: "1746284582.000001"}}
				observer.present["1746284582.000001"] = true
				if r.cleanup() != nil || observer.writes != 1 || calls != 1 {
					t.Fatal("unique independent recovery did not clean exact resource without replay")
				}
			case "cleanup continues after denial":
				_ = r.record(liveReceipt{Kind: "message", Channel: p.PublicChannel, TS: "1746284582.000001"})
				_ = r.record(liveReceipt{Kind: "message", Channel: p.PublicChannel, TS: "1746284582.000002"})
				observer.present["1746284582.000001"] = true
				observer.present["1746284582.000002"] = true
				observer.denied = true
				if r.cleanup() == nil || observer.writes != 2 {
					t.Fatal("cleanup stopped after one failure")
				}
			}
		})
	}
}

func TestLivePlanRejectsUnapprovedTargetsAndUnknownFields(t *testing.T) {
	p := syntheticPlan(t)
	p.Recipients = []string{p.ActorID}
	if validateLivePlan(p) == nil {
		t.Fatal("actor accepted as consenting peer")
	}
	p.Recipients = nil
	path := filepath.Join(p.RunRoot, "plan.json")
	data, _ := json.Marshal(p)
	data = append(data[:len(data)-1], []byte(`,"token":"synthetic-forbidden"}`)...)
	_ = os.WriteFile(path, data, 0o600)
	_, err := loadLivePlan(func(key string) string {
		switch key {
		case "SLICK_LIVE_CONFIRM":
			return "designated-fixtures"
		case "SLICK_LIVE_PLAN":
			return path
		case "SLICK_LIVE_BINARY":
			return p.Binary
		}
		return ""
	})
	if err == nil {
		t.Fatal("unreviewed secret field accepted")
	}
}

func TestLiveChildDeadlineIsIndependentAndDiagnosticsStayPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX subprocess fixture")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	_, err := executeLiveChild(ctx, "/bin/sh", []string{"-c", "printf synthetic-private-diagnostic >&2; exec sleep 20"}, []string{"PATH=/usr/bin:/bin"}, "")
	if err == nil || ctx.Err() == nil || strings.Contains(err.Error(), "synthetic-private") {
		t.Fatal("child deadline or diagnostic isolation lost")
	}
}

func TestLiveChildInheritedPipeHasBoundedWait(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX subprocess fixture")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	start := time.Now()
	out, err := executeLiveChild(ctx, "/bin/sh", []string{"-c", "sleep 20 & printf '%s' \"$!\""}, []string{"PATH=/usr/bin:/bin"}, "")
	pid, parseErr := strconv.Atoi(string(out))
	if parseErr == nil {
		process, findErr := os.FindProcess(pid)
		if findErr == nil {
			_ = process.Kill()
		}
	}
	if parseErr != nil || !errors.Is(err, exec.ErrWaitDelay) || time.Since(start) > 4*time.Second {
		t.Fatal("inherited output pipe did not obey WaitDelay")
	}
}

func TestLiveChildOutputCaptureIsBounded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX subprocess fixture")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	out, err := executeLiveChild(ctx, "/bin/sh", []string{"-c", "head -c 9437184 /dev/zero"}, []string{"PATH=/usr/bin:/bin"}, "")
	if err == nil || len(out) > 8<<20 {
		t.Fatal("private output capture exceeded its limit")
	}
}

func TestLiveIndependentReaderRejectsForeignResources(t *testing.T) {
	for _, row := range []string{"foreign recipient membership", "foreign message actor", "file permission is not absence", "incomplete reaction users", "missing parent is not reply absence"} {
		t.Run(row, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.FormValue("token") != "xoxp-explicit-synthetic" || r.URL.RawQuery != "" {
					t.Error("independent SDK request/auth contract changed")
				}
				w.Header().Set("Content-Type", "application/json")
				body := ""
				switch r.URL.Path {
				case "/api/users.info":
					body = `{"ok":true,"user":{"id":"UPEER","team_id":"TTEST"}}`
				case "/api/conversations.open":
					body = `{"ok":true,"channel":{"id":"DTEST"}}`
				case "/api/conversations.info":
					body = `{"ok":true,"channel":{"id":"DTEST","is_im":true}}`
				case "/api/conversations.members":
					body = `{"ok":true,"members":["UTEST","UPEER","UUNAPPROVED"]}`
				case "/api/conversations.history":
					body = `{"ok":true,"messages":[{"user":"UOTHER","text":"synthetic-run-123","ts":"1746284582.000001"}]}`
					if row == "incomplete reaction users" {
						body = strings.ReplaceAll(body, "UOTHER", "UTEST")
					}
				case "/api/conversations.replies":
					body = `{"ok":false,"error":"thread_not_found"}`
				case "/api/reactions.get":
					body = `{"ok":true,"type":"message","message":{"user":"UTEST","text":"synthetic-run-123","ts":"1746284582.000001","reactions":[{"name":"eyes","count":1,"users":[]}]}}`
				case "/api/files.info":
					body = `{"ok":false,"error":"missing_scope"}`
				default:
					t.Error("unexpected independent SDK request")
					w.WriteHeader(500)
				}
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			observer := &slackObserver{client: slack.New("xoxp-explicit-synthetic", slack.OptionAPIURL(server.URL+"/api/"), slack.OptionHTTPClient(&http.Client{Timeout: time.Second, Transport: liveReadTransport{base: http.DefaultTransport, origin: server.URL}})), actor: "UTEST", team: "TTEST", runID: "synthetic-run-123"}
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			if row == "foreign recipient membership" {
				if _, err := observer.Conversation(ctx, []string{"UPEER"}); err == nil {
					t.Fatal("foreign conversation member admitted")
				}
				return
			}
			receipt := liveReceipt{Kind: "message", Channel: "CTEST", TS: "1746284582.000001"}
			if row == "file permission is not absence" {
				receipt = liveReceipt{Kind: "file", FileID: "FTEST"}
			}
			if row == "incomplete reaction users" {
				receipt.Kind = "reaction"
				receipt.Emoji = "eyes"
			}
			if row == "missing parent is not reply absence" {
				receipt.ParentTS = "1746284582.000000"
			}
			if present, err := observer.Inspect(ctx, receipt); err == nil || present {
				t.Fatal("foreign or inaccessible resource considered safe/absent")
			}
		})
	}
}

func TestLiveIndependentReaderExhaustsPagesBeforeAbsence(t *testing.T) {
	for _, row := range []string{"history continues", "reply continues", "schedule continues", "empty final page", "repeated cursor", "missing acknowledgement", "permission failure"} {
		t.Run(row, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodPost || r.FormValue("token") != "xoxp-explicit-synthetic" {
					t.Error("pagination SDK request/auth changed")
				}
				w.Header().Set("Content-Type", "application/json")
				body := `{"ok":true,"messages":[],"has_more":true,"response_metadata":{"next_cursor":"A"}}`
				if row == "schedule continues" {
					body = `{"ok":true,"scheduled_messages":[],"response_metadata":{"next_cursor":"A"}}`
				}
				if r.FormValue("cursor") == "A" {
					body = `{"ok":true,"messages":[{"user":"UTEST","text":"synthetic-run-123","ts":"1746284582.000001"}]}`
					if row == "schedule continues" {
						body = `{"ok":true,"scheduled_messages":[{"id":"QTEST","channel_id":"CTEST","text":"synthetic-run-123","post_at":2000000000}]}`
					}
					if row == "empty final page" {
						body = `{"ok":true,"messages":[]}`
					}
					if row == "repeated cursor" {
						body = `{"ok":true,"messages":[],"has_more":true,"response_metadata":{"next_cursor":"A"}}`
					}
				}
				if row == "missing acknowledgement" {
					body = `{"messages":[]}`
				}
				if row == "permission failure" {
					body = `{"ok":false,"error":"missing_scope"}`
				}
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			observer := &slackObserver{client: slack.New("xoxp-explicit-synthetic", slack.OptionAPIURL(server.URL+"/api/"), slack.OptionHTTPClient(&http.Client{Timeout: time.Second, Transport: liveReadTransport{base: http.DefaultTransport, origin: server.URL}})), actor: "UTEST", runID: "synthetic-run-123"}
			receipt := liveReceipt{Kind: "message", Channel: "CTEST", TS: "1746284582.000001"}
			if row == "reply continues" {
				receipt.ParentTS = "1746284582.000000"
			}
			if row == "schedule continues" {
				receipt.Kind = "scheduled"
				receipt.ScheduledID = "QTEST"
			}
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			present, err := observer.Inspect(ctx, receipt)
			switch row {
			case "repeated cursor", "missing acknowledgement", "permission failure":
				if err == nil || present {
					t.Fatal("incomplete/error listing established absence")
				}
			case "empty final page":
				if err != nil || present || calls.Load() != 2 {
					t.Fatal("absence was not verified through final page")
				}
			default:
				if err != nil || !present || calls.Load() != 2 {
					t.Fatal("continuing empty page concealed exact receipt")
				}
			}
		})
	}
}

type syntheticLiveTransport func(*http.Request) (*http.Response, error)

func (f syntheticLiveTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestLiveReconciliationRequiresUniqueExhaustedOwnedMatch(t *testing.T) {
	for _, row := range []string{"unique receipt", "empty continuing page", "multiple matches", "repeated cursor", "permission failure", "foreign actor"} {
		t.Run(row, func(t *testing.T) {
			calls := 0
			wire := syntheticLiveTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Host != "fixture.invalid" || r.FormValue("token") != "xoxp-explicit-synthetic" {
					t.Fatal("reconciliation escaped synthetic fixture")
				}
				body := `{"ok":true,"messages":[{"user":"UTEST","text":"synthetic-run-123-created-1","ts":"1746284582.000001"}]}`
				switch row {
				case "empty continuing page":
					if calls == 1 {
						body = `{"ok":true,"messages":[],"has_more":true,"response_metadata":{"next_cursor":"A"}}`
					}
				case "multiple matches":
					body = `{"ok":true,"messages":[{"user":"UTEST","text":"synthetic-run-123-created-1","ts":"1746284582.000001"},{"user":"UTEST","text":"synthetic-run-123-created-1","ts":"1746284582.000002"}]}`
				case "repeated cursor":
					body = `{"ok":true,"messages":[],"has_more":true,"response_metadata":{"next_cursor":"A"}}`
				case "permission failure":
					body = `{"ok":false,"error":"missing_scope"}`
				case "foreign actor":
					body = strings.ReplaceAll(body, "UTEST", "UOTHER")
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			observer := &slackObserver{client: slack.New("xoxp-explicit-synthetic", slack.OptionAPIURL("https://fixture.invalid/api/"), slack.OptionHTTPClient(&http.Client{Transport: liveReadTransport{base: wire, origin: "https://fixture.invalid"}})), actor: "UTEST", runID: "synthetic-run-123"}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			matches, err := observer.Recover(ctx, liveReceipt{Kind: "uncertain-send", Channel: "CTEST", Marker: "synthetic-run-123-created-1", Oldest: "1746284580.000000", Latest: "1746284590.999999"})
			switch row {
			case "repeated cursor", "permission failure":
				if err == nil {
					t.Fatal("incomplete recovery claimed exhausted results")
				}
			case "multiple matches":
				if err != nil || len(matches) != 2 {
					t.Fatal("multiple owned receipts were collapsed")
				}
			case "foreign actor":
				if err != nil || len(matches) != 0 {
					t.Fatal("foreign actor was recoverable for deletion")
				}
			default:
				if err != nil || len(matches) != 1 || matches[0].TS != "1746284582.000001" {
					t.Fatal("unique receipt recovery failed")
				}
				if row == "empty continuing page" && calls != 2 {
					t.Fatal("recovery did not exhaust continuation")
				}
			}
		})
	}
}

func TestLiveWatchRelayRejectsUnapprovedForwarding(t *testing.T) {
	for _, row := range []string{"empty approved poll", "incomplete approved poll", "wrong credential", "wrong target", "mutation path"} {
		t.Run(row, func(t *testing.T) {
			var forwarded atomic.Int32
			upstream := &http.Client{Transport: syntheticLiveTransport(func(r *http.Request) (*http.Response, error) {
				forwarded.Add(1)
				if r.URL.String() != "https://slack.com/api/reactions.get" {
					t.Error("relay changed canonical destination")
				}
				payload := `{"ok":true,"type":"message","message":{"ts":"1746284582.000001","reactions":[]}}`
				if row == "incomplete approved poll" {
					payload = `{"ok":true,"type":"message","message":{"ts":"1746284582.000001","reactions":[{"name":"eyes","count":1,"users":[]}]}}`
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload)), Request: r}, nil
			})}
			relay, ready := newWatchRelay("xoxp-explicit-synthetic", liveReceipt{Channel: "CTEST", TS: "1746284582.000001"}, "UTEST", upstream)
			defer relay.Close()
			values := url.Values{"token": {"xoxp-explicit-synthetic"}, "channel": {"CTEST"}, "timestamp": {"1746284582.000001"}, "full": {"true"}}
			path := "/api/reactions.get"
			switch row {
			case "wrong credential":
				values.Set("token", "xoxp-other-synthetic")
			case "wrong target":
				values.Set("channel", "COTHER")
			case "mutation path":
				path = "/api/chat.postMessage"
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			request, _ := http.NewRequestWithContext(ctx, http.MethodPost, relay.URL+path, strings.NewReader(values.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			response, err := (&http.Client{Timeout: time.Second}).Do(request)
			if err != nil {
				t.Fatal("synthetic relay request failed")
			}
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			switch row {
			case "empty approved poll":
				select {
				case <-ready:
				case <-ctx.Done():
					t.Fatal("empty poll did not establish readiness")
				}
				if forwarded.Load() != 1 {
					t.Fatal("approved poll forwarding count changed")
				}
			case "incomplete approved poll":
				if response.StatusCode != 200 || forwarded.Load() != 1 {
					t.Fatal("approved partial poll was not forwarded exactly once")
				}
				select {
				case <-ready:
					t.Fatal("incomplete user list established empty-poll readiness")
				default:
				}
			default:
				if response.StatusCode != 400 || forwarded.Load() != 0 {
					t.Fatal("unapproved request reached upstream")
				}
				select {
				case <-ready:
					t.Fatal("rejected request established readiness")
				default:
				}
			}
		})
	}
}

func TestLivePlanRequiresExactNineUniqueKeys(t *testing.T) {
	plan := livePlan{RunRoot: "/synthetic", ConfigPath: "/synthetic/config.toml", Workspace: liveWorkspace, TeamID: "TTEST", ActorID: "UTEST", PublicChannel: "CTEST", PrivateChannel: "", Recipients: []string{}, RunID: "synthetic-run-123"}
	valid, _ := json.Marshal(plan)
	for _, name := range []string{"explicit empty optional values", "missing private channel", "missing recipients", "duplicate run id", "escaped duplicate key", "null recipients", "null private channel", "unknown key"} {
		t.Run(name, func(t *testing.T) {
			data := valid
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(valid, &fields)
			switch name {
			case "missing private channel":
				delete(fields, "private_channel_id")
				data, _ = json.Marshal(fields)
			case "missing recipients":
				delete(fields, "recipient_ids")
				data, _ = json.Marshal(fields)
			case "duplicate run id":
				data = append(slices.Clone(valid[:len(valid)-1]), []byte(`,"run_id":"synthetic-run-123"}`)...)
			case "escaped duplicate key":
				data = append(slices.Clone(valid[:len(valid)-1]), []byte(`,"private_\u0063hannel_id":""}`)...)
			case "null recipients":
				fields["recipient_ids"] = json.RawMessage("null")
				data, _ = json.Marshal(fields)
			case "null private channel":
				fields["private_channel_id"] = json.RawMessage("null")
				data, _ = json.Marshal(fields)
			case "unknown key":
				fields["unreviewed"] = json.RawMessage(`"synthetic-private"`)
				data, _ = json.Marshal(fields)
			}
			_, err := decodeLivePlan(bytes.NewReader(data))
			if (err == nil) != (name == "explicit empty optional values") {
				t.Fatal("plan schema accepted duplicate/missing/unknown keys or rejected explicit empty values")
			}
		})
	}
}

func TestLiveReactionAbsenceRequiresCompleteUsers(t *testing.T) {
	for _, tc := range []struct {
		name             string
		count            int
		users            []string
		present, unknown bool
	}{
		{name: "empty reaction list"},
		{name: "complete other actor", count: 1, users: []string{"UOTHER"}},
		{name: "incomplete users", count: 1, users: []string{}, unknown: true},
		{name: "observed actor in partial list", count: 2, users: []string{"UTEST"}, present: true},
		{name: "zero count with actor", count: 0, users: []string{"UTEST"}, unknown: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			items := []slack.ItemReaction{}
			if tc.name != "empty reaction list" {
				items = append(items, slack.ItemReaction{Name: "eyes", Count: tc.count, Users: tc.users})
			}
			present, err := reactionForActor(items, "eyes", "UTEST")
			if present != tc.present || (err != nil) != tc.unknown {
				t.Fatal("reaction presence/absence exceeded reported user evidence")
			}
		})
	}
}

type absentReplyObserver struct {
	syntheticObserver
	replyReads int
}

func (s *absentReplyObserver) Inspect(ctx context.Context, v liveReceipt) (bool, error) {
	if v.ParentTS != "" {
		s.replyReads++
		return false, errors.New("thread_not_found")
	}
	return s.syntheticObserver.Inspect(ctx, v)
}

func TestLiveVerifiedReplyAbsenceSurvivesParentDeletion(t *testing.T) {
	p := syntheticPlan(t)
	base := syntheticObserver{team: p.TeamID, actor: p.ActorID, present: map[string]bool{}}
	observer := &absentReplyObserver{syntheticObserver: base}
	r, err := newLiveRunner(p, "xoxp-explicit-synthetic", observer, func(context.Context, string, []string, []string, string) ([]byte, error) {
		t.Fatal("cleanup unexpectedly executed CLI")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.stop)
	parent := liveReceipt{Kind: "message", Channel: p.PublicChannel, TS: "1746284582.000001"}
	reply := liveReceipt{Kind: "message", Channel: p.PublicChannel, TS: "1746284582.000002", ParentTS: parent.TS}
	if !r.record(parent) || !r.record(reply) || r.markAbsent(reply) != nil {
		t.Fatal("could not retain independent reply absence proof")
	}
	if r.cleanup() != nil || observer.replyReads != 0 {
		t.Fatal("verified reply was re-queried after parent disappeared")
	}
}

func syntheticLiveEnvelope(command string, data any) []byte {
	out, _ := json.Marshal(map[string]any{"ok": true, "data": data, "errors": []any{}, "meta": map[string]any{"command": command, "workspace": liveWorkspace, "exit_code": 0}})
	return out
}

func TestLiveScheduledCLIRequiresExhaustedAbsence(t *testing.T) {
	for _, row := range []string{"receipt on next page", "empty final page", "repeated cursor"} {
		t.Run(row, func(t *testing.T) {
			p := syntheticPlan(t)
			observer := &syntheticObserver{team: p.TeamID, actor: p.ActorID, present: map[string]bool{}}
			calls := 0
			execute := func(_ context.Context, _ string, args, _ []string, _ string) ([]byte, error) {
				calls++
				if liveCLIPath(args) != "message.scheduled.list" || !slices.Contains(args, p.PublicChannel) {
					t.Fatal("scheduled list changed request target")
				}
				data := map[string]any{"scheduled_messages": []any{}, "pagination": map[string]any{"has_more": true, "next_cursor": "A"}}
				if calls > 1 && row != "repeated cursor" {
					data["pagination"] = map[string]any{"has_more": false}
					if row == "receipt on next page" {
						data["scheduled_messages"] = []any{map[string]any{"id": "QTEST", "channel": p.PublicChannel, "post_at": 2000000000, "text_preview": p.RunID}}
					}
				}
				return syntheticLiveEnvelope("message.scheduled.list", data), nil
			}
			r := syntheticRunner(t, p, observer, execute)
			if r.preflight() != nil {
				t.Fatal("synthetic preflight")
			}
			found, err := r.scheduledContains(t.Context(), liveReceipt{Kind: "scheduled", Channel: p.PublicChannel, ScheduledID: "QTEST"})
			if calls != 2 || (row == "repeated cursor" && err == nil) || (row != "repeated cursor" && (err != nil || found != (row == "receipt on next page"))) {
				t.Fatal("CLI scheduled page inspection invented absence or lost exact receipt")
			}
		})
	}
}

func TestLiveDiagnosticSummaryOmitsUnreviewedValues(t *testing.T) {
	p := syntheticPlan(t)
	observer := &syntheticObserver{team: p.TeamID, actor: p.ActorID}
	r := syntheticRunner(t, p, observer, func(context.Context, string, []string, []string, string) ([]byte, error) {
		return []byte(`{"errors":[{"type":"auth_failure","message":"synthetic-private-response"}]}`), errors.New("xoxp-synthetic-private-provider")
	})
	called := false
	r.onCommand = func(path string, exit int, kind, code string) {
		called = true
		if path != "unknown" || exit != -1 || kind != "auth_failure" || code != "" {
			t.Fatal("diagnostic summary leaked or invented command/error fields")
		}
	}
	_, _ = r.execute(t.Context(), []string{"message", "scheduled", "synthetic-private-command"}, r.env, "")
	if !called {
		t.Fatal("CLI result was not summarized")
	}
}

func TestLiveReadHealthUsesLocalEnvelopeScope(t *testing.T) {
	for _, row := range []struct {
		name      string
		args      []string
		scope     string
		wantError bool
	}{
		{"current", []string{"health", "current"}, "health", false},
		{"history", []string{"health", "history"}, "health", false},
		{"api-test", []string{"health", "api-test"}, "health", false},
		{"check", []string{"health", "check"}, "health", false},
		{"health rejects account scope", []string{"health", "current"}, liveWorkspace, true},
		{"ordinary rejects health scope", []string{"lookup", "user", "--user", "UTEST"}, "health", true},
	} {
		t.Run(row.name, func(t *testing.T) {
			p := syntheticPlan(t)
			observer := &syntheticObserver{team: p.TeamID, actor: p.ActorID}
			calls := 0
			r := syntheticRunner(t, p, observer, func(_ context.Context, _ string, args, _ []string, _ string) ([]byte, error) {
				calls++
				i := slices.Index(args, "--workspace")
				if i < 0 || i+1 >= len(args) || args[i+1] != p.Workspace {
					t.Fatal("local envelope scope changed authenticated workspace selection")
				}
				out, _ := json.Marshal(map[string]any{"ok": true, "data": map[string]any{"synthetic": true}, "errors": []any{}, "meta": map[string]any{"command": liveCLIPath(args), "workspace": row.scope, "exit_code": 0}})
				return out, nil
			})
			if r.preflight() != nil {
				t.Fatal("synthetic preflight")
			}
			data, err := r.read(t.Context(), row.args...)
			if calls != 1 || (err != nil) != row.wantError || (!row.wantError && !bytes.Contains(data, []byte("synthetic"))) {
				t.Fatal("read accepted the wrong envelope scope or rejected the documented local scope")
			}
		})
	}
}

func TestLiveRecipientAdmissionUsesConversationType(t *testing.T) {
	for _, row := range []string{"C-prefixed MPIM", "single IM", "wrong type", "wrong members", "wrong ID", "archived"} {
		t.Run(row, func(t *testing.T) {
			ids := []string{"UPEER", "UPEER2"}
			if row == "single IM" {
				ids = ids[:1]
			}
			infoReads, memberReads := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Method != http.MethodPost || req.FormValue("token") != "xoxp-explicit-synthetic" || req.URL.RawQuery != "" {
					t.Error("independent SDK request/auth contract changed")
				}
				w.Header().Set("Content-Type", "application/json")
				var data map[string]any
				switch req.URL.Path {
				case "/api/users.info":
					id := req.FormValue("user")
					if !slices.Contains(ids, id) {
						t.Error("unexpected recipient lookup")
					}
					data = map[string]any{"user": map[string]any{"id": id, "team_id": "TTEST"}}
				case "/api/conversations.open":
					if req.FormValue("users") != strings.Join(ids, ",") {
						t.Error("conversation recipients changed")
					}
					data = map[string]any{"channel": map[string]any{"id": "CTESTMPIM"}}
				case "/api/conversations.info":
					infoReads++
					if req.FormValue("channel") != "CTESTMPIM" {
						t.Error("conversation info target changed")
					}
					id := "CTESTMPIM"
					if row == "wrong ID" {
						id = "COTHER"
					}
					data = map[string]any{"channel": map[string]any{"id": id, "is_im": row == "single IM", "is_mpim": row != "single IM" && row != "wrong type", "is_archived": row == "archived"}}
				case "/api/conversations.members":
					memberReads++
					if req.FormValue("channel") != "CTESTMPIM" {
						t.Error("membership target changed")
					}
					if req.FormValue("cursor") == "" {
						data = map[string]any{"members": []string{}, "response_metadata": map[string]any{"next_cursor": "next"}}
					} else {
						if req.FormValue("cursor") != "next" {
							t.Error("membership cursor changed")
						}
						members := append([]string{"UTEST"}, ids...)
						if row == "wrong members" {
							members[len(members)-1] = "UFOREIGN"
						}
						data = map[string]any{"members": members}
					}
				default:
					t.Error("unexpected SDK method")
					data = map[string]any{}
				}
				data["ok"] = true
				_ = json.NewEncoder(w).Encode(data)
			}))
			defer server.Close()
			observer := &slackObserver{client: slack.New("xoxp-explicit-synthetic", slack.OptionAPIURL(server.URL+"/api/"), slack.OptionHTTPClient(&http.Client{Timeout: time.Second, Transport: liveReadTransport{base: http.DefaultTransport, origin: server.URL}})), actor: "UTEST", team: "TTEST"}
			r := &liveRunner{ready: true, ctx: t.Context(), observer: observer, plan: livePlan{Recipients: ids}, conversations: map[string]string{}}
			channel, err := r.admitRecipients(ids)
			want := row == "C-prefixed MPIM" || row == "single IM"
			if want {
				if err != nil || channel != "CTESTMPIM" || infoReads != 1 || memberReads != 2 || !r.allowedChannel(channel) {
					t.Fatal("valid typed conversation was not admitted after complete membership proof")
				}
			} else if err == nil || channel != "" || len(r.conversations) != 0 {
				t.Fatal("wrong conversation type/identity/membership was admitted")
			}
		})
	}
}

func TestLiveThreadPaginationRequiresCompleteDistinctPages(t *testing.T) {
	const first = `{"messages":[{"ts":"1746284582.000002","thread_ts":"1746284582.000001"}],"pagination":{"has_more":true,"next_cursor":"next-page"}}`
	const last = `{"messages":[{"ts":"1746284582.000003","thread_ts":"1746284582.000001"}],"pagination":{"has_more":false,"next_cursor":""}}`
	for _, tc := range []struct {
		name  string
		pages []string
		want  string
	}{
		{name: "all replies across two pages", pages: []string{first, last}},
		{name: "missing owned reply", pages: []string{first, `{"messages":[],"pagination":{"has_more":false}}`}, want: "concealed an owned reply"},
		{name: "duplicate owned reply", pages: []string{first, strings.Replace(last, `"messages":[`, `"messages":[{"ts":"1746284582.000002","thread_ts":"1746284582.000001"},`, 1)}, want: "duplicated a reply"},
		{name: "reply changed parent", pages: []string{first, strings.ReplaceAll(last, "1746284582.000001", "1746284582.000099")}, want: "changed its parent"},
		{name: "repeated cursor", pages: []string{first, strings.ReplaceAll(last, `"has_more":false,"next_cursor":""`, `"has_more":true,"next_cursor":"next-page"`)}, want: "continuation is missing or repeated"},
		{name: "missing pagination", pages: []string{first, `{"messages":[]}`}, want: "lost messages or pagination"},
		{name: "single page is not pagination", pages: []string{`{"messages":[{"ts":"1746284582.000002","thread_ts":"1746284582.000001"},{"ts":"1746284582.000003","thread_ts":"1746284582.000001"}],"pagination":{"has_more":false}}`}, want: "pagination was not exercised"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := syntheticPlan(t)
			calls := 0
			execute := func(_ context.Context, _ string, args, _ []string, _ string) ([]byte, error) {
				if calls >= len(tc.pages) {
					t.Fatal("unexpected extra page request")
				}
				for flag, want := range map[string]string{"--channel": p.PublicChannel, "--thread": "1746284582.000001", "--max-items": "1"} {
					i := slices.Index(args, flag)
					if i < 0 || i+1 >= len(args) || args[i+1] != want {
						t.Fatalf("thread request changed %s", flag)
					}
				}
				i := slices.Index(args, "--cursor")
				if (calls == 0 && i >= 0) || (calls > 0 && (i < 0 || i+1 >= len(args) || args[i+1] != "next-page")) {
					t.Fatal("thread cursor was not forwarded exactly")
				}
				data := tc.pages[calls]
				calls++
				return []byte(`{"ok":true,"meta":{"command":"history.list","workspace":"live-oauth-isolated","exit_code":0},"data":` + data + `,"errors":[]}`), nil
			}
			r := syntheticRunner(t, p, &syntheticObserver{team: p.TeamID, actor: p.ActorID}, execute)
			if r.preflight() != nil {
				t.Fatal("synthetic thread preflight")
			}
			parent := liveReceipt{Kind: "message", Channel: p.PublicChannel, TS: "1746284582.000001"}
			replies := []liveReceipt{{Kind: "message", Channel: p.PublicChannel, TS: "1746284582.000002", ParentTS: parent.TS}, {Kind: "message", Channel: p.PublicChannel, TS: "1746284582.000003", ParentTS: parent.TS}}
			r.receipts = append([]liveReceipt{parent}, replies...)
			err := r.threadReplies(t.Context(), parent, replies)
			if tc.want == "" && err != nil {
				t.Fatal(err)
			}
			if tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("thread pagination must reject %s, got %v", tc.name, err)
			}
			if calls != len(tc.pages) {
				t.Fatalf("read %d pages, want %d", calls, len(tc.pages))
			}
		})
	}
}

func TestLiveFileAbsenceRequiresDocumentedSlackResponse(t *testing.T) {
	for _, code := range []string{"file_deleted", "file_not_found", "missing_scope", "no_permission", "fatal_error"} {
		t.Run(code, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != "/api/files.info" || r.URL.RawQuery != "" || r.FormValue("file") != "FTEST" || r.FormValue("token") != "xoxp-explicit-synthetic" {
					t.Error("exact file inspection request/auth changed")
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": code})
			}))
			defer server.Close()
			observer := &slackObserver{client: slack.New("xoxp-explicit-synthetic", slack.OptionAPIURL(server.URL+"/api/"), slack.OptionHTTPClient(&http.Client{Timeout: time.Second, Transport: liveReadTransport{base: http.DefaultTransport, origin: server.URL}}))}
			present, err := observer.Inspect(t.Context(), liveReceipt{Kind: "file", FileID: "FTEST"})
			absent := code == "file_deleted" || code == "file_not_found"
			if present || (err == nil) != absent {
				t.Fatalf("file absence for %s: present=%v, error=%v", code, present, err)
			}
			if calls != 1 {
				t.Fatalf("file read was retried: %d calls", calls)
			}
		})
	}
}

func TestLiveListPaginationRejectsIncompleteTraversal(t *testing.T) {
	const first = `{"users":[{"id":"UONE"}],"pagination":{"has_more":true,"next_cursor":"next-page"}}`
	const last = `{"users":[{"id":"UTWO"}],"pagination":{"has_more":false}}`
	for _, tc := range []struct {
		name      string
		pages     []string
		wantError bool
	}{
		{name: "complete", pages: []string{first, last}},
		{name: "empty intermediate page", pages: []string{`{"users":[],"pagination":{"has_more":true,"next_cursor":"next-page"}}`, `{"users":[{"id":"UONE"},{"id":"UTWO"}],"pagination":{"has_more":false}}`}},
		{name: "duplicate row", pages: []string{first, `{"users":[{"id":"UONE"},{"id":"UTWO"}],"pagination":{"has_more":false}}`}, wantError: true},
		{name: "missing identity", pages: []string{first, `{"users":[{}],"pagination":{"has_more":false}}`}, wantError: true},
		{name: "missing pagination", pages: []string{first, `{"users":[]}`}, wantError: true},
		{name: "repeated cursor", pages: []string{first, strings.Replace(last, `"has_more":false`, `"has_more":true,"next_cursor":"next-page"`, 1)}, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := syntheticPlan(t)
			calls := 0
			r := syntheticRunner(t, p, &syntheticObserver{team: p.TeamID, actor: p.ActorID}, func(_ context.Context, _ string, args, _ []string, _ string) ([]byte, error) {
				if calls >= len(tc.pages) {
					t.Fatal("unexpected pagination replay")
				}
				index := slices.Index(args, "--cursor")
				if (calls == 0 && index >= 0) || (calls > 0 && (index < 0 || index+1 >= len(args) || args[index+1] != "next-page")) {
					t.Fatal("list cursor not forwarded exactly")
				}
				data := tc.pages[calls]
				calls++
				return []byte(`{"ok":true,"meta":{"command":"lookup.user","workspace":"live-oauth-isolated","exit_code":0},"data":` + data + `,"errors":[]}`), nil
			})
			if r.preflight() != nil {
				t.Fatal("synthetic list preflight")
			}
			rows, pages, err := r.listPages(t.Context(), []string{"lookup", "user", "--max-items", "1"}, "users", "id")
			if (err != nil) != tc.wantError {
				t.Fatalf("list traversal error=%v, want failure=%v", err, tc.wantError)
			}
			if calls != len(tc.pages) || pages != len(tc.pages) {
				t.Fatal("list traversal did not consume the expected pages")
			}
			if !tc.wantError && (len(rows) != 2 || rows["UONE"] == nil || rows["UTWO"] == nil) {
				t.Fatal("complete traversal lost distinct rows")
			}
		})
	}
}
