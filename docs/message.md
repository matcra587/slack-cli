# slick message

Send, edit, and delete Slack messages. Markdown input is converted to Block
Kit by default; `--blocks` accepts raw Block Kit JSON. Slack-side message
operations require `chat:write` (and matching channel scopes); user-token
profiles can edit and delete only their own messages.

```text
slick message send     Send a Slack message
slick message edit     Edit an owned Slack message
slick message delete   Delete an owned Slack message
```

For thread replies, see [`reply`](reply.md). For reactions, see
[`react`](react.md).

## message send

```sh
slick message send --channel C1234567890 --message "Deploy complete"
echo "Deploy complete" | slick message send --channel C1234567890 --file -

# DMs (single user, group DM, or by email)
slick message send --user U1234567890 --message "Build artifact ready"
slick message send --user dev@example.com,ops@example.com --message "Review please"

# Raw Block Kit input
slick message send --channel C1234567890 --blocks --file blocks.json

# Preview without sending
slick message send --channel C1234567890 --message "Deploy complete" --dry-run
```

### Flags

```text
-c, --channel <CHANNEL>  Channel ID, name, or alias
-u, --user <USER>…       User ID or alias; repeat or comma-separate for group DM
-m, --message <TEXT>     Message body
-f, --file <FILE>        Read message body from file or - for stdin
-N, --filename <NAME>    Filename metadata for stdin sources
-b, --blocks             Treat message source as raw Block Kit JSON
-n, --dry-run            Preview without sending
```

If `default_channel` is set in config, both `--channel` and `--user` can be
omitted.

`--user` accepts Slack user IDs, configured aliases, and email addresses
(resolved via `users.lookupByEmail`). Repeat the flag or comma-separate values
to open a group DM.

### Markdown handling

Markdown converts to semantic Block Kit blocks where possible (paragraphs,
headings, tables). Unsupported block-level constructs — lists, blockquotes,
fenced code, raw HTML — are preserved as readable section blocks rather than
dropped.

### Output

Human (real send):

```text
Message sent channel=C7N2Q8L4P ts=1746284582.123456 age=now permalink=p1746284582123456
```

Human (`--dry-run`). `ts` is the literal `"dry-run"`, no `permalink` is
emitted, and `dry_run=true` joins the event:

```text
Message sent channel=C7N2Q8L4P ts=dry-run dry_run=true
```

JSON envelope (real send):

```json
{
  "meta": {"command": "message.send", "workspace": "default", "timestamp": "…", "request_id": "…"},
  "data": {
    "message": {"type": "message", "text": "Deploy complete", "ts": "1746284582.123456", "channel": "C7N2Q8L4P"},
    "permalink": "https://example.slack.com/archives/C7N2Q8L4P/p1746284582123456",
    "attribution": true
  },
  "errors": []
}
```

JSON envelope (`--dry-run`). No Slack call is made, so `ts` is the literal
string `"dry-run"`, `permalink` is absent, and `dry_run: true` joins the
data record:

```json
{
  "data": {
    "message": {"type": "message", "text": "Deploy complete", "ts": "dry-run", "channel": "C7N2Q8L4P"},
    "dry_run": true,
    "attribution": true
  }
}
```

`permalink` is rendered with an OSC 8 hyperlink wrapper on supporting
terminals; the underlined short form (`p1746284582123456`) opens the full
URL.

## message edit

Edit your own message. Slack permits edits only to messages your token
identity sent.

```sh
slick message edit --channel C1234567890 --timestamp 1746284582.123456 --message "Corrected text"
slick message edit --channel C1234567890 --timestamp 1746284582.123456 --message "Updated" --dry-run
```

### Flags

```text
-c, --channel <CHANNEL>  Channel ID, name, or alias
-t, --timestamp <TS>     Message timestamp
-m, --message <TEXT>     Message body
-f, --file <FILE>        Read message body from file or - for stdin
-b, --blocks             Treat message source as raw Block Kit JSON
-n, --dry-run            Preview without mutating
```

Human (`--dry-run`):

```text
Message edited channel=C7N2Q8L4P ts=1746284582.123456 age=2m dry_run=true
```

JSON envelope (real edit). Slack's `chat.update` does not return a
permalink, and slick suppresses `data.message.blocks` even when the edit
re-renders Block Kit — verify rendered output via
[`history list`](history.md#history-list) or the Slack UI when needed.
The `attribution` field is still present:

```json
{
  "data": {
    "message": {
      "type": "message",
      "text": "Corrected text",
      "ts": "1746284582.123456",
      "channel": "C1234567890"
    },
    "attribution": true
  }
}
```

## message delete

Delete your own message. Requires `--force` unless you also pass `--dry-run`.

```sh
slick message delete --channel C1234567890 --timestamp 1746284582.123456 --dry-run
slick message delete --channel C1234567890 --timestamp 1746284582.123456 --force
```

### Flags

```text
-c, --channel <CHANNEL>  Channel ID, name, or alias
-t, --timestamp <TS>     Message timestamp
-n, --dry-run            Preview without mutating
-F, --force              Confirm deletion
```

### Output

Human:

```text
Message deleted channel=C7N2Q8L4P ts=1746284582.123456 age=2m dry_run=true
```

JSON envelope keeps `ts` (formerly `timestamp` before v0.4.0):

```json
{
  "data": {"channel": "C7N2Q8L4P", "ts": "1746284582.123456", "dry_run": true}
}
```

The `deleted` field was removed in v0.4.0; the action label conveys success
and `errors[]` conveys failure.

## Common errors

| Error | Cause | Fix |
|-------|-------|-----|
| `validation_error: --channel or --user is required` | No destination given and `default_channel` isn't set in config. | Provide `--channel`/`--user`, or set `default_channel`. |
| `not_in_channel` (`not_found`, exit 2) | App or user is not a member of the channel. | Add the bot to the channel, or use a user token that is a member. |
| `channel_not_found` | Bad channel ID or alias not configured. | Verify with [`lookup channel`](lookup.md#lookup-channel). |
| `user_not_found` | Email or alias did not resolve. | Use a known Slack user ID or verify via [`lookup user`](lookup.md#lookup-user). |
| `cant_update_message` / `cant_delete_message` (`validation_error`, exit 4) | Slack disallows the edit/delete for this token identity. | Use the user token that originally sent the message. |
| Raw Block Kit `validation_error` with no stdout data | Malformed JSON, unsupported block types, missing required fields, or Slack limits. | Inspect the `--blocks` JSON; the error envelope describes the rule that failed. |

## See also

*   [`reply`](reply.md) — thread replies (separate top-level command).
*   [`react`](react.md) — reactions on messages.
*   [`history`](history.md) — verify rendered output after edits.
*   [`status`](status.md), [`auth`](auth.md), [`config`](config.md).
*   [README](https://github.com/matcra587/slack-cli#readme) and [index](index.md).
