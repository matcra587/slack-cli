package agenthelp_test

import (
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/internal/agenthelp"
)

func TestGuideDocumentsBlocksRawAndAttributionConfig(t *testing.T) {
	guide := agenthelp.GetGuideSection("send_msg")
	for _, fragment := range []string{
		"--blocks",
		"raw Block Kit",
		"validates Slack Block Kit JSON rules",
		"Unsupported block-level Markdown preserves original source text",
		"--output",
		"output mode only",
		"attribution.enabled",
		"attribution.message",
		"attribution.emoji",
		"Do not repeat attribution text",
		"realistic content such as a PR review",
	} {
		if !strings.Contains(guide, fragment) {
			t.Fatalf("send_msg guide missing %q:\n%s", fragment, guide)
		}
	}
	if strings.Contains(guide, "agent_attribution = false") {
		t.Fatalf("send_msg guide uses legacy attribution key:\n%s", guide)
	}
}

func TestGuideDocumentsBestEffortScopeAndPermissionErrors(t *testing.T) {
	guide := agenthelp.GetGuideSection("send_dm")
	for _, fragment := range []string{
		"Scope validation is best-effort",
		"missing_scope",
		"auth_failure",
		"not_in_channel",
		"no_permission",
		"fixed structured error contract",
	} {
		if !strings.Contains(guide, fragment) {
			t.Fatalf("send_dm guide missing %q:\n%s", fragment, guide)
		}
	}
}

func TestGuideDocumentsSlackErrorClasses(t *testing.T) {
	guide := agenthelp.GetGuideSection("send_msg")
	for _, fragment := range []string{
		"`missing_scope` and `no_permission` map to structured auth failures",
		"`not_in_channel` maps to `not_found`",
	} {
		if !strings.Contains(guide, fragment) {
			t.Fatalf("send_msg guide missing %q:\n%s", fragment, guide)
		}
	}
	if strings.Contains(guide, "`not_in_channel`, and `no_permission` map to structured auth failures") {
		t.Fatalf("send_msg guide maps not_in_channel to auth failure:\n%s", guide)
	}
}

func TestGuideDocumentsSafeTokenSourcesOnly(t *testing.T) {
	guide := agenthelp.GetGuideSection("auth_setup")
	for _, fragment := range []string{"--token-stdin", "--token-file", "--token-env", "SLACK_CLI_TOKEN_<PROFILE>"} {
		if !strings.Contains(guide, fragment) {
			t.Fatalf("auth_setup guide missing %q:\n%s", fragment, guide)
		}
	}
	if strings.Contains(guide, "--token <xox") || strings.Contains(guide, "--token xox") {
		t.Fatalf("auth_setup guide documents raw token argv usage:\n%s", guide)
	}
}

func TestGuideDocumentsCoreContract(t *testing.T) {
	guide := agenthelp.GetGuideSection("core_contract")
	for _, fragment := range []string{
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
	} {
		if !strings.Contains(guide, fragment) {
			t.Fatalf("core_contract guide missing %q:\n%s", fragment, guide)
		}
	}
}

func TestGuideDocumentsScheduledHumanAndJSONTargets(t *testing.T) {
	guide := agenthelp.GetGuideSection("schedule_msg")
	for _, fragment := range []string{
		"`ID / CHANNEL / DM / POST_AT / TEXT`",
		"display-only",
		"raw `data.scheduled_messages[].channel`",
		"Do not parse human `CHANNEL`",
		"slick message send --user <user-id-or-slack-profile-email>",
		"real scheduled `--user` sends open the DM or MPIM before scheduling",
		"`--dry-run --user --schedule`",
	} {
		if !strings.Contains(guide, fragment) {
			t.Fatalf("schedule_msg guide missing %q:\n%s", fragment, guide)
		}
	}
}

func TestGuideDocumentsScheduledUserTargetsInSendRunbook(t *testing.T) {
	guide := agenthelp.GetGuideSection("send_msg")
	for _, fragment := range []string{
		"Scheduled DM command",
		"slick message send --user <user-id-or-slack-profile-email>",
		"pass exactly one explicit target: `--channel` or `--user`",
	} {
		if !strings.Contains(guide, fragment) {
			t.Fatalf("send_msg guide missing %q:\n%s", fragment, guide)
		}
	}
	for _, stale := range []string{
		"scheduled sends require `--channel`; `--user` is not supported with `--schedule`",
		"`--user` is not supported with `--schedule`",
		"channel-only scheduled sends",
	} {
		if strings.Contains(guide, stale) {
			t.Fatalf("send_msg guide contains stale scheduled-target guidance %q:\n%s", stale, guide)
		}
	}
}

func TestGuideDocumentsConfigRunbook(t *testing.T) {
	config := agenthelp.GetGuideSection("config_prefs")
	for _, fragment := range []string{
		"slick config init",
		"config file not found",
		"default_channel",
		"attribution.message",
		"~/.config/slick/config.toml",
	} {
		if !strings.Contains(config, fragment) {
			t.Fatalf("config_prefs guide missing %q:\n%s", fragment, config)
		}
	}
}

func TestGuideDocumentsPromotedReplyAndReactCommands(t *testing.T) {
	react := agenthelp.GetGuideSection("react")
	for _, fragment := range []string{"slick react add", "slick react remove", "slick react list", "meta.command", "react.add", "react.remove", "react.list"} {
		if !strings.Contains(react, fragment) {
			t.Fatalf("react guide missing %q:\n%s", fragment, react)
		}
	}
	if strings.Contains(react, "slack reaction") || strings.Contains(react, "probationary") {
		t.Fatalf("react guide documents legacy/probationary command:\n%s", react)
	}

	reply := agenthelp.GetGuideSection("reply")
	for _, fragment := range []string{"slick reply", "--parent", "thread_ts", "Command metadata uses `reply`"} {
		if !strings.Contains(reply, fragment) {
			t.Fatalf("reply guide missing %q:\n%s", fragment, reply)
		}
	}
	if strings.Contains(reply, "slack thread reply") || strings.Contains(reply, "probationary") {
		t.Fatalf("reply guide documents legacy/probationary command:\n%s", reply)
	}
}

func TestGuideDocumentsStatusCommandMetadata(t *testing.T) {
	guide := agenthelp.GetGuideSection("set_status")
	for _, fragment := range []string{
		"meta.command",
		"status.set",
		"status.clear",
		"data.text",
		"data.emoji",
		"data.expiration",
	} {
		if !strings.Contains(guide, fragment) {
			t.Fatalf("set_status guide missing %q:\n%s", fragment, guide)
		}
	}
	if strings.Contains(guide, "The action label (`status.set`") {
		t.Fatalf("set_status guide tells JSON callers to parse action label:\n%s", guide)
	}
}

func TestGuideDocumentsMessageEditOutputShape(t *testing.T) {
	guide := agenthelp.GetGuideSection("edit_msg")
	for _, fragment := range []string{
		"data.message.channel",
		"data.message.ts",
		"data.message.text",
		"does not include returned `data.message.blocks`",
	} {
		if !strings.Contains(guide, fragment) {
			t.Fatalf("edit_msg guide missing %q:\n%s", fragment, guide)
		}
	}
}

func TestGuideDocumentsSlackDecidedBotDMBehavior(t *testing.T) {
	guide := agenthelp.GetGuideSection("send_dm")
	for _, fragment := range []string{
		"slick message send --user",
		"Slack decides",
		"bot-token",
		"structured error",
		"Slack profile email",
		"users_not_found",
		"data.message.channel",
		"add `--schedule <when>`",
	} {
		if !strings.Contains(guide, fragment) {
			t.Fatalf("send_dm guide missing %q:\n%s", fragment, guide)
		}
	}
	if strings.Contains(guide, "slack dm send") || strings.Contains(guide, "rejects bot-token") {
		t.Fatalf("send_dm guide still documents local bot-token rejection:\n%s", guide)
	}
}

func TestGuideDocumentsLookupSurface(t *testing.T) {
	destination := agenthelp.GetGuideSection("discover_destination")
	for _, fragment := range []string{"slick lookup channel", "--types im", "plain mode renders tables"} {
		if !strings.Contains(destination, fragment) {
			t.Fatalf("discover_destination guide missing %q:\n%s", fragment, destination)
		}
	}
	for _, removed := range []string{"slack channel list", "slack dm list"} {
		if strings.Contains(destination, removed) {
			t.Fatalf("discover_destination guide documents removed command %q:\n%s", removed, destination)
		}
	}

	user := agenthelp.GetGuideSection("lookup_user")
	for _, fragment := range []string{"slick lookup user", "--user", "timezone"} {
		if !strings.Contains(user, fragment) {
			t.Fatalf("lookup_user guide missing %q:\n%s", fragment, user)
		}
	}
	if strings.Contains(user, "slack user list") || strings.Contains(user, "slack user info") {
		t.Fatalf("lookup_user guide documents removed user commands:\n%s", user)
	}
}

func TestGuideDocumentsUserLookupDMAndTimestampWorkflows(t *testing.T) {
	lookup := agenthelp.GetGuideSection("lookup_user")
	for _, fragment := range []string{
		"slick lookup user --filter ansible --max-items 20",
		"data.users[].id",
		"slick message send --user <user-id>",
	} {
		if !strings.Contains(lookup, fragment) {
			t.Fatalf("lookup_user guide missing %q:\n%s", fragment, lookup)
		}
	}

	history := agenthelp.GetGuideSection("read_history")
	for _, fragment := range []string{
		"Use history to discover message timestamps",
		"data.messages[].ts",
		"Use the parent message `ts` with `slick reply --parent`",
		"Use any message or reply `ts` with `slick react add --timestamp`",
	} {
		if !strings.Contains(history, fragment) {
			t.Fatalf("read_history guide missing %q:\n%s", fragment, history)
		}
	}
}

func TestGuideDocumentsOperationalRunbooks(t *testing.T) {
	developerReview := agenthelp.GetGuideSection("developer_review")
	for _, fragment := range []string{
		"Runbook:",
		"Inputs:",
		"Preflight:",
		"Send the parent",
		"Parse and store",
		"Quirks:",
	} {
		if !strings.Contains(developerReview, fragment) {
			t.Fatalf("developer_review guide missing %q:\n%s", fragment, developerReview)
		}
	}

	cleanup := agenthelp.GetGuideSection("cleanup_msgs")
	for _, fragment := range []string{
		"Runbook:",
		"Inputs:",
		"meta.pagination.has_more",
		"meta.pagination.next_cursor",
		"data.matches[].channel.id",
		"errors[0].retry_after_seconds",
		"message_not_found",
		"Quirks:",
	} {
		if !strings.Contains(cleanup, fragment) {
			t.Fatalf("cleanup_msgs guide missing %q:\n%s", fragment, cleanup)
		}
	}

	search := agenthelp.GetGuideSection("search_msgs")
	for _, fragment := range []string{
		"--cursor <meta.pagination.next_cursor>",
		"repeat search and delete until paginated search returns zero matches",
	} {
		if !strings.Contains(search, fragment) {
			t.Fatalf("search_msgs guide missing %q:\n%s", fragment, search)
		}
	}

	safeMutation := agenthelp.GetGuideSection("safe_mutation")
	for _, fragment := range []string{
		"chat.postMessage",
		"Separate CLI processes do not share proactive throttle state",
		"structured `rate_limit` errors",
	} {
		if !strings.Contains(safeMutation, fragment) {
			t.Fatalf("safe_mutation guide missing %q:\n%s", fragment, safeMutation)
		}
	}

	cacheMetadata := agenthelp.GetGuideSection("cache_metadata")
	for _, fragment := range []string{
		"slick cache users",
		"slick cache channels",
		"--refresh",
		"--ttl-minutes 10080",
		"~/.cache/slick/<profile>/",
	} {
		if !strings.Contains(cacheMetadata, fragment) {
			t.Fatalf("cache_metadata guide missing %q:\n%s", fragment, cacheMetadata)
		}
	}

	health := agenthelp.GetGuideSection("check_health")
	for _, fragment := range []string{
		"slick health check --output=json",
		"slick health current --output=json",
		"slick health history --limit 20 --output=json",
		"slick health api-test --output=json",
		"data.healthy",
		"No Slack token or scopes",
	} {
		if !strings.Contains(health, fragment) {
			t.Fatalf("check_health guide missing %q:\n%s", fragment, health)
		}
	}
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
