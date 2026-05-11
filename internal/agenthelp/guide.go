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
		Steps:       []string{"Use channel ID and message timestamp", "Pass one emoji as :name: or name, or comma-separate/repeat --emoji to apply multiple in order", "Use add/remove/list for the desired action", "Use --dry-run before add/remove in live channels", "Read JSON response for data.mutations[] and data.target"},
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
		Description: "Workspace message search workflow",
		Steps:       []string{"Use lookup messages with structured Slack query text", "Requires a user token with search:read; bot-token profiles cannot use search.messages", "Bound output with --max-items", "Use JSON for full text and metadata"},
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
		Steps:       []string{"Use agent schema, not the removed root schema alias", "Use --compact for a smaller command tree", "Use agent guide <workflow> to load the task runbook"},
	},
	{
		Name:        "lookup_user",
		Description: "Find user IDs, presence, status, and timezone",
		Steps:       []string{"Use lookup user to list users or inspect one user", "Fetch presence, status, and timezone when needed", "Prefer stable user IDs", "Use --presence only when the token has presence visibility"},
	},
	{
		Name:        "cache_metadata",
		Description: "Prime users and channels for repeated lookup and shell completion",
		Steps:       []string{"Run cache users and cache channels once per work session", "Use --refresh when Slack membership changed", "Use --ttl-minutes to choose daily or weekly refresh windows", "Clear stale resources with cache clear users or cache clear channels"},
	},
	{
		Name:        "send_dm",
		Description: "Send direct messages while handling token limits",
		Steps:       []string{"Use message send --user with user IDs, aliases, or email addresses", "Repeat --user or comma-separate values for group DMs", "Slack decides whether the active bot-token or user-token profile may open the DM", "Handle structured errors where Slack rejects the target", "Use a user-token profile for DM-anyone workflows when bot-token limits get in the way"},
	},
	{
		Name:        "set_status",
		Description: "Set or clear the authenticated user's Slack status",
		Steps:       []string{"Use status set with text, emoji, and optional expiration", "Use status clear to remove status", "Requires a user token with users.profile:write", "Use --dry-run before mutating during tests", "Keep JSON output for automation"},
	},
	{
		Name:        "safe_mutation",
		Description: "Preview high-impact changes and parse JSON results",
		Steps:       []string{"Use --dry-run before high-impact mutations", "Keep JSON output for machine parsing", "Treat deletes as destructive operations", "Do not parse rich/plain output in automation", "Structured errors are on stderr with fixed exit codes"},
	},
	{
		Name:        "developer_review",
		Description: "Runbook for posting a review-style message with reactions and a thread",
		Steps:       []string{"Collect channel ID, review subject, decision, and optional run ID", "Send one human-readable parent message from stdin", "Parse parent channel, timestamp, and permalink", "Add reactions by channel and timestamp", "Reply with follow-up detail and verify with react list plus thread history"},
	},
	{
		Name:        "cleanup_msgs",
		Description: "Runbook for cleaning test messages found through paginated search",
		Steps:       []string{"Collect exact run IDs", "Search every page with lookup messages and next_cursor", "Delete by exact channel and timestamp only", "Use controlled parallelism with rate-limit retries", "Repeat search and delete until paginated search returns zero matches"},
	},
}

const guide = `# Slack CLI Agent Guide

Use this as the operational runbook source. ` + "`slick agent schema --compact`" + ` has
the machine command tree; ` + "`slick agent guide <workflow>`" + ` tells an agent how to
perform the task, what to parse, and which quirks matter.

## core_contract
- Runbook: apply this before any workflow when deciding output mode, parsing, and error handling.
- Inputs: caller intent, whether output is for a human or automation, and the active command.
- Command sequence: use JSON for automation; use rich or plain output only for human inspection.
- Output modes: ` + "`--json`" + `, ` + "`--plain`" + `, ` + "`--compact`" + `, and ` + "`--raw`" + ` are mutually exclusive.
- Parse: stdout is command data only. stderr is diagnostics, progress, warnings, and structured errors.
- Parse: failures in JSON mode put ` + "`errors[0].type`" + `, ` + "`errors[0].message`" + `, and ` + "`errors[0].exit_code`" + ` on stderr.
- Exit codes: auth ` + "`1`" + `, not found ` + "`2`" + `, rate limit ` + "`3`" + `, validation ` + "`4`" + `, server ` + "`5`" + `, canceled ` + "`6`" + `, timeout ` + "`7`" + `.
- Quirks: ` + "`--compact`" + ` strips the envelope on success; use it only when the caller wants command-specific JSON.
- Quirks: ` + "`--raw`" + ` is output-only. It does not select raw Block Kit input; use command-local ` + "`--blocks`" + ` for that.

## auth_setup
- Runbook: use this before first Slack API use, after token/profile errors, or when preparing a manifest for install.
- Inputs: profile name, auth method, token type, app name, callback port if OAuth must be stable.
- Preflight: run ` + "`slick auth status --json`" + ` to see whether the active profile is already valid.
- Manifest: run ` + "`slick manifest template --preset messaging --type user --name <app-name>`" + ` for a messaging user-token app.
- Manifest: run ` + "`slick manifest template --preset readonly --type user --name <app-name>`" + ` for a safer read-only app.
- OAuth command: run ` + "`slick auth login --workspace <profile> --method oauth --oauth-client-id <id>`" + `. Local OAuth uses PKCE and does not need a client secret.
- Token command: pass token material with ` + "`--token-stdin`" + `, ` + "`--token-file <path>`" + `, or ` + "`--token-env <VAR>`" + `. ` + "`--token-env`" + ` takes an environment variable name, not a token value.
- Parse: auth status should confirm workspace ID, token type, and secret reference without exposing credential material.
- Storage: credentials live in keychain or a configured secret backend such as ` + "`op://...`" + `. Never put plaintext ` + "`xox*`" + ` tokens in TOML or argv.
- Runtime token override: ` + "`SLACK_CLI_TOKEN_<PROFILE>`" + ` beats ` + "`SLACK_CLI_TOKEN`" + ` and configured secret references for that profile.
- Quirks: profile names are case-insensitive. ` + "`Default`" + ` and ` + "`default`" + ` refer to the same profile.
- Quirks: if OAuth returns ` + "`bad_client_secret`" + `, import a CLI-generated manifest or enable PKCE in Slack.
- Quirks: if Slack rejects the redirect URL, copy the exact local callback URL printed during login. Random callback ports must match exactly.

## config_prefs
- Runbook: use this to change profile preferences, not credentials.
- Inputs: profile name, preference key, desired value, and whether replacement is intentional.
- Create preferences: run ` + "`slick config init`" + `. It does not ask for tokens, token type, workspace ID, or workspace display name.
- Missing config: if any command reports ` + "`config file not found`" + `, run ` + "`slick config init`" + ` before retrying.
- Set default channel: run ` + "`slick config set workspaces.<profile>.default_channel <channel-id>`" + `.
- Set attribution text: run ` + "`slick config set workspaces.<profile>.attribution.message <text>`" + `.
- Set attribution emoji: run ` + "`slick config set workspaces.<profile>.attribution.emoji <emoji>`" + `.
- Inspect config path: run ` + "`slick config path`" + `.
- Inspect effective preferences: run ` + "`slick config list --json`" + `.
- Parse: config JSON shows effective preferences but must not expose credential material.
- Quirks: auth commands own credentials: ` + "`slick auth login`" + `, ` + "`slick auth status`" + `, ` + "`slick auth switch`" + `, and ` + "`slick auth logout`" + `.
- Quirks: default channel is the fallback for ` + "`slick message send`" + ` when neither ` + "`--channel`" + ` nor ` + "`--user`" + ` is passed.
- Quirks: config path uses XDG config home by default, including on macOS. Default is ` + "`~/.config/slick/config.toml`" + `. ` + "`SLICK_CONFIG`" + ` overrides it; legacy ` + "`SLACK_CLI_CONFIG`" + ` still works. Path inputs expand ` + "`~`" + ` and environment variables.
- Quirks: configuration precedence is environment, then config file, then defaults.
- Quirks: profile-scoped runtime tokens use ` + "`SLACK_CLI_TOKEN_<PROFILE>`" + ` with uppercase names and non-alphanumeric characters replaced by underscores.

## send_msg
- Runbook: use this to send a channel message or DM and capture the Slack timestamp/permalink.
- Inputs: target channel ID, user ID, user email, or configured alias; markdown body or Block Kit JSON; workspace/profile; and whether attribution should be enabled.
- Channel command: ` + "`slick message send --channel <channel-id-or-alias> --message <markdown> --json`" + `.
- Stdin command: ` + "`printf '%s\n' \"$body\" | slick message send --channel <channel-id-or-alias> --file - --json`" + `.
- Multiline command: use real stdin, such as a heredoc piped to ` + "`slick message send --channel <id> --file - --json`" + `; do not type literal ` + "`\\n`" + ` when you need a visible newline.
- DM command: ` + "`slick message send --user <user-id-or-email> --message <markdown> --json`" + `. There is no ` + "`slick dm`" + ` command.
- Group DM command: repeat ` + "`--user`" + ` or comma-separate values, such as ` + "`slick message send --user alice@example.com,bob@example.com --user U123 --message <markdown> --json`" + `.
- Raw Block Kit command: pass ` + "`--blocks`" + ` only when the body is a raw Block Kit JSON array. ` + "`--blocks`" + ` validates Slack Block Kit JSON rules before any Slack mutation.
- Parse: keep ` + "`data.message.channel`" + `, ` + "`data.message.ts`" + `, ` + "`data.message.thread_ts`" + ` for replies, and ` + "`data.permalink`" + ` when present.
- Agent attribution: agent mode can be triggered by env vars or ` + "`--agent`" + `. Configure with ` + "`attribution.enabled`" + `, ` + "`attribution.message`" + `, ` + "`attribution.emoji`" + `, ` + "`--agent-message`" + `, and ` + "`--agent-emoji`" + `.
- Quirks: ` + "`--channel`" + ` and ` + "`--user`" + ` are mutually exclusive. Passing neither uses configured ` + "`default_channel`" + ` when present; otherwise it is a validation error.
- Quirks: email DM targeting calls Slack ` + "`users.lookupByEmail`" + ` and requires ` + "`users:read.email`" + `.
- Quirks: Markdown is converted to Block Kit by default. ` + "`--raw`" + ` is output-only and does not select raw Block Kit input.
- Quirks: Unsupported block-level Markdown preserves original source text in readable Block Kit sections.
- Quirks: Do not repeat attribution text in the message body. If attribution is enabled, Slack shows it in the context block below the message.
- Quirks: for live workflow tests, send realistic content such as a PR review, incident update, or release note; synthetic marker text can hide UI issues.
- Quirks: Slack timestamps are strings such as ` + "`1746284582.123456`" + `. Keep them as strings.
- Error handling: ` + "`missing_scope`" + ` and ` + "`no_permission`" + ` map to structured auth failures. ` + "`not_in_channel`" + ` maps to ` + "`not_found`" + `; change destination, membership, or app access before retrying.

## react
- Runbook: use this to add, remove, or inspect emoji reactions on a known message.
- Inputs: channel ID, message timestamp, one or more emoji names, desired action.
- Add command: ` + "`slick react add --channel <channel-id> --timestamp <message-ts> --emoji :thumbsup: --json`" + `.
- Multi-emoji command: ` + "`slick react add --channel <channel-id> --timestamp <message-ts> --emoji thumbsup,white_check_mark,rocket --json`" + ` applies the emojis in input order. Repeat ` + "`--emoji`" + ` instead of comma-separating if a value contains a comma.
- Remove command: ` + "`slick react remove --channel <channel-id> --timestamp <message-ts> --emoji :thumbsup: --json`" + `.
- List command: ` + "`slick react list --channel <channel-id> --timestamp <message-ts> --json`" + `.
- Parse: ` + "`data.mutations[]`" + ` lists ` + "`{channel, ts, emoji, dry_run}`" + ` for each emoji applied (length 1 for the single-emoji case, length N for ordered multi-emoji); ` + "`data.target.channel`" + ` and ` + "`data.target.ts`" + ` identify the target. In JSON, ` + "`meta.command`" + ` (` + "`react.add`" + ` vs ` + "`react.remove`" + `) conveys which side was applied. List output uses ` + "`data.reactions[]`" + ` with reaction names, counts, and users.
- Quirks: timestamps are channel-scoped Slack strings such as ` + "`1746284582.123456`" + `.
- Quirks: emoji may be passed as ` + "`thumbsup`" + ` or ` + "`:thumbsup:`" + `.
- Quirks: ordered multi-emoji halts on the first failure; ` + "`data.mutations[]`" + ` will be absent on the error envelope, so retry the remaining emojis from a known-good state rather than assuming partial success.
- Quirks: use ` + "`--dry-run`" + ` for add/remove before touching a live message.
- Verification: after add/remove, run ` + "`slick react list`" + ` on the same channel and timestamp.
- Command metadata uses ` + "`react.add`" + `, ` + "`react.remove`" + `, and ` + "`react.list`" + `.

## reply
- Runbook: use this to post or verify a thread reply.
- Inputs: channel ID, parent message timestamp, reply body, optional raw Block Kit JSON.
- Message command: ` + "`slick reply --channel <channel-id> --parent <parent-message-ts> --message <markdown> --json`" + `.
- Stdin command: ` + "`printf '%s\n' \"$reply\" | slick reply --channel <channel-id> --parent <parent-message-ts> --file - --json`" + `.
- Raw Block Kit command: add ` + "`--blocks`" + ` only when the reply body is raw Block Kit JSON.
- Parse: keep ` + "`data.message.thread_ts`" + ` and ` + "`data.message.ts`" + ` to confirm nesting.
- Verify: run ` + "`slick history list --channel <channel-id> --thread <parent-message-ts> --json`" + `.
- Quirks: ` + "`--parent`" + ` is the parent message timestamp, not a permalink or search result index.
- Quirks: ` + "`--dry-run`" + ` validates the local payload and returns ` + "`thread_ts`" + ` without calling Slack.
- Command metadata uses ` + "`reply`" + `.

## read_history
- Runbook: Use history to discover message timestamps before reacting, editing, deleting, or replying.
- Inputs: channel ID, optional time range, optional user ID, optional thread timestamp.
- Parent history command: ` + "`slick history list --channel <channel-id> --max-items <n> --json`" + `.
- Thread command: ` + "`slick history list --channel <channel-id> --thread <parent-ts> --max-items <n> --json`" + `.
- Filter command: add ` + "`--since <slack-ts>`" + `, ` + "`--until <slack-ts>`" + `, or ` + "`--user <user-id>`" + ` when narrowing results.
- Parse: use ` + "`data.messages[].ts`" + ` as the timestamp for ` + "`slick reply --parent`" + `, ` + "`slick react add --timestamp`" + `, ` + "`slick message edit --timestamp`" + `, or ` + "`slick message delete --timestamp`" + `.
- Parse: Use the parent message ` + "`ts`" + ` with ` + "`slick reply --parent`" + `.
- Parse: Use any message or reply ` + "`ts`" + ` with ` + "`slick react add --timestamp`" + `, ` + "`slick message edit --timestamp`" + `, or ` + "`slick message delete --timestamp`" + `.
- Parse: messages include ` + "`text`" + `, ` + "`blocks`" + ` when available, reply metadata, and ` + "`permalink`" + ` when fetched.
- Pagination: use ` + "`meta.pagination.cursor`" + ` and ` + "`meta.pagination.has_more`" + ` when another page is needed.
- Quirks: bound every read with ` + "`--max-items`" + ` in automation.
- Quirks: plain mode renders history as a table for humans. JSON mode preserves the envelope and metadata for agents.
- Quirks: history text may flatten display formatting; inspect ` + "`blocks`" + ` when Slack-native structure matters.

## search_msgs
- Runbook: use this for workspace-wide message search, especially cleanup by run ID.
- Inputs: exact query text, maximum page size, optional cursor from a previous page.
- Search command: ` + "`slick lookup messages --query <query> --max-items <n> --json`" + `.
- Pagination command: if ` + "`meta.pagination.has_more`" + ` is true, run ` + "`slick lookup messages --query <query> --max-items <n> --cursor <meta.pagination.next_cursor> --json`" + `.
- Parse: use JSON fields, not plain snippets. Result targets are under ` + "`data.matches[].channel.id`" + ` and ` + "`data.matches[].ts`" + `.
- Parse: JSON includes full text and metadata. Plain output truncates snippets for humans; ` + "`--full`" + ` only affects human plain output.
- Auth: this requires a Slack user token with ` + "`search:read`" + `. Bot-token profiles cannot use Slack Web API ` + "`search.messages`" + `.
- Manifest: search-capable manifests need ` + "`--type user`" + ` or ` + "`--type both`" + `; with both, ` + "`search:read`" + ` belongs under user scopes only.
- Quirks: search can be slower and more rate-limited than channel history.
- Quirks: for cleanup by run ID, collect every paginated search page before deleting. Deleting newer matches can reveal older matches, so repeat search and delete until paginated search returns zero matches.

## upload_file
- Runbook: use this only when explicitly testing the hidden file upload workflow.
- Status: probationary and not promoted. Command entries are hidden from help and shell completion; guide/schema may mention this workflow with that status.
- Inputs: channel ID, file path or stdin bytes, filename for stdin, optional comment body.
- File command: ` + "`slick file upload --channel <channel-id> --file <path> --json`" + `.
- Stdin command: ` + "`slick file upload --channel <channel-id> --file - --filename <name> --json`" + `.
- Comment command: add ` + "`--message <markdown>`" + ` for an upload comment.
- Raw comment command: add ` + "`--blocks --message <json>`" + ` only when the comment is raw Block Kit JSON; it does not affect uploaded file bytes.
- Parse: keep ` + "`data.file.permalink`" + ` when Slack returns it.
- Quirks: stdin upload requires ` + "`--filename`" + `. Missing files, directories, and missing stdin metadata fail locally before Slack upload endpoints.
- Quirks: upload progress and diagnostics go to stderr; stdout remains command data.
- Scope: real uploads require ` + "`files:write`" + `. Use ` + "`--dry-run`" + ` to verify payload shape without the scope.

## edit_msg
- Runbook: use this to correct one of the authenticated user's own messages.
- Inputs: channel ID, exact message timestamp, replacement markdown or raw Block Kit JSON.
- Markdown command: ` + "`slick message edit --channel <channel-id> --timestamp <message-ts> --message <markdown> --json`" + `.
- Stdin command: ` + "`printf '%s\n' \"$body\" | slick message edit --channel <channel-id> --timestamp <message-ts> --file - --json`" + `.
- Raw Block Kit command: add ` + "`--blocks`" + ` only when the replacement is raw Block Kit JSON.
- Parse: keep ` + "`data.message.channel`" + `, ` + "`data.message.ts`" + `, and ` + "`data.message.text`" + ` when present. Edit output does not include returned ` + "`data.message.blocks`" + `; verify rendered Block Kit through history or the Slack UI when needed.
- Quirks: Slack only allows editing own messages where token scopes permit it.
- Quirks: the timestamp is unique only inside a channel; always pass both channel and timestamp.
- Safety: use ` + "`--dry-run`" + ` before editing incident, release, or high-visibility channels.
- Error handling: if Slack rejects the edit as not-owned or not permitted, return the structured error. Do not try delete/send fallback.

## delete_msg
- Runbook: use this to delete one of the authenticated user's own messages.
- Inputs: channel ID and exact Slack message timestamp.
- Dry-run command: ` + "`slick message delete --channel <channel-id> --timestamp <message-ts> --dry-run --force --json`" + `.
- Delete command: ` + "`slick message delete --channel <channel-id> --timestamp <message-ts> --force --json`" + `.
- Parse: deletion success is scoped to the channel and timestamp pair.
- Quirks: ` + "`--force`" + ` is required for real delete. ` + "`--dry-run --force`" + ` is safe and should not call Slack mutation endpoints.
- Quirks: never delete by "last message" or search-result index. This CLI intentionally requires exact channel and timestamp.
- Quirks: prefer editing over deleting when preserving thread context matters.
- Bulk cleanup: build targets from JSON search results with exact ` + "`channel.id`" + ` and ` + "`ts`" + `. Use controlled parallelism, retry ` + "`rate_limit`" + ` errors after ` + "`retry_after_seconds`" + `, and treat ` + "`message_not_found`" + ` as already clean.

## developer_review
- Runbook: use this when asked to post a developer-style review, incident update, release note, or realistic live message workflow.
- Inputs: target channel ID or alias, review subject or URL, decision text, optional run ID, optional follow-up detail.
- Preflight: run ` + "`slick lookup channel --channel <channel-id-or-alias> --json`" + ` when the destination is unfamiliar; use ` + "`--dry-run`" + ` first in high-visibility channels.
- Compose: write one parent body in stdin. Use Slack mrkdwn naturally: bold section labels, bullet lists, numbered requested changes, inline code, links, and emoji.
- Send the parent: pipe real multiline stdin into ` + "`slick message send --channel <id> --file - --json`" + `; do not type literal ` + "`\\n`" + ` when a real newline is required.
- Parse and store: keep ` + "`data.message.channel`" + `, ` + "`data.message.ts`" + `, and ` + "`data.permalink`" + `. The timestamp is the parent key for reactions and replies.
- React: ` + "`slick react add --channel <id> --timestamp <parent-ts> --emoji eyes --json`" + `, ` + "`white_check_mark`" + `, or another natural review emoji.
- Reply: pipe real multiline stdin into ` + "`slick reply --channel <id> --parent <parent-ts> --file - --json`" + `.
- Verify: run ` + "`slick react list --channel <id> --timestamp <parent-ts> --json`" + ` and ` + "`slick history list --channel <id> --thread <parent-ts> --json`" + `.
- Quirks: agent attribution renders as a context block, so do not repeat the attribution phrase in the body.
- Quirks: Slack history text can flatten display formatting; use the posted UI or Block Kit fields when exact visual rendering matters.

## cleanup_msgs
- Runbook: use this after live tests that include a distinctive run ID in each message.
- Inputs: exact run IDs to remove, maximum page size, and a modest worker count for deletes.
- Discover: ` + "`slick lookup messages --query <run-id> --max-items <n> --json`" + `.
- Paginate: while ` + "`meta.pagination.has_more`" + ` is true, run ` + "`slick lookup messages --query <run-id> --max-items <n> --cursor <meta.pagination.next_cursor> --json`" + `.
- Build targets: use ` + "`data.matches[].channel.id`" + ` and ` + "`data.matches[].ts`" + ` only. Do not delete by snippet, visual order, or plain output.
- Delete: ` + "`slick message delete --channel <channel-id> --timestamp <ts> --force --json`" + `.
- Retry: on structured ` + "`rate_limit`" + ` errors, sleep for ` + "`errors[0].retry_after_seconds`" + ` before retrying that target.
- Missing target: treat ` + "`message_not_found`" + ` as already clean; another worker or earlier pass may have deleted it.
- Repeat: rerun paginated search and delete until every run ID returns zero matches.
- Quirks: a single ` + "`--max-items`" + ` page is not proof of cleanup; search result windows can shift as deletes happen.
- Quirks: separate CLI processes do not share proactive throttle state, so keep cleanup parallelism modest.

## discover_destination
- Runbook: use this before posting when the target channel, DM, or membership state is uncertain.
- Inputs: target name, channel ID, user ID, or desired conversation type.
- Channel list command: ` + "`slick lookup channel --types public_channel,private_channel --max-items <n> --json`" + `.
- Existing DM list command: ` + "`slick lookup channel --types im --max-items <n> --json`" + `.
- Exact lookup command: ` + "`slick lookup channel --channel <channel-id-or-alias> --json`" + `.
- Parse: use IDs from ` + "`data.channels[]`" + `; check ` + "`is_member`" + ` and ` + "`is_archived`" + ` before posting.
- Quirks: plain mode renders tables for humans. Agents should keep JSON output and parse IDs from ` + "`data.channels`" + `.
- Quirks: prefer IDs such as ` + "`C123...`" + ` and ` + "`D123...`" + ` over display names; human names can change.
- Quirks: ` + "`--types all`" + ` includes public channels, private channels, IMs, and MPIMs. Use narrower types when possible.

## inspect_schema
- Runbook: use this when an agent needs command inventory, flags, output contracts, or another workflow runbook.
- Schema command: ` + "`slick agent schema --json`" + ` for the full machine schema.
- Compact schema command: ` + "`slick agent schema --compact`" + ` for a smaller command tree.
- Guide list command: ` + "`slick agent guide`" + ` to list workflows.
- Workflow runbook command: ` + "`slick agent guide <workflow>`" + ` to load the task runbook.
- Parse: schema includes commands, flags, input shapes, output schemas, env vars, exit codes, examples, workflows, best practices, and anti-patterns.
- Quirks: the old root ` + "`slick schema`" + ` alias is intentionally removed; schema discovery lives under ` + "`agent schema`" + `.
- Quirks: ` + "`schema.Commands`" + ` excludes hidden probationary commands. ` + "`schema.Workflows`" + ` may mention probationary workflows with status text.
- Quirks: examples are examples, not guaranteed-safe invocations. Apply ` + "`--dry-run`" + ` to mutations first.
- Quirks: if a guide section conflicts with ` + "`agent schema`" + `, trust the schema for flags and command existence, then file a bug against the guide.

## lookup_user
- Runbook: use this to find stable user IDs, presence, status, and timezone before DM or human-aware work.
- Inputs: user filter text or exact user ID, whether presence is needed.
- List command: ` + "`slick lookup user --max-items <n> --json`" + `.
- Filter command: ` + "`slick lookup user --filter ansible --max-items 20 --json`" + `.
- Exact command: ` + "`slick lookup user --user <user-id> --json`" + `.
- Presence command: add ` + "`--presence`" + ` only when the token has presence visibility.
- Deleted users: list mode excludes deleted and deactivated users by default; add ` + "`--include-deleted`" + ` only when auditing old accounts.
- Cache command: ` + "`slick cache users --json`" + ` primes active users for shell completion and repeated discovery.
- Parse: use ` + "`data.users[].id`" + ` for commands, ` + "`data.users[].tz`" + ` for scheduling, and ` + "`data.users[].presence`" + ` when presence is available.
- Next step: pass ` + "`data.users[].id`" + ` to ` + "`slick message send --user <user-id>`" + ` for DM workflows.
- Quirks: prefer user IDs such as ` + "`U123...`" + ` in commands; names and display names can change.
- Quirks: presence and custom status depend on token scopes and Slack workspace policy. Missing fields are not always an error.

## cache_metadata
- Runbook: use this before repeated lookup, shell completion, or large live tests that may hit Slack rate limits.
- Cache users: ` + "`slick cache users --json`" + ` caches active users only. Deleted and deactivated users stay out of the cache.
- Cache channels: ` + "`slick cache channels --json`" + ` caches active public channels, private channels, IMs, and MPIMs.
- Refresh: add ` + "`--refresh`" + ` to force a Slack API fetch even when the cache is fresh.
- TTL: default freshness is 1440 minutes. Use ` + "`--ttl-minutes 10080`" + ` for a weekly refresh window.
- Bounds: use ` + "`--page-size <n>`" + ` and ` + "`--max-pages <n>`" + ` to control pagination when priming large workspaces.
- Clear: ` + "`slick cache clear users`" + ` or ` + "`slick cache clear channels`" + ` removes one cache resource. ` + "`slick cache clear`" + ` removes all resources for the active profile.
- Parse: ` + "`slick cache clear <resource>`" + ` returns ` + "`{profile, resource, cleared}`" + ` where ` + "`cleared=true`" + ` means the resource existed and was removed; ` + "`cleared=false`" + ` means the cache was already empty. ` + "`slick cache clear`" + ` (no resource) returns ` + "`{profile, resources}`" + ` where ` + "`resources[]`" + ` lists every removed name; an empty or absent slice means nothing was removed. On a partial failure the error envelope includes ` + "`details.partial`" + ` with the resources removed before the failure.
- Path: cache files live under XDG cache home, normally ` + "`~/.cache/slick/<profile>/`" + `.
- Quirks: shell completion prefers fresh cached users/channels before live Slack API calls.
- Quirks: cache files are metadata only; they do not store tokens or message content.

## send_dm
- Runbook: use this to send a direct message to a Slack user.
- Inputs: stable user ID, email address, configured alias, message body, workspace/profile, and token type expectations.
- Preflight: use ` + "`slick lookup user --user <user-id> --json`" + ` if the ID is uncertain.
- Command: ` + "`slick message send --user <user-id-or-email> --message <markdown> --json`" + `.
- Group DM command: repeat ` + "`--user`" + ` or comma-separate values.
- Stdin command: ` + "`printf '%s\n' \"$body\" | slick message send --user <user-id-or-email> --file - --json`" + `.
- Parse: keep ` + "`data.message.channel`" + ` and ` + "`data.message.ts`" + ` from JSON output.
- Raw Block Kit: use ` + "`--blocks`" + ` only when the DM body is raw Block Kit JSON.
- Quirks: ` + "`message send --user`" + ` opens the DM through Slack before posting. Slack decides whether the active bot-token or user-token profile may open the DM.
- Quirks: email recipients are resolved with Slack ` + "`users.lookupByEmail`" + ` before opening the DM.
- Quirks: user-token profiles are the normal choice for "DM anyone as me" workflows; bot-token profiles depend on app install and conversation access.
- Quirks: ` + "`--dry-run`" + ` verifies message composition but cannot prove Slack will open the DM.
- Error handling: Scope validation is best-effort when token metadata is available. ` + "`missing_scope`" + ` and ` + "`no_permission`" + ` use ` + "`auth_failure`" + `; destination or membership errors such as ` + "`not_in_channel`" + ` still use the fixed structured error contract.

## set_status
- Runbook: use this to set or clear the authenticated user's Slack status.
- Inputs: status text, emoji, optional expiration, workspace/profile, and token type.
- Set command: ` + "`slick status set --text <text> --emoji :headphones: --expires-in 2h --json`" + `.
- Positional command: ` + "`slick status set \"In a meeting\" :calendar: --json`" + `.
- Clear command: ` + "`slick status clear --json`" + `.
- Dry-run: use ` + "`--dry-run`" + ` to preview the status payload without calling Slack.
- Parse: keep ` + "`data.text`" + `, ` + "`data.emoji`" + `, and ` + "`data.expiration`" + ` when present. In JSON, ` + "`meta.command`" + ` (` + "`status.set`" + ` vs ` + "`status.clear`" + `) tells you which path ran.
- Quirks: status requires a user token with ` + "`users.profile:write`" + `; bot-token profiles cannot set a user's status.

## safe_mutation
- Runbook: use this before send, reply, edit, delete, react, and file upload mutations in important channels.
- Inputs: target channel or user, intended mutation, whether the target is high-impact, expected output mode.
- Preflight: run a read-only lookup in the same profile before changing a high-traffic channel.
- Dry-run: use ` + "`--dry-run`" + ` before destructive or high-visibility mutations.
- Execute: keep JSON output for automation and parse explicit IDs, timestamps, and permalinks.
- Evidence: for real writes, record the returned channel, timestamp, permalink, and dry-run status in the calling agent's evidence.
- Failure parsing: do not scrape human text. Parse stderr JSON for ` + "`type`" + `, ` + "`message`" + `, and ` + "`exit_code`" + `.
- Rate limits: retry only after respecting Slack's retry guidance. The CLI maps rate-limit failures to exit code ` + "`3`" + ` and structured ` + "`rate_limit`" + ` errors.
- Quirks: ` + "`chat.postMessage`" + ` has special same-channel limits. Separate CLI processes do not share proactive throttle state, so shell fanout can still hit structured ` + "`rate_limit`" + ` errors.
- Quirks: keep mutation fanout modest unless the purpose is a deliberate rate-limit test.
- Quirks: use ` + "`--plain`" + ` only for human inspection, never for machine parsing.
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
		if after, ok := strings.CutPrefix(line, "## "); ok {
			heading := strings.ToLower(strings.TrimSpace(after))
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
