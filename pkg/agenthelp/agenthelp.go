package agenthelp

import (
	"strings"

	"github.com/matcra587/slack-cli/internal/agent"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type Schema struct {
	Version       string              `json:"version"`
	Description   string              `json:"description"`
	Auth          AuthInfo            `json:"auth"`
	Output        OutputInfo          `json:"output"`
	GlobalFlags   []FlagInfo          `json:"global_flags"`
	Commands      []CommandInfo       `json:"commands"`
	InputShapes   map[string][]string `json:"input_shapes"`
	OutputSchemas map[string][]string `json:"output_schemas"`
	Env           []string            `json:"env"`
	ExitCodes     map[string]int      `json:"exit_codes"`
	Examples      map[string][]string `json:"examples"`
	Workflows     []Workflow          `json:"workflows"`
	BestPractices []string            `json:"best_practices"`
	AntiPatterns  []string            `json:"anti_patterns"`
}

type AuthInfo struct {
	Type        string `json:"type"`
	EnvVar      string `json:"env_var"`
	Description string `json:"description"`
}

type OutputInfo struct {
	Default     string   `json:"default"`
	PlainFlag   string   `json:"plain_flag"`
	CompactFlag string   `json:"compact_flag"`
	RawFlag     string   `json:"raw_flag"`
	Notes       []string `json:"notes"`
}

type CommandInfo struct {
	Name        string        `json:"name"`
	FullPath    string        `json:"full_path"`
	Description string        `json:"description"`
	Flags       []FlagInfo    `json:"flags,omitempty"`
	ReadOnly    bool          `json:"read_only"`
	Subcommands []CommandInfo `json:"subcommands,omitempty"`
}

type FlagInfo struct {
	Name        string `json:"name"`
	Shorthand   string `json:"shorthand,omitempty"`
	Description string `json:"description"`
	Default     string `json:"default,omitempty"`
	Type        string `json:"type"`
}

type Workflow struct {
	Name  string   `json:"name"`
	Steps []string `json:"steps"`
}

type CompactSchema struct {
	Version  string           `json:"version"`
	Commands []CompactCommand `json:"commands"`
}

type CompactCommand struct {
	Name        string           `json:"name"`
	Flags       []string         `json:"flags,omitempty"`
	Subcommands []CompactCommand `json:"subcommands,omitempty"`
}

var readOnlyVerbs = []string{"list", "info", "history", "search", "schema", "guide", "status"}

func GenerateSchema(root *cobra.Command) Schema {
	return Schema{
		Version:     "1.0",
		Description: "Slack CLI - agent-first command line interface for Slack workspaces",
		Auth: AuthInfo{
			Type:        "bearer",
			EnvVar:      "SLACK_CLI_TOKEN",
			Description: "Slack token via environment or configured secret ref. Never store plaintext tokens in TOML.",
		},
		Output: OutputInfo{
			Default:     "JSON in non-TTY/agent mode, rich text in TTY",
			PlainFlag:   "--plain",
			CompactFlag: "--compact",
			RawFlag:     "--raw",
			Notes: []string{
				"stdout is command data",
				"stderr is diagnostics",
				"mutation commands support --dry-run",
				"agent-originated messages include attribution unless explicitly disabled",
			},
		},
		GlobalFlags:   extractGlobalFlags(root),
		Commands:      walkCommands(root),
		InputShapes:   inputShapes(),
		OutputSchemas: outputSchemas(),
		Env:           agent.KnownEnvVars(),
		ExitCodes:     exitCodes(),
		Examples:      examples(),
		Workflows:     workflowSchemas(),
		BestPractices: []string{
			"Prefer channel/user IDs over display names in automation.",
			"Use --file - for multiline message bodies.",
			"Use --dry-run before destructive or high-visibility mutations.",
			"Use --compact when another tool expects command-specific JSON only.",
		},
		AntiPatterns: []string{
			"Do not store Slack tokens in TOML or source files.",
			"Do not disable agent attribution unless explicitly required.",
			"Do not parse human-readable output in scripts; use JSON.",
		},
	}
}

func inputShapes() map[string][]string {
	return map[string][]string{
		"message.send": {"--channel <id|alias>", "--user <id|alias>", "--message <markdown>", "--file <path|->", "stdin markdown when --file -", "--raw Block Kit JSON array"},
		"history.list": {"--channel <id|alias>", "--max-items <n>", "--since <slack-ts>", "--until <slack-ts>", "--user <id>", "--thread <ts>"},
		"file.upload":  {"--channel <id|alias>", "--file <path|->", "--filename <name> required for stdin", "--message <markdown>"},
		"manifest":     {"template --name <name>", "template --format <json|yaml>", "local manifest generation only"},
		"auth.login":   {"--workspace-name <name>", "--auth-method <oauth|token>", "--token <xoxb|xoxp>", "--team-id <workspace-id>", "--client-id <id>", "--oauth-redirect-url <local-url>"},
	}
}

func outputSchemas() map[string][]string {
	return map[string][]string{
		"json_envelope": {"meta.command", "meta.workspace", "meta.timestamp", "meta.request_id", "data", "errors"},
		"compact":       {"command-specific data only"},
		"raw":           {"Slack-native Block Kit or API-native structured data"},
		"plain":         {"human-readable text"},
	}
}

func exitCodes() map[string]int {
	return map[string]int{
		"auth_failure":     1,
		"not_found":        2,
		"rate_limit":       3,
		"validation_error": 4,
		"server_error":     5,
	}
}

func examples() map[string][]string {
	return map[string][]string{
		"message":  {"echo 'Deploy complete' | slack message send --channel '#alerts' --file -", "slack message send --user U123 --message 'Need review'"},
		"history":  {"slack history list --channel C123 --max-items 50"},
		"file":     {"tar czf - build/ | slack file upload --channel C123 --file - --filename build.tgz"},
		"manifest": {"slack manifest template --name example --format json > manifest.json", "slack manifest template --name example --format yaml > manifest.yaml"},
		"auth":     {"slack auth login", "slack auth login --workspace-name default --auth-method token --token xoxb-..."},
		"schema":   {"slack agent schema --compact"},
	}
}

func workflowSchemas() []Workflow {
	catalog := WorkflowCatalog()
	workflows := make([]Workflow, 0, len(catalog))
	for _, item := range catalog {
		workflows = append(workflows, Workflow{Name: item.Name, Steps: append([]string(nil), item.Steps...)})
	}
	return workflows
}

func GenerateCompactSchema(root *cobra.Command) CompactSchema {
	return CompactSchema{Version: "1.0", Commands: walkCommandsCompact(root)}
}

func extractGlobalFlags(cmd *cobra.Command) []FlagInfo {
	var flags []FlagInfo
	cmd.PersistentFlags().VisitAll(func(flag *pflag.Flag) {
		if flag.Hidden {
			return
		}
		flags = append(flags, flagInfo(flag))
	})
	return flags
}

func walkCommands(cmd *cobra.Command) []CommandInfo {
	var commands []CommandInfo
	for _, child := range cmd.Commands() {
		if skipCommand(child) {
			continue
		}
		commands = append(commands, CommandInfo{
			Name:        child.Name(),
			FullPath:    child.CommandPath(),
			Description: child.Short,
			Flags:       extractLocalFlags(child),
			ReadOnly:    isReadOnly(child.Name()),
			Subcommands: walkCommands(child),
		})
	}
	return commands
}

func walkCommandsCompact(cmd *cobra.Command) []CompactCommand {
	var commands []CompactCommand
	for _, child := range cmd.Commands() {
		if skipCommand(child) {
			continue
		}
		commands = append(commands, CompactCommand{
			Name:        child.Name(),
			Flags:       extractFlagNames(child),
			Subcommands: walkCommandsCompact(child),
		})
	}
	return commands
}

func extractLocalFlags(cmd *cobra.Command) []FlagInfo {
	var flags []FlagInfo
	cmd.LocalFlags().VisitAll(func(flag *pflag.Flag) {
		if flag.Hidden {
			return
		}
		flags = append(flags, flagInfo(flag))
	})
	return flags
}

func extractFlagNames(cmd *cobra.Command) []string {
	var flags []string
	cmd.LocalFlags().VisitAll(func(flag *pflag.Flag) {
		if flag.Hidden {
			return
		}
		if flag.Shorthand != "" {
			flags = append(flags, "-"+flag.Shorthand+"/--"+flag.Name)
			return
		}
		flags = append(flags, "--"+flag.Name)
	})
	return flags
}

func flagInfo(flag *pflag.Flag) FlagInfo {
	return FlagInfo{
		Name:        flag.Name,
		Shorthand:   flag.Shorthand,
		Description: flag.Usage,
		Default:     flag.DefValue,
		Type:        flag.Value.Type(),
	}
}

func skipCommand(cmd *cobra.Command) bool {
	return cmd.Hidden || cmd.Name() == "help" || cmd.Name() == "completion"
}

func isReadOnly(name string) bool {
	for _, verb := range readOnlyVerbs {
		if name == verb {
			return true
		}
	}
	return strings.HasPrefix(name, "list")
}
