---
description: "Upload files to Slack channels with slick’s probationary file upload command."
---

# slick file

**Upload** a file to a Slack channel.

!!! warning "Probationary"
    This command is hidden from `slick --help` and shell completion. It remains
    experimental. Fixture checks do not prove live compatibility for every Slack plan, token
    type, or native platform. Read the
    `upload-file` agent guide before using it.

Use a Slack channel ID or an alias configured in the selected workspace profile.
Aliases resolve locally before the Slack request and in dry-run previews.

Upload data must contain at least one byte. This also applies to dry runs.
A filename must contain a non-space character and cannot contain NUL, carriage
returns, or newlines. Unicode and spaces within a filename are preserved.
Binary and whitespace-only file contents are uploaded without trimming.
A failed stdin read stops the upload; partial data is not sent.

## file upload

=== "Basic"

    Upload a local file to a channel:

    ```sh
    slick file upload --channel C1234567890 --file ./report.txt
    ```

=== "Stdin"

    Stream from stdin with an explicit filename and title:

    ```sh
    tar czf - ./dist |
      slick file upload \
        --channel C1234567890 \
        --file - \
        --filename dist.tgz \
        --title "Build artifact"
    ```

=== "Thread"

    Reply in a thread with the upload:

    ```sh
    slick file upload --channel C1234567890 --file ./report.txt --thread 1746284582.123456
    ```

=== "Dry run"

    Preview without uploading:

    ```sh
    slick file upload --channel C1234567890 --file ./report.txt --dry-run
    ```

### Flags

??? note "Flags"

    | Flag | Value | Description |
    |------|-------|-------------|
    | `-c`, `--channel` | `<CHANNEL>` | Channel ID or configured alias |
    | `-f`, `--file` | `<FILE>` | Path to upload or `-` for stdin |
    | `-N`, `--filename` | `<NAME>` | Filename for stdin or override |
    | `-T`, `--title` | `<TITLE>` | Slack file title |
    | `-m`, `--message` | `<TEXT>` | Initial comment |
    | `-b`, `--blocks` | | Treat upload message as raw [Block Kit](https://docs.slack.dev/block-kit/) JSON |
    | `-t`, `--thread` | `<TS>` | Thread timestamp |
    | `-n`, `--dry-run` | | Preview without uploading |

--8<-- "includes/attribution-flags.md"

Raw comment input with `--blocks` must be a JSON array. Blank input, `null`,
and objects fail locally. `--blocks --message '[]' --no-attribution` sends an
empty comment block array; it does not post the text `[]`. Omit both
`--blocks` and `--message`, and use `--no-attribution`, when no comment is needed.

Progress and diagnostics go to stderr; stdout stays reserved for command
data.

### Output

`file.name` is the uploaded filename. The optional `file.title` is its Slack
display title; setting `--title` does not replace the filename.

=== "Human"

    Human (real upload — post-v0.4.0 the JSON fields now match the Slack file
    object shape: `file_id` → `id`, `file_name` → `name`, `size_human` dropped
    in favour of human-mode-only formatting of the numeric
    `size`):

    ```text
    File uploaded channel=C1234567890 id=F1234567890 name=slick-test.txt size="48 B"
    ```

    Human (`--dry-run`). `id` is the literal `"dry-run"` and the event carries
    `dry_run=true`; no Slack call is made:

    ```text
    File uploaded channel=C1234567890 id=dry-run name=report.txt dry_run=true size="13 B"
    ```

=== "JSON"

    JSON data excerpt (real upload):

    ```json
    {
      "data": {
        "file": {"id": "F1234567890", "name": "slick-test.txt", "size": 48},
        "channel": "C1234567890",
        "attribution": false
      }
    }
    ```

    JSON data excerpt (`--dry-run`). `file.id` is the literal `"dry-run"` and the
    data record carries `dry_run: true`; no Slack call is made:

    ```json
    {
      "data": {
        "file": {"id": "dry-run", "name": "report.txt", "size": 13},
        "channel": "C1234567890",
        "attribution": false,
        "dry_run": true
      }
    }
    ```

Slack's `files.completeUploadExternal` does not populate `permalink` in the
upload response; slick attempts to resolve it with `files.info` after upload.
If that lookup fails, `permalink` stays absent.

## Upload failures and recovery

Human terminal sessions show the upload phases through
[terminal status](index.md#terminal-status).
The status remains active until Slack completes the upload and slick writes the result.

The upload has three steps: `prepare` gets a URL and file ID, `transfer` sends
the bytes, and `complete` finalizes the file. Failures during these steps keep
the normal error type and exit code. They also include `details.stage` and,
once preparation is acknowledged, `details.file_id`. Human errors show these
fields on stderr.

A failure at `complete` can mean the file was saved but the reply was lost or
malformed. Inspect the file in Slack before starting another upload. Slick
does not restart or delete a failed upload. The SDK's bounded handling of HTTP
429 rate limits still applies; transport errors, malformed replies, and 5xx
responses do not restart the workflow.

For example, an incomplete completion reply produces a full failure envelope:

```json
{
  "ok": false,
  "meta": {
    "exit_code": 5,
    "command": "file.upload",
    "workspace": "default",
    "timestamp": "2026-01-01T00:00:00Z",
    "request_id": "00000000-0000-4000-8000-000000000000"
  },
  "data": null,
  "errors": [{
    "type": "server_error",
    "message": "Slack API request failed",
    "exit_code": 5,
    "details": {"stage": "complete", "file_id": "F1234567890"}
  }]
}
```

Optional `files.info` enrichment needs `files:read`. A permission failure,
timeout, malformed reply, or a response for another file does not undo an
acknowledged upload. Slick keeps its known file ID, filename, title, and size;
optional metadata such as the permalink can remain absent. Valid metadata can
supply Slack's normalized filename and display title.

Upload requests do not follow HTTP redirects. A redirect fails at its current
upload stage, with a known file ID retained when available. Slick does not
replay the request or automatically restart the upload.

## Common errors

| Error | Cause | Fix |
|-------|-------|-----|
| `not_in_channel` (`not_found`, exit 2) | App/user is not a member of the channel. | Add the bot, or use a user-token profile. |
| `validation_error: --file is required` | Missing `--file`. | Provide the path or `-` for stdin. |
| `validation_error: --filename required for stdin` | Stdin upload with no filename. | Pass `--filename`. |
| `validation_error: upload file must not be empty` | The path or stdin contains no bytes. | Supply a nonempty file; dry runs apply the same check. |
| `validation_error: filename must not be blank` | The filename contains only whitespace. | Use a nonblank filename. |
| `missing_scope` (`auth_failure`, exit 1) | Token lacks `files:write`. | Use the `files` or `full` manifest preset. |

File type, size, image-only, storage, and request-shape restrictions use
`validation_error` (exit 4). Change the input or resolve the workspace limit
before another attempt. A workspace that blocks all uploads, a restricted
account, or a posting denial uses `auth_failure` (exit 1). `invalid_channel`
uses `not_found` (exit 2).

Slack's `request_timeout` error means POST data was missing or truncated; slick
reports it as validation, not a caller deadline. An actual caller deadline
still exits 7. Service failures, migration outages, and unknown Slack errors
retain exit 5. None of these errors automatically restart the upload.

## See also

*   [`message`](message.md) — send a message without uploading a file.
*   [`manifest`](manifest.md) — `files` preset enables `files:write`.
*   Slack API methods:
  [`files.getUploadURLExternal`](https://docs.slack.dev/reference/methods/files.getUploadURLExternal/),
  [`files.completeUploadExternal`](https://docs.slack.dev/reference/methods/files.completeUploadExternal/),
  [`files.info`](https://docs.slack.dev/reference/methods/files.info/).
