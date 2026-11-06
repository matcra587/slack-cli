package agent_test

import (
	"testing"

	"github.com/matcra587/slack-cli/internal/agent"
)

func TestManualAttributionUsesCustomLabelAndPreservesMessagePrecedence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, label, message, want string }{
		{"default", "", "", "Sent via slick"},
		{"explicit default", "slick", "", "Sent via slick"},
		{"custom label", "Release helper", "", "Sent via slick (Release helper)"},
		{"custom message", "Release helper", "Prepared by release automation", "Prepared by release automation"},
		{"removed label", "", "Prepared by release automation", "Prepared by release automation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := agent.NewAttribution(agent.Detection{Active: true, Category: agent.CategoryCLI}, agent.Options{Label: tc.label, Message: tc.message})
			if got.Message != tc.want {
				t.Fatalf("manual attribution=%q, want %q", got.Message, tc.want)
			}
		})
	}
}
