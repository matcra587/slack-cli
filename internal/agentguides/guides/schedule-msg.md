---
slug: schedule-msg
title: "Schedule, list, and delete future messages"
description: "Schedule, list, and delete future messages"
when_to_use: "Before following the schedule msg workflow in slick."
commands: ["slick message send", "slick message scheduled list", "slick message scheduled delete"]
aliases: ["schedule_msg"]
---

## Decide

Goal: Create, list, or cancel scheduled messages.

### action

*   Create: scheduling is a modifier on `message send` — append `--schedule <when>` to any `send_msg` invocation.
*   List: `slick message scheduled list` (optional `--channel` filter, `--limit`, `--cursor`).
*   Cancel: `slick message scheduled delete` — requires exact `--channel` (raw ID) and `--scheduled-id` from a prior list or send.

### when (create only)

*   Format: RFC3339 (`2026-06-01T15:00:00-04:00`), Go duration (`90m`, `2h30m`), or Unix seconds. Never natural language.
*   Range: future, within 120 days. Fractional inputs round down to whole Unix seconds before validation; if that second is not in the future, previews and real sends fail locally.

### Workflow checkpoints

*   Use message send --schedule with --channel or --user
*   For scheduled DMs, pass user IDs, aliases, or Slack-profile email with --user
*   Use RFC3339, Go duration, or Unix seconds; natural language is rejected
*   Parse scheduled_message_id because scheduled sends have no ts until they fire
*   For real scheduled --user sends, parse data.channel as the raw DM/MPIM channel ID
*   List with message scheduled list and pagination
*   Use JSON list output for delete targets; human CHANNEL is display-only
*   Delete with exact channel and scheduled-id

## Run

*   List (all): `slick message scheduled list --limit <n> --output=json`
*   List (one channel): `slick message scheduled list --channel <id-or-alias> --limit <n> --output=json`
*   Next page: `slick message scheduled list --cursor <meta.pagination.next_cursor> --limit <n> --output=json`
*   Cancel: `slick message scheduled delete --channel <raw-channel-id> --scheduled-id <QID> --output=json`

### Behavior

*   Delete supports `--dry-run` and does not require `--force`. Slack cannot cancel a message within 60 seconds of its scheduled send time.
*   List returns pending messages scheduled by the same token through the API; it excludes messages scheduled in the Slack UI. Listing needs authentication but has no required OAuth scope.
*   List `--limit` is a page size: 0 uses Slack’s default, positive values go to Slack, and negative values fail locally.
*   Friendly metadata (`channel_name`, `channel_hr`, `channel_url`, `is_dm`, `channel_type`, `channel_user`) is best-effort and may be absent in error envelopes.

## Save

> Requires `--output=json`.

List:

*   `data.scheduled_messages[].id` [string, required] — pass to `--scheduled-id` for delete.
*   `data.scheduled_messages[].channel` [string, required] — raw Slack conversation ID for `--channel` on delete.
*   `data.scheduled_messages[].post_at` / `.post_at_iso` [int64 / string, required] — fire time.
*   `data.scheduled_messages[].text_preview` [string, optional] — capped at 200 Unicode chars.
*   `meta.pagination.has_more` / `.next_cursor` [bool / string] — paginate until `has_more=false`.

Delete:

*   `data.channel`, `data.scheduled_message_id` [string, required] — confirmation echo.

## Preconditions

*   Empty or whitespace-only bodies reject locally, including file/stdin sources. Use raw `[]` only for an explicit existing-message block clear.

*   Raw Block Kit supplies `blocks` without literal JSON notification text. Raw previews expose validated blocks, omitting unknown fields. Scheduled raw receipts include the submitted `data.blocks` plan with empty `data.text`. Markdown keeps readable source fallback; removing `--blocks` restores it.

*   Channel and recipient inputs and stored alias values trim surrounding whitespace. Blank alias targets fail locally; they never fall back to another destination. Recipient order is preserved.

*   Delete needs raw channel/DM/MPIM IDs — friendly `#channel` or `@user` labels in human output are display-only.
*   Pending sends have no `ts` until Slack posts them; correlate on `scheduled_message_id`.

## Recover

| Symptom | Cause | Next |
|---|---|---|
| Invalid Slack acknowledgement (`server_error`, exit 5) | The response did not confirm a usable result | Inspect Slack before retrying; a mutation may already have happened. Slick does not replay it automatically. |
| `not_found` on delete | Wrong `--channel` (used DM alias instead of raw ID) | Re-list, copy `data.scheduled_messages[].channel` verbatim |
| `auth_failure: missing_scope` | Slack rejected the token’s permissions; cancellation requires `chat:write` | Check the reported scopes; use → `auth_setup` if the app needs them |

## Next

*   Then: → `send_msg` (create a new scheduled send)
*   Composes: → `cleanup_msgs` (after firing, scheduled sends become regular messages — clean them up with search + delete)
