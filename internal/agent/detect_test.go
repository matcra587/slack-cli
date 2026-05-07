package agent_test

import (
	"os"
	"testing"

	"github.com/matcra587/slack-cli/internal/agent"
)

func TestMain(m *testing.M) {
	clearAmbientAgentEnvironment()
	os.Exit(m.Run())
}

func clearAmbientAgentEnvironment() {
	for _, key := range agent.KnownEnvVars() {
		_ = os.Unsetenv(key)
	}
}

func TestDetectRecognizesAIAgentEnvironmentList(t *testing.T) {
	tests := []struct {
		env  string
		name string
	}{
		{"CLAUDE_CODE", "Claude Code"},
		{"CLAUDECODE", "Claude Code"},
		{"CURSOR_AGENT", "Cursor"},
		{"CURSOR_TERMINAL", "Cursor"},
		{"CODEX", "Codex"},
		{"OPENAI_CODEX", "Codex"},
		{"AIDER", "Aider"},
		{"CLINE", "Cline"},
		{"WINDSURF", "Windsurf"},
		{"WINDSURF_AGENT", "Windsurf"},
		{"GITHUB_COPILOT", "GitHub Copilot"},
		{"COPILOT", "GitHub Copilot"},
		{"CODEIUM", "Codeium"},
		{"AMAZON_Q", "Amazon Q"},
		{"AWS_Q_DEVELOPER", "Amazon Q"},
		{"GEMINI_CODE_ASSIST", "Gemini Code Assist"},
		{"SRC_CODY", "Cody"},
	}

	for _, tt := range tests {
		t.Run(tt.env, func(t *testing.T) {
			t.Setenv(tt.env, "1")

			got := agent.Detect(agent.Options{})
			if !got.Active {
				t.Fatal("Detect Active = false, want true")
			}
			if got.Name != tt.name {
				t.Fatalf("Detect Name = %q, want %q", got.Name, tt.name)
			}
			if got.Category != agent.CategoryAI {
				t.Fatalf("Detect Category = %q, want AI", got.Category)
			}
			if got.Source != tt.env {
				t.Fatalf("Detect Source = %q, want %q", got.Source, tt.env)
			}
		})
	}
}

func TestDetectHonorsFalseyEnvironmentValues(t *testing.T) {
	for _, value := range []string{"", "0", "false", "no"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("CLAUDE_CODE", value)

			got := agent.Detect(agent.Options{})
			if got.Active {
				t.Fatalf("Detect Active = true for falsey value %q", value)
			}
		})
	}
}

func TestDetectRecognizesCICronAndGenericAutomation(t *testing.T) {
	tests := []struct {
		env      string
		category agent.Category
	}{
		{"GITHUB_ACTIONS", agent.CategoryCI},
		{"BUILDKITE", agent.CategoryCI},
		{"JENKINS_URL", agent.CategoryCI},
		{"GITLAB_CI", agent.CategoryCI},
		{"CIRCLECI", agent.CategoryCI},
		{"TRAVIS", agent.CategoryCI},
		{"BITBUCKET_BUILD_NUMBER", agent.CategoryCI},
		{"TEAMCITY_VERSION", agent.CategoryCI},
		{"TF_BUILD", agent.CategoryCI},
		{"CI", agent.CategoryCI},
		{"CRON", agent.CategoryCron},
		{"CRON_JOB", agent.CategoryCron},
		{"SLACK_CLI_AGENT", agent.CategoryAutomation},
		{"FORCE_AGENT_MODE", agent.CategoryAutomation},
	}

	for _, tt := range tests {
		t.Run(tt.env, func(t *testing.T) {
			t.Setenv(tt.env, "true")

			got := agent.Detect(agent.Options{})
			if !got.Active {
				t.Fatal("Detect Active = false, want true")
			}
			if got.Category != tt.category {
				t.Fatalf("Detect Category = %q, want %q", got.Category, tt.category)
			}
		})
	}
}

func TestDetectSupportsFlagAndExplicitOptOut(t *testing.T) {
	profileOn := true
	profileOff := false

	if got := agent.Detect(agent.Options{Force: true}); !got.Active || got.Source != "flag" {
		t.Fatalf("flag detection = %#v", got)
	}
	if got := agent.Detect(agent.Options{ProfileAttribution: &profileOn}); !got.Active || got.Source != "profile" {
		t.Fatalf("profile attribution should force attribution: %#v", got)
	}
	t.Setenv("CLAUDE_CODE", "1")
	if got := agent.Detect(agent.Options{ProfileAttribution: &profileOff}); got.Active {
		t.Fatalf("explicit profile opt-out should disable attribution: %#v", got)
	}
	if got := agent.Detect(agent.Options{Force: true, NoAttribution: true}); got.Active {
		t.Fatalf("per-command opt-out should disable attribution: %#v", got)
	}
}
