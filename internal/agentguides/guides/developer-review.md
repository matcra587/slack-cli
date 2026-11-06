---
slug: developer-review
title: "Runbook for posting a review-style message with reactions and a thread"
description: "Runbook for posting a review-style message with reactions and a thread"
when_to_use: "Before following the developer review workflow in slick."
commands: ["slick message send", "slick reply", "slick react add"]
aliases: ["developer_review"]
---

## Decide

Goal: Post a realistic developer-style review (or incident update / release note) as a parent message, then decorate with reactions and thread replies.

This is a composition workflow — no command is unique to it. The contract is the sequence and the discipline.

*   Pick the channel. Unfamiliar destination? Preflight with → `discover_destination`.
*   Pick the body shape: prose + bullets + inline code → Markdown via stdin; specific Slack rendering → `--blocks`.
*   High-visibility (incidents, release, exec channels)? Run the parent with `--dry-run` first.

**Run** (compose → send → decorate → verify)

1.  Compose one parent body in stdin (real newlines, NOT literal `\n`). Use mrkdwn naturally: bold section labels, bullet lists, numbered requested changes, inline code, links, emoji.
2.  Send parent: `slick message send --channel <id> --file - --output=json`
3.  Save `data.message.channel`, `data.message.ts`, `data.permalink`.
4.  React: `slick react add --channel <id> --timestamp <parent-ts> --emoji eyes --output=json` (or `white_check_mark`, etc.)
5.  Reply with detail: `slick reply --channel <id> --parent <parent-ts> --file - --output=json`
6.  Verify: `slick react list --channel <id> --timestamp <parent-ts> --output=json` + `slick history list --channel <id> --thread <parent-ts> --output=json`

### Workflow checkpoints

*   Collect channel ID, review subject, decision, and optional run ID
*   Send one human-readable parent message from stdin
*   Parse parent channel, timestamp, and permalink
*   Add reactions by channel and timestamp
*   Reply with follow-up detail and verify with react list plus thread history

## Run

### Behavior

*   Attribution auto-attaches (toggle / override): see → `core_contract`.
*   Slack history text flattens display formatting; for exact visual rendering use the Slack UI or compare `blocks`.

## Save

*   Carry `data.message.ts` (from step 2) as `parent-ts` through every subsequent step. It is the join key for the whole review.

## Preconditions

## Recover

## Next

*   Composes: this workflow composes → `send_msg` + → `react` + → `reply` + → `read_history`.
*   Then: → `cleanup_msgs` after live-test runs (include a distinctive run ID in the body for findability).
