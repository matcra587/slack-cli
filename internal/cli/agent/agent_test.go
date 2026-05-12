package agent_test

import (
	"testing"

	agentpkg "github.com/matcra587/slack-cli/internal/agent"
	cliagent "github.com/matcra587/slack-cli/internal/cli/agent"
	cliruntime "github.com/matcra587/slack-cli/internal/cli/runtime"
)

func TestDetectAttributionUsesInternalAgentDetection(t *testing.T) {
	t.Setenv("CLAUDE_CODE", "1")

	got := cliagent.DetectAttribution(cliruntime.AttributionFlags{})
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

	off := false
	got := cliagent.DetectAttribution(cliruntime.AttributionFlags{Attribution: &off})
	if got.Enabled {
		t.Fatalf("DetectAttribution opt-out = %#v, want disabled", got)
	}
}

func TestDetectAttributionHonorsCommandOptIn(t *testing.T) {
	// No env trigger; profile says off. --attribution should still win.
	profileOff := false
	on := true
	got := cliagent.DetectAttribution(cliruntime.AttributionFlags{
		Attribution:        &on,
		ProfileAttribution: &profileOff,
	})
	if !got.Enabled {
		t.Fatalf("DetectAttribution opt-in = %#v, want enabled", got)
	}
	if got.Category != agentpkg.CategoryCLI {
		t.Fatalf("DetectAttribution Category = %q, want CLI", got.Category)
	}
}
