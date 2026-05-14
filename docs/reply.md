# slick reply

Reply to a Slack thread by parent timestamp. `reply` is a top-level command,
not a subcommand of `message`, so the runbook for thread responses lives
here.

```sh
slick reply --channel C1234567890 --parent 1746284582.123456 --message "Investigating"
echo "Fixed in deploy 1234" | slick reply --channel C1234567890 --parent 1746284582.123456 --file -
slick reply --channel C1234567890 --parent 1746284582.123456 --blocks --file blocks.json
```

## Flags

```text
-c, --channel <CHANNEL>  Channel ID, name, or alias
-p, --parent <TS>        Parent message timestamp
-m, --message <TEXT>     Message body
-f, --file <FILE>        Read message body from file or - for stdin
-b, --blocks             Treat message source as raw Block Kit JSON
-n, --dry-run            Preview without sending
    --attribution                Force attribution on for this command
    --no-attribution             Disable attribution for this command
    --attribution-label <LABEL>      Override attribution label
    --attribution-emoji <EMOJI>      Override attribution emoji
    --attribution-message <TEXT>     Override attribution message
```

## Output

Human (`--dry-run`):

```text
Reply posted channel=C1234567890 ts=dry-run dry_run=true
```

A real reply renders the same shape but with the real Slack `ts` and a
`permalink=...` field appended.

JSON envelope (real reply) mirrors [`message send`](message.md#message-send)
but the data record includes `thread_ts`, and the `permalink` carries
extra query parameters identifying the parent thread:

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

JSON envelope (`--dry-run`). `ts` is the literal `"dry-run"`, `permalink`
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
