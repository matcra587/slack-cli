package agent_test

import (
	"testing"

	"github.com/matcra587/slack-cli/internal/agent"
)

func TestAttributionUsesCategoryDefaults(t *testing.T) {
	tests := []struct {
		category agent.Category
		label    string
		emoji    string
	}{
		{agent.CategoryAI, "agent mode", ":robot_face:"},
		{agent.CategoryCI, "CI/CD pipeline", ":gear:"},
		{agent.CategoryAutomation, "automation", ":wrench:"},
		{agent.CategoryCron, "cron job", ":clock1:"},
	}

	for _, tt := range tests {
		t.Run(string(tt.category), func(t *testing.T) {
			got := agent.NewAttribution(agent.Detection{Active: true, Category: tt.category}, agent.Options{})
			if got.Label != tt.label {
				t.Fatalf("Label = %q, want %q", got.Label, tt.label)
			}
			if got.Emoji != tt.emoji {
				t.Fatalf("Emoji = %q, want %q", got.Emoji, tt.emoji)
			}
			if got.Message != "Sent via slack-cli ("+tt.label+")" {
				t.Fatalf("Message = %q, want default message for %q", got.Message, tt.label)
			}
		})
	}
}

func TestAttributionUsesCustomPresentation(t *testing.T) {
	got := agent.NewAttribution(agent.Detection{Active: true, Category: agent.CategoryCI}, agent.Options{
		Label:   "build pipeline",
		Emoji:   ":hammer:",
		Message: "Sent from deploy job",
	})

	if got.Label != "build pipeline" {
		t.Fatalf("Label = %q, want build pipeline", got.Label)
	}
	if got.Emoji != ":hammer:" {
		t.Fatalf("Emoji = %q, want :hammer:", got.Emoji)
	}
	if got.Message != "Sent from deploy job" {
		t.Fatalf("Message = %q, want custom message", got.Message)
	}
}
