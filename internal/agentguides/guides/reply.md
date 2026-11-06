---
slug: reply
title: "Reply to a message thread by parent timestamp"
description: "Reply to a message thread by parent timestamp"
when_to_use: "Before following the reply workflow in slick."
commands: ["slick reply"]
---

## Decide

Goal: Post a threaded reply to a parent message, returning the new `ts` and the shared `thread_ts` for follow-ups.

### target

*   `--channel <id>` + `--parent <parent-ts>` (parent ts comes from `send_msg` `data.message.ts` or `read_history`).

### body

*   One line: `--message <markdown>`
*   Multiline or generated: pipe to `--file -`
*   Raw Block Kit: `--blocks` (required for real mentions / explicit `<url|label>` links)

### guards

*   Dry run: `--dry-run` validates locally and returns `thread_ts` without calling Slack.

### Workflow checkpoints

*   Use channel ID and parent message timestamp
*   Post with reply --parent
*   Use --file - for multiline replies
*   Use --blocks only for raw Block Kit replies
*   Read JSON response for reply ts and thread_ts

## Run

*   Message: `slick reply --channel <id> --parent <parent-ts> --message <markdown> --output=json`
*   Stdin: `printf '%s\n' "$reply" | slick reply --channel <id> --parent <parent-ts> --file - --output=json`
*   Raw Block Kit: `slick reply --channel <id> --parent <parent-ts> --blocks --file reply.json --output=json`

### Behavior

*   Markdown is escaped on the wire (`&`, `<`, `>` → HTML entities). Sentinels like `<!channel>`, `<@U123>`, `<#C123>`, `<url|label>` in `--message` render as literal text — they do NOT fire mentions. Use `--blocks` to send real mentions or explicit linked text.
*   Attribution auto-attaches (toggle / override): see → `core_contract`.

## Save

> Requires `--output=json`.
*   `data.message.ts` [string, required] — the reply's own ts (use for `react` / `edit_msg` on the reply).
*   `data.message.thread_ts` [string, required] — shared thread anchor; equals `--parent` on the first reply.
*   `data.permalink` [string, required] — Slack URL.

## Preconditions

*   `--parent` is the parent message timestamp, not a permalink or a search-result index.
*   The parent must still exist; a deleted parent breaks the thread.

## Recover

| Symptom | Cause | Next |
|---|---|---|
| `not_found: message_not_found` | Parent ts wrong or parent deleted | → `read_history` to re-find the parent |
| `thread_not_found` | Parent isn't in this channel | Verify `--channel` matches the parent's channel |

## Next

*   Then: → `read_history` with `--thread <thread_ts>` to see the full thread.
*   Alternative: → `edit_msg` or `react` on the reply's own `ts`.
