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

*   Search is slower and more strictly rate-limited than channel history; backoff is automatic on structured `rate_limit` errors.
*   Result snippets in plain output are display-only; for automation parse JSON fields.

## Save

> Requires `--output=json`.
*   `data.matches[].channel.id` [string, required] — pass verbatim as `--channel` for downstream operations.
*   `data.matches[].ts` [string, required] — message ts for `react` / `edit_msg` / `delete_msg`.
*   `data.matches[].text` [string] — full text in JSON; plain output truncates and `--full` only affects human plain.
*   `meta.pagination.has_more` / `.next_cursor` [bool / string] — paginate until `has_more=false`.

## Preconditions

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
