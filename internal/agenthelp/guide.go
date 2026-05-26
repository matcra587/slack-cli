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
		Steps:       []string{"Use JSON for automation and human output for humans", "Treat stdout as command data only", "Treat stderr as diagnostics and structured errors", "Set --output to one of auto, human, json, compact (auto-detected by default)", "Parse fixed exit codes and error types on failure"},
	},
	{
		Name:        "check_health",
		Description: "Check Slack service health and Web API reachability",
		Steps:       []string{"Use health check for a combined Slack Status plus api.test probe", "Use health current for active incidents", "Use health history for recent Slack incidents", "No Slack token or scopes are required", "Parse data.healthy, data.api_ok, data.status, and data.active_incidents[] from JSON"},
	},
	{
		Name:        "send_msg",
		Description: "Send a markdown message and read ts/permalink from JSON",
		Steps:       []string{"Choose workspace/profile", "Pass message body with --message or --file -", "Use --channel or --user, never both", "Use Slack-profile email for DM --user targeting", "Use --schedule with --channel or --user and RFC3339/duration/Unix seconds", "Use --blocks only for raw Block Kit input", "Dry-run first for high-visibility sends", "Read JSON response for ts/permalink or scheduled_message_id"},
	},
	{
		Name:        "schedule_msg",
		Description: "Schedule, list, and delete future messages",
		Steps:       []string{"Use message send --schedule with --channel or --user", "For scheduled DMs, pass user IDs, aliases, or Slack-profile email with --user", "Use RFC3339, Go duration, or Unix seconds; natural language is rejected", "Parse scheduled_message_id because scheduled sends have no ts until they fire", "For real scheduled --user sends, parse data.channel as the raw DM/MPIM channel ID", "List with message scheduled list and pagination", "Use JSON list output for delete targets; human CHANNEL is display-only", "Delete with exact channel and scheduled-id"},
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
		Steps:       []string{"Use agent schema, not the removed root schema alias", "Use --compact (the local agent-schema flag) for a smaller command tree; --output=compact only strips the JSON envelope", "Use agent guide <workflow> to load the task runbook"},
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
		Steps:       []string{"Use message send --user with user IDs, aliases, or Slack-profile email addresses", "Repeat --user or comma-separate values for group DMs", "Add --schedule to schedule the DM instead of sending now", "Email lookup uses Slack profile email; users_not_found means the address did not match", "Slack decides whether the active bot-token or user-token profile may open the DM", "Use returned data.message.channel or scheduled data.channel for follow-up operations", "Handle structured errors where Slack rejects the target", "Use a user-token profile for DM-anyone workflows when bot-token limits get in the way"},
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
Goal: Apply the cross-cutting output-mode, stdout/stderr, exit-code, and error-shape contract every other workflow inherits.

**Decide**
- TTY humans: ` + "`--output=human`" + ` (or omit; ` + "`auto`" + ` picks ` + "`human`" + ` on a TTY).
- Automation: ` + "`--output=json`" + ` for envelope (` + "`json`" + ` mode), ` + "`--output=compact`" + ` for envelope-less ` + "`data`" + ` only (` + "`compact`" + ` mode).

**Save**
> Requires ` + "`--output=json`" + `.
- ` + "`meta.command`" + `, ` + "`meta.workspace`" + `, ` + "`meta.timestamp`" + `, ` + "`meta.request_id`" + ` [string, required] — envelope identity.
- ` + "`data`" + ` [object, required] — command-specific payload.
- ` + "`errors`" + ` [array, required] — empty on success.

**Behavior**
- stdout is command data only. stderr is diagnostics, progress, warnings, and structured errors.
- JSON-mode failures emit ` + "`errors[0].type`" + `, ` + "`errors[0].message`" + `, ` + "`errors[0].exit_code`" + ` on stderr; stdout stays empty.
- Exit codes: auth ` + "`1`" + `, not found ` + "`2`" + `, rate limit ` + "`3`" + `, validation ` + "`4`" + `, server ` + "`5`" + `, canceled ` + "`6`" + `, timeout ` + "`7`" + `.
- Slack channels/DMs/permalinks may render as OSC 8 hyperlinks for humans. Do not scrape — parse JSON for raw IDs, channel_name/channel_hr/channel_url, timestamps, full URLs.
- ` + "`--output`" + ` selects output mode only; it does NOT switch input to Block Kit. Use command-local ` + "`--blocks`" + ` for that.
- Attribution auto-attaches to ` + "`send_msg`" + ` / ` + "`reply`" + ` / ` + "`edit_msg`" + ` / ` + "`upload_file`" + ` comments as a context block on agent/CI detection — do NOT repeat in body. Toggle ` + "`--attribution`" + ` / ` + "`--no-attribution`" + `; override copy ` + "`--attribution-{label,emoji,message}`" + `.
- Slack ts values stay strings (e.g. ` + "`1746284582.123456`" + `); never cast to float.

## check_health
Goal: Decide whether a failing Slack call is the Slack service, the Web API, your network, or your auth — before blaming auth or scopes.

**Decide**
- Combined probe (status + api.test): ` + "`slick health check`" + `.
- Status only: ` + "`slick health current`" + ` (or ` + "`history`" + ` for past incidents).
- API only: ` + "`slick health api-test`" + `.
- Narrow to one service: ` + "`--service <Messaging|Search|Files|Apps/Integrations/APIs>`" + `.

**Run**
- Combined: ` + "`slick health check --output=json`" + `
- Service-filtered: ` + "`slick health check --service Messaging --output=json`" + `
- API probe: ` + "`slick health api-test --output=json`" + `
- History: ` + "`slick health history --limit 20 --output=json`" + `

**Save**
> Requires ` + "`--output=json`" + `.
- ` + "`data.healthy`" + ` [bool, required] — combined verdict.
- ` + "`data.api_ok`" + ` [bool, required] — Web API reachability.
- ` + "`data.status`" + ` [string, required] — Slack Status overall.
- ` + "`data.active_incidents[]`" + ` [array, optional] — each row has ` + "`id`" + `, ` + "`title`" + `, ` + "`type`" + `, ` + "`status`" + `, ` + "`url`" + `, ` + "`date_created`" + `, ` + "`date_updated`" + `, ` + "`services[]`" + `, ` + "`note_count`" + `.

**Behavior**
- These commands do NOT use the configured workspace, token, or scopes. A failing health command points at Slack-side service, network, timeout, or malformed-response issues — not at profile auth.
- Slack Status ` + "`current`" + ` can report ` + "`status=active`" + ` for services unrelated to your workflow. Filter with ` + "`--service`" + ` when only one matters.

**Next**
- Alternative: → ` + "`auth_setup`" + ` (when health is fine but real Slack calls still fail — likely a scope/profile issue)

## auth_setup
Goal: Wire a Slack workspace profile with a valid token before any other workflow can call Slack.

**Decide**

# command shape
- Bare ` + "`slick auth login`" + ` opens an interactive form on a TTY — the only place that prompts.
- Headless: add ` + "`--workspace-name <profile>`" + ` plus either ` + "`--method oauth ...`" + ` or a token source flag. Default method is ` + "`token`" + `, so passing only ` + "`--token-stdin|file|env`" + ` is enough.

# app shape
- ` + "`--type user`" + ` (default; "act as me") vs ` + "`--type bot`" + ` (app identity) when generating the manifest.

# scope footprint
- ` + "`--preset messaging`" + ` (send + read), ` + "`--preset readonly`" + `, ` + "`--preset files`" + `, ` + "`--preset search`" + `, ` + "`--preset full`" + `.

# guard
- Already authenticated? Run preflight first.

**Run**
- Preflight: ` + "`slick auth status --output=json`" + `
- Manifest: ` + "`slick manifest template --preset messaging --type user --name <app-name>`" + ` (swap ` + "`--preset`" + ` for a different scope footprint)
- OAuth login (browser, PKCE): ` + "`slick auth login --workspace-name <profile> --method oauth --oauth-client-id <id> --oauth-callback-port <port>`" + `
- Token login (no token in argv): pipe via ` + "`--token-stdin`" + `, point at ` + "`--token-file <path>`" + `, or pass an env var name (NOT its value) with ` + "`--token-env <VAR>`" + `

**Save**
> Requires ` + "`--output=json`" + `.
- ` + "`data.workspaces[].workspace`" + `, ` + "`data.workspaces[].team_id`" + `, ` + "`data.workspaces[].token_type`" + ` [string, required] — confirmation without credential material.
- ` + "`data.workspaces[].validation_state`" + ` [string, required] — ` + "`valid`" + `, ` + "`missing`" + `, or ` + "`invalid`" + `.

**Preconditions**
- Profile names are case-insensitive (` + "`Default`" + ` and ` + "`default`" + ` collide).
- ` + "`--token-env`" + ` takes an environment-variable name, not the token value itself.

**Behavior**
- Credentials live in OS keychain or a configured secret backend (e.g. ` + "`op://...`" + `). Never put plaintext ` + "`xox*`" + ` tokens in TOML, argv, stdout, stderr, or logs.
- Runtime override: ` + "`SLACK_CLI_TOKEN_<PROFILE>`" + ` beats ` + "`SLACK_CLI_TOKEN`" + ` and the configured secret reference for that profile. Profile-suffix uppercased with non-alphanumeric → ` + "`_`" + `.

**Recover**
| Symptom | Cause | Next |
|---|---|---|
| ` + "`bad_client_secret`" + ` from OAuth | App configured for confidential client | Import a slick-generated manifest, or enable PKCE in Slack |
| Slack rejects the OAuth redirect URL | Random callback port mismatch | Copy the exact local callback URL printed during login; pass it back as ` + "`--oauth-redirect-url`" + ` |
| ` + "`auth_failure: missing_scope`" + ` after login | Manifest lacks the scope the workflow needs | Edit manifest preset → reauth |

**Next**
- Then: → ` + "`config_prefs`" + ` (set ` + "`default_channel`" + `, attribution defaults)
- Then: → ` + "`send_msg`" + ` (verify auth with a real send)

## config_prefs
Goal: Change profile preferences (default channel, attribution defaults) without touching credentials.

**Decide**
- File missing? → ` + "`slick config init`" + ` first (it does NOT ask for tokens).
- Default destination: ` + "`workspaces.<profile>.default_channel`" + ` (fallback for ` + "`message send`" + ` when neither ` + "`--channel`" + ` nor ` + "`--user`" + ` is passed).
- Attribution defaults: ` + "`workspaces.<profile>.attribution.{enabled,message,emoji}`" + `.

**Run**
- Init: ` + "`slick config init`" + `
- Set default channel: ` + "`slick config set workspaces.<profile>.default_channel <channel-id>`" + `
- Set attribution copy: ` + "`slick config set workspaces.<profile>.attribution.message <text>`" + `
- Set attribution emoji: ` + "`slick config set workspaces.<profile>.attribution.emoji <emoji>`" + `
- Inspect path: ` + "`slick config path`" + `
- Inspect effective: ` + "`slick config list --output=json`" + `

**Save**
> Requires ` + "`--output=json`" + `.
- ` + "`data.settings[]`" + ` [array, required] — ` + "`{key, value, description}`" + ` rows; never contains credential material.

**Behavior**
- Credentials are owned by ` + "`slick auth login|status|switch|logout`" + ` — not by ` + "`config`" + `.
- Config path: ` + "`~/.config/slick/config.toml`" + ` (XDG, including macOS). ` + "`SLICK_CONFIG`" + ` overrides; legacy ` + "`SLACK_CLI_CONFIG`" + ` still works. Path inputs expand ` + "`~`" + ` and env vars.
- Precedence: environment → config file → defaults.
- Runtime token override is profile-scoped: ` + "`SLACK_CLI_TOKEN_<PROFILE>`" + `, name uppercased and non-alphanumeric replaced with ` + "`_`" + `.

**Recover**
| Symptom | Cause | Next |
|---|---|---|
| ` + "`config file not found`" + ` | Profile never initialized | Run ` + "`slick config init`" + ` |
| ` + "`unknown key`" + ` from ` + "`config set`" + ` | Typo or wrong scope path | Use ` + "`slick config list`" + ` to see effective keys |

## send_msg
Goal: Post a message to a channel, user, or group DM and capture the Slack timestamp and permalink for follow-up.

**Decide**

# target
- Channel: ` + "`--channel <id-or-alias>`" + ` (falls back to ` + "`default_channel`" + ` only when not scheduled).
- User: ` + "`--user <user-id-or-email>`" + ` (repeat or comma-separate for group DM).

# body
- One line: ` + "`--message <markdown>`" + `.
- Multiline / generated: ` + "`--file -`" + ` (pipe stdin; real newlines, not ` + "`\\n`" + `).
- Raw Block Kit: ` + "`--blocks`" + ` (required for real mentions and explicit ` + "`<url|label>`" + ` links).

# modifiers
- Future send: ` + "`--schedule <RFC3339|Go-duration|Unix-seconds>`" + ` (no natural language).
- ` + "`--dry-run`" + ` validates locally and returns ` + "`ts=\"dry-run\"`" + `.

**Run**
- Canonical: ` + "`slick message send --channel <id-or-alias> --message <markdown> --output=json`" + `
- Adapt: swap ` + "`--channel`" + `→` + "`--user`" + ` for DMs; replace ` + "`--message`" + ` with ` + "`--file -`" + ` for stdin or with ` + "`--blocks --file blocks.json`" + ` for raw Block Kit; append ` + "`--schedule 90m`" + ` to schedule.

**Save**
> Requires ` + "`--output=json`" + `.

Immediate: ` + "`data.message.ts`" + `, ` + "`data.message.thread_ts`" + ` (when threaded), ` + "`data.permalink`" + `, ` + "`data.message.channel`" + ` / ` + "`data.message.channel_url`" + `.

Scheduled: ` + "`data.channel`" + ` (raw ID), ` + "`data.scheduled_message_id`" + ` (→ ` + "`schedule_msg`" + ` to cancel); ` + "`data.message.ts`" + ` absent until Slack posts.

Dry run: ` + "`data.message.ts == \"dry-run\"`" + ` or ` + "`data.scheduled_message_id == \"dry-run\"`" + `.

**Preconditions**
- ` + "`--channel`" + ` and ` + "`--user`" + ` are mutually exclusive; scheduled sends ignore ` + "`default_channel`" + ` and need an explicit target. Email targeting needs ` + "`users:read.email`" + `.

**Behavior**
- Markdown is escaped (` + "`&`" + `, ` + "`<`" + `, ` + "`>`" + ` → entities); sentinels ` + "`<!channel>`" + `, ` + "`<@U123>`" + `, ` + "`<url|label>`" + ` in ` + "`--message`" + ` render as literal text — use ` + "`--blocks`" + ` for wire syntax.
- Attribution auto-attaches (toggle / override): see → ` + "`core_contract`" + `.

**Recover**
| Symptom | Cause | Next |
|---|---|---|
| ` + "`validation_error: --channel or --user is required`" + ` | No target, no ` + "`default_channel`" + ` | Retry with explicit target |
| ` + "`not_found: not_in_channel`" + ` | Token identity not in channel | Invite, or → ` + "`auth_setup`" + ` for user-token profile |
| ` + "`not_found: users_not_found`" + ` | Email not a Slack profile here | → ` + "`lookup_user`" + `, retry with ` + "`--user U…`" + ` |
| ` + "`auth_failure: missing_scope`" + ` | Manifest lacks ` + "`chat:write`" + ` / ` + "`users:read.email`" + ` | → ` + "`auth_setup`" + ` |
| ` + "`validation_error`" + ` on ` + "`--schedule`" + ` | Past, natural language, or >120 days out | Future RFC3339 / Go duration / Unix seconds |

**Next**
- Then: → ` + "`reply`" + ` / → ` + "`edit_msg`" + ` / → ` + "`react`" + ` on the saved ts.
- Alternative: → ` + "`schedule_msg`" + ` to list or cancel pending scheduled sends.

## schedule_msg
Goal: Create, list, or cancel scheduled messages.

**Decide**

# action
- Create: scheduling is a modifier on ` + "`message send`" + ` — append ` + "`--schedule <when>`" + ` to any ` + "`send_msg`" + ` invocation.
- List: ` + "`slick message scheduled list`" + ` (optional ` + "`--channel`" + ` filter, ` + "`--limit`" + `, ` + "`--cursor`" + `).
- Cancel: ` + "`slick message scheduled delete`" + ` — requires exact ` + "`--channel`" + ` (raw ID) and ` + "`--scheduled-id`" + ` from a prior list or send.

# when (create only)
- Format: RFC3339 (` + "`2026-06-01T15:00:00-04:00`" + `), Go duration (` + "`90m`" + `, ` + "`2h30m`" + `), or Unix seconds. Never natural language.
- Range: future, within 120 days.

**Run**
- List (all): ` + "`slick message scheduled list --limit <n> --output=json`" + `
- List (one channel): ` + "`slick message scheduled list --channel <id-or-alias> --limit <n> --output=json`" + `
- Next page: ` + "`slick message scheduled list --cursor <meta.pagination.next_cursor> --limit <n> --output=json`" + `
- Cancel: ` + "`slick message scheduled delete --channel <raw-channel-id> --scheduled-id <QID> --output=json`" + `

**Save**
> Requires ` + "`--output=json`" + `.

List:
- ` + "`data.scheduled_messages[].id`" + ` [string, required] — pass to ` + "`--scheduled-id`" + ` for delete.
- ` + "`data.scheduled_messages[].channel`" + ` [string, required] — raw Slack conversation ID for ` + "`--channel`" + ` on delete.
- ` + "`data.scheduled_messages[].post_at`" + ` / ` + "`.post_at_iso`" + ` [int64 / string, required] — fire time.
- ` + "`data.scheduled_messages[].text_preview`" + ` [string, optional] — capped at 200 Unicode chars.
- ` + "`meta.pagination.has_more`" + ` / ` + "`.next_cursor`" + ` [bool / string] — paginate until ` + "`has_more=false`" + `.

Delete:
- ` + "`data.channel`" + `, ` + "`data.scheduled_message_id`" + ` [string, required] — confirmation echo.

**Preconditions**
- Delete needs raw channel/DM/MPIM IDs — friendly ` + "`#channel`" + ` or ` + "`@user`" + ` labels in human output are display-only.
- Pending sends have no ` + "`ts`" + ` until Slack posts them; correlate on ` + "`scheduled_message_id`" + `.

**Behavior**
- Delete supports ` + "`--dry-run`" + ` but does NOT require ` + "`--force`" + ` (Slack treats cancellation as cheap).
- Friendly metadata (` + "`channel_name`" + `, ` + "`channel_hr`" + `, ` + "`channel_url`" + `, ` + "`is_dm`" + `, ` + "`channel_type`" + `, ` + "`channel_user`" + `) is best-effort and may be absent in error envelopes.

**Recover**
| Symptom | Cause | Next |
|---|---|---|
| ` + "`not_found`" + ` on delete | Wrong ` + "`--channel`" + ` (used DM alias instead of raw ID) | Re-list, copy ` + "`data.scheduled_messages[].channel`" + ` verbatim |
| ` + "`auth_failure: missing_scope`" + ` | Manifest lacks ` + "`chat:write`" + ` (delete) or scheduled-message read scope | → ` + "`auth_setup`" + ` to widen manifest |

**Next**
- Then: → ` + "`send_msg`" + ` (create a new scheduled send)
- Composes: → ` + "`cleanup_msgs`" + ` (after firing, scheduled sends become regular messages — clean them up with search + delete)

## react
Goal: Add, remove, or list emoji reactions on a known channel message.

**Decide**

# direction
- Add: ` + "`slick react add`" + `
- Remove: ` + "`slick react remove`" + `
- Inspect: ` + "`slick react list`" + `

# target
- Always needs ` + "`--channel <id>`" + ` + ` + "`--timestamp <slack-ts>`" + ` (get the ts from ` + "`read_history`" + ` or saved ` + "`data.message.ts`" + ` from ` + "`send_msg`" + `).

# emoji (add/remove only)
- One: ` + "`--emoji :thumbsup:`" + ` or ` + "`--emoji thumbsup`" + ` (colons optional).
- Many (ordered): ` + "`--emoji thumbsup,white_check_mark,rocket`" + ` or repeat ` + "`--emoji`" + ` (use repeat when a value itself contains a comma).

**Run**
- Add: ` + "`slick react add --channel <id> --timestamp <ts> --emoji :thumbsup: --output=json`" + `
- Add ordered batch: ` + "`slick react add --channel <id> --timestamp <ts> --emoji thumbsup,rocket --output=json`" + `
- Remove: ` + "`slick react remove --channel <id> --timestamp <ts> --emoji :thumbsup: --output=json`" + `
- List: ` + "`slick react list --channel <id> --timestamp <ts> --output=json`" + `

**Save**
> Requires ` + "`--output=json`" + `.

Add / remove:
- ` + "`data.mutations[]`" + ` [array, required] — one entry per emoji actually applied, fields ` + "`{channel, ts, emoji, dry_run}`" + `.
- ` + "`data.target.channel`" + `, ` + "`data.target.ts`" + ` [string, required] — echo of the target.
- ` + "`meta.command`" + ` is ` + "`react.add`" + ` or ` + "`react.remove`" + `.

List:
- ` + "`data.reactions[]`" + ` [array] — ` + "`{name, count, users[]}`" + `.
- ` + "`meta.command`" + ` is ` + "`react.list`" + `.

**Preconditions**
- Slack timestamps are strings (` + "`1746284582.123456`" + `); never cast to float.
- Ordered multi-emoji is atomic-per-emoji: a failure aborts the rest. On an error envelope ` + "`data.mutations[]`" + ` is absent — re-derive remaining work from a fresh ` + "`react list`" + ` instead of assuming partial state.

**Behavior**
- ` + "`--dry-run`" + ` validates locally and short-circuits the Slack call; ` + "`data.mutations[].dry_run=true`" + `.
- Verification cycle: after add/remove, ` + "`react list`" + ` on the same channel + ts.

**Recover**
| Symptom | Cause | Next |
|---|---|---|
| ` + "`not_found: message_not_found`" + ` | Wrong ` + "`--timestamp`" + ` or ts deleted | → ` + "`read_history`" + ` to re-find the message |
| ` + "`already_reacted`" + ` / ` + "`no_reaction`" + ` | Emoji already present (add) or absent (remove) | Treat as success or skip; ` + "`react list`" + ` to confirm |
| ` + "`auth_failure: missing_scope`" + ` | Manifest lacks ` + "`reactions:write`" + ` or ` + "`reactions:read`" + ` | → ` + "`auth_setup`" + ` |

**Next**
- Then: → ` + "`reply`" + ` (continue the thread)
- Alternative: → ` + "`edit_msg`" + ` (modify the target instead of decorating)

## reply
Goal: Post a threaded reply to a parent message, returning the new ` + "`ts`" + ` and the shared ` + "`thread_ts`" + ` for follow-ups.

**Decide**

# target
- ` + "`--channel <id>`" + ` + ` + "`--parent <parent-ts>`" + ` (parent ts comes from ` + "`send_msg`" + ` ` + "`data.message.ts`" + ` or ` + "`read_history`" + `).

# body
- One line: ` + "`--message <markdown>`" + `
- Multiline or generated: pipe to ` + "`--file -`" + `
- Raw Block Kit: ` + "`--blocks`" + ` (required for real mentions / explicit ` + "`<url|label>`" + ` links)

# guards
- Dry run: ` + "`--dry-run`" + ` validates locally and returns ` + "`thread_ts`" + ` without calling Slack.

**Run**
- Message: ` + "`slick reply --channel <id> --parent <parent-ts> --message <markdown> --output=json`" + `
- Stdin: ` + "`printf '%s\\n' \"$reply\" | slick reply --channel <id> --parent <parent-ts> --file - --output=json`" + `
- Raw Block Kit: ` + "`slick reply --channel <id> --parent <parent-ts> --blocks --file reply.json --output=json`" + `

**Save**
> Requires ` + "`--output=json`" + `.
- ` + "`data.message.ts`" + ` [string, required] — the reply's own ts (use for ` + "`react`" + ` / ` + "`edit_msg`" + ` on the reply).
- ` + "`data.message.thread_ts`" + ` [string, required] — shared thread anchor; equals ` + "`--parent`" + ` on the first reply.
- ` + "`data.permalink`" + ` [string, required] — Slack URL.

**Preconditions**
- ` + "`--parent`" + ` is the parent message timestamp, not a permalink or a search-result index.
- The parent must still exist; a deleted parent breaks the thread.

**Behavior**
- Markdown is escaped on the wire (` + "`&`" + `, ` + "`<`" + `, ` + "`>`" + ` → HTML entities). Sentinels like ` + "`<!channel>`" + `, ` + "`<@U123>`" + `, ` + "`<#C123>`" + `, ` + "`<url|label>`" + ` in ` + "`--message`" + ` render as literal text — they do NOT fire mentions. Use ` + "`--blocks`" + ` to send real mentions or explicit linked text.
- Attribution auto-attaches (toggle / override): see → ` + "`core_contract`" + `.

**Recover**
| Symptom | Cause | Next |
|---|---|---|
| ` + "`not_found: message_not_found`" + ` | Parent ts wrong or parent deleted | → ` + "`read_history`" + ` to re-find the parent |
| ` + "`thread_not_found`" + ` | Parent isn't in this channel | Verify ` + "`--channel`" + ` matches the parent's channel |

**Next**
- Then: → ` + "`read_history`" + ` with ` + "`--thread <thread_ts>`" + ` to see the full thread.
- Alternative: → ` + "`edit_msg`" + ` or ` + "`react`" + ` on the reply's own ` + "`ts`" + `.

## read_history
Goal: Discover message timestamps in a channel or thread so other workflows (` + "`react`" + `, ` + "`reply`" + `, ` + "`edit_msg`" + `, ` + "`delete_msg`" + `) have exact ` + "`ts`" + ` values to act on.

**Decide**

# scope
- Channel timeline: ` + "`--channel <id>`" + ` only.
- Thread: add ` + "`--thread <parent-ts>`" + ` to get only that thread's replies.

# narrowing
- Time window: ` + "`--since <slack-ts>`" + `, ` + "`--until <slack-ts>`" + ` (Slack ts strings, not RFC3339).
- Author: ` + "`--user <user-id>`" + `.

# bound
- Always pass ` + "`--max-items <n>`" + ` in automation — Slack will keep paging until exhausted otherwise.

**Run**
- Channel: ` + "`slick history list --channel <id> --max-items <n> --output=json`" + `
- Thread: ` + "`slick history list --channel <id> --thread <parent-ts> --max-items <n> --output=json`" + `
- Filtered: append ` + "`--since`" + ` / ` + "`--until`" + ` / ` + "`--user`" + `
- Next page: ` + "`slick history list --channel <id> --cursor <meta.pagination.cursor> --max-items <n> --output=json`" + `

**Save**
> Requires ` + "`--output=json`" + `.
- ` + "`data.messages[].ts`" + ` [string, required] — feed to ` + "`react --timestamp`" + `, ` + "`reply --parent`" + ` (when it's a parent), ` + "`message edit/delete --timestamp`" + `.
- ` + "`data.messages[].text`" + ` [string] and ` + "`data.messages[].blocks`" + ` [array, optional] — text may flatten display formatting; check ` + "`blocks`" + ` when Slack-native structure matters.
- ` + "`data.messages[].permalink`" + ` [string, optional] — present when fetched.
- ` + "`meta.pagination.cursor`" + ` / ` + "`.has_more`" + ` [string / bool] — paginate until ` + "`has_more=false`" + `.

**Behavior**
- Plain mode (TTY / ` + "`--output=human`" + `) renders a table for humans; JSON mode is the agent contract.
- A reply's ` + "`ts`" + ` is its own message ts; the thread anchor is its ` + "`thread_ts`" + `.

**Recover**
| Symptom | Cause | Next |
|---|---|---|
| Empty ` + "`data.messages[]`" + ` | Filter too tight or channel actually empty | Drop ` + "`--since/--until/--user`" + ` and re-run |
| ` + "`channel_not_found`" + ` | Bot/user identity isn't in the channel | → ` + "`discover_destination`" + ` then invite |
| ` + "`auth_failure: missing_scope`" + ` | Manifest lacks ` + "`channels:history`" + ` / ` + "`groups:history`" + ` / ` + "`im:history`" + ` / ` + "`mpim:history`" + ` for the conversation type | → ` + "`auth_setup`" + ` |

**Next**
- Then: → ` + "`react`" + `, ` + "`reply`" + `, ` + "`edit_msg`" + `, ` + "`delete_msg`" + ` (all need the saved ts).
- Alternative: → ` + "`search_msgs`" + ` for workspace-wide discovery instead of one channel.

## search_msgs
Goal: Workspace-wide message search — the only way to find content without already knowing the channel.

**Decide**
- Looking for a known run ID, phrase, or user mention across the workspace? Use this.
- Inside one channel? Prefer → ` + "`read_history`" + ` (faster, cheaper, less rate-limited).
- Need user identity, not message text? → ` + "`lookup_user`" + `.

**Run**
- Search: ` + "`slick lookup messages --query <query> --max-items <n> --output=json`" + `
- Next page: ` + "`slick lookup messages --query <query> --cursor <meta.pagination.next_cursor> --max-items <n> --output=json`" + `

**Save**
> Requires ` + "`--output=json`" + `.
- ` + "`data.matches[].channel.id`" + ` [string, required] — pass verbatim as ` + "`--channel`" + ` for downstream operations.
- ` + "`data.matches[].ts`" + ` [string, required] — message ts for ` + "`react`" + ` / ` + "`edit_msg`" + ` / ` + "`delete_msg`" + `.
- ` + "`data.matches[].text`" + ` [string] — full text in JSON; plain output truncates and ` + "`--full`" + ` only affects human plain.
- ` + "`meta.pagination.has_more`" + ` / ` + "`.next_cursor`" + ` [bool / string] — paginate until ` + "`has_more=false`" + `.

**Preconditions**
- Needs a user-token profile with ` + "`search:read`" + `. Bot-token profiles cannot call Slack Web API ` + "`search.messages`" + ` at all.
- For mixed manifests, ` + "`search:read`" + ` belongs under user scopes only (` + "`--type user`" + ` or ` + "`--type both`" + `).

**Behavior**
- Search is slower and more strictly rate-limited than channel history; backoff is automatic on structured ` + "`rate_limit`" + ` errors.
- Result snippets in plain output are display-only; for automation parse JSON fields.

**Recover**
| Symptom | Cause | Next |
|---|---|---|
| ` + "`auth_failure: missing_scope`" + ` (` + "`search:read`" + `) | Bot-token profile or manifest without user-scope search | → ` + "`auth_setup`" + ` with ` + "`--preset search`" + ` and ` + "`--type user`" + ` |
| Zero matches but you know the message exists | Slack search index lag (can be minutes) | Retry later; or → ` + "`read_history`" + ` if you know the channel |

**Next**
- Composes: → ` + "`cleanup_msgs`" + ` (the canonical search-then-delete-by-run-ID loop).
- Then: → ` + "`react`" + ` / ` + "`reply`" + ` / ` + "`edit_msg`" + ` on any single match.

## upload_file
Goal: Upload a file (path or stdin bytes) to a channel and capture its permalink.

> Status: probationary. ` + "`slick file upload`" + ` is hidden from help and completion; use only when explicitly testing the upload surface.

**Decide**

# source
- Path: ` + "`--file <path>`" + ` (path expansion via ` + "`~`" + ` / env vars supported)
- Stdin: ` + "`--file -`" + ` (REQUIRES ` + "`--filename <name>`" + `)

# optional comment
- Markdown: ` + "`--message <markdown>`" + `
- Raw Block Kit comment: ` + "`--blocks`" + ` + ` + "`--message <json>`" + ` (affects comment only, not file bytes)

# guard
- ` + "`--dry-run`" + ` validates payload locally; skips the real Slack upload (and the ` + "`files:write`" + ` scope check).

**Run**
- Path: ` + "`slick file upload --channel <id> --file <path> --output=json`" + `
- Stdin: ` + "`cat artefact.bin | slick file upload --channel <id> --file - --filename <name> --output=json`" + `
- With comment: append ` + "`--message <markdown>`" + `

**Save**
> Requires ` + "`--output=json`" + `.
- ` + "`data.file.permalink`" + ` [string, optional] — Slack URL when returned.
- ` + "`data.file.id`" + `, ` + "`data.file.channels[]`" + ` [string / array] — Slack file metadata.

**Preconditions**
- Real uploads need ` + "`files:write`" + `.
- Missing path, directory targets, and stdin without ` + "`--filename`" + ` fail locally before any Slack call.

**Behavior**
- Upload progress and diagnostics go to stderr; stdout stays the command-data channel.
- Comment Markdown is escaped on the wire (` + "`&`" + `, ` + "`<`" + `, ` + "`>`" + ` → entities); sentinels — ` + "`<!channel>`" + `, ` + "`<@U123>`" + `, ` + "`<#C123>`" + `, ` + "`<url|label>`" + ` — render as literal text and do NOT fire mentions. Use ` + "`--blocks`" + ` for the comment payload to mention or link intentionally.

**Recover**
| Symptom | Cause | Next |
|---|---|---|
| Local error ` + "`stdin requires --filename`" + ` | Piped to ` + "`--file -`" + ` without naming the artifact | Re-run with ` + "`--filename <name>`" + ` |
| ` + "`auth_failure: missing_scope`" + ` | Manifest lacks ` + "`files:write`" + ` | → ` + "`auth_setup`" + ` with ` + "`--preset files`" + ` |

**Next**
- Then: → ` + "`send_msg`" + ` to announce or link the upload separately.

## edit_msg
Goal: Rewrite the text or Block Kit body of one of the authenticated identity's own messages, in place.

**Decide**

# target
- ` + "`--channel <id>`" + ` + ` + "`--timestamp <message-ts>`" + ` (timestamps are unique only inside a channel — always pass both).

# replacement body
- One line: ` + "`--message <markdown>`" + `
- Stdin: ` + "`--file -`" + `
- Raw Block Kit: ` + "`--blocks`" + ` + ` + "`--file <json>`" + `

# guard
- ` + "`--dry-run`" + ` validates locally without mutating Slack — use it before editing incident, release, or high-visibility channels.

**Run**
- Markdown: ` + "`slick message edit --channel <id> --timestamp <ts> --message <markdown> --output=json`" + `
- Stdin: ` + "`printf '%s\\n' \"$body\" | slick message edit --channel <id> --timestamp <ts> --file - --output=json`" + `
- Raw Block Kit: ` + "`slick message edit --channel <id> --timestamp <ts> --blocks --file edit.json --output=json`" + `

**Save**
> Requires ` + "`--output=json`" + `.
- ` + "`data.message.channel`" + `, ` + "`data.message.ts`" + ` [string, required] — echo of the target.
- ` + "`data.message.text`" + ` [string, optional] — Slack-returned post-edit text when present.
- Returned ` + "`blocks`" + ` are NOT included; if you need to verify rendered Block Kit, → ` + "`read_history`" + ` or check the Slack UI.

**Preconditions**
- Slack only allows editing messages owned by the token identity; bot tokens can only edit bot-posted messages, user tokens only user-posted ones.

**Behavior**
- Markdown is escaped on the wire (` + "`&`" + `, ` + "`<`" + `, ` + "`>`" + ` → entities); sentinels — ` + "`<!channel>`" + `, ` + "`<@U123>`" + `, ` + "`<#C123>`" + `, ` + "`<url|label>`" + ` — render as literal text and do NOT fire mentions. Use ` + "`--blocks`" + ` for the new content to mention or link intentionally.

**Recover**
| Symptom | Cause | Next |
|---|---|---|
| ` + "`cant_update_message`" + ` / ` + "`message_not_found`" + ` | Wrong owner, wrong ts, or message deleted | Stop — return the structured error. Do NOT fall back to delete+send (creates a new ts and breaks references). |
| ` + "`auth_failure: missing_scope`" + ` | Manifest lacks ` + "`chat:write`" + ` | → ` + "`auth_setup`" + ` |

**Next**
- Then: → ` + "`read_history`" + ` to confirm the rendered result when ` + "`--blocks`" + ` was used.
- Alternative: if you can't edit, prefer a → ` + "`reply`" + ` correction over → ` + "`delete_msg`" + ` (preserves thread context).

## delete_msg
Goal: Permanently delete one of the authenticated identity's own messages by exact channel + ts.

**Decide**
- Single deletion? Use this directly.
- Reversible correction? Prefer → ` + "`edit_msg`" + ` (preserves thread + references).
- Bulk by run ID? → ` + "`cleanup_msgs`" + ` (drives this workflow from search results).

**Run**
- Preview (no Slack mutation): ` + "`slick message delete --channel <id> --timestamp <ts> --dry-run --force --output=json`" + `
- Real delete: ` + "`slick message delete --channel <id> --timestamp <ts> --force --output=json`" + `

**Save**
> Requires ` + "`--output=json`" + `.
- ` + "`data.channel`" + `, ` + "`data.ts`" + ` [string, required] — echo of the deleted target.
- ` + "`meta.command`" + ` is ` + "`message.delete`" + `.

**Preconditions**
- ` + "`--force`" + ` is mandatory for real deletes (` + "`--dry-run --force`" + ` is safe and never calls mutation endpoints).
- Never delete by "last message", search-result index, or display position — this CLI intentionally requires exact channel + ts.
- The token identity must own the message.

**Behavior**
- Deletion is irreversible. There is no undo.
- Slack returns ` + "`message_not_found`" + ` for already-deleted messages — bulk callers should treat that as "already clean".

**Recover**
| Symptom | Cause | Next |
|---|---|---|
| ` + "`message_not_found`" + ` | Already deleted (race with another worker) | Treat as success in cleanup loops |
| ` + "`cant_delete_message`" + ` | Wrong owner identity | Switch profile via → ` + "`auth_setup`" + ` or accept that this message stays |
| ` + "`rate_limit`" + ` | Bulk pressure | Sleep ` + "`errors[0].retry_after_seconds`" + `, retry same target |

**Next**
- Composes: → ` + "`cleanup_msgs`" + ` for the canonical search-then-delete loop.

## developer_review
Goal: Post a realistic developer-style review (or incident update / release note) as a parent message, then decorate with reactions and thread replies.

This is a composition workflow — no command is unique to it. The contract is the sequence and the discipline.

**Decide**
- Pick the channel. Unfamiliar destination? Preflight with → ` + "`discover_destination`" + `.
- Pick the body shape: prose + bullets + inline code → Markdown via stdin; specific Slack rendering → ` + "`--blocks`" + `.
- High-visibility (incidents, release, exec channels)? Run the parent with ` + "`--dry-run`" + ` first.

**Run** (compose → send → decorate → verify)
1. Compose one parent body in stdin (real newlines, NOT literal ` + "`\\n`" + `). Use mrkdwn naturally: bold section labels, bullet lists, numbered requested changes, inline code, links, emoji.
2. Send parent: ` + "`slick message send --channel <id> --file - --output=json`" + `
3. Save ` + "`data.message.channel`" + `, ` + "`data.message.ts`" + `, ` + "`data.permalink`" + `.
4. React: ` + "`slick react add --channel <id> --timestamp <parent-ts> --emoji eyes --output=json`" + ` (or ` + "`white_check_mark`" + `, etc.)
5. Reply with detail: ` + "`slick reply --channel <id> --parent <parent-ts> --file - --output=json`" + `
6. Verify: ` + "`slick react list --channel <id> --timestamp <parent-ts> --output=json`" + ` + ` + "`slick history list --channel <id> --thread <parent-ts> --output=json`" + `

**Save**
- Carry ` + "`data.message.ts`" + ` (from step 2) as ` + "`parent-ts`" + ` through every subsequent step. It is the join key for the whole review.

**Behavior**
- Attribution auto-attaches (toggle / override): see → ` + "`core_contract`" + `.
- Slack history text flattens display formatting; for exact visual rendering use the Slack UI or compare ` + "`blocks`" + `.

**Next**
- Composes: this workflow composes → ` + "`send_msg`" + ` + → ` + "`react`" + ` + → ` + "`reply`" + ` + → ` + "`read_history`" + `.
- Then: → ` + "`cleanup_msgs`" + ` after live-test runs (include a distinctive run ID in the body for findability).

## cleanup_msgs
Goal: After a live test, delete every message tagged with a distinctive run ID — even ones spread across channels.

This composes → ` + "`search_msgs`" + ` and → ` + "`delete_msg`" + ` in a loop. The loop is the contract.

**Decide**
- Tag every live-test message with a unique run ID up front (e.g. ` + "`slick-live-2026-05-26-abcd`" + `). Without it, cleanup is guesswork.
- Use modest parallelism for the delete fan-out — separate CLI processes do NOT share proactive throttle state.

**Run** (loop until search returns zero matches, capped)
1. Discover: ` + "`slick lookup messages --query <run-id> --max-items <n> --output=json`" + `
2. Paginate until ` + "`meta.pagination.has_more=false`" + `: re-run with ` + "`--cursor <meta.pagination.next_cursor>`" + `.
3. Build targets ONLY from ` + "`data.matches[].channel.id`" + ` + ` + "`data.matches[].ts`" + `. Never delete by snippet, visual order, or plain output.
4. Delete each: ` + "`slick message delete --channel <id> --timestamp <ts> --force --output=json`" + `.
5. Repeat steps 1–4 until paginated search returns zero matches OR an iteration cap fires (see Behavior).

**Behavior**
- Hard cap: stop after 10 outer loop iterations or if match count fails to decrease across two consecutive loops. A non-decreasing count means the query matches non-test content; escalate to the user with the residue rather than looping forever.
- On structured ` + "`rate_limit`" + ` errors, sleep ` + "`errors[0].retry_after_seconds`" + ` before retrying that target.
- ` + "`message_not_found`" + ` = already clean (another worker won the race). Treat as success.
- A single ` + "`--max-items`" + ` page is NOT proof of cleanup — search-result windows can shift mid-delete, so the outer re-run loop is mandatory.

**Recover**
| Symptom | Cause | Next |
|---|---|---|
| Loop never terminates | Run-ID query matches non-test content | Tighten query (add timestamp prefix, channel filter, or longer ID) |
| Persistent ` + "`cant_delete_message`" + ` | Different identity posted some matches | Either accept residue or → ` + "`auth_setup`" + ` switch profile and re-run |

**Next**
- Composes: → ` + "`search_msgs`" + ` (discover) + → ` + "`delete_msg`" + ` (execute).

## discover_destination
Goal: Resolve a channel name/alias/ID to the exact Slack conversation ID and confirm the identity is a member before posting.

**Decide**

# question
- I know the exact ID: skip → ` + "`send_msg`" + ` directly (but consider an exact lookup to check ` + "`is_member`" + ` first).
- I know a name or alias: exact lookup.
- I'm browsing: filtered list.

# scope (narrow when possible)
- Public + private channels: ` + "`--types public_channel,private_channel`" + `
- Existing DMs only: ` + "`--types im`" + `
- Group DMs: ` + "`--types mpim`" + `
- Everything: ` + "`--types all`" + ` (use sparingly)

**Run**
- Exact lookup: ` + "`slick lookup channel --channel <id-or-alias> --output=json`" + `
- Browse channels: ` + "`slick lookup channel --types public_channel,private_channel --max-items <n> --output=json`" + `
- List existing DMs: ` + "`slick lookup channel --types im --max-items <n> --output=json`" + `

**Save**
> Requires ` + "`--output=json`" + `.
- ` + "`data.channels[].id`" + ` [string, required] — pass verbatim to ` + "`--channel`" + ` elsewhere. Prefer over display names (names can change).
- ` + "`data.channels[].is_member`" + ` [bool, required] — false ⇒ bot/user identity must be invited first.
- ` + "`data.channels[].is_archived`" + ` [bool, required] — true ⇒ posts will fail.

**Behavior**
- Plain mode renders human tables; JSON mode is the agent contract.
- Channel IDs are stable (` + "`C…`" + `, ` + "`G…`" + `, ` + "`D…`" + `, ` + "`MP…`" + `); display names are not.

**Next**
- Then: → ` + "`send_msg`" + ` once ` + "`is_member=true`" + ` and ` + "`is_archived=false`" + `.
- Alternative: → ` + "`lookup_user`" + ` for DM targets by user identity instead of conversation ID.
- Composes: → ` + "`cache_metadata`" + ` (` + "`slick cache channels`" + `) keeps this and shell completion cheap across many calls.

## inspect_schema
Goal: Self-discover commands, flags, output contracts, and other workflows from inside the CLI itself.

**Decide**
- Need the full inventory: ` + "`slick agent schema --output=json`" + `.
- Want a smaller command tree: ` + "`slick agent schema --compact`" + ` (shape change). NOTE: ` + "`--compact`" + ` (no value) is different from ` + "`--output=compact`" + ` (envelope strip only).
- Just want workflow names: ` + "`slick agent guide`" + ` (no args).
- Want one workflow's runbook: ` + "`slick agent guide <workflow>`" + `.

**Run**
- Full schema: ` + "`slick agent schema --output=json`" + `
- Compact: ` + "`slick agent schema --compact --output=json`" + `
- Workflow list: ` + "`slick agent guide`" + `
- Workflow runbook: ` + "`slick agent guide <workflow>`" + `

**Save**
- ` + "`schema.Commands`" + ` — visible commands + flags + input/output shapes (hidden probationary entries are excluded).
- ` + "`schema.Workflows`" + ` — workflow names; may mention probationary workflows with status text.
- ` + "`schema.EnvVars`" + `, ` + "`schema.ExitCodes`" + `, ` + "`schema.Examples`" + `, ` + "`schema.BestPractices`" + `, ` + "`schema.AntiPatterns`" + ` — reference matter.

**Behavior**
- The old root ` + "`slick schema`" + ` alias was removed — discovery lives under ` + "`agent schema`" + `.
- Examples are examples, NOT guaranteed-safe invocations. Apply ` + "`--dry-run`" + ` to mutations first.
- Conflict resolution: if a guide section disagrees with ` + "`agent schema`" + ` on flags or command existence, trust the schema (it's generated from the cobra tree) and file a bug against the guide.

**Next**
- Then: the workflow you discovered — every workflow name in this guide doubles as a ` + "`slick agent guide <name>`" + ` argument.

## lookup_user
Goal: Resolve a name, email, or partial filter to a stable Slack user ID (and optionally presence, status, timezone) before DM or human-aware work.

**Decide**

# question
- I have an exact user ID: confirm with exact lookup if you need presence/status/tz.
- I have part of a name: filter list.
- I'm browsing: paginated list.

# extras (opt-in)
- ` + "`--presence`" + ` — only when the token has presence visibility.
- ` + "`--include-deleted`" + ` — list mode excludes deactivated users by default; opt in only for audits.

**Run**
- List: ` + "`slick lookup user --max-items <n> --output=json`" + `
- Filter: ` + "`slick lookup user --filter <text> --max-items 20 --output=json`" + `
- Exact: ` + "`slick lookup user --user <user-id> --output=json`" + `
- Cache (prime for shell completion + repeat discovery): ` + "`slick cache users --output=json`" + `

**Save**
> Requires ` + "`--output=json`" + `.
- ` + "`data.users[].id`" + ` [string, required] — feed to ` + "`message send --user`" + ` and elsewhere.
- ` + "`data.users[].tz`" + ` [string, optional] — IANA tz for scheduling-aware logic.
- ` + "`data.users[].presence`" + ` [string, optional] — only when ` + "`--presence`" + ` is set AND the token allows it.

**Behavior**
- Prefer IDs (` + "`U123…`" + `) in downstream commands; display names can change without warning.
- Missing presence / custom-status fields are NOT errors; they reflect scope/policy.

**Next**
- Then: → ` + "`send_dm`" + ` (the DM-by-user wrapper).
- Composes: → ` + "`cache_metadata`" + ` to keep this cheap across many calls.

## cache_metadata
Goal: Prime, refresh, or clear local caches of active users and channels so shell completion and repeat lookups don't hit Slack.

**Decide**
- Priming for completion / live tests: ` + "`cache users`" + ` and/or ` + "`cache channels`" + `.
- Forcing a refresh: add ` + "`--refresh`" + ` to ignore the TTL.
- Resetting one cache or all: ` + "`cache clear <resource>`" + ` or ` + "`cache clear`" + ` (no arg = all).

**Run**
- Prime users: ` + "`slick cache users --output=json`" + ` (active only — deactivated users stay out)
- Prime channels: ` + "`slick cache channels --output=json`" + ` (active public + private + IM + MPIM)
- Force refresh: append ` + "`--refresh`" + `
- Tune freshness: ` + "`--ttl-minutes 10080`" + ` for a weekly window (default 1440)
- Tune pagination on large workspaces: ` + "`--page-size <n>`" + ` + ` + "`--max-pages <n>`" + `
- Clear one resource: ` + "`slick cache clear users`" + ` or ` + "`slick cache clear channels`" + `
- Clear all for the active profile: ` + "`slick cache clear`" + `

**Save**
> Requires ` + "`--output=json`" + `.

Clear one resource:
- ` + "`{profile, resource, cleared}`" + ` — ` + "`cleared=true`" + ` ⇒ removed something; ` + "`false`" + ` ⇒ cache already empty.

Clear all:
- ` + "`{profile, resources}`" + ` — ` + "`resources[]`" + ` lists removed names. Empty/absent slice ⇒ nothing removed.
- On partial failure: ` + "`errors[0].details.partial`" + ` carries resources removed before the failure.

**Behavior**
- Cache files live under XDG cache home, normally ` + "`~/.cache/slick/<profile>/`" + `.
- Caches are metadata only — they never store tokens or message content.
- Shell completion prefers fresh cached entries before falling back to live Slack API calls.

**Next**
- Then: any → ` + "`lookup_user`" + ` / → ` + "`discover_destination`" + ` invocation now hits the cache first.

## send_dm
Goal: Send a direct message to a user by ID or Slack-profile email, returning the DM conversation ID for follow-up.

This is → ` + "`send_msg`" + ` with ` + "`--user`" + ` instead of ` + "`--channel`" + `. Same body shapes, same envelope.

**Decide**

# recipient
- One user by ID (preferred): ` + "`--user U123...`" + `
- One user by Slack-profile email: ` + "`--user alice@example.com`" + ` (needs ` + "`users:read.email`" + `)
- Group DM: repeat ` + "`--user`" + ` or comma-separate values

# unsure of the ID?
- Preflight → ` + "`lookup_user`" + `.

# body
- Same as → ` + "`send_msg`" + ` (` + "`--message`" + ` / ` + "`--file -`" + ` / ` + "`--blocks`" + `).

# modifiers
- Schedule: add ` + "`--schedule <when>`" + ` (real scheduled DM sends still return the raw DM/MPIM ID in ` + "`data.channel`" + ` — usable as a ` + "`schedule_msg`" + ` delete target).

**Run**
- ID: ` + "`slick message send --user <user-id> --message <markdown> --output=json`" + `
- Email: ` + "`slick message send --user <slack-profile-email> --message <markdown> --output=json`" + `
- Stdin: ` + "`printf '%s\\n' \"$body\" | slick message send --user <id-or-email> --file - --output=json`" + `
- Group DM: ` + "`slick message send --user U123 --user U456 --message <markdown> --output=json`" + `

**Save**
> Requires ` + "`--output=json`" + `.
- ` + "`data.message.channel`" + ` [string, required] — the DM/MPIM conversation ID Slack opened. Reuse for ` + "`read_history`" + `, ` + "`reply`" + `, etc.
- ` + "`data.message.ts`" + ` [string, required] — message timestamp.

**Preconditions**
- Slack decides whether the active token (bot vs user) may open the DM at all.
- For "DM anyone as me", a user-token profile is the normal choice; bot-token profiles depend on app install + conversation access.
- ` + "`--dry-run`" + ` verifies composition but cannot prove Slack will open the DM.

**Behavior**
- ` + "`message send --user`" + ` opens the DM through Slack before posting; the returned ` + "`data.message.channel`" + ` is that opened conversation.
- Email recipients route through Slack ` + "`users.lookupByEmail`" + ` first; ` + "`users_not_found`" + ` means the email isn't a Slack profile in this workspace.

**Recover**
| Symptom | Cause | Next |
|---|---|---|
| ` + "`not_found: users_not_found`" + ` | Email doesn't match a Slack profile here | → ` + "`lookup_user`" + ` with filter, then retry with the ID |
| ` + "`auth_failure: missing_scope`" + ` (` + "`users:read.email`" + `) | Email targeting without the scope | → ` + "`auth_setup`" + ` |
| ` + "`auth_failure: no_permission`" + ` (open DM) | Bot can't DM this person | Switch to user-token profile or invite into a shared channel first |

**Next**
- Then: → ` + "`reply`" + ` / → ` + "`react`" + ` on the new DM message.

## set_status
Goal: Set or clear the authenticated user's Slack profile status (text, emoji, optional expiration).

**Decide**

# action
- Set: ` + "`slick status set`" + `
- Clear: ` + "`slick status clear`" + `

# body (set only)
- Flagged: ` + "`--text <text> --emoji :headphones: --expires-in 2h`" + `
- Positional shorthand: ` + "`slick status set \"In a meeting\" :calendar:`" + `

# guard
- ` + "`--dry-run`" + ` previews the status payload without calling Slack.

**Run**
- Set (flagged): ` + "`slick status set --text \"deep work\" --emoji :headphones: --expires-in 2h --output=json`" + `
- Set (positional): ` + "`slick status set \"In a meeting\" :calendar: --output=json`" + `
- Clear: ` + "`slick status clear --output=json`" + `

**Save**
> Requires ` + "`--output=json`" + `.
- ` + "`data.text`" + `, ` + "`data.emoji`" + ` [string, optional] — echo of what was set; absent on clear.
- ` + "`data.expiration`" + ` [int64, optional] — Unix seconds when present.
- ` + "`meta.command`" + ` is ` + "`status.set`" + ` or ` + "`status.clear`" + ` — branch on this, not on data fields.

**Preconditions**
- Requires a **user-token** profile with ` + "`users.profile:write`" + `. Bot tokens cannot set a user's status.

**Recover**
| Symptom | Cause | Next |
|---|---|---|
| ` + "`auth_failure: missing_scope`" + ` (` + "`users.profile:write`" + `) | Bot-token profile or missing scope | → ` + "`auth_setup`" + ` with a user-token profile |

**Next**
- Alternative: → ` + "`config_prefs`" + ` for non-Slack-side defaults (e.g. ` + "`default_channel`" + `).

## safe_mutation
Goal: A cross-cutting checklist for ` + "`send_msg`" + `, ` + "`reply`" + `, ` + "`edit_msg`" + `, ` + "`delete_msg`" + `, ` + "`react`" + `, and ` + "`upload_file`" + ` against high-visibility targets (incidents, release, exec channels).

This is not a command — it's the discipline that wraps the mutating workflows.

**Decide**

# is this destination high-impact?
- Yes (incident, release, exec, prod alerts) → preflight + dry-run + recorded evidence.
- No → still keep JSON output, but skip the recorded-evidence step.

# what stage am I in?
- Pre-flight: read-only lookup with the same profile.
- Pre-write: ` + "`--dry-run`" + ` to validate the payload.
- Real write: machine-parseable invocation.
- Post-write: record evidence.

**Run** (sequence, per mutation)
1. Preflight: → ` + "`discover_destination`" + ` (channel known + ` + "`is_member=true`" + `) or → ` + "`read_history`" + ` (right message ts).
2. Dry-run: same command with ` + "`--dry-run --output=json`" + `; verify payload shape and ts placeholders.
3. Real write: drop ` + "`--dry-run`" + `, keep ` + "`--output=json`" + `, **add the target workflow's per-command confirm flags** (` + "`--force`" + ` for → ` + "`delete_msg`" + `, ` + "`--blocks`" + ` when the input is raw Block Kit, etc. — defer to the target's Decide block).
4. Evidence (for important writes): record returned ` + "`data.message.channel`" + `, ` + "`data.message.ts`" + `, ` + "`data.permalink`" + `, and the dry-run flag value.

**Preconditions**
- Always ` + "`--output=json`" + ` for automation — never parse ` + "`--output=human`" + ` (it's display-only).
- Parse stderr JSON for ` + "`type`" + ` / ` + "`message`" + ` / ` + "`exit_code`" + ` on failure; do NOT scrape human text.

**Behavior**
- Rate limits map to exit code ` + "`3`" + ` + structured ` + "`rate_limit`" + ` errors with ` + "`errors[0].retry_after_seconds`" + `. Sleep before retrying that target.
- ` + "`chat.postMessage`" + ` has same-channel limits. Separate CLI processes do NOT share proactive throttle state, so shell fanout can still hit ` + "`rate_limit`" + ` even with conservative single-process pacing.

**Recover**
| Symptom | Cause | Next |
|---|---|---|
| ` + "`rate_limit`" + ` (exit ` + "`3`" + `) | Same-channel or workspace fanout exceeded Slack budget | Sleep ` + "`errors[0].retry_after_seconds`" + `, retry. Reduce parallelism. |
| ` + "`validation_error`" + ` requesting ` + "`--force`" + ` | Real write to ` + "`delete_msg`" + ` without the confirm flag | Add ` + "`--force`" + ` (delete is the only workflow that requires it). |
| Unexpected ` + "`validation_error`" + ` after dry-run was clean | Real Slack state diverged (channel archived, user deactivated) between dry-run and write | Re-preflight, then retry |
| Exit ` + "`6`" + ` (canceled) mid-flight | Caller (agent or user) interrupted the command | Treat as unfinished — re-preflight before any retry; do not assume Slack state matches your last dry-run. |

**Next**
- Composes: every mutating workflow listed in the Goal can be wrapped in this pattern.
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
