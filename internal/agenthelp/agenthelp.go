package agenthelp

import (
	"slices"
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
			Description: "Slack token via SLACK_CLI_TOKEN_<PROFILE>, SLACK_CLI_TOKEN, or configured secret ref. Never store plaintext tokens in TOML or argv.",
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
				"--raw is output-only; use command-local --blocks for raw Block Kit input",
				"Markdown uses source-preserving Markdown fallback sections for unsupported block-level constructs",
				"--blocks validates Slack Block Kit JSON rules before Slack mutation",
				"Slack errors such as missing_scope, not_in_channel, and no_permission map to fixed exit codes",
				"--json, --plain, --compact, and --raw are mutually exclusive",
			},
		},
		GlobalFlags:   extractGlobalFlags(root),
		Commands:      walkCommands(root),
		InputShapes:   inputShapes(),
		OutputSchemas: outputSchemas(),
		Env:           schemaEnvVars(),
		ExitCodes:     exitCodes(),
		Examples:      examples(),
		Workflows:     workflowSchemas(),
		BestPractices: []string{
			"Prefer channel/user IDs over display names in automation.",
			"Use --file - for multiline message bodies.",
			"Use --blocks when message-like input is already Slack Block Kit JSON.",
			"Use --dry-run before destructive or high-visibility mutations.",
			"Use --compact when another tool expects command-specific JSON only.",
			"Follow meta.pagination.next_cursor while meta.pagination.has_more is true.",
			"Prime users and channels with cache commands before repeated lookup or completion-heavy work.",
			"Load the workflow runbook with slick agent guide <workflow> before operating.",
		},
		AntiPatterns: []string{
			"Do not store Slack tokens in TOML or source files.",
			"Do not pass raw Slack tokens in argv.",
			"Do not use --raw to select raw Block Kit input; --raw is an output mode.",
			"Do not disable agent attribution unless explicitly required.",
			"Do not parse human-readable output in scripts; use JSON.",
			"Do not assume one search page means there are no more matches.",
			"Do not duplicate attribution text in the message body.",
		},
	}
}

func inputShapes() map[string][]string {
	return map[string][]string{
		"message.send":   {"--channel <id|alias>", "--user <id|alias>", "--channel and --user are mutually exclusive", "--message <markdown>", "--file <path|->", "stdin markdown when --file -", "source-preserving Markdown fallback for unsupported block-level constructs", "--blocks Block Kit JSON array", "--blocks validates Slack Block Kit JSON rules"},
		"reply":          {"--channel <id|alias>", "--parent <slack-ts>", "--message <markdown>", "--file <path|->", "--blocks Block Kit JSON array", "--dry-run"},
		"react.add":      {"--channel <id|alias>", "--timestamp <slack-ts>", "--emoji <name|:name:>", "--dry-run"},
		"react.remove":   {"--channel <id|alias>", "--timestamp <slack-ts>", "--emoji <name|:name:>", "--dry-run"},
		"react.list":     {"--channel <id|alias>", "--timestamp <slack-ts>"},
		"lookup.channel": {"--channel <id|alias> for one conversation", "--types <public_channel,private_channel,im,mpim>", "--max-items <n>", "--filter <text>"},
		"lookup.user":    {"--user <id|alias> for one user", "--max-items <n>", "--filter <text>", "--presence", "--include-deleted"},
		"cache.users":    {"--refresh", "--ttl-minutes <n>", "--page-size <n>", "--max-pages <n>", "active users only"},
		"cache.channels": {"--refresh", "--ttl-minutes <n>", "--page-size <n>", "--max-pages <n>", "active public/private/DM/MPIM conversations"},
		"cache.clear":    {"optional resource: users or channels", "no resource clears all cache files for the profile"},
		"history.list":   {"--channel <id|alias>", "--max-items <n>", "--since <slack-ts>", "--until <slack-ts>", "--user <id>", "--thread <ts>"},
		"file.upload":    {"--channel <id|alias>", "--file <path|->", "--filename <name> required for stdin", "--message <markdown>", "--blocks Block Kit JSON array for --message comment"},
		"manifest":       {"template --name <name>", "template --format <json|yaml>", "local manifest generation only"},
		"auth.login":     {"--workspace <name>", "--method <oauth|token>", "--token-stdin", "--token-file <path>", "--token-env <env-var-name>", "--oauth-client-id <id>", "--oauth-redirect-url <local-url>", "--force"},
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
		"canceled":         6,
		"timeout":          7,
	}
}

func examples() map[string][]string {
	return map[string][]string{
		"message":  {"echo 'Deploy complete' | slick message send --channel '#alerts' --file -", "slick message send --user U123 --message 'Need review'", "slick message send --user dev@example.com,ops@example.com --message 'PR is ready'", "slick message send --channel C123 --blocks --file blocks.json"},
		"reply":    {"slick reply --channel C123 --parent 1746284582.123456 --message 'Investigating'", "echo 'details' | slick reply --channel C123 --parent 1746284582.123456 --file -"},
		"react":    {"slick react add --channel C123 --timestamp 1746284582.123456 --emoji eyes", "slick react remove --channel C123 --timestamp 1746284582.123456 --emoji eyes", "slick react list --channel C123 --timestamp 1746284582.123456"},
		"status":   {"slick status set --text 'Heads down' --emoji :headphones: --expires-in 2h", "slick status clear"},
		"history":  {"slick history list --channel C123 --max-items 50"},
		"lookup":   {"slick lookup channel --max-items 20", "slick lookup channel --types im", "slick lookup user --presence", "slick lookup user --user U123", "slick lookup messages --query 'deploy failed' --max-items 10"},
		"cache":    {"slick cache users", "slick cache channels", "slick cache users --refresh", "slick cache clear users"},
		"file":     {"probationary, not promoted: tar czf - build/ | slick file upload --channel C123 --file - --filename build.tgz"},
		"manifest": {"slick manifest template --name example --format json > manifest.json", "slick manifest template --name example --format yaml > manifest.yaml"},
		"auth":     {"slick auth login", "printf '%s\\n' \"$SLACK_TOKEN\" | slick auth login --workspace default --method token --token-stdin", "slick auth login --workspace default --method token --token-file ./slack-token.txt", "slick auth login --workspace default --method token --token-env SLACK_CLI_TOKEN_DEFAULT"},
		"schema":   {"slick agent schema", "slick agent schema --compact", "slick agent schema --raw"},
	}
}

func schemaEnvVars() []string {
	env := []string{
		"SLACK_CLI_TOKEN_<PROFILE>",
		"SLACK_CLI_TOKEN",
		"SLICK_CONFIG",
		"SLACK_CLI_CONFIG",
		"XDG_CACHE_HOME",
		"SLACK_CLI_BASE_URL",
		"SLACK_CLI_CALLBACK_PORT",
	}
	env = append(env, agent.KnownEnvVars()...)
	return env
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
	if slices.Contains(readOnlyVerbs, name) {
		return true
	}
	return strings.HasPrefix(name, "list")
}
