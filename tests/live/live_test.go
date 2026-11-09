//go:build live

// Package live_test validates only explicitly approved isolated Slack fixtures.
// No ordinary account defaults, status changes, credential replacement or revoke.
package live_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	slack "github.com/slack-go/slack"
)

func setupLive(t *testing.T) (*liveRunner, *slackObserver) {
	t.Helper()
	p, err := loadLivePlan(os.Getenv)
	if errors.Is(err, errLiveDisabled) {
		t.Skip("live disabled: explicit designated-fixtures confirmation required")
	}
	if err != nil {
		t.Fatal("invalid isolated live plan")
	}
	if deadline, ok := t.Deadline(); ok && time.Until(deadline) < 11*time.Minute {
		t.Fatal("live runner requires at least 11 minutes including cleanup reserve")
	}
	// Separate scenario ledgers let focused tests share the approved plan safely.
	switch t.Name() {
	case "TestLiveThreadPagination":
		p.RunID = p.RunID[:min(len(p.RunID), 56)] + "-thread"
	case "TestLiveFileUpload":
		p.RunID = p.RunID[:min(len(p.RunID), 56)] + "-upload"
	}
	// Read the sole approved credential only after the complete plan/config gate.
	token := os.Getenv("SLICK_LIVE_ACCESS_TOKEN")
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	t.Cleanup(transport.CloseIdleConnections)
	observer := &slackObserver{runID: p.RunID, actor: p.ActorID, team: p.TeamID, public: p.PublicChannel, private: p.PrivateChannel, client: slack.New(token, slack.OptionHTTPClient(&http.Client{Transport: liveReadTransport{base: transport, origin: "https://slack.com"}, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}))}
	runner, err := newLiveRunner(p, token, observer, executeLiveChild)
	if err != nil {
		t.Fatal("isolated live runner could not initialize")
	}
	t.Cleanup(func() {
		if runner.cleanup() != nil {
			t.Error("live cleanup incomplete; exact run requires private inspection")
		}
	})
	if runner.preflight() != nil {
		t.Fatal("live identity/channel preflight failed; no mutations authorized")
	}
	runner.allowFiles = slices.Contains(observer.scopes, "files:read") && slices.Contains(observer.scopes, "files:write")
	runner.onCommand = func(path string, exit int, kind, code string) {
		t.Logf("CLI %s exit=%d type=%s code=%s", path, exit, kind, code)
	}
	return runner, observer
}

// TestLiveThreadPagination requires continuation from its own larger thread.
func TestLiveThreadPagination(t *testing.T) {
	r, s := setupLive(t)
	liveCall(t, r, "send", r.plan.PublicChannel, "", "Release pagination review "+r.plan.RunID)
	parent := lastReceipt(t, r)
	readMessage(t, r, s, parent, r.plan.RunID)
	replies := make([]liveReceipt, 0, 5)
	for i := range 5 {
		body := fmt.Sprintf("Review item %d of 5: %s", i+1, r.plan.RunID)
		liveCall(t, r, "reply", parent.Channel, parent.TS, body)
		reply := lastReceipt(t, r)
		readMessage(t, r, s, reply, body)
		replies = append(replies, reply)
	}
	ctx, cancel := context.WithTimeout(r.ctx, time.Minute)
	defer cancel()
	if err := r.threadReplies(ctx, parent, replies); err != nil {
		t.Fatal(err)
	}
}

func TestLiveFileUpload(t *testing.T) {
	r, s := setupLive(t)
	if !slices.Contains(s.scopes, "files:read") || !slices.Contains(s.scopes, "files:write") {
		t.Skip("upload unverified: files:read and files:write are both required before creation")
	}
	path := filepath.Join(r.plan.RunRoot, "artifact-"+r.plan.RunID+".txt")
	if os.WriteFile(path, []byte("Synthetic release review "+r.plan.RunID), 0o600) != nil {
		t.Fatal("cannot prepare isolated upload")
	}
	liveCall(t, r, "upload", r.plan.PublicChannel, path, "Review "+r.plan.RunID)
	ctx, cancel := context.WithTimeout(r.ctx, 20*time.Second)
	present, err := s.Inspect(ctx, lastReceipt(t, r))
	cancel()
	if err != nil || !present {
		t.Fatal("independent file readback failed")
	}
}

func liveCall(t *testing.T, r *liveRunner, op, ch, target, body string) {
	t.Helper()
	out, err := r.command(op, ch, target, body)
	if err != nil {
		t.Fatal(err)
	}
	commands := map[string]string{"send": "message.send", "send-users": "message.send", "send-users-repeated": "message.send", "reply": "reply", "edit": "message.edit", "delete": "message.delete", "add": "react.add", "remove": "react.remove", "watch": "react.watch", "schedule": "message.send", "schedule-users": "message.send", "cancel": "message.scheduled.delete", "upload": "file.upload"}
	checkLiveFrame(t, out, commands[op], r.plan.Workspace)
}

func checkLiveFrame(t *testing.T, out []byte, command, workspace string) json.RawMessage {
	t.Helper()
	data, err := decodeLiveResult(out, command, workspace)
	if err != nil {
		t.Fatal("live result envelope mismatch")
	}
	return data
}

func lastReceipt(t *testing.T, r *liveRunner) liveReceipt {
	t.Helper()
	if len(r.receipts) == 0 {
		t.Fatal("missing exact live receipt")
	}
	return r.receipts[len(r.receipts)-1]
}

func readMessage(t *testing.T, r *liveRunner, s *slackObserver, v liveReceipt, want string) *slack.Message {
	t.Helper()
	ctx, cancel := context.WithTimeout(r.ctx, 20*time.Second)
	defer cancel()
	m, err := s.message(ctx, v)
	blocks, _ := json.Marshal(m)
	if err != nil || m == nil || m.Timestamp != v.TS || (!strings.Contains(m.Text, want) && !strings.Contains(string(blocks), want)) {
		t.Fatal("independent message readback failed")
	}
	if v.ParentTS != "" && m.ThreadTimestamp != v.ParentTS {
		t.Fatal("independent reply parent mismatch")
	}
	if len(m.Blocks.BlockSet) == 0 {
		t.Fatal("independent readback lost rich content blocks")
	}
	return m
}

// One lifecycle owns exact receipts; cleanup runs even after a fatal assertion.
func TestLiveApprovedFixtures(t *testing.T) {
	r, s := setupLive(t)
	var publicParent liveReceipt
	for index, channel := range []string{r.plan.PublicChannel, r.plan.PrivateChannel} {
		if channel == "" {
			t.Run("private channel lifecycle", func(t *testing.T) { t.Skip("private channel not planned") })
			continue
		}
		t.Run([]string{"public channel lifecycle", "private channel lifecycle"}[index], func(t *testing.T) {
			body := "*Release review* `" + r.plan.RunID + "`: ready for approval.\n\n- Checks complete\n- Documentation reviewed 👀\n\n| Check | Result |\n| --- | --- |\n| **Tests** | `Ready` |\n| Docs | Reviewed |"
			liveCall(t, r, "send", channel, "", body)
			parent := lastReceipt(t, r)
			if index == 0 {
				publicParent = parent
			}
			message := readMessage(t, r, s, parent, r.plan.RunID)
			assertNativeTable(t, message)
			liveCall(t, r, "edit", channel, parent.TS, "Release review "+r.plan.RunID+": approved.")
			readMessage(t, r, s, parent, "approved")
			liveCall(t, r, "reply", channel, parent.TS, "Follow-up "+r.plan.RunID+": checks complete.")
			reply := lastReceipt(t, r)
			readMessage(t, r, s, reply, r.plan.RunID)
			if index == 0 {
				watchBeforeAdd(t, r, s, parent)
			} else {
				liveCall(t, r, "add", channel, parent.TS, "")
			}
			ctx, cancel := context.WithTimeout(r.ctx, 20*time.Second)
			item, err := s.client.GetReactionsContext(ctx, slack.NewRefToMessage(channel, parent.TS), slack.GetReactionsParameters{Full: true})
			cancel()
			present, stateErr := reactionForActor(item.Reactions, "eyes", r.plan.ActorID)
			if err != nil || stateErr != nil || !present {
				t.Fatal("independent reaction readback failed")
			}
			assertCLIReactions(t, r, parent, true)
			liveCall(t, r, "watch", channel, parent.TS, "")
			liveCall(t, r, "remove", channel, parent.TS, "")
			ctx, cancel = context.WithTimeout(r.ctx, 20*time.Second)
			item, err = s.client.GetReactionsContext(ctx, slack.NewRefToMessage(channel, parent.TS), slack.GetReactionsParameters{Full: true})
			cancel()
			present, stateErr = reactionForActor(item.Reactions, "eyes", r.plan.ActorID)
			if err != nil || stateErr != nil || present {
				t.Fatal("independent reaction removal failed")
			}
			assertCLIReactions(t, r, parent, false)
			if r.markAbsent(liveReceipt{Kind: "reaction", Channel: channel, TS: parent.TS, Emoji: "eyes"}) != nil {
				t.Fatal("cannot retain reaction absence proof")
			}
			liveCall(t, r, "schedule", channel, "", "Queued review "+r.plan.RunID)
			scheduled := lastReceipt(t, r)
			ctx, cancel = context.WithTimeout(r.ctx, 20*time.Second)
			present, err = s.Inspect(ctx, scheduled)
			cancel()
			if err != nil || !present {
				t.Fatal("independent schedule readback failed")
			}
			assertCLIScheduled(t, r, s, scheduled, true)
			liveCall(t, r, "cancel", channel, scheduled.ScheduledID, "")
			ctx, cancel = context.WithTimeout(r.ctx, 20*time.Second)
			present, err = s.Inspect(ctx, scheduled)
			cancel()
			if err != nil || present {
				t.Fatal("independent schedule cancellation failed")
			}
			assertCLIScheduled(t, r, s, scheduled, false)
		})
	}
	t.Run("recipient workflows", func(t *testing.T) {
		if len(r.plan.Recipients) < 2 {
			t.Skip("DM/group-DM unverified: two consenting peer IDs must be explicitly planned")
		}
		for _, ids := range [][]string{r.plan.Recipients[:1], r.plan.Recipients[:2]} {
			name := "DM"
			if len(ids) == 2 {
				name = "group DM"
			}
			t.Run(name, func(t *testing.T) {
				channel, err := r.admitRecipients(ids)
				if err != nil {
					t.Fatal("independent recipient conversation preflight failed")
				}
				target := strings.Join(ids, ",")
				liveCall(t, r, "send-users", channel, target, "Recipient review "+r.plan.RunID+": ready for approval.")
				readMessage(t, r, s, lastReceipt(t, r), r.plan.RunID)
				if len(ids) == 2 {
					liveCall(t, r, "send-users-repeated", channel, target, "Repeated recipients "+r.plan.RunID+": checks complete.")
					readMessage(t, r, s, lastReceipt(t, r), r.plan.RunID)
				}
				liveCall(t, r, "schedule-users", channel, target, "Recipient queue "+r.plan.RunID)
				scheduled := lastReceipt(t, r)
				assertCLIScheduled(t, r, s, scheduled, true)
				liveCall(t, r, "cancel", channel, scheduled.ScheduledID, "")
				assertCLIScheduled(t, r, s, scheduled, false)
			})
		}
	})
	t.Run("read-only output modes", func(t *testing.T) {
		for _, mode := range []string{"json", "compact", "human"} {
			ctx, cancel := context.WithTimeout(r.ctx, 20*time.Second)
			out, err := r.execute(ctx, []string{"history", "list", "--workspace", r.plan.Workspace, "--channel", r.plan.PublicChannel, "--max-items", "10", "--output=" + mode, "--color=never", "--timeout=10s"}, r.env, "")
			cancel()
			if err != nil || len(out) == 0 {
				t.Fatal("live read-only output failed")
			}
			if mode != "human" {
				data := out
				if mode == "json" {
					data = checkLiveFrame(t, out, "history.list", r.plan.Workspace)
				}
				var result struct{ Messages []json.RawMessage }
				if json.Unmarshal(data, &result) != nil || result.Messages == nil {
					t.Fatal("live history output shape mismatch")
				}
			}
		}
	})
	t.Run("CLI lookup channel and user", func(t *testing.T) { assertCLILookups(t, r) })
	t.Run("CLI unique run search", func(t *testing.T) { assertCLISearch(t, r, s, publicParent) })
	t.Run("CLI isolated cache lifecycle", func(t *testing.T) { assertCLICaches(t, r) })
	t.Run("CLI health reads", func(t *testing.T) { assertCLIHealth(t, r) })
	t.Run("CLI exact message deletion", func(t *testing.T) { deleteOwnedMessages(t, r, s) })
	t.Run("auth last read only", func(t *testing.T) {
		if r.preflight() != nil {
			t.Fatal("final independent identity check failed")
		}
		ctx, cancel := context.WithTimeout(r.ctx, 20*time.Second)
		defer cancel()
		out, err := r.execute(ctx, []string{"auth", "status", "--workspace", r.plan.Workspace, "--output=json", "--timeout=10s"}, r.env, "")
		if err != nil {
			t.Fatal("read-only auth status failed")
		}
		var status struct {
			Workspaces []struct {
				Workspace     string
				Authenticated bool
			}
		}
		data := checkLiveFrame(t, out, "auth.status", r.plan.Workspace)
		if json.Unmarshal(data, &status) != nil || len(status.Workspaces) != 1 || status.Workspaces[0].Workspace != r.plan.Workspace || !status.Workspaces[0].Authenticated {
			t.Fatal("read-only auth status identity mismatch")
		}
	})
}

func TestLiveExcludedOperations(t *testing.T) {
	for _, name := range []string{"status mutation", "OAuth replacement", "token revocation"} {
		t.Run(name, func(t *testing.T) { t.Skip("excluded from live authorization") })
	}
}

// Both processes use the one approved actor. The relay provides an actual
// successful empty-poll barrier without exposing payloads or changing Slack.
func watchBeforeAdd(t *testing.T, r *liveRunner, s *slackObserver, target liveReceipt) {
	t.Helper()
	ctx, cancel := context.WithTimeout(r.ctx, 20*time.Second)
	defer cancel()
	item, err := s.client.GetReactionsContext(ctx, slack.NewRefToMessage(target.Channel, target.TS), slack.GetReactionsParameters{Full: true})
	present, stateErr := reactionForActor(item.Reactions, "eyes", r.plan.ActorID)
	if err != nil || stateErr != nil || present {
		t.Fatal("watch fixture must begin without the selected actor reaction")
	}
	token := ""
	for _, entry := range r.env {
		if value, ok := strings.CutPrefix(entry, "SLICK_LIVE_ACCESS_TOKEN="); ok {
			token = value
		}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	upstream := &http.Client{Transport: liveReadTransport{base: transport, origin: "https://slack.com"}, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	relay, ready := newWatchRelay(token, target, r.plan.ActorID, upstream)
	defer relay.Close()
	var output []byte
	var commandErr error
	done := make(chan struct{})
	env := append(slices.Clone(r.env), "SLACK_CLI_BASE_URL="+relay.URL)
	args := []string{"react", "watch", "--workspace", r.plan.Workspace, "--channel", target.Channel, "--timestamp", target.TS, "--emoji", "eyes", "--user", r.plan.ActorID, "--interval=1s", "--timeout=12s", "--output=json", "--color=never", "--no-throttle"}
	go func() { output, commandErr = r.execute(ctx, args, env, ""); close(done) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("watch child did not reap within cleanup bound")
		}
	}()
	select {
	case <-ready:
	case <-done:
		t.Fatal("watch ended before its first empty Slack poll")
	case <-ctx.Done():
		t.Fatal("watch did not establish an empty Slack poll within budget")
	}
	// This separate ordinary CLI process performs the approved reaction mutation.
	liveCall(t, r, "add", target.Channel, target.TS, "")
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("watch did not finish after the approved reaction")
	}
	if commandErr != nil {
		t.Fatal("watch polling command failed; diagnostics withheld")
	}
	data := checkLiveFrame(t, output, "react.watch", r.plan.Workspace)
	var result struct {
		Target struct {
			Channel string
			TS      string `json:"ts"`
		}
		Reactions []slack.ItemReaction
	}
	decodeErr := json.Unmarshal(data, &result)
	present, stateErr = reactionForActor(result.Reactions, "eyes", r.plan.ActorID)
	if decodeErr != nil || result.Target.Channel != target.Channel || result.Target.TS != target.TS || stateErr != nil || !present {
		t.Fatal("watch polling matched the wrong target or actor")
	}
}

func liveRead(t *testing.T, r *liveRunner, args ...string) json.RawMessage {
	t.Helper()
	data, err := r.read(r.ctx, args...)
	if err != nil {
		t.Fatalf("live read %s failed", liveCLIPath(args))
	}
	return data
}

func assertCLIReactions(t *testing.T, r *liveRunner, target liveReceipt, want bool) {
	t.Helper()
	data := liveRead(t, r, "react", "list", "--channel", target.Channel, "--timestamp", target.TS)
	var result struct {
		Target    struct{ Channel, TS string }
		Reactions []slack.ItemReaction
	}
	if json.Unmarshal(data, &result) != nil || result.Target.Channel != target.Channel || result.Target.TS != target.TS || result.Reactions == nil {
		t.Fatal("CLI reaction list lost exact target or array")
	}
	present, err := reactionForActor(result.Reactions, "eyes", r.plan.ActorID)
	if err != nil || present != want {
		t.Fatal("CLI reaction list could not establish selected actor state")
	}
}

func assertCLIScheduled(t *testing.T, r *liveRunner, s *slackObserver, target liveReceipt, want bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(r.ctx, 30*time.Second)
	defer cancel()
	present, err := r.scheduledContains(ctx, target)
	if err != nil || present != want {
		t.Fatal("CLI scheduled list lost exact receipt or complete absence")
	}
	present, err = s.Inspect(ctx, target)
	if err != nil || present != want {
		t.Fatal("independent scheduled state disagreed")
	}
	if !want && r.markAbsent(target) != nil {
		t.Fatal("cannot retain exact scheduled absence proof")
	}
}

func assertCLILookups(t *testing.T, r *liveRunner) {
	t.Helper()
	for _, channel := range []string{r.plan.PublicChannel, r.plan.PrivateChannel} {
		if channel == "" {
			continue
		}
		data := liveRead(t, r, "lookup", "channel", "--channel", channel)
		var result struct{ Channel struct{ ID string } }
		if json.Unmarshal(data, &result) != nil || result.Channel.ID != channel {
			t.Fatal("CLI channel lookup changed approved identity")
		}
	}
	for _, id := range append([]string{r.plan.ActorID}, r.plan.Recipients...) {
		data := liveRead(t, r, "lookup", "user", "--user", id)
		var result struct{ User struct{ ID string } }
		if json.Unmarshal(data, &result) != nil || result.User.ID != id {
			t.Fatal("CLI user lookup changed approved identity")
		}
	}
}

func assertCLISearch(t *testing.T, r *liveRunner, s *slackObserver, parent liveReceipt) {
	t.Helper()
	if !slices.Contains(s.scopes, "search:read") {
		t.Skip("search unverified: search:read unavailable")
	}
	if parent.TS == "" {
		t.Skip("search unverified: owned parent unavailable")
	}
	ctx, cancel := context.WithTimeout(r.ctx, 45*time.Second)
	defer cancel()
	for {
		cursor := ""
		seen := map[string]bool{"": true}
		for {
			args := []string{"lookup", "messages", "--query", `"` + r.plan.RunID + `"`, "--max-items", "100"}
			if cursor != "" {
				args = append(args, "--cursor", cursor)
			}
			data, err := r.read(ctx, args...)
			if err != nil {
				if ctx.Err() != nil {
					t.Skip("search indexing/read budget expired; owned result remains unverified")
				}
				t.Fatal("CLI unique-run search failed")
			}
			var page struct {
				Matches []struct {
					TS, Text, User string
					Channel        struct{ ID string }
				}
				Paging    *livePaging `json:"pagination"`
				Truncated bool
			}
			if json.Unmarshal(data, &page) != nil || page.Matches == nil || page.Paging == nil {
				t.Fatal("CLI search lost result/pagination shape")
			}
			for _, match := range page.Matches {
				if match.TS == parent.TS && match.Channel.ID == parent.Channel && match.User == r.plan.ActorID {
					if !strings.Contains(match.Text, r.plan.RunID) {
						t.Fatal("CLI search lost owned message text")
					}
					return
				}
			}
			if page.Truncated {
				t.Skip("Slack search cap prevented complete owned-result inspection")
			}
			more, err := nextLivePage(*page.Paging, seen)
			if err != nil {
				t.Fatal("CLI search pagination incomplete")
			}
			if !more {
				break
			}
			cursor = page.Paging.Next
		}
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			t.Skip("search indexing budget expired; owned result remains unverified")
		case <-timer.C:
		}
	}
}

func assertCLICaches(t *testing.T, r *liveRunner) {
	t.Helper()
	root := ""
	for _, entry := range r.env {
		if value, ok := strings.CutPrefix(entry, "XDG_CACHE_HOME="); ok {
			root = value
		}
	}
	if root == "" || !strings.HasPrefix(root, r.plan.RunRoot+string(filepath.Separator)) {
		t.Fatal("cache path is not isolated")
	}
	_ = liveRead(t, r, "cache", "clear")
	folder := filepath.Join(root, "slick", r.plan.Workspace)
	if os.MkdirAll(folder, 0o700) != nil {
		t.Fatal("cannot prepare isolated cache sentinel")
	}
	sentinel := filepath.Join(folder, "unrelated.txt")
	if os.WriteFile(sentinel, []byte("unrelated owned setting"), 0o600) != nil {
		t.Fatal("cache sentinel write")
	}
	for _, resource := range []string{"users", "channels"} {
		path := filepath.Join(folder, resource+".json")
		for _, step := range []string{"cold", "warm", "refresh"} {
			prior, _ := os.ReadFile(path)
			args := []string{"cache", resource, "--page-size", "100", "--max-pages", "20"}
			if step == "refresh" {
				args = append(args, "--refresh")
			}
			data := liveRead(t, r, args...)
			var result struct {
				FromCache bool `json:"from_cache"`
				Count     int
				Truncated bool
				Fetched   string `json:"fetched_at"`
			}
			if json.Unmarshal(data, &result) != nil || result.FromCache != (step == "warm") || result.Count <= 0 || result.Truncated {
				t.Fatal("CLI cache cold/warm/refresh contract incomplete")
			}
			var fields map[string]json.RawMessage
			var rows []struct{ ID string }
			if json.Unmarshal(data, &fields) != nil || json.Unmarshal(fields[resource], &rows) != nil || len(rows) != result.Count {
				t.Fatal("CLI cache lost reviewed resource rows")
			}
			wantID := r.plan.ActorID
			if resource == "channels" {
				wantID = r.plan.PublicChannel
			}
			if !slices.ContainsFunc(rows, func(v struct{ ID string }) bool { return v.ID == wantID }) {
				t.Fatal("CLI cache lost approved target identity")
			}
			if _, err := time.Parse(time.RFC3339Nano, result.Fetched); err != nil {
				t.Fatal("CLI cache lost fetch timestamp")
			}
			current, err := os.ReadFile(path)
			info, statErr := os.Stat(path)
			if err != nil || statErr != nil || info.Mode().Perm()&0o077 != 0 || (step == "warm" && !bytes.Equal(prior, current)) {
				t.Fatal("cache persistence/protection changed")
			}
		}
		data := liveRead(t, r, "cache", "clear", resource)
		var result struct{ Cleared bool }
		if json.Unmarshal(data, &result) != nil || !result.Cleared {
			t.Fatal("CLI cache clear did not report existing resource")
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("CLI cache clear left resource present")
		}
		body, err := os.ReadFile(sentinel)
		if err != nil || string(body) != "unrelated owned setting" {
			t.Fatal("cache clear changed unrelated isolated setting")
		}
	}
}

func assertCLIHealth(t *testing.T, r *liveRunner) {
	t.Helper()
	for _, op := range []string{"current", "history", "api-test", "check"} {
		args := []string{"health", op}
		if op == "history" {
			args = append(args, "--limit", "1")
		}
		data := liveRead(t, r, args...)
		var fields map[string]json.RawMessage
		if json.Unmarshal(data, &fields) != nil {
			t.Fatal("health output not an object")
		}
		switch op {
		case "current":
			var status string
			if json.Unmarshal(fields["status"], &status) != nil || status == "" {
				t.Fatal("health current lost service status")
			}
		case "history":
			var rows []json.RawMessage
			if json.Unmarshal(fields["incidents"], &rows) != nil || rows == nil || len(rows) > 1 {
				t.Fatal("health history lost bounded incidents")
			}
		case "api-test":
			if string(fields["ok"]) != "true" {
				t.Fatal("health api-test was not acknowledged")
			}
		case "check":
			if string(fields["api_ok"]) != "true" {
				t.Fatal("health check lost API acknowledgement")
			}
		}
	}
}

func deleteOwnedMessages(t *testing.T, r *liveRunner, s *slackObserver) {
	t.Helper()
	for _, receipt := range slices.Backward(r.receipts) {
		if receipt.Kind != "message" || r.finished[receiptKey(receipt)] {
			continue
		}
		ctx, cancel := context.WithTimeout(r.ctx, 20*time.Second)
		present, err := s.Inspect(ctx, receipt)
		cancel()
		if err != nil || !present {
			t.Fatal("owned message unavailable before CLI deletion")
		}
		liveCall(t, r, "delete", receipt.Channel, receipt.TS, "")
		ctx, cancel = context.WithTimeout(r.ctx, 20*time.Second)
		present, err = s.Inspect(ctx, receipt)
		cancel()
		if err != nil || present {
			t.Fatal("independent message deletion verification failed")
		}
		if r.markAbsent(receipt) != nil {
			t.Fatal("cannot retain exact message absence proof")
		}
	}
}

func assertNativeTable(t *testing.T, message *slack.Message) {
	t.Helper()
	tableFound, richFound := false, false
	for _, block := range message.Blocks.BlockSet {
		var table *slack.TableBlock
		switch v := block.(type) {
		case *slack.TableBlock:
			table = v
		case slack.TableBlock:
			table = &v
		}
		if table == nil {
			continue
		}
		tableFound = true
		if len(table.Rows) < 2 {
			t.Fatal("native table lost rows")
		}
		for _, row := range table.Rows {
			for _, cell := range row {
				if cell == nil {
					continue
				}
				if cell.TableCellType() == slack.TableCellRichText {
					if rich, ok := cell.(*slack.TableRichTextCell); ok && len(rich.Elements) > 0 {
						richFound = true
					}
				}
			}
		}
		body, _ := json.Marshal(table)
		if !strings.Contains(string(body), "Tests") || !strings.Contains(string(body), "Ready") {
			t.Fatal("native table lost rich content")
		}
	}
	if !tableFound || !richFound {
		t.Fatal("independent SDK readback did not contain native table/rich_text cells")
	}
}
