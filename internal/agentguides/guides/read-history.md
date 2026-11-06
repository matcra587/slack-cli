---
slug: read-history
title: "Read channel history or thread replies with bounded pagination"
description: "Read channel history or thread replies with bounded pagination"
when_to_use: "Before following the read history workflow in slick."
commands: ["slick history list"]
aliases: ["read_history"]
---

## Decide

Goal: Discover message timestamps in a channel or thread so other workflows (`react`, `reply`, `edit_msg`, `delete_msg`) have exact `ts` values to act on.

### scope

*   Channel timeline: `--channel <id>` only.
*   Thread: add `--thread <parent-ts>` to get only that thread's replies.

### narrowing

*   Time window: `--since <slack-ts>`, `--until <slack-ts>` (Slack ts strings, not RFC3339).
*   Author: `--user <user-id>`.

### bound

*   Each invocation fetches one page. Pass `--max-items <n>` to set its size: 1–999 for channel history or 1–1000 with `--thread`. Omitted or zero uses Slack's default.
*   To include replies alongside channel messages, pass `--include-replies`. Add `--reply-limit <n>` to request 1–999 replies per parent. Omitted or zero uses Slack's page default. Do not combine `--include-replies` with `--thread`.

### Workflow checkpoints

*   Use history list for parent messages
*   Use --thread for replies
*   Bound output with --max-items and time filters
*   Parse next cursors from meta.pagination
*   Use JSON for structured message data and plain output for humans

## Run

*   Channel: `slick history list --channel <id> --max-items <n> --output=json`
*   Thread: `slick history list --channel <id> --thread <parent-ts> --max-items <n> --output=json`
*   Filtered: append `--since` / `--until` / `--user`
*   Next page: `slick history list --channel <id> --cursor <meta.pagination.next_cursor> --max-items <n> --output=json`

### Behavior

*   Plain mode (TTY / `--output=human`) renders a table for humans; JSON mode is the agent contract.
*   A reply's `ts` is its own message ts; the thread anchor is its `thread_ts`.

## Save

> Requires `--output=json`.
*   `data.messages[].ts` [string, required] — feed to `react --timestamp`, `reply --parent` (when it's a parent), `message edit/delete --timestamp`.
*   `data.messages[].text` [string] and `data.messages[].blocks` [array, optional] — text may flatten display formatting; check `blocks` when Slack-native structure matters.
*   `data.messages[].attachments` / `.files` [arrays, optional] — preserve SDK-decoded attachment content, forwarded files, and file metadata. Empty or missing arrays are omitted. slick does not fetch file URLs; `file_access` can describe restricted metadata.
*   `data.messages[].client_msg_id` [string, optional] and `.edited` [object, optional] — retain the client identifier and editing user/timestamp when Slack returns them. Keep timestamp strings exact.
*   `data.messages[].permalink` [string, optional] — present when fetched.
*   `meta.pagination.next_cursor` / `.has_more` [string / bool] — paginate until `has_more=false`.
*   When `has_more=true` without a cursor, use `data.pagination.next_until` as `--until` for channel history, or `next_since` as `--since` for a thread. Keep other filters and omit the old cursor. A filtered empty page can still have a continuation.
*   With `--include-replies`, inspect each parent's `replies_pagination` separately. To continue it, use `--thread <parent-ts>` and that object's next cursor or time boundary.

## Preconditions

*   Channel inputs, stored aliases, thread/time bounds, and user IDs trim surrounding whitespace. Empty optional values clear that filter; an empty thread value selects channel history. Keep timestamp digits unchanged and pass opaque cursors verbatim.

## Recover

| Symptom | Cause | Next |
|---|---|---|
| Empty `data.messages[]` | Filter too tight or channel actually empty | Drop `--since/--until/--user` and re-run |
| `channel_not_found` | Bot/user identity isn't in the channel | → `discover_destination` then invite |
| `auth_failure: missing_scope` | Manifest lacks `channels:history` / `groups:history` / `im:history` / `mpim:history` for the conversation type | → `auth_setup` |

## Next

*   Then: → `react`, `reply`, `edit_msg`, `delete_msg` (all need the saved ts).
*   Alternative: → `search_msgs` for workspace-wide discovery instead of one channel.
