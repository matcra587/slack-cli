package main

import (
	"testing"

	"github.com/matcra587/slack-cli/internal/agent"
)

func TestDetectAgentModeUsesInternalAgentDetection(t *testing.T) {
	t.Setenv("CLAUDE_CODE", "1")

	got := DetectAgentMode(AgentFlags{})
	if !got.Enabled {
		t.Fatal("DetectAgentMode Enabled = false, want true")
	}
	if got.Category != agent.CategoryAI {
		t.Fatalf("DetectAgentMode Category = %q, want AI", got.Category)
	}
	if got.Emoji != ":robot_face:" {
		t.Fatalf("DetectAgentMode Emoji = %q, want :robot_face:", got.Emoji)
	}
}

func TestDetectAgentModeHonorsCommandOptOut(t *testing.T) {
	t.Setenv("CLAUDE_CODE", "1")

	got := DetectAgentMode(AgentFlags{NoAgentAttribution: true})
	if got.Enabled {
		t.Fatalf("DetectAgentMode opt-out = %#v, want disabled", got)
	}
}
