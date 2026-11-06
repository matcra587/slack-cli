---
slug: delete-msg
title: "Delete own messages with dry-run and force safeguards"
description: "Delete own messages with dry-run and force safeguards"
when_to_use: "Before following the delete msg workflow in slick."
commands: ["slick message delete"]
aliases: ["delete_msg"]
---

## Decide

Goal: Permanently delete one of the authenticated identity's own messages by exact channel + ts.

*   Single deletion? Use this directly.
*   Reversible correction? Prefer → `edit_msg` (preserves thread + references).
*   Bulk by run ID? → `cleanup_msgs` (drives this workflow from search results).

### Workflow checkpoints

*   Use channel ID and exact message timestamp
*   Preview destructive deletes with --dry-run
*   Require --force for deletion
*   Prefer edit when thread context should be preserved

## Run

*   Preview (no Slack mutation): `slick message delete --channel <id> --timestamp <ts> --dry-run --force --output=json`
*   Real delete: `slick message delete --channel <id> --timestamp <ts> --force --output=json`

### Behavior

*   Deletion is irreversible. There is no undo.
*   Configured channel aliases resolve in previews and real deletes. Pass an explicit channel; deletion never uses `default_channel`. Keep the timestamp as a string.
*   Slack returns `message_not_found` for already-deleted messages — bulk callers should treat that as "already clean".

## Save

> Requires `--output=json`.
*   `data.channel`, `data.ts` [string, required] — echo of the deleted target.
*   `meta.command` is `message.delete`.

## Preconditions

*   `--force` is mandatory for real deletes (`--dry-run --force` is safe and never calls mutation endpoints).
*   Never delete by "last message", search-result index, or display position — this CLI intentionally requires exact channel + ts.
*   The token identity must own the message.

## Recover

| Symptom | Cause | Next |
|---|---|---|
| Invalid Slack acknowledgement (`server_error`, exit 5) | The response did not confirm a usable result | Inspect Slack before retrying; a mutation may already have happened. Slick does not replay it automatically. |
| `message_not_found` | Already deleted (race with another worker) | Treat as success in cleanup loops |
| `cant_delete_message` | Wrong owner identity | Switch profile via → `auth_setup` or accept that this message stays |
| `rate_limit` | Bulk pressure | Wait for the delay when supplied. Otherwise stop or use bounded backoff; keep attempts bounded. |

## Next

*   Composes: → `cleanup_msgs` for the canonical search-then-delete loop.
