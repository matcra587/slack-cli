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
*   Credential lookup failures use safe diagnostics and exit `1`; provider output is hidden. Cancellation uses exit `6` and deadlines use exit `7`, including during credential lookup. Empty credentials stop before Slack requests.
*   Exit codes: auth `1`, not found `2`, rate limit `3`, validation `4`, server `5`, canceled `6`, timeout `7`.
*   Slack channels/DMs/permalinks may render as OSC 8 hyperlinks for humans. Do not scrape — parse JSON for raw IDs, channel_name/channel_hr/channel_url, timestamps, full URLs.
*   Raw `--blocks` input must be a JSON array. To clear existing blocks while preserving text, edit with `--blocks --message '[]' --no-attribution`. New messages/replies/schedules require a nonempty block set after attribution.
*   `--output` selects output mode only; it does NOT switch input to Block Kit. Use command-local `--blocks` for that.
*   `--timeout` / `-I` defaults to 30s. Positive values add a deadline; zero/negative values preserve the caller context without adding one. An earlier caller deadline still wins, and command-specific limits remain.
*   Attribution auto-attaches to `send_msg` / `reply` / `edit_msg` / `upload_file` comments as a context block on agent/CI detection — do NOT repeat in body. Toggle `--attribution` / `--no-attribution`; override copy `--attribution-{label,emoji,message}`.
*   Paginated commands retain `data.pagination` in JSON and `pagination` in compact output; `meta.pagination` remains compatible. List counts retain `items_returned=0` for empty pages. Human tables and empty pages show pagination, including `has_more=false` on final pages.
*   `io` / exit 8 means the result could not be written, in any output mode. A mutation may already have completed; inspect remote state before retrying. Slick does not retry the failed result destination.
*   `--debug` / `-D` adds stderr diagnostics. SDK logs summarize HTTP status and events; request payloads, response headers/bodies and unknown diagnostic text are omitted. Machine stdout is unchanged.
*   Slack ts values stay strings (e.g. `1746284582.123456`); never cast to float.
*   Agent schema leaves use `read_only=true` for Slack reads, local discovery, and manifest/completion generation. Explicit mutations of Slack, credentials, configuration, caches, or exported files keep it false or omitted. Routing parents also keep the conservative default. Compact and selected-subtree schemas preserve the classification.

## Save

> Requires `--output=json`.
*   `ok` [bool, required] reports command success. `meta.command`, `meta.workspace`, `meta.timestamp`, `meta.request_id` [string, required] — envelope identity.
*   `meta.exit_code` [integer, required] is 0 on success and matches the process on failure.
*   Flag and argument parsing failures use the selected command's canonical name in `meta.command`, including alias invocations.
*   Early errors retain the requested workspace (canonical spelling when configured) or configured default. An unavailable config preserves the requested label; formatting does not resolve credentials or call Slack.
*   `data` [object or null, required] — command-specific payload on success; null on failure.
*   `errors` [array, required] — empty on success.
*   On scope failures, `errors[].details.needed` is an array of scope names when available. `needed_match` is `all` or `any` for local checks, or `reported` for Slack's list without an inferred rule. Both fields are omitted when there are no validated scope names.

## Preconditions

## Recover

## Next

### HTTP rate limits

*   HTTP `429` uses `rate_limit` / exit 3, including when the retry delay is unknown.
*   A usable nonnegative integer `Retry-After` allows at most two automatic retries. `--no-throttle` only disables proactive pacing.
*   Missing, invalid, duplicate, or excessively long timing returns the error without an automatic retry. `retry_after_seconds` is omitted when unknown; known integer timing is preserved when a wait cannot be scheduled.
*   Cancellation and deadlines stop waits with exit 6 or 7. After an error, inspect remote state before retrying an uncertain mutation.
*   Unexpected HTTP statuses such as `401` and `403` use exit 5. Slack JSON authentication and permission errors use exit 1.
