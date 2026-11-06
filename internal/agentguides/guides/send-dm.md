---
slug: send-dm
title: "Send direct messages while handling token limits"
description: "Send direct messages while handling token limits"
when_to_use: "Before following the send dm workflow in slick."
commands: ["slick message send"]
aliases: ["send_dm"]
---

## Decide

Goal: Send a direct message to a user by ID or Slack-profile email, returning the DM conversation ID for follow-up.

This is → `send_msg` with `--user` instead of `--channel`. Same body shapes, same envelope.

### recipient

*   One user by ID (preferred): `--user U123...`
*   One user by Slack-profile email: `--user alice@example.com` (needs `users:read.email`)
*   Group DM: repeat `--user` or comma-separate values

### unsure of the ID?

*   Preflight → `lookup_user`.

### body

*   Same as → `send_msg` (`--message` / `--file -` / `--blocks`).

### modifiers

*   Schedule: add `--schedule <when>` (real scheduled DM sends still return the raw DM/MPIM ID in `data.channel` — usable as a `schedule_msg` delete target).

### Workflow checkpoints

*   Use message send --user with user IDs, aliases, or Slack-profile email addresses
*   Repeat --user or comma-separate values for group DMs
*   Add --schedule to schedule the DM instead of sending now
*   Email lookup uses Slack profile email; users_not_found means the address did not match
*   Slack decides whether the active bot-token or user-token profile may open the DM
*   Use returned data.message.channel or scheduled data.channel for follow-up operations
*   Handle structured errors where Slack rejects the target
*   Use a user-token profile for DM-anyone workflows when bot-token limits get in the way

## Run

*   ID: `slick message send --user <user-id> --message <markdown> --output=json`
*   Email: `slick message send --user <slack-profile-email> --message <markdown> --output=json`
*   Stdin: `printf '%s\n' "$body" | slick message send --user <id-or-email> --file - --output=json`
*   Group DM: `slick message send --user U123 --user U456 --message <markdown> --output=json`

### Behavior

*   `message send --user` opens the DM through Slack before posting; the returned `data.message.channel` is that opened conversation.
*   Email recipients route through Slack `users.lookupByEmail` first; `users_not_found` means the email isn't a Slack profile in this workspace.

## Save

> Requires `--output=json`.
*   `data.message.channel` [string, required] — the DM/MPIM conversation ID Slack opened. Reuse for `read_history`, `reply`, etc.
*   `data.message.ts` [string, required] — message timestamp.

## Preconditions

*   Slack decides whether the active token (bot vs user) may open the DM at all.
*   For "DM anyone as me", a user-token profile is the normal choice; bot-token profiles depend on app install + conversation access.
*   `--dry-run` verifies composition but cannot prove Slack will open the DM.

## Recover

| Symptom | Cause | Next |
|---|---|---|
| `not_found: users_not_found` | Email doesn't match a Slack profile here | → `lookup_user` with filter, then retry with the ID |
| `auth_failure: missing_scope` (`users:read.email`) | Email targeting without the scope | → `auth_setup` |
| `auth_failure: no_permission` (open DM) | Bot can't DM this person | Switch to user-token profile or invite into a shared channel first |

## Next

*   Then: → `reply` / → `react` on the new DM message.
