//go:build live

// Package live_test contains tests that exercise slick against a real Slack
// workspace. Run with:
//
//	mise run test:live
//
// The tests pick up your authenticated slick session by default. With no env
// overrides, they use:
//
//	workspace = config's default_workspace
//	channel   = workspaces.<workspace>.default_channel
//
// Override either with env vars when you need a different target:
//
//	SLICK_LIVE_WORKSPACE=<name>    (override the config-derived workspace)
//	SLICK_LIVE_CHANNEL=Cxxxxxxx    (override the config-derived channel)
//
// Tests skip with a helpful message when no workspace + channel can be
// resolved (e.g. fresh environment without `slick config init`).
//
// The tests build the slick binary, post real messages tagged with a run ID,
// verify Slack accepted them, and clean up by deleting/un-reacting the same
// targets. A failed test that escapes cleanup leaves residue tagged with the
// run ID for manual triage.
package live_test

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// --- message lifecycle ------------------------------------------------------

func TestLiveMessageSendAndDelete(t *testing.T) {
	binary := slickBinary(t)
	env := requireLiveEnv(t, binary)
	runID := newRunID(t)

	body := fmt.Sprintf(`live-send-%s PR review: github.com/matcra587/slack-cli/pull/42

Summary
- CLI behavior looks right for normal message send/edit/reply flows.
- Rate-limit handling is structured, but process-level fanout can still hit Slack 429s.
- Markdown rendering is fixed: *bold*, _italic_, `+"`code`"+`, and 🫎 all render through Block Kit.

LGTM after the docs note lands. 👀`, runID)
	channel, ts := postMessage(t, binary, env, body)
	cleanupMessage(t, binary, env, runID, channel, ts)
}

func TestLiveMessageEdit(t *testing.T) {
	binary := slickBinary(t)
	env := requireLiveEnv(t, binary)
	runID := newRunID(t)

	initial := fmt.Sprintf(`live-edit-%s incident report — *INVESTIGATING*

Symptom: elevated 429s on `+"`chat.postMessage`"+` since 23:00 UTC.
Hypothesis: process-level fanout missing the special-tier route.
Next: route the call through the throttler's `+"`tier_special`"+` bucket.`, runID)
	channel, ts := postMessage(t, binary, env, initial)
	cleanupMessage(t, binary, env, runID, channel, ts)

	updated := fmt.Sprintf(`live-edit-%s incident report — *RESOLVED*

Root cause: `+"`chat.postMessage`"+` was on the default tier; bursts spilled into 429.
Fix: PR #42 routes the call through `+"`tier_special`"+`. Deployed at 23:42 UTC.
Action items:
1. Add a live-test note for same-channel burst behavior.
2. Document the fanout limitation in CLAUDE.md.

Closing — incident over. 🫡`, runID)
	stdout, stderr, err := runSlick(t, binary, "",
		"message", "edit",
		"--workspace", env.workspace,
		"--channel", channel,
		"--timestamp", ts,
		"--message", updated,
		"--json",
	)
	if err != nil {
		t.Fatalf("message edit failed: %v\nstderr=%s", err, stderr)
	}
	envelope := decodeEnvelope(t, stdout)
	if got := mustString(t, envelope, "data", "message", "ts"); got != ts {
		t.Fatalf("edit returned ts=%q, want original %q", got, ts)
	}
	if got := mustString(t, envelope, "data", "message", "text"); got != "" && !strings.Contains(got, "RESOLVED") {
		t.Fatalf("edit returned text=%q, want substring \"RESOLVED\"", got)
	}
}

func TestLiveThreadReply(t *testing.T) {
	binary := slickBinary(t)
	env := requireLiveEnv(t, binary)
	runID := newRunID(t)

	parent := fmt.Sprintf(`live-reply-%s PR review: github.com/matcra587/slack-cli/pull/43

Requested changes
1. Move `+"`chat.postMessage`"+` to Slack's special tier.
2. Keep cross-process locking out for now; document the fanout limitation instead.
3. Add one live-test note showing same-channel burst behavior.`, runID)
	channel, parentTS := postMessage(t, binary, env, parent)
	cleanupMessage(t, binary, env, runID, channel, parentTS)

	replyBody := fmt.Sprintf("live-reply-%s Done — pushed the docs note in commit `abc1234`. Closing the loop on item 3. 👍", runID)
	stdout, stderr, err := runSlick(t, binary, "",
		"reply",
		"--workspace", env.workspace,
		"--channel", channel,
		"--parent", parentTS,
		"--message", replyBody,
		"--json",
	)
	if err != nil {
		t.Fatalf("reply failed: %v\nstderr=%s", err, stderr)
	}
	envelope := decodeEnvelope(t, stdout)
	replyTS := mustString(t, envelope, "data", "message", "ts")
	if replyTS == "" {
		t.Fatalf("reply envelope missing ts: %s", stdout)
	}
	if replyTS == parentTS {
		t.Fatalf("reply ts %q equals parent ts; expected a distinct thread reply", replyTS)
	}
	// Slack thread_ts on the reply should equal the parent ts; the deleting
	// the parent cascades the thread, no separate cleanup needed.
	if got := mustString(t, envelope, "data", "message", "thread_ts"); got != "" && got != parentTS {
		t.Fatalf("reply thread_ts=%q, want parent %q", got, parentTS)
	}
}

func TestLiveReactionAddAndRemove(t *testing.T) {
	binary := slickBinary(t)
	env := requireLiveEnv(t, binary)
	runID := newRunID(t)

	body := fmt.Sprintf(`live-react-%s Release notes — slick v0.4.0

- Added: ordered multi-emoji react support (`+"`react add --emoji thumbsup,rocket,sparkles`"+`).
- Added: live test matrix gated by the `+"`live`"+` build tag (`+"`mise run test:live`"+`).
- Fixed: Markdown bold/italic/code rendering through Block Kit.

Approve with 🚀 once you've smoked it locally.`, runID)
	channel, ts := postMessage(t, binary, env, body)
	cleanupMessage(t, binary, env, runID, channel, ts)

	const emojis = "white_check_mark,rocket,sparkles"
	if _, stderr, err := runSlick(t, binary, "",
		"react", "add",
		"--workspace", env.workspace,
		"--channel", channel,
		"--timestamp", ts,
		"--emoji", emojis,
		"--json",
	); err != nil {
		t.Fatalf("react add failed: %v\nstderr=%s", err, stderr)
	}

	if _, stderr, err := runSlick(t, binary, "",
		"react", "remove",
		"--workspace", env.workspace,
		"--channel", channel,
		"--timestamp", ts,
		"--emoji", emojis,
		"--json",
	); err != nil {
		t.Fatalf("react remove failed: %v\nstderr=%s", err, stderr)
	}
}

// --- markdown rendering -----------------------------------------------------

// TestLiveMarkdownRoundTripsThroughBlockKit verifies that markdown features
// (bold, italic, code spans, bulleted + numbered lists, unicode emoji) sent
// via `slick message send` survive the Block Kit conversion: the message
// posted to Slack has a non-empty `blocks` payload when read back through
// `slick history list`. Slack's `text` field flattens visual formatting, so
// the assertion is on the structured `blocks` instead.
func TestLiveMarkdownRoundTripsThroughBlockKit(t *testing.T) {
	binary := slickBinary(t)
	env := requireLiveEnv(t, binary)
	runID := newRunID(t)

	body := fmt.Sprintf(`live-markdown-%s Markdown coverage probe

Inline marks: *bold*, _italic_, `+"`code`"+`, and 🫎 unicode emoji.

Bulleted list:
- alpha
- bravo
- charlie

Numbered list:
1. first
2. second
3. third

Closing line — testing rich_text round-trip through Block Kit.`, runID)
	channel, ts := postMessage(t, binary, env, body)
	cleanupMessage(t, binary, env, runID, channel, ts)

	stdout, stderr, err := runSlick(t, binary, "",
		"history", "list",
		"--workspace", env.workspace,
		"--channel", env.channel,
		"--max-items", "10",
		"--json",
	)
	if err != nil {
		t.Fatalf("history list failed: %v\nstderr=%s", err, stderr)
	}
	envelope := decodeEnvelope(t, stdout)
	messages, ok := envelope["data"].(map[string]any)["messages"].([]any)
	if !ok {
		t.Fatalf("envelope missing data.messages array: %s", stdout)
	}
	var found map[string]any
	for _, raw := range messages {
		msg := raw.(map[string]any)
		if msg["ts"] == ts {
			found = msg
			break
		}
	}
	if found == nil {
		t.Fatalf("history list did not return ts=%s", ts)
	}
	blocks, ok := found["blocks"].([]any)
	if !ok || len(blocks) == 0 {
		t.Fatalf("message ts=%s missing/empty blocks payload (markdown→Block Kit pipeline likely skipped): %v", ts, found)
	}
}

// --- discovery --------------------------------------------------------------

func TestLiveHistoryList(t *testing.T) {
	binary := slickBinary(t)
	env := requireLiveEnv(t, binary)
	runID := newRunID(t)

	want := map[string]bool{}
	updates := []string{
		fmt.Sprintf(`live-history-%s Deploy update — *starting* slick v0.4.0 rollout

Touching:
- `+"`internal/cli/runtime`"+` (NowFunc/RequestIDFunc cleanup)
- `+"`tests/live`"+` (matrix expansion)
Watch this thread for ETA.`, runID),
		fmt.Sprintf(`live-history-%s Deploy update — *complete* slick v0.4.0 is live

Smoke checks:
1. `+"`mise run test:live`"+` → all green except status (scope-gated).
2. `+"`mise run check`"+` → 0 issues.
Closing the deploy thread. 🟢`, runID),
	}
	for _, body := range updates {
		_, ts := postMessage(t, binary, env, body)
		cleanupMessage(t, binary, env, runID, env.channel, ts)
		want[ts] = true
	}

	stdout, stderr, err := runSlick(t, binary, "",
		"history", "list",
		"--workspace", env.workspace,
		"--channel", env.channel,
		"--max-items", "20",
		"--json",
	)
	if err != nil {
		t.Fatalf("history list failed: %v\nstderr=%s", err, stderr)
	}
	envelope := decodeEnvelope(t, stdout)
	messages := envelope["data"].(map[string]any)["messages"]
	got := map[string]bool{}
	for _, raw := range messages.([]any) {
		msg := raw.(map[string]any)
		if ts, ok := msg["ts"].(string); ok {
			got[ts] = true
		}
	}
	for ts := range want {
		if !got[ts] {
			t.Fatalf("history did not return ts=%s; got=%v", ts, got)
		}
	}
}

func TestLiveSearchMessagesCommandWorks(t *testing.T) {
	binary := slickBinary(t)
	env := requireLiveEnv(t, binary)
	runID := newRunID(t)

	// Slack search.messages is asynchronously indexed; results may take
	// seconds to minutes to appear. The test verifies the command path
	// (auth, scope, envelope shape) rather than result content.
	body := fmt.Sprintf(`live-search-%s Slack indexing seed — search.messages is asynchronously indexed

This message exists so the search command path (auth, scope, envelope) is exercised.
The test does not assert on result content because indexing latency is unbounded.`, runID)
	channel, ts := postMessage(t, binary, env, body)
	cleanupMessage(t, binary, env, runID, channel, ts)

	stdout, stderr, err := runSlick(t, binary, "",
		"lookup", "messages",
		"--workspace", env.workspace,
		"--query", runID,
		"--max-items", "5",
		"--json",
	)
	if err != nil {
		t.Fatalf("lookup messages failed: %v\nstderr=%s", err, stderr)
	}
	envelope := decodeEnvelope(t, stdout)
	if _, ok := envelope["data"].(map[string]any)["matches"]; !ok {
		t.Fatalf("envelope missing data.matches: %s", stdout)
	}
}

// TestLiveSearchMessagesPopulatesText round-trips a real send through
// search.messages and asserts the match's text field is non-empty and
// contains the run ID. This guards the searchTextFromBlocks fallback
// (Slack returns Text="" for Block Kit messages and stashes the body
// in Blocks; without the fallback, matches arrive textless and this
// test catches that regression).
//
// Indexing latency is unbounded; the test polls up to 60s and skips
// (rather than fails) when Slack hasn't indexed the message in time.
func TestLiveSearchMessagesPopulatesText(t *testing.T) {
	binary := slickBinary(t)
	env := requireLiveEnv(t, binary)
	runID := newRunID(t)

	body := fmt.Sprintf(`live-search-text-%s Release note: rolled out search-text
extraction so lookup messages results carry the message body even when Slack
returns it inside blocks rather than the top-level text field.`, runID)
	channel, ts := postMessage(t, binary, env, body)
	cleanupMessage(t, binary, env, runID, channel, ts)

	deadline := time.Now().Add(60 * time.Second)
	interval := 5 * time.Second
	var matches []any
	for time.Now().Before(deadline) {
		stdout, stderr, err := runSlick(t, binary, "",
			"lookup", "messages",
			"--workspace", env.workspace,
			"--query", runID,
			"--max-items", "5",
			"--json",
		)
		if err != nil {
			t.Fatalf("lookup messages failed: %v\nstderr=%s", err, stderr)
		}
		envelope := decodeEnvelope(t, stdout)
		data, _ := envelope["data"].(map[string]any)
		got, _ := data["matches"].([]any)
		if len(got) > 0 {
			matches = got
			break
		}
		time.Sleep(interval)
	}
	if len(matches) == 0 {
		t.Skipf("search did not index run %s within 60s; skipping text assertion", runID)
		return
	}

	match, ok := matches[0].(map[string]any)
	if !ok {
		t.Fatalf("matches[0] is not an object: %T %+v", matches[0], matches[0])
	}
	text, _ := match["text"].(string)
	if text == "" {
		t.Fatalf("matches[0].text is empty — text-from-blocks fallback failed.\nmatch=%+v", match)
	}
	if !strings.Contains(text, runID) {
		t.Fatalf("matches[0].text does not contain run ID %q: %q", runID, text)
	}
}

func TestLiveCachePopulate(t *testing.T) {
	binary := slickBinary(t)
	env := requireLiveEnv(t, binary)

	for _, resource := range []string{"users", "channels"} {
		stdout, stderr, err := runSlick(t, binary, "",
			"cache", resource,
			"--workspace", env.workspace,
			"--refresh",
			"--json",
		)
		if err != nil {
			t.Fatalf("cache %s failed: %v\nstderr=%s", resource, err, stderr)
		}
		envelope := decodeEnvelope(t, stdout)
		data, ok := envelope["data"].(map[string]any)
		if !ok {
			t.Fatalf("cache %s envelope missing data: %s", resource, stdout)
		}
		items, ok := data[resource].([]any)
		if !ok {
			t.Fatalf("cache %s envelope missing data.%s array: %s", resource, resource, stdout)
		}
		if len(items) == 0 {
			t.Fatalf("cache %s returned 0 items; expected at least one", resource)
		}
	}
}

// --- profile mutation -------------------------------------------------------

func TestLiveStatusSetAndClear(t *testing.T) {
	binary := slickBinary(t)
	env := requireLiveEnv(t, binary)
	runID := newRunID(t)

	statusText := fmt.Sprintf("Heads down — slick v0.4 release prep (live-status-%s)", runID)
	t.Cleanup(func() {
		_, stderr, err := runSlick(t, binary, "",
			"status", "clear",
			"--workspace", env.workspace,
			"--json",
		)
		if err != nil && missingScopeFromStderr(stderr) == "" {
			t.Logf("cleanup status clear failed (run id %s): %v\nstderr=%s", runID, err, stderr)
		}
	})

	stdout, stderr, err := runSlick(t, binary, "",
		"status", "set",
		"--workspace", env.workspace,
		"--text", statusText,
		"--emoji", "test_tube",
		"--expires-in", "5m",
		"--json",
	)
	if err != nil {
		if scope := missingScopeFromStderr(stderr); scope != "" {
			t.Skipf("workspace token missing required scope %q; status set/clear needs a user token with users.profile:write", scope)
		}
		t.Fatalf("status set failed: %v\nstderr=%s", err, stderr)
	}
	envelope := decodeEnvelope(t, stdout)
	if got := mustString(t, envelope, "data", "status_text"); got != "" && got != statusText {
		t.Fatalf("status_text=%q, want %q", got, statusText)
	}

	if _, stderr, err := runSlick(t, binary, "",
		"status", "clear",
		"--workspace", env.workspace,
		"--json",
	); err != nil {
		t.Fatalf("status clear failed: %v\nstderr=%s", err, stderr)
	}
}

// --- output mode sweep ------------------------------------------------------

func TestLiveOutputModesProduceExpectedShapes(t *testing.T) {
	binary := slickBinary(t)
	env := requireLiveEnv(t, binary)
	runID := newRunID(t)

	cases := []struct {
		name      string
		flag      string
		assertion func(t *testing.T, stdout string)
	}{
		{
			name: "json",
			flag: "--json",
			assertion: func(t *testing.T, stdout string) {
				envelope := decodeEnvelope(t, stdout)
				if _, ok := envelope["meta"].(map[string]any); !ok {
					t.Fatalf("--json output missing meta envelope: %s", stdout)
				}
			},
		},
		{
			name: "compact",
			flag: "--compact",
			assertion: func(t *testing.T, stdout string) {
				var data map[string]any
				if err := json.Unmarshal([]byte(stdout), &data); err != nil {
					t.Fatalf("--compact output is not JSON: %v\n%s", err, stdout)
				}
				if _, hasMeta := data["meta"]; hasMeta {
					t.Fatalf("--compact output should not include meta envelope: %s", stdout)
				}
				if _, hasMessage := data["message"]; !hasMessage {
					t.Fatalf("--compact output missing message field: %s", stdout)
				}
			},
		},
		{
			name: "plain",
			flag: "--plain",
			assertion: func(t *testing.T, stdout string) {
				trimmed := strings.TrimSpace(stdout)
				if strings.HasPrefix(trimmed, "{") {
					t.Fatalf("--plain output looks like JSON: %s", stdout)
				}
				if trimmed == "" {
					t.Fatalf("--plain output empty")
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := fmt.Sprintf(`live-output-%s-%s Output mode probe — *%s*

Verifying that `+"`message send`"+` emits the expected envelope shape for `+"`%s`"+` mode.
This message is short on purpose so the assertion path is the focus.`, runID, tc.name, tc.name, tc.name)
			stdout, stderr, err := runSlick(t, binary, "",
				"message", "send",
				"--workspace", env.workspace,
				"--channel", env.channel,
				"--message", body,
				tc.flag,
			)
			if err != nil {
				t.Fatalf("message send (%s) failed: %v\nstderr=%s", tc.name, err, stderr)
			}
			tc.assertion(t, stdout)

			// Plain and compact modes don't emit a structured ts/channel that the
			// envelope path uses; re-fetch from history to clean up so we don't
			// leave residue regardless of output mode.
			channel, ts := lookupRecentByText(t, binary, env, body)
			if ts != "" {
				cleanupMessage(t, binary, env, runID, channel, ts)
			}
		})
	}
}

// --- helpers ----------------------------------------------------------------

type liveEnv struct {
	workspace string
	channel   string
}

func requireLiveEnv(t *testing.T, binary string) liveEnv {
	t.Helper()
	workspace := os.Getenv("SLICK_LIVE_WORKSPACE")
	channel := os.Getenv("SLICK_LIVE_CHANNEL")
	if workspace == "" || channel == "" {
		ws, ch := discoverLiveDefaults(t, binary)
		if workspace == "" {
			workspace = ws
		}
		if channel == "" {
			channel = ch
		}
	}
	if workspace == "" {
		t.Skip("no workspace resolved; run `slick auth login` or set SLICK_LIVE_WORKSPACE")
	}
	if channel == "" {
		t.Skipf("no channel resolved for workspace %q; run `slick config set workspaces.%s.default_channel <Cxxxx>` or set SLICK_LIVE_CHANNEL", workspace, workspace)
	}
	if !strings.HasPrefix(channel, "C") && !strings.HasPrefix(channel, "G") && !strings.HasPrefix(channel, "D") {
		t.Fatalf("resolved channel %q does not look like a Slack channel ID", channel)
	}
	return liveEnv{workspace: workspace, channel: channel}
}

// discoverLiveDefaults shells out to `slick config list --json` and returns
// the configured default workspace + that workspace's default_channel.
// Returns ("", "") when the user has no slick config yet.
func discoverLiveDefaults(t *testing.T, binary string) (workspace, channel string) {
	t.Helper()
	stdout, _, err := runSlick(t, binary, "", "config", "list", "--json")
	if err != nil {
		return "", ""
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		return "", ""
	}
	data, _ := envelope["data"].(map[string]any)
	if data == nil {
		return "", ""
	}
	workspace, _ = data["default_workspace"].(string)
	if workspace == "" {
		return "", ""
	}
	settings, _ := data["settings"].([]any)
	wantKey := "workspaces." + workspace + ".default_channel"
	for _, raw := range settings {
		setting, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if key, _ := setting["key"].(string); key == wantKey {
			channel, _ = setting["value"].(string)
			break
		}
	}
	return workspace, channel
}

// slickBinary returns the path to the slick binary produced by `mise run
// build`. test:live declares build as a dependency, so the binary is fresh
// when this target runs.
func slickBinary(t *testing.T) string {
	t.Helper()
	path := fmt.Sprintf("../../dist/slick-%s-%s", runtime.GOOS, runtime.GOARCH)
	if _, err := os.Stat(path); err != nil {
		t.Skipf("slick binary not found at %s; run `mise run test:live` (got %v)", path, err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("filepath.Abs(%s): %v", path, err)
	}
	return abs
}

func runSlick(t *testing.T, binary, stdin string, args ...string) (string, string, error) {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Env = os.Environ()
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// postMessage sends body to env.channel and returns (channel, ts) from the
// envelope. body should embed the runID on its first line so manual triage
// can grep for residue when cleanup fails.
func postMessage(t *testing.T, binary string, env liveEnv, body string) (channel, ts string) {
	t.Helper()
	stdout, stderr, err := runSlick(t, binary, "",
		"message", "send",
		"--workspace", env.workspace,
		"--channel", env.channel,
		"--message", body,
		"--json",
	)
	if err != nil {
		t.Fatalf("postMessage failed: %v\nstderr=%s", err, stderr)
	}
	envelope := decodeEnvelope(t, stdout)
	channel = mustString(t, envelope, "data", "message", "channel")
	ts = mustString(t, envelope, "data", "message", "ts")
	if channel == "" || ts == "" {
		t.Fatalf("postMessage envelope missing channel/ts: %s", stdout)
	}
	return channel, ts
}

func cleanupMessage(t *testing.T, binary string, env liveEnv, runID, channel, ts string) {
	t.Helper()
	t.Cleanup(func() {
		_, stderr, err := runSlick(t, binary, "",
			"message", "delete",
			"--workspace", env.workspace,
			"--channel", channel,
			"--timestamp", ts,
			"--force",
			"--json",
		)
		if err != nil {
			t.Logf("cleanup delete failed (run id %s, channel %s, ts %s): %v\nstderr=%s",
				runID, channel, ts, err, stderr)
		}
	})
}

// lookupRecentByText scans recent channel history for the supplied exact text
// and returns its (channel, ts) for cleanup. Returns ("", "") if no match.
// Used by output-mode tests where the command output doesn't reveal ts/channel.
func lookupRecentByText(t *testing.T, binary string, env liveEnv, text string) (string, string) {
	t.Helper()
	stdout, stderr, err := runSlick(t, binary, "",
		"history", "list",
		"--workspace", env.workspace,
		"--channel", env.channel,
		"--max-items", "20",
		"--json",
	)
	if err != nil {
		t.Logf("lookupRecentByText history list failed: %v\nstderr=%s", err, stderr)
		return "", ""
	}
	envelope := decodeEnvelope(t, stdout)
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		return "", ""
	}
	messages, ok := data["messages"].([]any)
	if !ok {
		return "", ""
	}
	for _, raw := range messages {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if msgText, _ := msg["text"].(string); msgText == text {
			ts, _ := msg["ts"].(string)
			channel, _ := msg["channel"].(string)
			if channel == "" {
				channel = env.channel
			}
			return channel, ts
		}
	}
	return "", ""
}

// missingScopeFromStderr extracts the scope name from a structured error
// envelope (e.g. {"errors":[{"type":"auth_failure","message":"missing required
// Slack scope: users.profile:write"...}]}). Returns "" if stderr isn't
// recognisable as a missing-scope error. Used by tests that gracefully skip
// when the live workspace token lacks the scope they exercise.
func missingScopeFromStderr(stderr string) string {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return ""
	}
	var envelope struct {
		Errors []struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal([]byte(stderr), &envelope); err != nil {
		return ""
	}
	const prefix = "missing required Slack scope: "
	for _, e := range envelope.Errors {
		if e.Type != "auth_failure" {
			continue
		}
		if rest, ok := strings.CutPrefix(e.Message, prefix); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

func newRunID(t *testing.T) string {
	t.Helper()
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return hex.EncodeToString(b[:])
}

func decodeEnvelope(t *testing.T, stdout string) map[string]any {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("envelope decode: %v\nstdout=%s", err, stdout)
	}
	return envelope
}

func mustString(t *testing.T, m map[string]any, path ...string) string {
	t.Helper()
	current := any(m)
	for _, key := range path {
		obj, ok := current.(map[string]any)
		if !ok {
			t.Fatalf("path %v: expected map at %q, got %T", path, key, current)
		}
		current = obj[key]
	}
	if current == nil {
		return ""
	}
	s, ok := current.(string)
	if !ok {
		t.Fatalf("path %v: expected string, got %T", path, current)
	}
	return s
}
