---
slug: send-msg
title: "Send a markdown message and read ts/permalink from JSON"
description: "Send a markdown message and read ts/permalink from JSON"
when_to_use: "Before following the send msg workflow in slick."
commands: ["slick message send"]
aliases: ["send_msg"]
---

## Decide

Goal: Post a message to a channel, user, or group DM and capture the Slack timestamp and permalink for follow-up.

### target

*   Channel: `--channel <id-or-alias>` (falls back to `default_channel` only when not scheduled).
*   User: `--user <user-id-or-email>` (repeat or comma-separate for group DM).

### body

*   One line: `--message <markdown>`.
*   Multiline / generated: `--file -` (pipe stdin; real newlines, not `\n`).
*   Raw Block Kit: `--blocks` (required for real mentions and explicit `<url|label>` links).

### modifiers

*   Future send: `--schedule <RFC3339|Go-duration|Unix-seconds>` (no natural language).
*   `--dry-run` validates locally and returns `ts="dry-run"`.

### Workflow checkpoints

*   Choose workspace/profile
*   Pass message body with --message or --file -
*   Use --channel or --user, never both
*   Use Slack-profile email for DM --user targeting
*   Use --schedule with --channel or --user and RFC3339/duration/Unix seconds
*   Use --blocks only for raw Block Kit input
*   Dry-run first for high-visibility sends
*   Read JSON response for ts/permalink or scheduled_message_id

## Run

*   Canonical: `slick message send --channel <id-or-alias> --message <markdown> --output=json`
*   Adapt: swap `--channel`→`--user` for DMs; replace `--message` with `--file -` for stdin or with `--blocks --file blocks.json` for raw Block Kit; append `--schedule 90m` to schedule.

### Behavior

*   Markdown is escaped (`&`, `<`, `>` → entities); sentinels `<!channel>`, `<@U123>`, `<url|label>` in `--message` render as literal text — use `--blocks` for wire syntax.
*   Attribution auto-attaches (toggle / override): see → `core_contract`.

## Save

Real sends return Slack's message text, author, and blocks when present. The
receipt can differ from the submitted text. Dry runs return the planned input.

> Requires `--output=json`.

Immediate: `data.message.ts`, `data.message.thread_ts` (when threaded), `data.permalink`, `data.message.channel` / `data.message.channel_url`.

Scheduled: `data.channel` (raw ID), `data.scheduled_message_id` (→ `schedule_msg` to cancel); `data.message.ts` absent until Slack posts.

Dry run: `data.message.ts == "dry-run"` or `data.scheduled_message_id == "dry-run"`.

## Preconditions

*   `--channel` and `--user` are mutually exclusive; scheduled sends ignore `default_channel` and need an explicit target. Email targeting needs `users:read.email`.

## Recover

| Symptom | Cause | Next |
|---|---|---|
| `validation_error: --channel or --user is required` | No target, no `default_channel` | Retry with explicit target |
| `not_found: not_in_channel` | Token identity not in channel | Invite, or → `auth_setup` for user-token profile |
| `not_found: users_not_found` | Email not a Slack profile here | → `lookup_user`, retry with `--user U…` |
| `auth_failure: missing_scope` | Manifest lacks `chat:write` / `users:read.email` | → `auth_setup` |
| `validation_error` on `--schedule` | Past, natural language, or >120 days out | Future RFC3339 / Go duration / Unix seconds |

## Next

*   Then: → `reply` / → `edit_msg` / → `react` on the saved ts.
*   Alternative: → `schedule_msg` to list or cancel pending scheduled sends.
