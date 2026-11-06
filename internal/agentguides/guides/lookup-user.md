---
slug: lookup-user
title: "Find user IDs, presence, status, and timezone"
description: "Find user IDs, presence, status, and timezone"
when_to_use: "Before following the lookup user workflow in slick."
commands: ["slick lookup user"]
aliases: ["lookup_user"]
---

## Decide

Goal: Resolve a name, email, or partial filter to a stable Slack user ID (and optionally presence, status, timezone) before DM or human-aware work.

### question

*   I have an exact user ID: confirm with exact lookup if you need presence/status/tz.
*   I have part of a name: filter list.
*   I'm browsing: paginated list.

### extras (opt-in)

*   `--presence` — requires an explicit `--user` and token presence visibility.
*   `--include-deleted` — list mode excludes deactivated users by default; opt in only for audits.

### Workflow checkpoints

*   Use lookup user to list users or inspect one user
*   Fetch presence, status, and timezone when needed
*   Prefer stable user IDs
*   Use --presence only when the token has presence visibility

## Run

*   List: `slick lookup user --max-items <n> --output=json`
*   Filter: `slick lookup user --filter <text> --max-items 20 --output=json`
*   Exact: `slick lookup user --user <user-id> --output=json`
*   Cache (prime for shell completion + repeat discovery): `slick cache users --output=json`

### Behavior

*   Prefer IDs (`U123…`) in downstream commands; display names can change without warning.
*   Missing presence / custom-status fields are NOT errors; they reflect scope/policy.

## Save

> Requires `--output=json`.
*   `data.users[].id` [string, required] — feed to `message send --user` and elsewhere.
*   `data.users[].tz` [string, optional] — IANA tz for scheduling-aware logic.
*   `data.user.presence` [string, optional] — only when `--presence` is set AND the token allows it.

## Preconditions

## Recover

## Next

*   Then: → `send_dm` (the DM-by-user wrapper).
*   Composes: → `cache_metadata` to keep this cheap across many calls.
