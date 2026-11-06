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
*   I know a configured alias: exact lookup.
*   I know a Slack channel name: filter the list, inspect the names, then use the matching ID.
*   I'm browsing: filtered list.

Only conversations the selected token can read appear. One accepted conversation
read scope is enough to request a list. `--types` narrows the results and does
not grant access; `--types all` does not require every read scope.

### scope (narrow when possible)

*   Public + private channels: `--types public_channel,private_channel`
*   Existing DMs only: `--types im`
*   Group DMs: `--types mpim`
*   Everything: `--types all` (use sparingly)
*   Page sizes: `--max-items 1` through `999`; `0` uses Slack's default.
*   Unknown types fail locally with exit 4, including alongside `all`.

### Workflow checkpoints

*   Use lookup channel for channels and DM conversations
*   Use --types im for existing DMs
*   Use plain table output for humans and JSON for agents
*   Inspect membership and archived state before posting
*   Prefer stable channel or DM IDs

## Run

*   Exact lookup: `slick lookup channel --channel <id-or-alias> --output=json`
*   Find a name: `slick lookup channel --filter <name> --max-items <n> --output=json`; follow `meta.pagination.next_cursor` until the matching name is found or no next page remains.
*   Browse channels: `slick lookup channel --types public_channel,private_channel --max-items <n> --output=json`
*   List existing DMs: `slick lookup channel --types im --max-items <n> --output=json`

### Behavior

*   Plain mode renders human tables; JSON mode is the agent contract.
*   `--channel` accepts an ID or configured alias; it does not resolve Slack display names. Whitespace around arguments and stored alias values is trimmed. An empty resolved alias fails locally with exit 4 before credentials or API requests.
*   Prefer IDs over display names, but re-resolve an ID when needed: Slack Connect can change channel IDs.
*   Membership and archived fields can be absent for some conversation types; do not treat absence as confirmation.

## Save

> Requires `--output=json`.
*   `data.channels[].id` [string, required] — pass verbatim to `--channel` elsewhere. Prefer over display names (names can change).
*   `data.channels[].is_member` [bool, optional] — false ⇒ bot/user identity must be invited first.
*   `data.channels[].is_archived` [bool, optional] — true ⇒ posts will fail.

## Preconditions

## Recover

## Next

*   Then: → `send_msg` once `is_member=true` and `is_archived=false`.
*   Alternative: → `lookup_user` for DM targets by user identity instead of conversation ID.
*   Composes: → `cache_metadata` (`slick cache channels`) keeps this and shell completion cheap across many calls.
