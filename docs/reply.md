---
description: "Reply in Slack threads and preserve timestamps for follow-up operations."
---

# slick reply

**Reply** to a Slack thread by parent timestamp. `reply` is a top-level command,
not a subcommand of `message`, so the runbook for thread responses lives
here.

```sh
slick reply --channel C1234567890 --parent 1746284582.123456 --message "Investigating"
echo "Fixed in deploy 1234" | slick reply --channel C1234567890 --parent 1746284582.123456 --file -
slick reply --channel C1234567890 --parent 1746284582.123456 --blocks --file blocks.json
```

Use a Slack channel ID or an alias configured in the selected workspace profile.
Aliases resolve locally before the Slack request and in dry-run previews.
Surrounding whitespace is removed from the channel and parent timestamp. The
parent otherwise stays an exact string. Replies require an explicit channel;
`default_channel` never supplies the target.

## Flags

??? note "Flags"

    | Flag | Value | Description |
    |------|-------|-------------|
    | `-c`, `--channel` | `<CHANNEL>` | Channel ID or configured alias |
    | `-p`, `--parent` | `<TS>` | Parent message timestamp |
    | `-m`, `--message` | `<TEXT>` | Message body |
    | `-f`, `--file` | `<FILE>` | Read message body from file or `-` for stdin |
    | `-b`, `--blocks` | | Treat message source as raw [Block Kit](https://docs.slack.dev/block-kit/) JSON |
    | `-n`, `--dry-run` | | Preview without sending |

--8<-- "includes/attribution-flags.md"

Use exactly one body source: `--message` or `--file`; `--file -` reads stdin.
Raw Block Kit omits notification text instead of posting the JSON source there.
Its dry-run preview contains validated `data.message.blocks` and omits `text`.
Removing `--blocks`, or passing `--blocks=false`, restores Markdown and its
readable fallback text. See [message source behavior](message.md#message-sources-and-raw-block-kit).

## Output

=== "Human"

    Human (`--dry-run`):

    ```text
    Reply posted channel=C1234567890 ts=dry-run dry_run=true
    ```

    A real reply uses Slack’s returned `ts` and adds `permalink=...` when
    the permalink lookup succeeds.

=== "JSON"

    JSON data excerpt (real reply) mirrors [`message send`](message.md#message-send)
    and uses Slack's returned message, including SDK-decoded attachments and
    files, `client_msg_id`, and `edited` metadata when present. slick does not
    fetch file URLs.
    `data.message.thread_ts` records the actual
    thread anchor when Slack includes it. If the parent was deleted and Slack
    posts outside the thread, this field is absent. Check the receipt rather
    than assuming the reply used the requested parent. A threaded reply's
    permalink can include query parameters identifying the thread:

    ```json
    {
      "data": {
        "message": {
          "type": "message",
          "text": "Investigating",
          "ts": "1778534628.573559",
          "thread_ts": "1746284582.123456",
          "channel": "C1234567890"
        },
        "permalink": "https://example.slack.com/archives/C1234567890/p1778534628573559?thread_ts=1746284582.123456&cid=C1234567890",
        "attribution": true
      }
    }
    ```

    JSON data excerpt (`--dry-run`). `ts` is the literal `"dry-run"`, `permalink`
    is absent, and `dry_run: true` is present:

    ```json
    {
      "data": {
        "message": {"type": "message", "text": "Investigating", "ts": "dry-run", "channel": "C1234567890", "thread_ts": "1746284582.123456"},
        "dry_run": true,
        "attribution": true
      }
    }
    ```

## Common errors

| Error | Cause | Fix |
|-------|-------|-----|
| `validation_error: parent is required` | Missing `--parent`. | Provide the parent message timestamp; verify with [`history list --thread`](history.md#history-list). |
| `message_not_found` (`not_found`, exit 2) | Parent timestamp does not exist in this channel. | Confirm channel and timestamp via [`history`](history.md). |
| Markdown handling | Same as [`message send`](message.md#message-send): block-level fallbacks preserve source text. | — |

## See also

*   [`message`](message.md) — top-level send/edit/delete.
*   [`history list --thread`](history.md#history-list) — read replies before posting.
*   Slack API method: [`chat.postMessage`](https://docs.slack.dev/reference/methods/chat.postMessage/).

A missing or invalid Slack acknowledgement returns `server_error` (exit 5).
The mutation may already have happened. Slick does not replay it automatically;
inspect Slack before deciding whether to try again. Sends require the returned
message identifiers, and scheduled sends require the assigned scheduling ID.
