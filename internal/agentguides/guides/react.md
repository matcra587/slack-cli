---
slug: react
title: "Add, remove, list, and watch emoji reactions"
description: "Add, remove, list, and watch emoji reactions"
when_to_use: "Before following the react workflow in slick."
commands: ["slick react add", "slick react remove", "slick react list", "slick react watch"]
---

## Decide

Goal: Add, remove, list, or watch emoji reactions on a known channel message.

### direction

*   Add: `slick react add`
*   Remove: `slick react remove`
*   Inspect: `slick react list`

### target

*   Always needs `--channel <id>` + `--timestamp <slack-ts>` (get the ts from `read_history` or saved `data.message.ts` from `send_msg`).

### emoji (add/remove only)

*   One: `--emoji :thumbsup:` or `--emoji thumbsup` (colons optional).
*   Many (ordered): `--emoji thumbsup,white_check_mark,rocket` or repeat `--emoji` (use repeat when a value itself contains a comma).

### Workflow checkpoints

*   Use channel ID and message timestamp
*   Pass one emoji as :name: or name, or comma-separate/repeat --emoji to apply multiple in order
*   Use add/remove/list for the desired action
*   Use --dry-run before add/remove in live channels
*   Read JSON response for data.mutations[] and data.target

## Run

To wait for approval before continuing, run:

```sh
slick react watch --channel C1234567890 --timestamp 1746284582.123456 --emoji :+1: --timeout 10m --output=json
```

A reaction that already exists satisfies the watch. `--user U1234567890`
requires that user to appear in Slack's returned user list, which can be
incomplete. The watch only reads; it never adds or removes reactions. It emits
one result on a match, exit 7 on timeout, or exit 6 on cancellation.

*   Add: `slick react add --channel <id> --timestamp <ts> --emoji :thumbsup: --output=json`
*   Add ordered batch: `slick react add --channel <id> --timestamp <ts> --emoji thumbsup,rocket --output=json`
*   Remove: `slick react remove --channel <id> --timestamp <ts> --emoji :thumbsup: --output=json`
*   List: `slick react list --channel <id> --timestamp <ts> --output=json`

### Behavior

*   `--dry-run` validates locally and short-circuits the Slack call; `data.mutations[].dry_run=true`.
*   Verification cycle: after add/remove, `react list` on the same channel + ts.

## Save

> Requires `--output=json`.

Add / remove:

*   `data.mutations[]` [array, required] — one entry per emoji actually applied, fields `{channel, ts, emoji, dry_run}`.
*   `data.target.channel`, `data.target.ts` [string, required] — echo of the target.
*   `meta.command` is `react.add` or `react.remove`.

List:

*   `data.reactions[]` [array] — `{name, count, users[]}`.
*   `meta.command` is `react.list`.

## Preconditions

*   Slack timestamps are strings (`1746284582.123456`); never cast to float.
*   Ordered multi-emoji is atomic-per-emoji: a failure aborts the rest. On an error envelope `data.mutations[]` is absent — re-derive remaining work from a fresh `react list` instead of assuming partial state.

## Recover

| Symptom | Cause | Next |
|---|---|---|
| `not_found: message_not_found` | Wrong `--timestamp` or ts deleted | → `read_history` to re-find the message |
| `already_reacted` / `no_reaction` | Emoji already present (add) or absent (remove) | Treat as success or skip; `react list` to confirm |
| `auth_failure: missing_scope` | Manifest lacks `reactions:write` or `reactions:read` | → `auth_setup` |

## Next

*   Then: → `reply` (continue the thread)
*   Alternative: → `edit_msg` (modify the target instead of decorating)
