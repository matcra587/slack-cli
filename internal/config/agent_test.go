package config_test

import (
	"testing"

	"github.com/matcra587/slack-cli/internal/config"
)

func TestWorkspaceAgentSettingsDefaultOn(t *testing.T) {
	settings := config.WorkspaceProfile{}.AgentSettings()

	if !settings.Attribution {
		t.Fatal("AgentSettings Attribution = false, want default true")
	}
	if settings.Label != "" {
		t.Fatalf("AgentSettings Label = %q, want no configured label", settings.Label)
	}
	if settings.Emoji != "" {
		t.Fatalf("AgentSettings Emoji = %q, want no configured emoji", settings.Emoji)
	}
	if settings.Message != "" {
		t.Fatalf("AgentSettings Message = %q, want no configured message", settings.Message)
	}
}

func TestWorkspaceAgentSettingsSupportsExplicitOptOut(t *testing.T) {
	disabled := false
	settings := config.WorkspaceProfile{
		AgentAttribution: &disabled,
	}.AgentSettings()

	if settings.Attribution {
		t.Fatal("AgentSettings Attribution = true, want explicit opt-out")
	}
}

func TestWorkspaceAgentSettingsUsesCustomLabelAndEmoji(t *testing.T) {
	enabled := true
	settings := config.WorkspaceProfile{
		AgentAttribution: &enabled,
		AgentLabel:       "CI/CD pipeline",
		AgentEmoji:       ":gear:",
		AgentMessage:     "Sent from release automation",
	}.AgentSettings()

	if !settings.Attribution {
		t.Fatal("AgentSettings Attribution = false, want true")
	}
	if settings.Label != "CI/CD pipeline" {
		t.Fatalf("AgentSettings Label = %q, want custom label", settings.Label)
	}
	if settings.Emoji != ":gear:" {
		t.Fatalf("AgentSettings Emoji = %q, want :gear:", settings.Emoji)
	}
	if settings.Message != "Sent from release automation" {
		t.Fatalf("AgentSettings Message = %q, want custom message", settings.Message)
	}
}

func TestWorkspaceAgentSettingsSupportsNestedAttributionConfig(t *testing.T) {
	disabled := false
	settings := config.WorkspaceProfile{
		Attribution: config.AttributionConfig{
			Enabled: &disabled,
			Label:   "deploy pipeline",
			Message: "Sent from deploy job",
			Emoji:   ":rocket:",
		},
	}.AgentSettings()

	if settings.Label != "deploy pipeline" {
		t.Fatalf("AgentSettings Label = %q, want nested label", settings.Label)
	}
	if settings.Attribution {
		t.Fatal("AgentSettings Attribution = true, want nested enabled=false")
	}
	if settings.Message != "Sent from deploy job" {
		t.Fatalf("AgentSettings Message = %q, want nested message", settings.Message)
	}
	if settings.Emoji != ":rocket:" {
		t.Fatalf("AgentSettings Emoji = %q, want nested emoji", settings.Emoji)
	}
}
