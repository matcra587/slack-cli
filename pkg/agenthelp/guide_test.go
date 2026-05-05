package agenthelp_test

import (
	"strings"
	"testing"

	"github.com/matcra587/slack-cli/pkg/agenthelp"
)

func TestGuideDocumentsBlocksRawAndAttributionConfig(t *testing.T) {
	guide := agenthelp.GetGuideSection("send_msg")
	for _, fragment := range []string{
		"--blocks",
		"raw Block Kit",
		"validates Slack Block Kit JSON rules",
		"Unsupported block-level Markdown preserves original source text",
		"--raw",
		"output-only",
		"attribution.enabled",
		"attribution.message",
		"attribution.emoji",
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
		"not_in_channel",
		"no_permission",
		"fixed exit-code contract",
	} {
		if !strings.Contains(guide, fragment) {
			t.Fatalf("send_dm guide missing %q:\n%s", fragment, guide)
		}
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
		"--json",
		"--plain",
		"--compact",
		"--raw",
		"mutually exclusive",
		"Exit codes",
		"errors[0].type",
	} {
		if !strings.Contains(guide, fragment) {
			t.Fatalf("core_contract guide missing %q:\n%s", fragment, guide)
		}
	}
}

func TestGuideDocumentsPromotedReplyAndReactCommands(t *testing.T) {
	react := agenthelp.GetGuideSection("react")
	for _, fragment := range []string{"slack react add", "slack react remove", "slack react list", "react.add", "react.remove", "react.list"} {
		if !strings.Contains(react, fragment) {
			t.Fatalf("react guide missing %q:\n%s", fragment, react)
		}
	}
	if strings.Contains(react, "slack reaction") || strings.Contains(react, "probationary") {
		t.Fatalf("react guide documents legacy/probationary command:\n%s", react)
	}

	reply := agenthelp.GetGuideSection("reply")
	for _, fragment := range []string{"slack reply", "--parent", "thread_ts", "Command metadata uses `reply`"} {
		if !strings.Contains(reply, fragment) {
			t.Fatalf("reply guide missing %q:\n%s", fragment, reply)
		}
	}
	if strings.Contains(reply, "slack thread reply") || strings.Contains(reply, "probationary") {
		t.Fatalf("reply guide documents legacy/probationary command:\n%s", reply)
	}
}

func TestGuideDocumentsSlackDecidedBotDMBehavior(t *testing.T) {
	guide := agenthelp.GetGuideSection("send_dm")
	for _, fragment := range []string{"slack message send --user", "Slack decides", "bot-token", "structured error"} {
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
	for _, fragment := range []string{"slack lookup channel", "--types im", "plain mode renders tables"} {
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
	for _, fragment := range []string{"slack lookup user", "--user", "timezone"} {
		if !strings.Contains(user, fragment) {
			t.Fatalf("lookup_user guide missing %q:\n%s", fragment, user)
		}
	}
	if strings.Contains(user, "slack user list") || strings.Contains(user, "slack user info") {
		t.Fatalf("lookup_user guide documents removed user commands:\n%s", user)
	}
}
