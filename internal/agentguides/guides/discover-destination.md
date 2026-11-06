---
slug: discover-destination
title: "Find channel and DM IDs before posting"
description: "Find channel and DM IDs before posting"
when_to_use: "Before following the discover destination workflow in slick."
commands: ["slick lookup channel"]
aliases: ["discover_destination"]
---

## Decide

Goal: Resolve a channel name/alias/ID to the exact Slack conversation ID and confirm the identity is a member before posting.

### question

*   I know the exact ID: skip → `send_msg` directly (but consider an exact lookup to check `is_member` first).
*   I know a name or alias: exact lookup.
*   I'm browsing: filtered list.

### scope (narrow when possible)

*   Public + private channels: `--types public_channel,private_channel`
*   Existing DMs only: `--types im`
*   Group DMs: `--types mpim`
*   Everything: `--types all` (use sparingly)

### Workflow checkpoints

*   Use lookup channel for channels and DM conversations
*   Use --types im for existing DMs
*   Use plain table output for humans and JSON for agents
*   Inspect membership and archived state before posting
*   Prefer stable channel or DM IDs

## Run

*   Exact lookup: `slick lookup channel --channel <id-or-alias> --output=json`
*   Browse channels: `slick lookup channel --types public_channel,private_channel --max-items <n> --output=json`
*   List existing DMs: `slick lookup channel --types im --max-items <n> --output=json`

### Behavior

*   Plain mode renders human tables; JSON mode is the agent contract.
*   Channel IDs are stable (`C…`, `G…`, `D…`, `MP…`); display names are not.

## Save

> Requires `--output=json`.
*   `data.channels[].id` [string, required] — pass verbatim to `--channel` elsewhere. Prefer over display names (names can change).
*   `data.channels[].is_member` [bool, required] — false ⇒ bot/user identity must be invited first.
*   `data.channels[].is_archived` [bool, required] — true ⇒ posts will fail.

## Preconditions

## Recover

## Next

*   Then: → `send_msg` once `is_member=true` and `is_archived=false`.
*   Alternative: → `lookup_user` for DM targets by user identity instead of conversation ID.
*   Composes: → `cache_metadata` (`slick cache channels`) keeps this and shell completion cheap across many calls.
