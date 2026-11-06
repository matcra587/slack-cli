---
slug: safe-mutation
title: "Preview high-impact changes and parse JSON results"
description: "Preview high-impact changes and parse JSON results"
when_to_use: "Before following the safe mutation workflow in slick."
commands: []
aliases: ["safe_mutation"]
---

## Decide

Goal: A cross-cutting checklist for `send_msg`, `reply`, `edit_msg`, `delete_msg`, `react`, and `upload_file` against high-visibility targets (incidents, release, exec channels).

This is not a command — it's the discipline that wraps the mutating workflows.

### is this destination high-impact?

*   Yes (incident, release, exec, prod alerts) → preflight + dry-run + recorded evidence.
*   No → still keep JSON output, but skip the recorded-evidence step.

### what stage am I in?

*   Pre-flight: read-only lookup with the same profile.
*   Pre-write: `--dry-run` to validate the payload.
*   Real write: machine-parseable invocation.
*   Post-write: record evidence.

**Run** (sequence, per mutation)

1.  Preflight: → `discover_destination` (channel known + `is_member=true`) or → `read_history` (right message ts).
2.  Dry-run: same command with `--dry-run --output=json`; verify payload shape and ts placeholders.
3.  Real write: drop `--dry-run`, keep `--output=json`, **add the target workflow's per-command confirm flags** (`--force` for → `delete_msg`, `--blocks` when the input is raw Block Kit, etc. — defer to the target's Decide block).
4.  Evidence (for important writes): record returned `data.message.channel`, `data.message.ts`, `data.permalink`, and the dry-run flag value.

### Workflow checkpoints

*   Use --dry-run before high-impact mutations
*   Keep JSON output for machine parsing
*   Treat deletes as destructive operations
*   Do not parse rich/plain output in automation
*   Machine errors are on stdout with fixed exit codes

## Run

### Behavior

*   Rate limits map to exit code `3` + structured `rate_limit` errors with `errors[0].retry_after_seconds`. Sleep before retrying that target.
*   `chat.postMessage` has same-channel limits. Separate CLI processes do NOT share proactive throttle state, so shell fanout can still hit `rate_limit` even with conservative single-process pacing.

## Save

## Preconditions

*   Always `--output=json` for automation — never parse `--output=human` (it's display-only).
*   Parse the stdout failure envelope for `type` / `message` / `exit_code` on failure; do NOT scrape human text.

## Recover

| Symptom | Cause | Next |
|---|---|---|
| `rate_limit` (exit `3`) | Same-channel or workspace fanout exceeded Slack budget | Sleep `errors[0].retry_after_seconds`, retry. Reduce parallelism. |
| `validation_error` requesting `--force` | Real write to `delete_msg` without the confirm flag | Add `--force` (delete is the only workflow that requires it). |
| Unexpected `validation_error` after dry-run was clean | Real Slack state diverged (channel archived, user deactivated) between dry-run and write | Re-preflight, then retry |
| Exit `6` (canceled) mid-flight | Caller (agent or user) interrupted the command | Treat as unfinished — re-preflight before any retry; do not assume Slack state matches your last dry-run. |

## Next

*   Composes: every mutating workflow listed in the Goal can be wrapped in this pattern.
