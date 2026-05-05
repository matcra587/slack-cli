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
		Steps:       []string{"Generate/import a manifest template when needed", "Use local OAuth PKCE or safe token auth", "Store credentials in keychain or 1Password, never TOML plaintext or argv", "Use profile names case-insensitively", "Verify with auth status before posting"},
	},
	{
		Name:        "config_prefs",
		Description: "Set profile preferences without touching auth",
		Steps:       []string{"Use config init for preferences only", "Use auth commands for credentials", "Set default channel and attribution text with config set", "Do not write plaintext tokens to TOML", "Remember env overrides beat config values"},
	},
	{
		Name:        "core_contract",
		Description: "Understand output modes, stderr/stdout, and fixed exit codes",
		Steps:       []string{"Use JSON for automation and rich output for humans", "Treat stdout as command data only", "Treat stderr as diagnostics and structured errors", "Choose exactly one of --json, --plain, --compact, or --raw", "Parse fixed exit codes and error types on failure"},
	},
	{
		Name:        "send_msg",
		Description: "Send a markdown message and read ts/permalink from JSON",
		Steps:       []string{"Choose workspace/profile", "Pass message body with --message or --file -", "Use --channel or --user, never both", "Use --blocks only for raw Block Kit input", "Dry-run first for high-visibility sends", "Read JSON response for ts and permalink"},
	},
	{
		Name:        "react",
		Description: "Add, remove, and list emoji reactions by channel and timestamp",
		Steps:       []string{"Use channel ID and message timestamp", "Pass emoji as :name: or name", "Use add/remove/list for the desired action", "Use --dry-run before add/remove in live channels", "Read JSON response for target and reaction details"},
	},
	{
		Name:        "reply",
		Description: "Reply to a message thread by parent timestamp",
		Steps:       []string{"Use channel ID and parent message timestamp", "Post with reply --parent", "Use --file - for multiline replies", "Use --blocks only for raw Block Kit replies", "Read JSON response for reply ts and thread_ts"},
	},
	{
		Name:        "read_history",
		Description: "Read channel history or thread replies with bounded pagination",
		Steps:       []string{"Use history list for parent messages", "Use --thread for replies", "Bound output with --max-items and time filters", "Parse next cursors from meta.pagination", "Use JSON for full metadata and plain only for humans"},
	},
	{
		Name:        "search_msgs",
		Description: "Probationary hidden workspace message search workflow",
		Steps:       []string{"Status: probationary and not promoted; command entries are hidden from help and shell completion, while agent schema/workflow guidance may mention this workflow with that status", "Search with structured Slack query text", "Bound output with --max-items", "Use JSON for full text and metadata"},
	},
	{
		Name:        "upload_file",
		Description: "Probationary hidden file upload workflow",
		Steps:       []string{"Status: probationary and not promoted; command entries are hidden from help and shell completion, while agent schema/workflow guidance may mention this workflow with that status", "Use file upload with --file path or --file -", "Provide --filename for stdin uploads", "Read JSON response for file permalink"},
	},
	{
		Name:        "edit_msg",
		Description: "Edit own messages by exact channel and timestamp",
		Steps:       []string{"Use channel ID and exact message timestamp", "Edit only own messages", "Preview high-impact edits with --dry-run", "Use --blocks only for raw Block Kit replacements"},
	},
	{
		Name:        "delete_msg",
		Description: "Delete own messages with dry-run and force safeguards",
		Steps:       []string{"Use channel ID and exact message timestamp", "Preview destructive deletes with --dry-run", "Require --force for deletion", "Prefer edit when thread context should be preserved"},
	},
	{
		Name:        "discover_destination",
		Description: "Find channel and DM IDs before posting",
		Steps:       []string{"Use lookup channel for channels and DM conversations", "Use --types im for existing DMs", "Use plain table output for humans and JSON for agents", "Inspect membership and archived state before posting", "Prefer stable channel or DM IDs"},
	},
	{
		Name:        "inspect_schema",
		Description: "Read the machine schema and workflow guide",
		Steps:       []string{"Use agent schema, not the removed root schema alias", "Use --compact for a smaller command tree", "Use agent guide <workflow> for task instructions"},
	},
	{
		Name:        "lookup_user",
		Description: "Find user IDs, presence, status, and timezone",
		Steps:       []string{"Use lookup user to list users or inspect one user", "Fetch presence, status, and timezone when needed", "Prefer stable user IDs", "Use --presence only when the token has presence visibility"},
	},
	{
		Name:        "send_dm",
		Description: "Send direct messages while handling token limits",
		Steps:       []string{"Use message send --user with a user ID", "Slack decides whether the active bot-token or user-token profile may open the DM", "Handle structured errors where Slack rejects the target", "Use a user-token profile for DM-anyone workflows when bot-token limits get in the way"},
	},
	{
		Name:        "safe_mutation",
		Description: "Preview high-impact changes and parse JSON results",
		Steps:       []string{"Use --dry-run before high-impact mutations", "Keep JSON output for machine parsing", "Treat deletes as destructive operations", "Do not parse rich/plain output in automation", "Structured errors are on stderr with fixed exit codes"},
	},
}

const guide = `# Slack CLI Agent Guide

Read this before posting. ` + "`slack agent schema --compact`" + ` has the
machine command tree; this guide has the operational contracts and gotchas that
agents usually miss.

## core_contract
- Default mode is agent-first JSON in non-TTY or detected agent mode. A real TTY gets rich human output.
- stdout is command data only. stderr is diagnostics, progress, warnings, and structured errors.
- ` + "`--json`" + `, ` + "`--plain`" + `, ` + "`--compact`" + `, and ` + "`--raw`" + ` are mutually exclusive.
- ` + "`--compact`" + ` strips the envelope on success. Use it when a caller wants command-specific JSON only.
- ` + "`--raw`" + ` is an output mode. It does not mean raw Block Kit input.
- Use command-local ` + "`--blocks`" + ` when message-like input is already Slack Block Kit JSON.
- Exit codes: auth ` + "`1`" + `, not found ` + "`2`" + `, rate limit ` + "`3`" + `, validation ` + "`4`" + `, server ` + "`5`" + `.
- Parse ` + "`errors[0].type`" + ` and ` + "`errors[0].exit_code`" + ` from stderr in JSON mode when a command fails.

## auth_setup
- Fast preflight:
  - ` + "`slack auth status --json`" + ` confirms profile validity.
  - ` + "`slack manifest template --preset messaging --type user --name <app-name>`" + ` prints an importable Slack app manifest.
  - ` + "`slack manifest template --preset readonly --type user --name <app-name>`" + ` is safer for read-only installs.
- Use ` + "`slack manifest template --preset messaging --type user --name <app-name>`" + ` to print an importable Slack app manifest. The CLI does not create Slack apps.
- Use one callback port for manifest generation and login. Set ` + "`SLACK_CLI_CALLBACK_PORT=<port>`" + `, or pass ` + "`slack manifest template --callback-port <port>`" + ` and ` + "`slack auth login --oauth-callback-port <port>`" + `.
- Without an explicit port, ` + "`auth login`" + ` listens on an OS-assigned local port. Slack still requires the redirect URL in the app to match the login URL exactly.
- For local OAuth, run ` + "`slack auth login`" + `, choose Slack OAuth, and paste the app's client ID. Local OAuth uses PKCE and does not need a client secret.
- OAuth derives workspace ID and display name from Slack after authorization.
- Token auth is supported with ` + "`--token-stdin`" + `, ` + "`--token-file <path>`" + `, or ` + "`--token-env <VAR>`" + `. ` + "`--token-env`" + ` takes an environment variable name, not a token value.
- Runtime token overrides resolve in this order: ` + "`SLACK_CLI_TOKEN_<PROFILE>`" + `, then ` + "`SLACK_CLI_TOKEN`" + `, then the configured keychain or 1Password reference. Profile env suffixes are uppercase with non-alphanumerics replaced by underscores.
- Credential material is stored as a structured keychain secret or read from a configured secret backend such as ` + "`op://...`" + `. Never put plaintext ` + "`xox*`" + ` tokens in TOML.
- Profile names are case-insensitive. ` + "`Default`" + ` and ` + "`default`" + ` refer to one profile; auth merges into the existing spelling.
- Use ` + "`slack auth status`" + ` to confirm the profile is valid before sending.
- If OAuth returns ` + "`bad_client_secret`" + `, the Slack app is treating the flow as client-secret OAuth. Enable PKCE in the app manifest or import a manifest generated by this CLI.
- If Slack rejects the redirect URL, copy the exact local callback URL printed during login into the Slack app OAuth redirect URLs. Random callback ports must match exactly.
- Do not pass tokens in argv. They show up in process listings. Use ` + "`--token-stdin`" + `, ` + "`--token-file`" + `, or ` + "`--token-env`" + `.

## config_prefs
- Use ` + "`slack config init`" + ` to create profile preferences. It does not ask for tokens, token type, workspace ID, or workspace display name.
- Use auth commands for credentials: ` + "`slack auth login`" + `, ` + "`slack auth status`" + `, ` + "`slack auth switch`" + `, and ` + "`slack auth logout`" + `.
- Use ` + "`slack config set workspaces.<profile>.default_channel <channel-id>`" + ` to set a default channel.
- Use ` + "`slack config set workspaces.<profile>.attribution.message <text>`" + ` and ` + "`slack config set workspaces.<profile>.attribution.emoji <emoji>`" + ` to customize attribution.
- Auth-owned fields may appear in TOML as keychain or secret references, but config commands do not edit them.
- If a profile has preferences but no credential reference, Slack API commands fail with an auth error. Run ` + "`slack auth login`" + ` or switch to an authenticated profile.
- Configuration precedence is environment, then config file, then defaults.
- Use ` + "`slack config path`" + ` to inspect which TOML file is active.
- Use ` + "`slack config list --json`" + ` to inspect effective profile preferences without exposing credential material.
- Profile-scoped runtime tokens use ` + "`SLACK_CLI_TOKEN_<PROFILE>`" + ` with uppercase names and non-alphanumeric characters replaced by underscores.
- ` + "`config init`" + ` is safe to rerun with ` + "`--force`" + ` only when you intend to replace profile preferences; it does not rotate credentials.

## send_msg
- Use ` + "`slack message send --channel <channel-id-or-alias> --message <markdown>`" + ` for short messages.
- Use ` + "`slack message send --channel <channel-id-or-alias> --file -`" + ` for multiline bodies from stdin.
- Use ` + "`slack message send --user <user-id> --message <markdown>`" + ` for DMs. There is no ` + "`slack dm`" + ` command.
- ` + "`--channel`" + ` and ` + "`--user`" + ` are mutually exclusive. Passing neither is a validation error.
- Use ` + "`--blocks`" + ` only when ` + "`--message`" + ` or ` + "`--file`" + ` content is already a raw Block Kit JSON array.
- ` + "`--blocks`" + ` validates Slack Block Kit JSON rules before any Slack mutation, including required fields and supported limits.
- Expect JSON by default in agent or non-TTY mode. In ` + "`--plain`" + ` mode this is human output only; do not parse it in automation.
- Read ` + "`data.message.ts`" + ` and ` + "`data.permalink`" + ` to confirm delivery. Slack timestamps are channel-scoped.
- Markdown is converted to Block Kit by default. ` + "`--raw`" + ` is output-only; it does not select raw Block Kit input.
- Unsupported block-level Markdown preserves original source text in readable Block Kit sections instead of being dropped.
- Agent attribution is added when agent mode is detected by env vars or ` + "`--agent`" + `. Common triggers include ` + "`CLAUDE_CODE`" + `, ` + "`CLAUDECODE`" + `, ` + "`CURSOR_TERMINAL`" + `, ` + "`CODEX`" + `, ` + "`GITHUB_ACTIONS`" + `, and ` + "`CI`" + `.
- False-like values such as ` + "`0`" + `, ` + "`false`" + `, and ` + "`no`" + ` do not enable agent mode.
- Attribution is allowed by default but can be explicitly disabled with ` + "`--no-agent-attribution`" + ` or ` + "`attribution.enabled = false`" + ` in the active profile.
- ` + "`attribution.enabled = true`" + ` forces attribution even without an agent env var. Customize text with ` + "`attribution.message`" + ` or ` + "`--agent-message`" + `, and emoji with ` + "`attribution.emoji`" + ` or ` + "`--agent-emoji`" + `.
- Block Kit context blocks carry the readable attribution.
- Use ` + "`--dry-run`" + ` before high-visibility sends.
- Markdown paragraph line breaks are preserved in Block Kit section text. If you need exact multi-line content, prefer ` + "`--file -`" + ` and inspect the dry-run JSON first.
- Slack timestamps are strings such as ` + "`1746284582.123456`" + `. Keep them as strings; do not parse them as floats.
- ` + "`missing_scope`" + `, ` + "`not_in_channel`" + `, and ` + "`no_permission`" + ` map to structured auth failures. Do not retry blindly without changing scopes or destination.
- Success path fields to keep: ` + "`data.message.channel`" + `, ` + "`data.message.ts`" + `, ` + "`data.message.thread_ts`" + ` for replies, and ` + "`data.permalink`" + ` when present.

## react
- Use ` + "`slack react add --channel <channel-id> --timestamp <message-ts> --emoji :thumbsup:`" + ` to react.
- Use ` + "`slack react remove --channel <channel-id> --timestamp <message-ts> --emoji :thumbsup:`" + ` to remove a reaction.
- Use ` + "`slack react list --channel <channel-id> --timestamp <message-ts>`" + ` to inspect reactions.
- Timestamps are Slack message timestamps such as ` + "`1746284582.123456`" + ` and are scoped to the channel.
- Emoji may be passed as ` + "`thumbsup`" + ` or ` + "`:thumbsup:`" + `.
- Use ` + "`--dry-run`" + ` for add/remove before touching a live message.
- JSON confirmation is under ` + "`data.reaction`" + ` and ` + "`data.target`" + `.
- Command metadata uses ` + "`react.add`" + `, ` + "`react.remove`" + `, and ` + "`react.list`" + `.

## reply
- Use ` + "`slack reply --channel <channel-id> --parent <parent-message-ts> --message <markdown>`" + ` to answer in a thread.
- The ` + "`--parent`" + ` value is the parent message timestamp, not a permalink or search result index.
- Read ` + "`data.message.thread_ts`" + ` and ` + "`data.message.ts`" + ` from JSON output to confirm nesting.
- Use ` + "`--file -`" + ` for multiline thread replies from stdin.
- Use ` + "`--blocks`" + ` when the thread reply body is already raw Block Kit JSON.
- Use ` + "`history list --channel <id> --thread <parent-ts>`" + ` to read back replies after posting.
- ` + "`--dry-run`" + ` validates the local payload and returns ` + "`thread_ts`" + ` without calling Slack.
- Command metadata uses ` + "`reply`" + `.

## read_history
- Use ` + "`slack history list --channel <channel-id> --max-items <n>`" + ` for parent messages.
- Use ` + "`slack history list --channel <channel-id> --thread <parent-ts> --max-items <n>`" + ` for thread replies.
- Use ` + "`--since`" + `, ` + "`--until`" + `, and ` + "`--user`" + ` to filter.
- Parent history includes reply counts and fetches full thread replies only when ` + "`--thread`" + ` or bounded ` + "`--include-replies`" + ` is used.
- Plain mode renders history as a table for humans. JSON mode preserves the envelope and full metadata for agents.
- Bound every read with ` + "`--max-items`" + ` in automation.
- Pagination appears under ` + "`meta.pagination`" + `. Keep ` + "`cursor`" + ` and ` + "`has_more`" + ` if you need another page.
- Use ` + "`--since`" + ` and ` + "`--until`" + ` with Slack timestamp strings, not local time strings, unless the command help explicitly says otherwise.
- JSON messages include ` + "`text`" + `, ` + "`blocks`" + ` when available, metadata such as ` + "`reply_count`" + `, and ` + "`permalink`" + ` when fetched.
- Do not assume history text preserves display formatting exactly; use ` + "`blocks`" + ` when you need Slack-native structure.

## search_msgs
- Status: probationary and not promoted. Command entries are hidden from help and shell completion; agent schema/workflow guidance may mention this workflow with that status. Use only when explicitly testing this workflow.
- Use ` + "`slack lookup messages --query <query> --max-items <n>`" + ` to search workspace messages.
- JSON output includes full text and metadata. Plain output truncates snippets for humans.
- Use ` + "`--full`" + ` only when human plain output really needs the complete text.
- This requires Slack ` + "`search:read`" + `. Without it, expect a structured auth failure.
- Keep search queries narrow and bounded. Search can be slower and more rate-limited than channel history.
- Do not build automations on plain snippets; parse JSON result text and permalinks.

## upload_file
- Status: probationary and not promoted. Command entries are hidden from help and shell completion; agent schema/workflow guidance may mention this workflow with that status. Use only when explicitly testing this workflow.
- Use ` + "`slack file upload --channel <channel-id> --file <path>`" + ` for files on disk.
- Use ` + "`slack file upload --channel <channel-id> --file - --filename <name>`" + ` for piped artifacts.
- Use ` + "`--message`" + ` for an upload comment; markdown is converted to Block Kit and attribution is appended when agent mode is active.
- Use ` + "`--blocks --message <json>`" + ` only for a raw Block Kit upload comment; it does not affect uploaded file bytes.
- Read ` + "`data.file.permalink`" + ` when Slack returns file permalink metadata.
- Upload progress and diagnostics go to stderr. stdout remains command data.
- Stdin upload requires ` + "`--filename`" + `. Without it, validation fails before calling Slack.
- Directory paths and missing paths fail locally before Slack upload endpoints.
- ` + "`files:write`" + ` is required for real uploads. Use ` + "`--dry-run`" + ` to verify payload shape without the scope.
- Promotion is blocked until live file-upload smoke and docs/help/schema exposure are deliberately completed.

## edit_msg
- Use ` + "`slack message edit --channel <channel-id> --timestamp <message-ts> --message <markdown>`" + ` to correct own messages.
- Slack only allows editing own messages where token scopes permit it.
- Use the exact ` + "`--timestamp`" + ` returned by send, history, or search JSON.
- Use ` + "`--blocks`" + ` when the replacement content is raw Block Kit JSON.
- Use ` + "`--dry-run`" + ` before editing messages in high-visibility channels.
- The message timestamp is unique only inside a channel. Always pass both channel and timestamp.
- Prefer dry-run plus history readback before editing incident or release channels.
- If Slack rejects the edit as not-owned or not permitted, return the structured error to the caller instead of trying delete/send fallback.

## delete_msg
- Use ` + "`slack message delete --channel <channel-id> --timestamp <message-ts> --force`" + ` to delete own messages.
- Run with ` + "`--dry-run`" + ` first to preview destructive changes.
- Delete targets are scoped by channel plus Slack timestamp.
- Prefer editing over deleting when preserving thread context matters.
- ` + "`--force`" + ` is required for real delete. ` + "`--dry-run --force`" + ` is safe and should not call Slack mutation endpoints.
- Never delete by “last message” or search-result index. This CLI intentionally requires exact channel and timestamp.

## discover_destination
- Use ` + "`slack lookup channel --max-items <n>`" + ` to discover public and private channel destinations.
- Use ` + "`slack lookup channel --types im --max-items <n>`" + ` to discover existing DM conversations.
- In automation, prefer IDs such as ` + "`C123...`" + ` and ` + "`D123...`" + ` over display names.
- Use ` + "`slack lookup channel --channel <channel-id>`" + ` before posting to unfamiliar channels.
- plain mode renders tables for list commands. Agents should keep JSON output and parse IDs from ` + "`data.channels`" + `.
- Check ` + "`is_member`" + ` and ` + "`is_archived`" + ` before posting. Archived channels and channels you are not in will fail at Slack.
- ` + "`--types all`" + ` includes public channels, private channels, IMs, and MPIMs. Use narrower types when you know the target class.
- Human names can change. Cache IDs, not names.

## inspect_schema
- Use ` + "`slack agent schema`" + ` for the full command tree, flags, output schema notes, env triggers, exit codes, and workflow metadata.
- Use ` + "`slack agent schema --compact`" + ` when a smaller nested command tree is enough.
- The old root ` + "`slack schema`" + ` alias is intentionally removed; schema discovery lives under ` + "`agent schema`" + `.
- Use ` + "`slack agent guide`" + ` to list workflows and ` + "`slack agent guide <workflow>`" + ` for task-specific instructions.
- ` + "`schema.Commands`" + ` excludes hidden probationary commands. ` + "`schema.Workflows`" + ` may mention probationary workflows with status text.
- Treat ` + "`schema.Examples`" + ` as examples, not guaranteed-safe invocations. Apply ` + "`--dry-run`" + ` to mutations first.
- If a guide section conflicts with ` + "`agent schema`" + `, trust the schema for flags and command existence, then file a bug against the guide.

## lookup_user
- Use ` + "`slack lookup user --max-items <n>`" + ` to find candidate users.
- Use ` + "`slack lookup user --user <user-id>`" + ` to fetch profile, presence, custom status, and timezone.
- Prefer user IDs such as ` + "`U123...`" + ` in commands.
- Check timezone before paging or scheduling humans.
- Use ` + "`--filter`" + ` for local narrowing when listing, and ` + "`--user`" + ` for exact lookup.
- Presence and custom status depend on token scopes and Slack workspace policy. Missing fields are not always an error.
- Parse ` + "`data.users[].id`" + `, ` + "`data.users[].tz`" + `, and ` + "`data.users[].presence`" + ` when scheduling or mentioning humans.

## send_dm
- Use ` + "`slack message send --user <user-id> --message <markdown>`" + ` for direct messages.
- ` + "`message send --user`" + ` opens the DM through Slack before posting. Slack decides whether a bot-token or user-token profile can open the requested DM.
- If Slack rejects a bot-token DM attempt, the CLI returns a structured error. Use a user-token profile for DM-anyone workflows where bot-token behavior cannot satisfy the request.
- Scope validation is best-effort when token metadata is available; Slack permission errors such as ` + "`missing_scope`" + `, ` + "`not_in_channel`" + `, and ` + "`no_permission`" + ` map to the fixed exit-code contract.
- Use ` + "`--blocks`" + ` only when the DM body is raw Block Kit JSON.
- Read ` + "`data.message.channel`" + ` and ` + "`data.message.ts`" + ` from JSON output.
- User-token profiles are the normal choice for “DM anyone as me” workflows. Bot-token profiles depend on app install and conversation access.
- Use ` + "`--dry-run`" + ` to verify message composition, but remember it cannot prove Slack will open the DM.

## safe_mutation
- Use ` + "`--dry-run`" + ` before send, reply, edit, delete, react, and file upload mutations when the target is high-impact.
- Treat delete as destructive; require an exact channel and timestamp.
- Keep JSON output for automation and parse explicit IDs, timestamps, and permalinks.
- Use ` + "`--plain`" + ` only for human inspection, never for machine parsing.
- All user-facing output should pass through clog. stdout is data; stderr is diagnostics, progress, warnings, and structured errors.
- For real writes, record the returned channel, timestamp, permalink, and dry-run status in the calling agent's evidence.
- If a command fails, do not scrape human text. Parse stderr JSON for ` + "`type`" + `, ` + "`message`" + `, and ` + "`exit_code`" + `.
- Retry rate limits only after respecting Slack's retry guidance. The CLI maps rate-limit failures to exit code ` + "`3`" + `.
- Before changing a high-traffic channel, run a read-only lookup and a dry-run mutation in the same profile.
`

func GetGuide() string {
	return guide
}

func WorkflowCatalog() []GuideWorkflow {
	workflows := append([]GuideWorkflow(nil), guideWorkflows...)
	slices.SortFunc(workflows, func(a, b GuideWorkflow) int {
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
