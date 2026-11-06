---
slug: cleanup-msgs
title: "Runbook for cleaning test messages found through paginated search"
description: "Runbook for cleaning test messages found through paginated search"
when_to_use: "Before following the cleanup msgs workflow in slick."
commands: ["slick lookup messages", "slick message delete"]
aliases: ["cleanup_msgs"]
---

## Decide

Goal: After a live test, delete every message tagged with a distinctive run ID — even ones spread across channels.

This composes → `search_msgs` and → `delete_msg` in a loop. The loop is the contract.

*   Tag every live-test message with a unique run ID up front (e.g. `slick-live-2026-05-26-abcd`). Without it, cleanup is guesswork.
*   Use modest parallelism for the delete fan-out — separate CLI processes do NOT share proactive throttle state.

**Run** (loop until search returns zero matches, capped)

1.  Discover: `slick lookup messages --query <run-id> --max-items <n> --output=json`
2.  Paginate until `meta.pagination.has_more=false`: re-run with `--cursor <meta.pagination.next_cursor>`.
3.  Build targets ONLY from `data.matches[].channel.id` + `data.matches[].ts`. Never delete by snippet, visual order, or plain output.
4.  Delete each: `slick message delete --channel <id> --timestamp <ts> --force --output=json`.
5.  Repeat steps 1–4 until paginated search returns zero matches OR an iteration cap fires (see Behavior).

### Workflow checkpoints

*   Collect exact run IDs
*   Search every page with lookup messages and next_cursor
*   Delete by exact channel and timestamp only
*   Use controlled parallelism with rate-limit retries
*   Repeat search and delete until paginated search returns zero matches

## Run

### Behavior

*   Hard cap: stop after 10 outer loop iterations or if match count fails to decrease across two consecutive loops. A non-decreasing count means the query matches non-test content; escalate to the user with the residue rather than looping forever.
*   On structured `rate_limit` errors, sleep `errors[0].retry_after_seconds` before retrying that target.
*   `message_not_found` = already clean (another worker won the race). Treat as success.
*   A single `--max-items` page is NOT proof of cleanup — search-result windows can shift mid-delete, so the outer re-run loop is mandatory.

## Save

## Preconditions

## Recover

| Symptom | Cause | Next |
|---|---|---|
| Loop never terminates | Run-ID query matches non-test content | Tighten query (add timestamp prefix, channel filter, or longer ID) |
| Persistent `cant_delete_message` | Different identity posted some matches | Either accept residue or → `auth_setup` switch profile and re-run |

## Next

*   Composes: → `search_msgs` (discover) + → `delete_msg` (execute).
