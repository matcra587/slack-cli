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

func TestDetectSupportsProfileAndExplicitOptOut(t *testing.T) {
	profileOn := true
	profileOff := false
	attrOff := false

	if got := agent.Detect(agent.Options{ProfileAttribution: &profileOn}); !got.Active || got.Source != "profile" {
		t.Fatalf("profile attribution should force attribution: %#v", got)
	}
	t.Setenv("CLAUDE_CODE", "1")
	if got := agent.Detect(agent.Options{ProfileAttribution: &profileOff}); got.Active {
		t.Fatalf("explicit profile opt-out should disable attribution: %#v", got)
	}
	if got := agent.Detect(agent.Options{Attribution: &attrOff}); got.Active {
		t.Fatalf("per-command opt-out should disable attribution: %#v", got)
	}
	if got := agent.Detect(agent.Options{ProfileAttribution: &profileOn, Attribution: &attrOff}); got.Active {
		t.Fatalf("per-command opt-out must win against profile opt-in: %#v", got)
	}
}

func TestDetectSupportsPerCommandOptIn(t *testing.T) {
	attrOn := true
	attrOff := false
	profileOff := false

	// --attribution alone (no env trigger): manual CLI run.
	if got := agent.Detect(agent.Options{Attribution: &attrOn}); !got.Active || got.Source != "flag" || got.Category != agent.CategoryCLI {
		t.Fatalf("--attribution alone should yield manual CLI attribution: %#v", got)
	}

	// --attribution overrides profile opt-out so a single command can still
	// attribute when profile.attribution.enabled = false.
	if got := agent.Detect(agent.Options{Attribution: &attrOn, ProfileAttribution: &profileOff}); !got.Active || got.Source != "flag" {
		t.Fatalf("--attribution must override profile opt-out: %#v", got)
	}

	// Env trigger still wins for category labeling even when --attribution
	// is also passed — we want "Claude Code" not "manual".
	t.Setenv("CLAUDE_CODE", "1")
	if got := agent.Detect(agent.Options{Attribution: &attrOn}); !got.Active || got.Source != "CLAUDE_CODE" || got.Category != agent.CategoryAI {
		t.Fatalf("env trigger should set category even with --attribution: %#v", got)
	}

	// --no-attribution still wins absolutely, even with env trigger active.
	if got := agent.Detect(agent.Options{Attribution: &attrOff}); got.Active {
		t.Fatalf("--no-attribution must override env trigger: %#v", got)
	}
}

// TestDetectFullPrecedenceMatrix walks the full profile × flag × env grid so
// the precedence rules survive future Detect refactors. The rules under test:
//
//  1. --no-attribution wins absolutely (force-off beats env and profile).
//  2. Profile opt-out (attribution.enabled=false) kills env detection unless
//     --attribution is also passed for this command.
//  3. Env trigger sets category even when --attribution is also passed; the
//     "I'm in Claude Code" signal beats the "manual CLI run" signal.
//  4. --attribution alone (no env, no profile) is a manual CLI attribution.
func TestDetectFullPrecedenceMatrix(t *testing.T) {
	on, off := true, false

	type cell struct {
		name       string
		profile    *bool
		attr       *bool
		envTrigger string // empty means "no env trigger active"
		wantActive bool
		wantSource string // ignored when wantActive is false
	}
	cells := []cell{
		// --- profile=true ---
		{"profile=on,flag=nil,env=none", &on, nil, "", true, "profile"},
		{"profile=on,flag=nil,env=claude", &on, nil, "CLAUDE_CODE", true, "CLAUDE_CODE"},
		{"profile=on,flag=off,env=none", &on, &off, "", false, ""},
		{"profile=on,flag=off,env=claude", &on, &off, "CLAUDE_CODE", false, ""},
		{"profile=on,flag=on,env=none", &on, &on, "", true, "flag"},
		{"profile=on,flag=on,env=claude", &on, &on, "CLAUDE_CODE", true, "CLAUDE_CODE"},

		// --- profile=false ---
		// Note: profile opt-out kills env detection unless --attribution is
		// passed. This is intentional — a user who wrote
		// `attribution.enabled = false` doesn't want Slack messages branded
		// just because they happened to run inside a CI shell.
		{"profile=off,flag=nil,env=none", &off, nil, "", false, ""},
		{"profile=off,flag=nil,env=claude", &off, nil, "CLAUDE_CODE", false, ""},
		{"profile=off,flag=off,env=none", &off, &off, "", false, ""},
		{"profile=off,flag=off,env=claude", &off, &off, "CLAUDE_CODE", false, ""},
		{"profile=off,flag=on,env=none", &off, &on, "", true, "flag"},
		{"profile=off,flag=on,env=claude", &off, &on, "CLAUDE_CODE", true, "CLAUDE_CODE"},
	}

	for _, c := range cells {
		t.Run(c.name, func(t *testing.T) {
			if c.envTrigger != "" {
				t.Setenv(c.envTrigger, "1")
			}
			got := agent.Detect(agent.Options{
				Attribution:        c.attr,
				ProfileAttribution: c.profile,
			})
			if got.Active != c.wantActive {
				t.Fatalf("Active = %v, want %v (full=%#v)", got.Active, c.wantActive, got)
			}
			if c.wantActive && got.Source != c.wantSource {
				t.Fatalf("Source = %q, want %q (full=%#v)", got.Source, c.wantSource, got)
			}
		})
	}
}
