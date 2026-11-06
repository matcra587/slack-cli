---
slug: search-msgs
title: "Workspace message search workflow"
description: "Workspace message search workflow"
when_to_use: "Before following the search msgs workflow in slick."
commands: ["slick lookup messages"]
aliases: ["search_msgs"]
---

## Decide

Goal: Workspace-wide message search — the only way to find content without already knowing the channel.

*   Looking for a known run ID, phrase, or user mention across the workspace? Use this.
*   Inside one channel? Prefer → `read_history` (faster, cheaper, less rate-limited).
*   Need user identity, not message text? → `lookup_user`.

### Workflow checkpoints

*   Use lookup messages with structured Slack query text
*   Requires a user token with search:read; bot-token profiles cannot use search.messages
*   Bound output with --max-items
*   Use JSON for full text and metadata

## Run

*   Search: `slick lookup messages --query <query> --max-items <n> --output=json`
*   Next page: `slick lookup messages --query <query> --cursor <meta.pagination.next_cursor> --max-items <n> --output=json`

### Behavior

*   Search uses `search.messages` with a user token and `search:read`. The SDK retries HTTP 429 only with usable timing and within its retry limit. On a structured `rate_limit` error, wait for `retry_after_seconds` when supplied. If it is absent, stop or use bounded backoff before another read.
*   Result snippets in plain output are display-only; for automation parse JSON fields.

## Save

> Requires `--output=json`.
*   `data.matches[].channel.id` [string, required] — pass verbatim as `--channel` for downstream operations.
*   `data.matches[].ts` [string, required] — message ts for `react` / `edit_msg` / `delete_msg`.
*   `data.matches[].text` [string] — full text in JSON; plain output truncates and `--full` only affects human plain.
*   `data.pagination.has_more` / `.next_cursor` [bool / string] — continue while a next cursor is available; JSON also mirrors paging in `meta.pagination`.
*   `data.truncated=true` means Slack’s 100-page limit was reached with more matches remaining. There is no next cursor; narrow the query. Current-page matches remain in the result.

## Preconditions

*   `--max-items` accepts 1–100, or 0 for the default 20. `--cursor` is a page number from 1–100 for this command.

*   Needs a user-token profile with `search:read`. Bot-token profiles cannot call Slack Web API `search.messages` at all.
*   For mixed manifests, `search:read` belongs under user scopes only (`--type user` or `--type both`).

## Recover

| Symptom | Cause | Next |
|---|---|---|
| `auth_failure: missing_scope` (`search:read`) | Bot-token profile or manifest without user-scope search | → `auth_setup` with `--preset search` and `--type user` |
| Zero matches but you know the message exists | Slack search index lag (can be minutes) | Retry later; or → `read_history` if you know the channel |

## Next

*   Composes: → `cleanup_msgs` (the canonical search-then-delete-by-run-ID loop).
*   Then: → `react` / `reply` / `edit_msg` on any single match.
