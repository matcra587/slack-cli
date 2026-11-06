---
slug: upload-file
title: "Probationary hidden file upload workflow"
description: "Probationary hidden file upload workflow"
when_to_use: "Before following the upload file workflow in slick."
commands: ["slick file upload"]
aliases: ["upload_file"]
---

## Decide

Goal: Upload a file (path or stdin bytes) to a channel and capture its permalink.

> Status: probationary. `slick file upload` is hidden from help and completion; use only when explicitly testing the upload surface.

### source

*   Path: `--file <path>` (path expansion via `~` / env vars supported)
*   Stdin: `--file -` (REQUIRES `--filename <name>`)

### optional comment

*   Markdown: `--message <markdown>`
*   Raw Block Kit comment: `--blocks` + `--message <json>` (affects comment only, not file bytes)

### guard

*   `--dry-run` validates payload locally; skips the real Slack upload (and the `files:write` scope check).

### Workflow checkpoints

*   Status: probationary and not promoted; command entries are hidden from help and shell completion, while agent schema/workflow guidance may mention this workflow with that status
*   Use file upload with --file path or --file -
*   Provide --filename for stdin uploads
*   Read JSON response for file permalink

## Run

*   Path: `slick file upload --channel <id> --file <path> --output=json`
*   Stdin: `cat artefact.bin | slick file upload --channel <id> --file - --filename <name> --output=json`
*   With comment: append `--message <markdown>`

### Behavior

*   Upload progress and diagnostics go to stderr; stdout stays the command-data channel.
*   Comment Markdown is escaped on the wire (`&`, `<`, `>` → entities); sentinels — `<!channel>`, `<@U123>`, `<#C123>`, `<url|label>` — render as literal text and do NOT fire mentions. Use `--blocks` for the comment payload to mention or link intentionally.

## Save

> Requires `--output=json`.
*   `data.file.permalink` [string, optional] — Slack URL when returned.
*   `data.file.id`, `data.file.channels[]` [string / array] — Slack file metadata.

## Preconditions

*   Real uploads need `files:write`.
*   Missing path, directory targets, and stdin without `--filename` fail locally before any Slack call.

## Recover

| Symptom | Cause | Next |
|---|---|---|
| Local error `stdin requires --filename` | Piped to `--file -` without naming the artifact | Re-run with `--filename <name>` |
| `auth_failure: missing_scope` | Manifest lacks `files:write` | → `auth_setup` with `--preset files` |

## Next

*   Then: → `send_msg` to announce or link the upload separately.
