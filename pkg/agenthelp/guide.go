package agenthelp

import (
	"slices"
	"strings"

	"github.com/gechr/clib/help"
)

type GuideWorkflow struct {
	Name        string
	Description string
	Steps       []string
}

var guideWorkflows = []GuideWorkflow{
	{
		Name:        "auth_setup",
		Description: "Generate a manifest and authenticate a profile",
		Steps:       []string{"Generate/import a manifest template when needed", "Use local OAuth PKCE or token auth", "Store credentials in keychain or 1Password, never TOML plaintext", "Use profile names case-insensitively"},
	},
	{
		Name:        "config_prefs",
		Description: "Set profile preferences without touching auth",
		Steps:       []string{"Use config init for preferences only", "Use auth commands for credentials", "Set default channel and attribution text with config set", "Do not write plaintext tokens to TOML"},
	},
	{
		Name:        "send_msg",
		Description: "Send a markdown message and read ts/permalink from JSON",
		Steps:       []string{"Choose workspace/profile", "Pass message body with --message or --file -", "Customize attribution only when useful", "Read JSON response for ts and permalink"},
	},
	{
		Name:        "react_emoji",
		Description: "Add, remove, or list emoji reactions by channel and timestamp",
		Steps:       []string{"Use channel ID and message timestamp", "Pass emoji as :name: or name", "Read JSON response for confirmation"},
	},
	{
		Name:        "reply_thread",
		Description: "Reply under a parent message timestamp",
		Steps:       []string{"Use channel ID and parent message timestamp", "Post with thread reply --parent", "Read JSON response for reply ts and thread_ts"},
	},
	{
		Name:        "read_history",
		Description: "Read channel history or thread replies with bounded pagination",
		Steps:       []string{"Use history list for parent messages", "Use --thread for replies", "Bound output with --max-items and time filters"},
	},
	{
		Name:        "search_msgs",
		Description: "Search workspace messages and keep JSON for full text",
		Steps:       []string{"Search with structured Slack query text", "Bound output with --max-items", "Use JSON for full text and metadata"},
	},
	{
		Name:        "upload_file",
		Description: "Upload a file path or piped stdin artifact",
		Steps:       []string{"Use file upload with --file path or --file -", "Provide --filename for stdin uploads", "Read JSON response for file permalink"},
	},
	{
		Name:        "edit_msg",
		Description: "Edit own messages by exact channel and timestamp",
		Steps:       []string{"Use channel ID and exact message timestamp", "Edit only own messages", "Preview high-impact edits with --dry-run"},
	},
	{
		Name:        "delete_msg",
		Description: "Delete own messages with dry-run and force safeguards",
		Steps:       []string{"Use channel ID and exact message timestamp", "Preview destructive deletes with --dry-run", "Require --force for deletion"},
	},
	{
		Name:        "discover_destination",
		Description: "Find channel and DM IDs before posting",
		Steps:       []string{"List channels and DMs", "Use plain table output for humans and JSON for agents", "Inspect channel metadata when needed", "Prefer stable channel or DM IDs"},
	},
	{
		Name:        "inspect_schema",
		Description: "Read the machine schema and workflow guide",
		Steps:       []string{"Use agent schema, not the removed root schema alias", "Use --compact for a smaller command tree", "Use agent guide <workflow> for task instructions"},
	},
	{
		Name:        "lookup_user",
		Description: "Find user IDs, presence, status, and timezone",
		Steps:       []string{"List users to find candidates", "Fetch user info for presence, status, and timezone", "Prefer stable user IDs"},
	},
	{
		Name:        "send_dm",
		Description: "Send direct messages while handling token limits",
		Steps:       []string{"Use dm send with a user ID", "Use user-token auth for DM-anyone workflows", "The CLI rejects bot-token DM sends"},
	},
	{
		Name:        "safe_mutation",
		Description: "Preview high-impact changes and parse JSON results",
		Steps:       []string{"Use --dry-run before high-impact mutations", "Keep JSON output for machine parsing", "Treat deletes as destructive operations"},
	},
}

const guide = `# Slack CLI Agent Guide

## auth_setup
- Use ` + "`slack manifest template --preset messaging --type user --name <app-name>`" + ` to print an importable Slack app manifest. The CLI does not create Slack apps.
- Use one callback port for manifest generation and login. Set ` + "`SLACK_CLI_CALLBACK_PORT=<port>`" + `, or pass ` + "`slack manifest template --callback-port <port>`" + ` and ` + "`slack auth login --oauth-callback-port <port>`" + `.
- Without an explicit port, ` + "`auth login`" + ` listens on an OS-assigned local port. Slack still requires the redirect URL in the app to match the login URL exactly.
- For local OAuth, run ` + "`slack auth login`" + `, choose Slack OAuth, and paste the app's client ID. Local OAuth uses PKCE and does not need a client secret.
- OAuth derives workspace ID and display name from Slack after authorization.
- Token auth is supported with ` + "`slack auth login --auth-method token --token <xox-token>`" + `. The CLI validates the token with Slack and derives workspace metadata when possible.
- Credential material is stored as a structured keychain secret or read from a configured secret backend such as ` + "`op://...`" + `. Never put plaintext ` + "`xox*`" + ` tokens in TOML.
- Profile names are case-insensitive. ` + "`Default`" + ` and ` + "`default`" + ` refer to one profile; auth merges into the existing spelling.
- Use ` + "`slack auth status`" + ` to confirm the profile is valid before sending.

## config_prefs
- Use ` + "`slack config init`" + ` to create profile preferences. It does not ask for tokens, token type, workspace ID, or workspace display name.
- Use auth commands for credentials: ` + "`slack auth login`" + `, ` + "`slack auth status`" + `, ` + "`slack auth switch`" + `, and ` + "`slack auth logout`" + `.
- Use ` + "`slack config set workspaces.<profile>.default_channel <channel-id>`" + ` to set a default channel.
- Use ` + "`slack config set workspaces.<profile>.attribution.message <text>`" + ` and ` + "`slack config set workspaces.<profile>.attribution.emoji <emoji>`" + ` to customize attribution.
- Auth-owned fields may appear in TOML as keychain or secret references, but config commands do not edit them.
- If a profile has preferences but no credential reference, Slack API commands fail with an auth error. Run ` + "`slack auth login`" + ` or switch to an authenticated profile.

## send_msg
- Use ` + "`slack message send --channel <channel-id-or-alias> --message <markdown>`" + ` for short messages.
- Use ` + "`slack message send --channel <channel-id-or-alias> --file -`" + ` for multiline bodies from stdin.
- Expect JSON by default in agent or non-TTY mode. In ` + "`--plain`" + ` mode this is human output only; do not parse it in automation.
- Read ` + "`data.message.ts`" + ` and ` + "`data.permalink`" + ` to confirm delivery. Slack timestamps are channel-scoped.
- Markdown is converted to Block Kit. Use ` + "`--raw`" + ` only when sending Slack-native Block Kit JSON.
- Agent attribution is added when agent mode is detected by env vars or ` + "`--agent`" + `. Common triggers include ` + "`CLAUDE_CODE`" + `, ` + "`CLAUDECODE`" + `, ` + "`CURSOR_TERMINAL`" + `, ` + "`CODEX`" + `, ` + "`GITHUB_ACTIONS`" + `, and ` + "`CI`" + `.
- False-like values such as ` + "`0`" + `, ` + "`false`" + `, and ` + "`no`" + ` do not enable agent mode.
- Attribution is allowed by default but can be explicitly disabled with ` + "`--no-agent-attribution`" + ` or ` + "`agent_attribution = false`" + `.
- Customize attribution with ` + "`--agent-emoji`" + ` and ` + "`--agent-message`" + `. Config supports the same shape under ` + "`[workspaces.<profile>.attribution]`" + `.
- Block Kit context blocks carry the readable attribution.
- Use ` + "`--dry-run`" + ` before high-visibility sends.

## react_emoji
- Use ` + "`slack reaction add --channel <channel-id> --timestamp <message-ts> --emoji :thumbsup:`" + ` to react.
- Use ` + "`slack reaction remove --channel <channel-id> --timestamp <message-ts> --emoji :thumbsup:`" + ` to remove a reaction.
- Use ` + "`slack reaction list --channel <channel-id> --timestamp <message-ts>`" + ` to inspect reactions.
- Timestamps are Slack message timestamps such as ` + "`1746284582.123456`" + ` and are scoped to the channel.
- Emoji may be passed as ` + "`thumbsup`" + ` or ` + "`:thumbsup:`" + `.

## reply_thread
- Use ` + "`slack thread reply --channel <channel-id> --parent <parent-message-ts> --message <markdown>`" + ` to answer in a thread.
- The ` + "`--parent`" + ` value is the parent message timestamp, not a permalink or search result index.
- Read ` + "`data.message.thread_ts`" + ` and ` + "`data.message.ts`" + ` from JSON output to confirm nesting.
- Use ` + "`--file -`" + ` for multiline thread replies from stdin.

## read_history
- Use ` + "`slack history list --channel <channel-id> --max-items <n>`" + ` for parent messages.
- Use ` + "`slack history list --channel <channel-id> --thread <parent-ts> --max-items <n>`" + ` for thread replies.
- Use ` + "`--since`" + `, ` + "`--until`" + `, and ` + "`--user`" + ` to filter.
- Parent history includes reply counts and fetches full thread replies only when ` + "`--thread`" + ` or bounded ` + "`--include-replies`" + ` is used.
- Plain mode renders history as a table for humans. JSON mode preserves the envelope and full metadata for agents.

## search_msgs
- Use ` + "`slack search messages --query <query> --max-items <n>`" + ` to search workspace messages.
- JSON output includes full text and metadata. Plain output truncates snippets for humans.
- Use ` + "`--full`" + ` only when human plain output really needs the complete text.

## upload_file
- Use ` + "`slack file upload --channel <channel-id> --file <path>`" + ` for files on disk.
- Use ` + "`slack file upload --channel <channel-id> --file - --filename <name>`" + ` for piped artifacts.
- Use ` + "`--message`" + ` for an upload comment; markdown is converted to Block Kit and attribution is appended when agent mode is active.
- Upload progress and diagnostics go to stderr. stdout remains command data.

## edit_msg
- Use ` + "`slack message edit --channel <channel-id> --timestamp <message-ts> --message <markdown>`" + ` to correct own messages.
- Slack only allows editing own messages where token scopes permit it.
- Use the exact ` + "`--timestamp`" + ` returned by send, history, or search JSON.
- Use ` + "`--dry-run`" + ` before editing messages in high-visibility channels.

## delete_msg
- Use ` + "`slack message delete --channel <channel-id> --timestamp <message-ts> --force`" + ` to delete own messages.
- Run with ` + "`--dry-run`" + ` first to preview destructive changes.
- Delete targets are scoped by channel plus Slack timestamp.
- Prefer editing over deleting when preserving thread context matters.

## discover_destination
- Use ` + "`slack channel list --max-items <n>`" + ` to discover public and private channel destinations.
- Use ` + "`slack dm list --max-items <n>`" + ` to discover existing DM conversations.
- In automation, prefer IDs such as ` + "`C123...`" + ` and ` + "`D123...`" + ` over display names.
- Use ` + "`slack channel info --channel <channel-id>`" + ` before posting to unfamiliar channels.
- plain mode renders tables for list commands. Agents should keep JSON output and parse IDs from ` + "`data.channels`" + `.

## inspect_schema
- Use ` + "`slack agent schema`" + ` for the full command tree, flags, output schema notes, env triggers, exit codes, and workflow metadata.
- Use ` + "`slack agent schema --compact`" + ` when a smaller nested command tree is enough.
- The old root ` + "`slack schema`" + ` alias is intentionally removed; schema discovery lives under ` + "`agent schema`" + `.
- Use ` + "`slack agent guide`" + ` to list workflows and ` + "`slack agent guide <workflow>`" + ` for task-specific instructions.

## lookup_user
- Use ` + "`slack user list --max-items <n>`" + ` to find candidate users.
- Use ` + "`slack user info --user <user-id>`" + ` to fetch profile, presence, custom status, and timezone.
- Prefer user IDs such as ` + "`U123...`" + ` in commands.
- Check timezone before paging or scheduling humans.

## send_dm
- Use ` + "`slack dm send --user <user-id> --message <markdown>`" + ` for direct messages.
- ` + "`dm send`" + ` requires user-token auth in this CLI. It rejects bot-token sends instead of trying an arbitrary DM open.
- If only bot auth is available, post to a channel or install/configure a user-token profile.
- Read ` + "`data.message.channel`" + ` and ` + "`data.message.ts`" + ` from JSON output.

## safe_mutation
- Use ` + "`--dry-run`" + ` before send, edit, delete, reaction, and file upload mutations when the target is high-impact.
- Treat delete as destructive; require an exact channel and timestamp.
- Keep JSON output for automation and parse explicit IDs, timestamps, and permalinks.
- Use ` + "`--plain`" + ` only for human inspection, never for machine parsing.
- All user-facing output should pass through clog. stdout is data; stderr is diagnostics, progress, warnings, and structured errors.
`

func GetGuide() string {
	return guide
}

func WorkflowCatalog() []GuideWorkflow {
	workflows := append([]GuideWorkflow(nil), guideWorkflows...)
	slices.SortFunc(workflows, func(a GuideWorkflow, b GuideWorkflow) int {
		return strings.Compare(a.Name, b.Name)
	})
	return workflows
}

func WorkflowCommandGroup() help.CommandGroup {
	workflows := WorkflowCatalog()
	commands := make(help.CommandGroup, 0, len(workflows))
	for _, workflow := range workflows {
		commands = append(commands, help.Command{Name: workflow.Name, Desc: workflow.Description})
	}
	return commands
}

func WorkflowNames() []string {
	workflows := WorkflowCatalog()
	names := make([]string, 0, len(workflows))
	for _, workflow := range workflows {
		names = append(names, workflow.Name)
	}
	return names
}

func GetGuideSection(section string) string {
	if strings.TrimSpace(section) == "" {
		return guide
	}
	target := strings.ToLower(strings.TrimSpace(section))
	lines := strings.Split(guide, "\n")
	var out strings.Builder
	capturing := false
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			heading := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "## ")))
			if heading == target {
				capturing = true
				out.WriteString(line + "\n")
				continue
			}
			if capturing {
				break
			}
		}
		if capturing {
			out.WriteString(line + "\n")
		}
	}
	if out.Len() == 0 {
		return guide
	}
	return strings.TrimRight(out.String(), "\n") + "\n"
}
