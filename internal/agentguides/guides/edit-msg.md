---
slug: edit-msg
title: "Edit own messages by exact channel and timestamp"
description: "Edit own messages by exact channel and timestamp"
when_to_use: "Before following the edit msg workflow in slick."
commands: ["slick message edit"]
aliases: ["edit_msg"]
---

## Decide

Goal: Rewrite the text or Block Kit body of one of the authenticated identity's own messages, in place.

### target

*   `--channel <id>` + `--timestamp <message-ts>` (timestamps are unique only inside a channel — always pass both).

### replacement body

*   One line: `--message <markdown>`
*   Stdin: `--file -`
*   Raw Block Kit: `--blocks` + `--file <json>`

### guard

*   `--dry-run` validates locally without mutating Slack — use it before editing incident, release, or high-visibility channels.

### Workflow checkpoints

*   Use channel ID and exact message timestamp
*   Edit only own messages
*   Preview high-impact edits with --dry-run
*   Use --blocks only for raw Block Kit replacements

## Run

*   Markdown: `slick message edit --channel <id> --timestamp <ts> --message <markdown> --output=json`
*   Stdin: `printf '%s\n' "$body" | slick message edit --channel <id> --timestamp <ts> --file - --output=json`
*   Raw Block Kit: `slick message edit --channel <id> --timestamp <ts> --blocks --file edit.json --output=json`

### Behavior

*   Configured channel aliases resolve in previews and real requests. Pass an explicit channel; `default_channel` is not used. Surrounding target whitespace is removed; timestamps stay exact strings.

*   Markdown is escaped on the wire (`&`, `<`, `>` → entities); sentinels — `<!channel>`, `<@U123>`, `<#C123>`, `<url|label>` — render as literal text and do NOT fire mentions. Use `--blocks` for the new content to mention or link intentionally.

## Save

> Requires `--output=json`.
*   `data.message.channel`, `data.message.ts` [string, required] — echo of the target.
*   `data.message.text` [string] — SDK-returned post-edit text, including empty. The SDK also uses empty when response text is omitted. Dry runs show requested text.
*   Returned `blocks` are NOT included; if you need to verify rendered Block Kit, → `read_history` or check the Slack UI.

## Preconditions

*   Empty or whitespace-only bodies reject locally, including file/stdin sources. Use raw `[]` only for an explicit existing-message block clear.

*   Raw Block Kit supplies `blocks` without literal JSON notification text. Raw previews expose validated blocks, omitting unknown fields. Scheduled raw receipts include the submitted `data.blocks` plan with empty `data.text`. Markdown keeps readable source fallback; removing `--blocks` restores it.

*   Slack only allows editing messages owned by the token identity; bot tokens can only edit bot-posted messages, user tokens only user-posted ones.

## Recover

| Symptom | Cause | Next |
|---|---|---|
| Invalid Slack acknowledgement (`server_error`, exit 5) | The response did not confirm a usable result | Inspect Slack before retrying; a mutation may already have happened. Slick does not replay it automatically. |
| `cant_update_message` / `message_not_found` | Wrong owner, wrong ts, or message deleted | Stop — return the structured error. Do NOT fall back to delete+send (creates a new ts and breaks references). |
| `auth_failure: missing_scope` | Manifest lacks `chat:write` | → `auth_setup` |

## Next

*   Then: → `read_history` to confirm the rendered result when `--blocks` was used.
*   Alternative: if you can't edit, prefer a → `reply` correction over → `delete_msg` (preserves thread context).
