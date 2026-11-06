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
*   Stdin: `--file -` (REQUIRES `--filename <name>`). Data must contain at least one byte.

### optional comment

*   Markdown: `--message <markdown>`
*   Raw Block Kit comment: `--blocks` + `--message <json-array>` (affects comment only, not file bytes). Objects, `null`, and blank input fail locally. Use `[]` with `--no-attribution` for an explicit empty comment; omit both flags for no comment.

### attribution

*   `--attribution` enables a caption for a manual run; `--no-attribution` disables it.
*   `--attribution-label "Release helper"` changes the generated caption to `Sent via slick (Release helper)` when attribution is enabled in a manual run.
*   `--attribution-message` overrides the generated caption. `--attribution-emoji` sets its leading emoji.

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
*   `data.file.id`, `data.file.name` [string] — uploaded file ID and filename.
*   `data.file.size` [integer] — byte count.
*   `data.file.title` [string, optional] — Slack display title.
*   Upload failures retain normal error classes and include `errors[0].details.stage` (`prepare`, `transfer`, or `complete`). `file_id` appears there only after acknowledged preparation.

## Preconditions

*   Real uploads need `files:write`.
*   Missing paths, directory targets, empty data, and stdin without `--filename` fail locally before credentials or Slack calls. Dry runs apply the same checks.
*   Filenames must be nonblank and cannot contain NUL or line breaks. Unicode and spaces are preserved. File data is not trimmed; a failed stdin read never uploads partial data.

## Recover

| Symptom | Cause | Next |
|---|---|---|
| Local error `stdin requires --filename` | Piped to `--file -` without naming the artifact | Re-run with `--filename <name>` |
| `auth_failure: missing_scope` | Manifest lacks `files:write` | → `auth_setup` with `--preset files` |

A `complete` failure can leave a completed file in Slack. Inspect the reported
file ID before starting a new upload. Slick does not restart or delete it after
a failed step. Cancellation and deadlines keep exit 6 and 7. Optional file-info
failures do not turn an acknowledged upload into failure; only matching file
metadata is used.

File type, size, image-only, and storage limits return validation exit 4.
A ban on all uploads or on posting returns auth exit 1. Slack's `request_timeout` means
missing or truncated POST data (exit 4); a caller deadline remains exit 7.
Resolve the restriction before starting another upload. Service failures and
unknown codes keep exit 5; a completed step can still have taken effect.

## Next

*   Then: → `send_msg` to announce or link the upload separately.
