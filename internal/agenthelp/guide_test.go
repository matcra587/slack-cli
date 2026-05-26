package agenthelp_test

import (
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/agenthelp"
)

// Tests in this file pin the *semantic* invariants of each workflow section in
// the R4 goal-oriented schema (Goal / Decide / Run / Save / Preconditions /
// Behavior / Recover / Next). They intentionally avoid pinning prose so the
// runbook can be reshaped without breaking the suite — but they DO pin:
//   - field names and exit codes (API contract),
//   - the mention-escape gotcha (security claim),
//   - scope-error classification (auth_failure vs not_found),
//   - command names and flag spellings (cobra wiring).

// requireFragments fails the test if any fragment is missing from the section.
// It uses Errorf+Fail so a single section reports every missing fragment at
// once, instead of stopping at the first.
func requireFragments(t *testing.T, section, body string, fragments ...string) {
	t.Helper()
	for _, fragment := range fragments {
		if !strings.Contains(body, fragment) {
			t.Errorf("%s guide missing %q", section, fragment)
		}
	}
}

func TestGuideSendMsgInvariants(t *testing.T) {
	guide := agenthelp.GetGuideSection("send_msg")
	requireFragments(t, "send_msg", guide,
		// Block Kit + raw input switch
		"--blocks",
		"raw Block Kit",
		"required for real mentions",
		// Output contract
		"Requires `--output=json`",
		"data.message.ts",
		"data.permalink",
		// Attribution: the surface lives in core_contract; send_msg only
		// needs to point at it so an agent loading send_msg in isolation
		// still finds the toggle/override flags.
		"core_contract",
		// Scheduled-target behavior
		"Scheduled",
		"--schedule 90m",
		"scheduled sends ignore `default_channel`",
		// Slack error classification
		"`auth_failure: missing_scope`",
		"`not_found: not_in_channel`",
	)
	for _, stale := range []string{
		"agent_attribution = false", // legacy config key
		"scheduled sends require `--channel`; `--user` is not supported with `--schedule`",
		"`--user` is not supported with `--schedule`",
		"`not_in_channel`, and `no_permission` map to structured auth failures", // wrong class
	} {
		if strings.Contains(guide, stale) {
			t.Errorf("send_msg guide contains stale fragment %q", stale)
		}
	}
}

func TestGuideAuthSetupRefusesRawTokenArgv(t *testing.T) {
	guide := agenthelp.GetGuideSection("auth_setup")
	requireFragments(t, "auth_setup", guide,
		"--token-stdin",
		"--token-file",
		"--token-env",
		"SLACK_CLI_TOKEN_<PROFILE>",
	)
	for _, leak := range []string{"--token <xox", "--token xox"} {
		if strings.Contains(guide, leak) {
			t.Errorf("auth_setup guide documents raw token argv usage: %q", leak)
		}
	}
}

func TestGuideCoreContractInvariants(t *testing.T) {
	guide := agenthelp.GetGuideSection("core_contract")
	requireFragments(t, "core_contract", guide,
		"stdout is command data only",
		"stderr is diagnostics",
		"--output",
		"`auto`",
		"`human`",
		"`json`",
		"`compact`",
		"Exit codes",
		"canceled `6`",
		"timeout `7`",
		"errors[0].type",
		// Attribution surface lives here, not in each mutating workflow.
		"--attribution",
		"--no-attribution",
		"--attribution-{label,emoji,message}",
	)
}

func TestGuideScheduleMsgInvariants(t *testing.T) {
	guide := agenthelp.GetGuideSection("schedule_msg")
	requireFragments(t, "schedule_msg", guide,
		// Listing
		"slick message scheduled list",
		"meta.pagination.next_cursor",
		"data.scheduled_messages[].id",
		// Cancellation
		"slick message scheduled delete",
		"--scheduled-id",
		// The "raw channel ID, NOT friendly label" invariant
		"raw",
		// Creation is a send_msg modifier, not a separate command
		"send_msg",
	)
}

func TestGuideConfigPrefsInvariants(t *testing.T) {
	guide := agenthelp.GetGuideSection("config_prefs")
	requireFragments(t, "config_prefs", guide,
		"slick config init",
		"default_channel",
		"attribution.message",
		"~/.config/slick/config.toml",
	)
}

func TestGuideReplyAndReactInvariants(t *testing.T) {
	react := agenthelp.GetGuideSection("react")
	requireFragments(t, "react", react,
		"slick react add",
		"slick react remove",
		"slick react list",
		"meta.command",
		"react.add",
		"react.remove",
		"react.list",
	)
	if strings.Contains(react, "slack reaction") || strings.Contains(react, "probationary") {
		t.Errorf("react guide documents legacy/probationary command")
	}

	reply := agenthelp.GetGuideSection("reply")
	requireFragments(t, "reply", reply,
		"slick reply",
		"--parent",
		"thread_ts",
	)
	if strings.Contains(reply, "slack thread reply") || strings.Contains(reply, "probationary") {
		t.Errorf("reply guide documents legacy/probationary command")
	}
}

func TestGuideSetStatusInvariants(t *testing.T) {
	guide := agenthelp.GetGuideSection("set_status")
	requireFragments(t, "set_status", guide,
		"meta.command",
		"status.set",
		"status.clear",
		"data.text",
		"data.emoji",
		"data.expiration",
	)
	if strings.Contains(guide, "The action label (`status.set`") {
		t.Errorf("set_status guide tells JSON callers to parse a human action label")
	}
}

func TestGuideEditMsgInvariants(t *testing.T) {
	guide := agenthelp.GetGuideSection("edit_msg")
	requireFragments(t, "edit_msg", guide,
		"data.message.channel",
		"data.message.ts",
		"data.message.text",
		// The fact that returned blocks are absent is a real API gotcha — keep it.
		"blocks",
		"NOT included",
	)
}

func TestGuideSendDMInvariants(t *testing.T) {
	guide := agenthelp.GetGuideSection("send_dm")
	requireFragments(t, "send_dm", guide,
		"slick message send --user",
		"Slack decides",
		"bot-token",
		"users_not_found",
		"data.message.channel",
		"--schedule",
		// Scope-class invariant: missing_scope must be auth_failure, not not_found.
		"missing_scope",
		"auth_failure",
		"no_permission",
	)
	if strings.Contains(guide, "slack dm send") || strings.Contains(guide, "rejects bot-token") {
		t.Errorf("send_dm guide still documents legacy command or local bot-token rejection")
	}
}

func TestGuideDiscoverDestinationInvariants(t *testing.T) {
	guide := agenthelp.GetGuideSection("discover_destination")
	requireFragments(t, "discover_destination", guide,
		"slick lookup channel",
		"--types im",
		"is_member",
		"is_archived",
	)
	for _, removed := range []string{"slack channel list", "slack dm list"} {
		if strings.Contains(guide, removed) {
			t.Errorf("discover_destination guide documents removed command %q", removed)
		}
	}
}

func TestGuideLookupUserInvariants(t *testing.T) {
	user := agenthelp.GetGuideSection("lookup_user")
	requireFragments(t, "lookup_user", user,
		"slick lookup user",
		"--filter",
		"--user",
		"--max-items",
		"data.users[].id",
	)
	if strings.Contains(user, "slack user list") || strings.Contains(user, "slack user info") {
		t.Errorf("lookup_user guide documents removed user commands")
	}
}

func TestGuideReadHistoryInvariants(t *testing.T) {
	guide := agenthelp.GetGuideSection("read_history")
	requireFragments(t, "read_history", guide,
		"slick history list",
		"--thread",
		"--max-items",
		"data.messages[].ts",
		// The reply/react chaining pattern is the whole point of read_history.
		"reply",
		"react",
	)
}

func TestGuideDeveloperReviewInvariants(t *testing.T) {
	guide := agenthelp.GetGuideSection("developer_review")
	requireFragments(t, "developer_review", guide,
		// Composition workflow — these four sub-workflows are the contract.
		"send_msg",
		"react",
		"reply",
		"read_history",
		// Attribution gotcha — pointer to core_contract is sufficient.
		"Attribution",
		"core_contract",
	)
}

func TestGuideCleanupMsgsInvariants(t *testing.T) {
	guide := agenthelp.GetGuideSection("cleanup_msgs")
	requireFragments(t, "cleanup_msgs", guide,
		"meta.pagination.has_more",
		"meta.pagination.next_cursor",
		"data.matches[].channel.id",
		"errors[0].retry_after_seconds",
		"message_not_found",
		// The "loop until zero matches" invariant is the entire workflow's correctness claim.
		"zero matches",
	)
}

func TestGuideSearchMsgsInvariants(t *testing.T) {
	guide := agenthelp.GetGuideSection("search_msgs")
	requireFragments(t, "search_msgs", guide,
		"slick lookup messages",
		"--cursor",
		"meta.pagination.next_cursor",
		"search:read",
		// search.messages is user-token only — bot tokens cannot use it.
		"user-token",
	)
}

func TestGuideSafeMutationInvariants(t *testing.T) {
	guide := agenthelp.GetGuideSection("safe_mutation")
	requireFragments(t, "safe_mutation", guide,
		"chat.postMessage",
		"share proactive throttle state",
		"rate_limit",
		"--dry-run",
	)
}

func TestGuideCacheMetadataInvariants(t *testing.T) {
	guide := agenthelp.GetGuideSection("cache_metadata")
	requireFragments(t, "cache_metadata", guide,
		"slick cache users",
		"slick cache channels",
		"--refresh",
		"--ttl-minutes 10080",
		"~/.cache/slick/<profile>/",
	)
}

func TestGuideCheckHealthInvariants(t *testing.T) {
	guide := agenthelp.GetGuideSection("check_health")
	requireFragments(t, "check_health", guide,
		"slick health check",
		"slick health current",
		"slick health history",
		"slick health api-test",
		"data.healthy",
		// Health probes intentionally bypass auth — that's the whole point.
		"do NOT use the configured workspace",
	)
}

func TestWorkflowCatalogIncludesOperationalRunbooks(t *testing.T) {
	names := map[string]bool{}
	for _, name := range agenthelp.WorkflowNames() {
		names[name] = true
	}
	for _, name := range []string{"cache_metadata", "check_health", "cleanup_msgs", "developer_review"} {
		if !names[name] {
			t.Fatalf("workflow names = %#v, want %s", names, name)
		}
	}
}

// TestMentionEscapeGotchaDocumented pins the security claim across every
// workflow that accepts user-supplied Markdown going to Slack. If a future
// rewrite drops this warning, the test fails — agents must keep being told that
// mention sentinels in --message render as literal text.
func TestMentionEscapeGotchaDocumented(t *testing.T) {
	for _, workflow := range []string{"send_msg", "reply", "edit_msg", "upload_file"} {
		guide := agenthelp.GetGuideSection(workflow)
		for _, fragment := range []string{"<!channel>", "<@U123>", "literal text"} {
			if !strings.Contains(guide, fragment) {
				t.Errorf("%s guide missing mention-escape fragment %q", workflow, fragment)
			}
		}
	}
}
