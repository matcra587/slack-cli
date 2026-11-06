---
slug: core-contract
title: "Understand output modes, stderr/stdout, and fixed exit codes"
description: "Understand output modes, stderr/stdout, and fixed exit codes"
when_to_use: "Before following the core contract workflow in slick."
commands: []
aliases: ["core_contract"]
---

## Decide

Goal: Apply the cross-cutting output-mode, stdout/stderr, exit-code, and error-shape contract every other workflow inherits.

*   TTY humans: `--output=human` (or omit; `auto` picks `human` on a TTY).
*   Automation: `--output=json` for envelope (`json` mode), `--output=compact` (`compact` mode) for `data` on success. Both modes use the full envelope on failure.

### Workflow checkpoints

*   Use JSON for automation and human output for humans
*   Treat stdout as command data only
*   Parse machine successes and failures on stdout; reserve stderr for diagnostics
*   Set --output to one of auto, human, json, compact (auto-detected by default)
*   Parse fixed exit codes and error types on failure

## Run

### Behavior

*   Machine success and failure are JSON records on stdout. stderr is diagnostics, progress, and explicit debug output; no duplicate human error is emitted for a machine failure.
*   JSON/compact failures emit a full envelope with `ok=false` and `errors[0].type`, `errors[0].message`, `errors[0].exit_code` on stdout; `data=null` on failure.
*   Exit codes: auth `1`, not found `2`, rate limit `3`, validation `4`, server `5`, canceled `6`, timeout `7`.
*   Slack channels/DMs/permalinks may render as OSC 8 hyperlinks for humans. Do not scrape — parse JSON for raw IDs, channel_name/channel_hr/channel_url, timestamps, full URLs.
*   `--output` selects output mode only; it does NOT switch input to Block Kit. Use command-local `--blocks` for that.
*   Attribution auto-attaches to `send_msg` / `reply` / `edit_msg` / `upload_file` comments as a context block on agent/CI detection — do NOT repeat in body. Toggle `--attribution` / `--no-attribution`; override copy `--attribution-{label,emoji,message}`.
*   Paginated commands retain `data.pagination` in JSON and `pagination` in compact output; `meta.pagination` remains compatible.
*   `io` / exit 8 means the result could not be written. A mutation may already have completed; inspect remote state before retrying.
*   Slack ts values stay strings (e.g. `1746284582.123456`); never cast to float.

## Save

> Requires `--output=json`.
*   `ok` [bool, required] reports command success. `meta.command`, `meta.workspace`, `meta.timestamp`, `meta.request_id` [string, required] — envelope identity.
*   `meta.exit_code` [integer, required] is 0 on success and matches the process on failure.
*   `data` [object or null, required] — command-specific payload on success; null on failure.
*   `errors` [array, required] — empty on success.

## Preconditions

## Recover

## Next
