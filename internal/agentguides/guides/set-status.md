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
*   Provide text or emoji. Nonempty flags take priority over matching positional values.
*   Each set sends all three status fields. Omitted text becomes empty, omitted emoji requests Slack's default, and omitted expiration becomes zero (no expiry).
*   `--until` accepts Unix seconds, RFC3339, `+duration`, or local `HH:MM[:SS]`. Local clocks use today if still future, otherwise the next calendar date.

### guard

*   `--dry-run` previews the status payload without resolving credentials, checking scopes, or calling Slack.

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
*   `data.text`, `data.emoji` [string, optional] — requested values, not a fresh profile read. Empty values are omitted; Slack can supply a default emoji.
*   `data.expiration` [int64, optional] — Unix seconds; zero is omitted.
*   `data.dry_run` [bool, optional] — present and true for a preview.
*   `meta.command` is `status.set` or `status.clear` — branch on this, not on data fields.

## Preconditions

*   Requires a **user-token** profile with `users.profile:write`. Bot tokens cannot set a user's status.

## Recover

| Symptom | Cause | Next |
|---|---|---|
| User-token requirement (`auth_failure`) | Bot-token profile selected | → `auth_setup` with a user-token profile |
| `missing_scope` (`auth_failure`) | Token lacks `users.profile:write` | → `auth_setup` with the required scope |
| Invalid acknowledgement (`server_error`, exit 5) | Slack did not confirm the update | Check your status before retrying; the update may already have happened |
| Result write failed (exit 8) | Output could not be delivered | Check your status before retrying; a successful update is not undone |

## Next

*   Alternative: → `config_prefs` for non-Slack-side defaults (e.g. `default_channel`).
