---
slug: set-status
title: "Set or clear the authenticated user's Slack status"
description: "Set or clear the authenticated user's Slack status"
when_to_use: "Before following the set status workflow in slick."
commands: ["slick status set", "slick status clear"]
aliases: ["set_status"]
---

## Decide

Goal: Set or clear the authenticated user's Slack profile status (text, emoji, optional expiration).

### action

*   Set: `slick status set`
*   Clear: `slick status clear`

### body (set only)

*   Flagged: `--text <text> --emoji :headphones: --expires-in 2h`
*   Positional shorthand: `slick status set "In a meeting" :calendar:`

### guard

*   `--dry-run` previews the status payload without calling Slack.

### Workflow checkpoints

*   Use status set with text, emoji, and optional expiration
*   Use status clear to remove status
*   Requires a user token with users.profile:write
*   Use --dry-run before mutating during tests
*   Keep JSON output for automation

## Run

*   Set (flagged): `slick status set --text "deep work" --emoji :headphones: --expires-in 2h --output=json`
*   Set (positional): `slick status set "In a meeting" :calendar: --output=json`
*   Clear: `slick status clear --output=json`

## Save

> Requires `--output=json`.
*   `data.text`, `data.emoji` [string, optional] — echo of what was set; absent on clear.
*   `data.expiration` [int64, optional] — Unix seconds when present.
*   `meta.command` is `status.set` or `status.clear` — branch on this, not on data fields.

## Preconditions

*   Requires a **user-token** profile with `users.profile:write`. Bot tokens cannot set a user's status.

## Recover

| Symptom | Cause | Next |
|---|---|---|
| `auth_failure: missing_scope` (`users.profile:write`) | Bot-token profile or missing scope | → `auth_setup` with a user-token profile |

## Next

*   Alternative: → `config_prefs` for non-Slack-side defaults (e.g. `default_channel`).
