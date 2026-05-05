---
name: slack-cli
description: Use when the user asks about Slack messages, channels, DMs, threads, reactions, users, Slack auth, app manifests, or anything that needs the matcra587/slack-cli binary
allowed-tools: Bash(slack agent:*) Bash(slack auth:*) Bash(slack config:*) Bash(slack manifest:*) Bash(slack message:*) Bash(slack history:*) Bash(slack lookup:*) Bash(slack react:*) Bash(slack reply:*) Bash(slack workspace:*)
---

# slack-cli

Operational skill for `matcra587/slack-cli`. The binary embeds its own
authoritative reference. **Read it first:**

```sh
slack agent guide              # full operational guide (markdown)
slack agent guide --help       # workflow list
slack agent schema --compact   # command tree + flag signatures (JSON)
```

This skill points to the embedded guide and records the contracts, gotchas, and
live-tested patterns that trip agents up before they read it.

## When to use

- Send, reply to, edit, delete, or react to Slack messages
- Read channel history or thread replies
- Look up channels, DMs, or users before posting
- Send DMs with `message send --user`
- Manage Slack auth, profiles, config preferences, or app manifests
- Author Slack Markdown or raw Block Kit input

## Setup check

```sh
slack auth status --json       # credential health
slack workspace list --json    # configured profiles
slack agent guide auth_setup   # auth workflow details
```

If auth is missing: use `slack manifest template --preset messaging --type user`
to generate an importable Slack app manifest, then run `slack auth login`.
OAuth uses PKCE, needs the app client ID, and derives the workspace ID/name from
Slack. Token auth is also supported, but never put tokens in argv: use
`--token-stdin`, `--token-file`, or `--token-env <VAR>`.

## Output mode contract

stdout is command data. stderr is diagnostics and structured errors.

| Flag | Effect |
| --- | --- |
| `--json` | Full envelope: `{meta, data, errors}` |
| `--compact` | Success-path data only |
| `--plain` | clog human output |
| `--raw` | API-native output shape where supported |

The output flags are mutually exclusive. Exit codes: `0` success, `1` auth,
`2` not-found, `3` rate limit, `4` validation, `5` server error.

TTY output is human-readable. Non-TTY and detected agent contexts use JSON.

## Auth and config

Use auth commands for credentials:

```sh
slack auth login
slack auth status
slack auth switch <workspace>
slack auth logout <workspace>
```

Use config commands for preferences only:

```sh
slack config init
slack config set workspaces.default.default_channel C1234567890
slack config set workspaces.default.attribution.enabled true
slack config set workspaces.default.attribution.message "Sent via slack-cli"
```

Config lives at `~/.config/slack-cli/config.toml` or `SLACK_CLI_CONFIG`.
Credentials are stored as structured keychain secrets or secret refs such as
`op://...`; plaintext `xox*` values do not belong in TOML.

## Destinations and timestamps

Look up destinations before writing:

```sh
slack lookup channel --max-items 20 --json
slack lookup channel --types im --json
slack lookup user --filter ansible --max-items 20 --json
slack lookup user --user U1234567890 --json
```

For DMs, parse `data.users[].id` and send with `message send --user`. There is
no `slack dm` command.

Slack timestamps are channel-scoped strings like `1746284582.123456`. Keep them
as strings. Get them from `data.message.ts` after sending or from
`data.messages[].ts` in history output. Use:

- parent `ts` with `slack reply --parent`
- any message or reply `ts` with `slack react add --timestamp`
- exact channel + `ts` with edit/delete

## Sending and replying

```sh
slack message send --channel C123 --message "Deploy **complete**" --json
echo "multiline" | slack message send --channel C123 --file - --json
slack message send --user U123 --message "Build artifact is ready" --json
slack reply \
  --channel C123 \
  --parent 1746284582.123456 \
  --message "Investigating" \
  --json
```

`--channel` and `--user` are mutually exclusive. Use `--blocks` only when the
input body is already a raw Slack Block Kit JSON array. `--raw` is an output
mode, not raw input.

Markdown input is converted to Block Kit. Unsupported block-level Markdown is
preserved as readable section text instead of being dropped.

## Attribution

Sent messages may include a Block Kit context block. Agent envs and `--agent`
enable agent-mode wording; profile `attribution.enabled = true` can force plain
slack-cli attribution even without an agent env. Override presentation with
`--agent-emoji`, `--agent-message`, or config keys under
`workspaces.<profile>.attribution.*`.

Use `--no-agent-attribution` only when the user explicitly wants no visible
automation attribution.

## Reading, replying, and reacting

```sh
slack history list --channel C123 --max-items 50 --json
slack history list --channel C123 --thread 1746284582.123456 --max-items 10 --json
slack react add --channel C123 --timestamp 1746284582.123456 --emoji eyes --json
slack react list --channel C123 --timestamp 1746284582.123456 --json
slack react remove --channel C123 --timestamp 1746284582.123456 --emoji eyes --json
```

Thread replies are normal reaction targets: use the reply's own `ts`.

## Mutations

Use dry-run before high-impact sends, replies, edits, deletes, reactions, or
file uploads.

```sh
slack message edit \
  --channel C123 \
  --timestamp 1746284582.123456 \
  --message "Corrected" \
  --json
slack message delete \
  --channel C123 \
  --timestamp 1746284582.123456 \
  --force \
  --json
```

Deletes require `--force` unless `--dry-run` is used. The CLI requires exact
channel + timestamp; it intentionally does not support "last message" or search
result indexes for destructive operations.

## Probationary surfaces

`lookup messages` and `file upload` are implemented but hidden from help and
shell completion. Use them only when explicitly testing those workflows, and
read their embedded guides first:

```sh
slack agent guide search_msgs
slack agent guide upload_file
```

## Gotchas

- There is no `slack dm`, `slack thread`, or `slack reaction` command.
- Use `slack reply`, `slack react`, and `slack message send --user`.
- Do not parse `--plain` output in automation.
- Do not pass Slack tokens in argv; use stdin, file, env-name, keychain, or
  1Password.
- Slack may return permission errors even after local scope checks; preserve the
  structured error.
- `missing_scope`/`no_permission` are auth failures; `channel_not_found`,
  `user_not_found`, and `not_in_channel` are not-found errors.

## When this skill isn't enough

Read the embedded guide and schema. They are kept in lockstep with the binary.

```sh
slack agent guide
slack agent schema --compact
```
