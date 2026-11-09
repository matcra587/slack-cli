package live_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const faultCanary = "SLICK_SYNTHETIC_DIAGNOSTIC_CANARY_DO_NOT_PRINT"

type faultCounts struct {
	Attempts, Forwarded, Rejected int
	Committed                     liveReceipt
	ControlError                  bool
}

type faultRelay struct {
	server *httptest.Server
	mu     sync.Mutex
	counts faultCounts
}

func (f *faultRelay) snapshot() faultCounts {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts
}

// Only canonical Slack or an explicit loopback fixture may receive credentials.
func faultOriginAllowed(base string) bool {
	u, err := url.Parse(base)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/api/" {
		return false
	}
	if base == "https://slack.com/api/" {
		return true
	}
	ip := net.ParseIP(u.Hostname())
	return u.Scheme == "http" && ip != nil && ip.IsLoopback()
}

func newFaultRelay(token, mode, base string, plan livePlan, upstream *http.Client) (*faultRelay, error) {
	if token == "" || !faultOriginAllowed(base) {
		return nil, errLiveSafety
	}
	f := &faultRelay{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, in *http.Request) {
		reject := func() {
			f.mu.Lock()
			f.counts.Rejected++
			f.mu.Unlock()
			w.WriteHeader(http.StatusBadRequest)
		}
		body, err := io.ReadAll(io.LimitReader(in.Body, (1<<20)+1))
		values, parseErr := url.ParseQuery(string(body))
		method := strings.TrimPrefix(in.URL.Path, "/api/")
		allowed := map[string][]string{
			"auth.test":          {"token"},
			"users.info":         {"token", "user", "include_locale"},
			"users.list":         {"token", "limit", "cursor", "include_locale", "team_id"},
			"conversations.info": {"token", "channel", "include_locale", "include_num_members"},
			"chat.postMessage":   {"token", "channel", "text", "blocks", "attachments", "parse", "link_names", "mrkdwn", "unfurl_links", "unfurl_media", "reply_broadcast", "as_user", "client_msg_id"},
		}
		keys, known := allowed[method]
		if err != nil || parseErr != nil || len(body) > 1<<20 || in.Method != http.MethodPost || in.URL.Path != "/api/"+method || in.URL.RawQuery != "" || !known || !strings.HasPrefix(in.Header.Get("Content-Type"), "application/x-www-form-urlencoded") || values.Get("token") != token || len(values["token"]) != 1 {
			reject()
			return
		}
		for key, items := range values {
			if !slices.Contains(keys, key) || len(items) != 1 {
				reject()
				return
			}
		}
		if (method == "users.info" && values.Get("user") != plan.ActorID) ||
			(method == "conversations.info" && values.Get("channel") != plan.PublicChannel) ||
			(method == "chat.postMessage" && (mode != "lost-write" || values.Get("channel") != plan.PublicChannel || !strings.Contains(values.Get("text"), plan.RunID+"-created-1") || values.Get("attachments") != "")) {
			reject()
			return
		}
		target := method == "users.info" || (mode == "cache-503" && method == "users.list") || method == "chat.postMessage"
		attempt := 0
		if target {
			f.mu.Lock()
			f.counts.Attempts++
			attempt = f.counts.Attempts
			f.mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		if target {
			switch {
			case mode == "429-exhausted" || (mode == "429-once" && attempt == 1):
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = io.WriteString(w, `{"ok":false,"error":"ratelimited"}`)
				return
			case mode == "malformed":
				_, _ = io.WriteString(w, `{"ok":true,"user":`+faultCanary)
				return
			case mode == "cache-503" && method == "users.list" && attempt == 2:
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = io.WriteString(w, `{"ok":false,"error":"service_unavailable"}`)
				return
			case mode == "deadline":
				select {
				case <-in.Context().Done():
				case <-time.After(3 * time.Second):
				}
				return
			case mode == "lost-write" && attempt > 1:
				// Detect a retry without permitting a second real mutation.
				w.WriteHeader(http.StatusConflict)
				_, _ = io.WriteString(w, `{"ok":false,"error":"duplicate_attempt_blocked"}`)
				return
			}
		}
		upstreamMethod := method
		if mode == "permission" && method == "users.info" {
			// A real Slack error is relayed, with the endpoint substitution explicit.
			upstreamMethod = "users.profile.get"
			body = []byte(url.Values{"token": {token}, "user": {plan.ActorID}}.Encode())
		}
		request, err := http.NewRequestWithContext(in.Context(), http.MethodPost, base+upstreamMethod, bytes.NewReader(body))
		if err != nil {
			reject()
			return
		}
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if target {
			f.mu.Lock()
			f.counts.Forwarded++
			f.mu.Unlock()
		}
		response, err := upstream.Do(request)
		if err != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		defer func() { _ = response.Body.Close() }()
		payload, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
		if err != nil || len(payload) > 8<<20 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		if mode == "lost-write" && method == "chat.postMessage" {
			var ack struct {
				OK          bool
				Channel, TS string
				Message     struct{ User string }
			}
			if json.Unmarshal(payload, &ack) != nil || !ack.OK || ack.Channel != plan.PublicChannel || !slackTS.MatchString(ack.TS) || (ack.Message.User != "" && ack.Message.User != plan.ActorID) {
				f.mu.Lock()
				f.counts.ControlError = true
				f.mu.Unlock()
			} else {
				receipt := liveReceipt{Kind: "message", Channel: ack.Channel, TS: ack.TS}
				f.mu.Lock()
				f.counts.Committed = receipt
				f.mu.Unlock()
				raw, _ := json.Marshal(receipt)
				path := filepath.Join(plan.RunRoot, "fault-committed-"+plan.RunID+".json")
				file, writeErr := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
				if writeErr == nil {
					_, writeErr = file.Write(raw)
					if writeErr == nil {
						writeErr = file.Sync()
					}
					closeErr := file.Close()
					if writeErr == nil {
						writeErr = closeErr
					}
				}
				if writeErr != nil {
					f.mu.Lock()
					f.counts.ControlError = true
					f.mu.Unlock()
				}
			}
		}
		if (mode == "drop-read" && method == "users.info") || (mode == "lost-write" && method == "chat.postMessage") {
			connection, _, hijackErr := w.(http.Hijacker).Hijack()
			if hijackErr == nil {
				_ = connection.Close()
			}
			return
		}
		for _, key := range []string{"Content-Type", "X-OAuth-Scopes", "Retry-After"} {
			for _, value := range response.Header.Values(key) {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(response.StatusCode)
		_, _ = w.Write(payload)
	}))
	return f, nil
}

// Diagnostics stay in bounded memory and are checked before any publication.
func executeFaultChild(ctx context.Context, binary string, args, env []string, stdin string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env, cmd.Stdin, cmd.WaitDelay = env, strings.NewReader(stdin), 2*time.Second
	var out, diagnostic liveOutputBuffer
	cmd.Stdout, cmd.Stderr = &out, &diagnostic
	err := cmd.Run()
	secret := ""
	for _, value := range env {
		if after, ok := strings.CutPrefix(value, "SLICK_LIVE_ACCESS_TOKEN="); ok {
			secret = after
		}
	}
	if strings.Contains(string(out.Bytes()), faultCanary) || strings.Contains(string(diagnostic.Bytes()), faultCanary) || (secret != "" && (strings.Contains(string(out.Bytes()), secret) || strings.Contains(string(diagnostic.Bytes()), secret))) {
		return nil, errors.New("sensitive data entered CLI output; raw diagnostics withheld")
	}
	return out.Bytes(), err
}

// These controls use synthetic credentials and loopback only.
func TestFaultRelayValidatesDestinationAndOwnedRequests(t *testing.T) {
	for _, row := range []struct {
		name, method, path, user, channel, text, token string
		want                                           int
	}{
		{name: "approved self read", method: "POST", path: "users.info", user: "UTEST", token: "xoxp-explicit-synthetic", want: 200},
		{name: "foreign actor", method: "POST", path: "users.info", user: "UOTHER", token: "xoxp-explicit-synthetic", want: 400},
		{name: "wrong credential", method: "POST", path: "users.info", user: "UTEST", token: "xoxp-other-synthetic", want: 400},
		{name: "query string", method: "POST", path: "users.info?extra=1", user: "UTEST", token: "xoxp-explicit-synthetic", want: 400},
		{name: "wrong HTTP method", method: "GET", path: "users.info", user: "UTEST", token: "xoxp-explicit-synthetic", want: 400},
		{name: "unknown method", method: "POST", path: "users.profile.set", token: "xoxp-explicit-synthetic", want: 400},
		{name: "foreign mutation target", method: "POST", path: "chat.postMessage", channel: "COTHER", text: "synthetic-run-123-created-1", token: "xoxp-explicit-synthetic", want: 400},
		{name: "missing owned marker", method: "POST", path: "chat.postMessage", channel: "CTEST", text: "unrelated", token: "xoxp-explicit-synthetic", want: 400},
	} {
		t.Run(row.name, func(t *testing.T) {
			p := syntheticPlan(t)
			var forwards atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				forwards.Add(1)
				if r.Method != "POST" || r.URL.Path != "/api/users.info" || r.FormValue("token") != "xoxp-explicit-synthetic" || r.FormValue("user") != "UTEST" {
					t.Error("upstream request contract changed")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"ok":true,"user":{"id":"UTEST"}}`)
			}))
			defer upstream.Close()
			relay, err := newFaultRelay("xoxp-explicit-synthetic", "lost-write", upstream.URL+"/api/", p, upstream.Client())
			if err != nil {
				t.Fatal(err)
			}
			defer relay.server.Close()
			values := url.Values{"token": {row.token}}
			if row.user != "" {
				values.Set("user", row.user)
			}
			if row.channel != "" {
				values.Set("channel", row.channel)
			}
			if row.text != "" {
				values.Set("text", row.text)
			}
			req, _ := http.NewRequestWithContext(t.Context(), row.method, relay.server.URL+"/api/"+row.path, strings.NewReader(values.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			response, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal("local relay request failed")
			}
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode != row.want || forwards.Load() != map[bool]int32{true: 1, false: 0}[row.want == 200] {
				t.Fatalf("relay status=%d, upstream calls=%d", response.StatusCode, forwards.Load())
			}
		})
	}
	for _, bad := range []string{"https://unapproved.example/api/", "http://slack.com/api/", "https://slack.com/api/?token=x", "https://user@slack.com/api/"} {
		if faultOriginAllowed(bad) {
			t.Fatal("unapproved credential destination accepted")
		}
	}
}

func TestFaultRelayRecordsCommitAndBlocksDuplicateMutation(t *testing.T) {
	p := syntheticPlan(t)
	var forwards atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwards.Add(1)
		if r.URL.Path != "/api/chat.postMessage" || r.FormValue("channel") != p.PublicChannel {
			t.Error("wrong mutation destination")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true,"channel":"CTEST","ts":"1746284582.000001","message":{"user":"UTEST"}}`)
	}))
	defer upstream.Close()
	relay, err := newFaultRelay("xoxp-explicit-synthetic", "lost-write", upstream.URL+"/api/", p, upstream.Client())
	if err != nil {
		t.Fatal(err)
	}
	defer relay.server.Close()
	for attempt := range 2 {
		values := url.Values{"token": {"xoxp-explicit-synthetic"}, "channel": {p.PublicChannel}, "text": {p.RunID + "-created-1"}}
		response, err := http.PostForm(relay.server.URL+"/api/chat.postMessage", values)
		if response != nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
		if attempt == 0 && err == nil {
			t.Fatal("commit acknowledgement was not interrupted")
		}
		if attempt == 1 && (err != nil || response.StatusCode != 409) {
			t.Fatal("second mutation was not blocked")
		}
	}
	counts := relay.snapshot()
	if forwards.Load() != 1 || counts.Attempts != 2 || counts.Forwarded != 1 || counts.Committed.TS != "1746284582.000001" || counts.ControlError {
		t.Fatal("commit/duplicate safety evidence incomplete")
	}
	info, err := os.Stat(filepath.Join(p.RunRoot, "fault-committed-"+p.RunID+".json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("exact commit recovery receipt was not protected")
	}
}

func TestFaultCaptureRejectsSensitiveDiagnostics(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("synthetic shell diagnostic check uses /bin/sh")
	}
	for _, row := range []struct {
		name, script string
		reject       bool
	}{
		{name: "credential stdout", script: `printf '%s' "$SLICK_LIVE_ACCESS_TOKEN"`, reject: true},
		{name: "credential stderr", script: `printf '%s' "$SLICK_LIVE_ACCESS_TOKEN" >&2`, reject: true},
		{name: "diagnostic canary", script: "printf '%s' " + faultCanary, reject: true},
		{name: "safe diagnostic", script: `printf 'safe output'; printf 'safe diagnostic' >&2`},
	} {
		t.Run(row.name, func(t *testing.T) {
			out, err := executeFaultChild(t.Context(), "/bin/sh", []string{"-c", row.script}, []string{"PATH=/usr/bin:/bin", "SLICK_LIVE_ACCESS_TOKEN=xoxp-explicit-synthetic"}, "")
			if (err != nil) != row.reject {
				t.Fatal("sensitive diagnostic guard did not match expectation")
			}
			if row.reject && len(out) != 0 {
				t.Fatal("sensitive output escaped the guard")
			}
		})
	}
}
