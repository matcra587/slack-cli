# slick agent

Agent tooling. `agent schema` emits the full command tree as JSON; `agent
guide` emits Markdown runbooks scoped to specific workflows. Both are
designed to be consumed by LLM agents rather than humans.

```text
slick agent schema  Output command schema as JSON
slick agent guide   Output workflow instructions for agents
```

## agent schema

Emit the command tree, flag inventory, output modes, and exit-code contract
as JSON.

```sh
slick agent schema                          # full schema, full envelope
slick agent schema --compact                # minimal schema (smaller records)
slick agent schema --output=compact         # full schema, no envelope (data only)
slick agent schema --compact -o compact     # minimal schema, no envelope
```

### Flags

```text
-c, --compact  Emit a minimal schema (smaller per-command records)
```

`--compact` here is the command-local schema-shape flag (short form `-c`).
It is distinct from the global `--output=compact` (short form `-o compact`),
which strips the JSON envelope. Both can be combined; use `-o compact` to
toggle the envelope independently.

The schema's top-level object includes:

*   `version`, `description` — schema version and a human-readable summary.
*   `auth` — supported auth shapes and required scopes.
*   `output` — output modes (`--output=auto|human|json|compact`).
*   `global_flags` — flags accepted on every command (workspace selection,
    output mode, debug, color, timeout, throttle). Attribution toggles
    (`--attribution`, `--no-attribution`, `--attribution-{label,emoji,message}`) are
    command-local on the four mutating commands (`message send`,
    `message edit`, `reply`, `file upload`) and appear under each command's
    `flags` entry rather than `global_flags`.
*   `commands` — the full command tree, with each entry carrying `name`,
    `full_path`, `description`, `read_only`, optional `flags`, and a
    recursive `subcommands` array.
*   `input_shapes`, `output_schemas` — JSON-shape descriptors for the
    structured inputs (e.g. Block Kit) and outputs each command emits.
*   `env` — environment variables slick reads, including config overrides
    and the agent-detection trigger set.
*   `exit_codes` — the 1..7 mapping with `errors[0].type` names.
*   `examples` — annotated invocation examples per command.
*   `workflows` — pointers into the `agent guide` workflow catalogue.
*   `best_practices`, `anti_patterns` — guidance an agent should follow or
    avoid (e.g. "Slack timestamps are channel-scoped strings — keep them
    as strings", "do not synthesise channel IDs from DM contexts").

This file is the source of truth for agents that need to build a command
plan without prompting the user.

## agent guide

Emit Markdown runbooks for specific workflows. Each runbook is a short
preconditions / commands / parse / quirks sequence.

```sh
slick agent guide --help          # list available workflows
slick agent guide send_msg        # one specific workflow
slick agent guide schedule_msg    # scheduled send/list/delete workflow
slick agent guide                 # the full guide (every workflow)
```

### Available workflows

```text
auth_setup            Generate a manifest and authenticate a profile
cache_metadata        Prime users and channels for repeated lookup and shell completion
cleanup_msgs          Runbook for cleaning test messages found through paginated search
config_prefs          Set profile preferences without touching auth
core_contract         Understand output modes, stderr/stdout, and fixed exit codes
delete_msg            Delete own messages with dry-run and force safeguards
developer_review      Runbook for posting a review-style message with reactions and a thread
discover_destination  Find channel and DM IDs before posting
edit_msg              Edit own messages by exact channel and timestamp
inspect_schema        Read the machine schema and workflow guide
lookup_user           Find user IDs, presence, status, and timezone
react                 Add, remove, and list emoji reactions by channel and timestamp
read_history          Read channel history or thread replies with bounded pagination
reply                 Reply to a message thread by parent timestamp
safe_mutation         Preview high-impact changes and parse JSON results
schedule_msg          Schedule, list, and delete future messages
search_msgs           Workspace message search workflow
send_dm               Send direct messages while handling token limits
send_msg              Send a markdown message and read ts/permalink from JSON
set_status            Set or clear the authenticated user's Slack status
upload_file           Probationary hidden file upload workflow
```

### Tone

Each runbook is direct, command-shaped, and stateless. The intent is
machine-parseable instructions an agent can follow without having to re-read
the whole project. The runbooks track the same JSON shapes documented in the
per-command pages of this site, including post-v0.4.0 renames (`ts`,
`id`/`name`, dropped action-result bools, `ClearData` split).

## See also

*   [`README`](https://github.com/matcra587/slack-cli#readme) — onboarding.
*   The in-repo skill at `.claude/skills/slack-cli/SKILL.md` wraps these
    outputs into a Claude Code / agent skill.
*   [README](https://github.com/matcra587/slack-cli#readme) and [index](index.md).
