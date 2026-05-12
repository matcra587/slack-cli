package agent

import "os"

type Category string

const (
	CategoryCLI        Category = "cli"
	CategoryAI         Category = "ai"
	CategoryCI         Category = "ci"
	CategoryAutomation Category = "automation"
	CategoryCron       Category = "cron"
)

type Options struct {
	// Attribution is the per-command override. nil means "defer to env
	// and profile". A non-nil true forces attribution on (overriding
	// profile.attribution.enabled = false); a non-nil false forces it
	// off (overriding env detection and profile opt-in).
	Attribution        *bool
	ProfileAttribution *bool
	Label              string
	Emoji              string
	Message            string
}

type Detection struct {
	Active   bool
	Source   string
	Name     string
	Category Category
}

type envTrigger struct {
	Key      string
	Name     string
	Category Category
}

var envTriggers = []envTrigger{
	{Key: "CLAUDE_CODE", Name: "Claude Code", Category: CategoryAI},
	{Key: "CLAUDECODE", Name: "Claude Code", Category: CategoryAI},
	{Key: "CURSOR_AGENT", Name: "Cursor", Category: CategoryAI},
	{Key: "CURSOR_TERMINAL", Name: "Cursor", Category: CategoryAI},
	{Key: "CODEX", Name: "Codex", Category: CategoryAI},
	{Key: "OPENAI_CODEX", Name: "Codex", Category: CategoryAI},
	{Key: "AIDER", Name: "Aider", Category: CategoryAI},
	{Key: "CLINE", Name: "Cline", Category: CategoryAI},
	{Key: "WINDSURF", Name: "Windsurf", Category: CategoryAI},
	{Key: "WINDSURF_AGENT", Name: "Windsurf", Category: CategoryAI},
	{Key: "GITHUB_COPILOT", Name: "GitHub Copilot", Category: CategoryAI},
	{Key: "COPILOT", Name: "GitHub Copilot", Category: CategoryAI},
	{Key: "CODEIUM", Name: "Codeium", Category: CategoryAI},
	{Key: "AMAZON_Q", Name: "Amazon Q", Category: CategoryAI},
	{Key: "AWS_Q_DEVELOPER", Name: "Amazon Q", Category: CategoryAI},
	{Key: "GEMINI_CODE_ASSIST", Name: "Gemini Code Assist", Category: CategoryAI},
	{Key: "SRC_CODY", Name: "Cody", Category: CategoryAI},
	{Key: "GITHUB_ACTIONS", Name: "GitHub Actions", Category: CategoryCI},
	{Key: "BUILDKITE", Name: "Buildkite", Category: CategoryCI},
	{Key: "JENKINS_URL", Name: "Jenkins", Category: CategoryCI},
	{Key: "GITLAB_CI", Name: "GitLab CI", Category: CategoryCI},
	{Key: "CIRCLECI", Name: "CircleCI", Category: CategoryCI},
	{Key: "TRAVIS", Name: "Travis CI", Category: CategoryCI},
	{Key: "BITBUCKET_BUILD_NUMBER", Name: "Bitbucket Pipelines", Category: CategoryCI},
	{Key: "TEAMCITY_VERSION", Name: "TeamCity", Category: CategoryCI},
	{Key: "TF_BUILD", Name: "Azure Pipelines", Category: CategoryCI},
	{Key: "CI", Name: "CI/CD", Category: CategoryCI},
	{Key: "CRON", Name: "cron", Category: CategoryCron},
	{Key: "CRON_JOB", Name: "cron", Category: CategoryCron},
	{Key: "SLACK_CLI_AGENT", Name: "automation", Category: CategoryAutomation},
	{Key: "FORCE_AGENT_MODE", Name: "automation", Category: CategoryAutomation},
}

func KnownEnvVars() []string {
	keys := make([]string, 0, len(envTriggers))
	for _, trigger := range envTriggers {
		keys = append(keys, trigger.Key)
	}
	return keys
}

func Detect(opts Options) Detection {
	forceOn := opts.Attribution != nil && *opts.Attribution
	forceOff := opts.Attribution != nil && !*opts.Attribution

	// Per-command opt-out wins absolutely.
	if forceOff {
		return Detection{}
	}

	// Profile opt-out kills attribution unless the caller explicitly
	// opted back in for this command via --attribution.
	if !forceOn && opts.ProfileAttribution != nil && !*opts.ProfileAttribution {
		return Detection{}
	}

	// Env triggers are the highest-priority category signal: if Claude
	// Code or a CI system is detected, we want that category even when
	// --attribution is also passed.
	for _, trigger := range envTriggers {
		if TruthyEnv(os.Getenv(trigger.Key)) {
			return Detection{
				Active:   true,
				Source:   trigger.Key,
				Name:     trigger.Name,
				Category: trigger.Category,
			}
		}
	}

	// --attribution without any env trigger: treat as a manual CLI run.
	if forceOn {
		return Detection{Active: true, Source: "flag", Name: "manual", Category: CategoryCLI}
	}

	if opts.ProfileAttribution != nil && *opts.ProfileAttribution {
		return Detection{Active: true, Source: "profile", Name: "profile", Category: CategoryCLI}
	}
	return Detection{}
}
