package agent_test

import (
	"testing"

	agentpkg "github.com/matcra587/slack-cli/internal/agent"
	cliagent "github.com/matcra587/slack-cli/internal/cli/agent"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
)

func TestDetectAttributionUsesInternalAgentDetection(t *testing.T) {
	t.Setenv("CLAUDE_CODE", "1")

	got := cliagent.DetectAttribution(cliruntime.AgentFlags{})
	if !got.Enabled {
		t.Fatal("DetectAttribution Enabled = false, want true")
	}
	if got.Category != agentpkg.CategoryAI {
		t.Fatalf("DetectAttribution Category = %q, want AI", got.Category)
	}
	if got.Emoji != ":robot_face:" {
		t.Fatalf("DetectAttribution Emoji = %q, want :robot_face:", got.Emoji)
	}
}

func TestDetectAttributionHonorsCommandOptOut(t *testing.T) {
	t.Setenv("CLAUDE_CODE", "1")

	got := cliagent.DetectAttribution(cliruntime.AgentFlags{NoAgentAttribution: true})
	if got.Enabled {
		t.Fatalf("DetectAttribution opt-out = %#v, want disabled", got)
	}
}
