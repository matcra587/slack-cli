# slick message

**Send**, **edit**, and **delete** Slack messages. Markdown input is converted to
[Block Kit](https://docs.slack.dev/block-kit/) by default; `--blocks` accepts
raw Block Kit JSON. Slack-side message
operations require `chat:write` (and matching channel scopes); user-token
profiles can edit and delete only their own messages.

For thread replies, see [`reply`](reply.md). For reactions, see
[`react`](react.md).

## message send

=== "Channel"

    Send to a channel by ID, name, or alias; `--file -` reads the body from stdin, and `--dry-run` previews without sending:

    ```sh
    slick message send --channel C1234567890 --message "Deploy complete"
    echo "Deploy complete" | slick message send --channel C1234567890 --file -
    slick message send --channel C1234567890 --message "Deploy complete" --dry-run
    ```

=== "DM"

    Send to one user, a group DM, or a Slack-profile email:

    ```sh
    slick message send --user U1234567890 --message "Build artifact ready"
    slick message send --user dev@example.com,ops@example.com --message "Review please"
    ```

=== "Block Kit"

    Send a raw Block Kit payload from a file:

    ```sh
    slick message send --channel C1234567890 --blocks --file blocks.json
    ```

=== "Schedule"

    Schedule a future send with RFC3339, a Go duration, or Unix seconds (no natural-language times):

    ```sh
    slick message send --channel C1234567890 --message "Deploy tomorrow" --schedule 2026-06-01T15:00:00-04:00
    slick message send --channel C1234567890 --message "Deploy in 90 minutes" --schedule 90m
    slick message send --user dev@example.com --message "DM follow-up" --schedule 90m
    ```

### Flags

??? note "Flags"

    | Flag | Value | Description |
    |------|-------|-------------|
    | `-c`, `--channel` | `<CHANNEL>` | Channel ID, name, or alias |
    | `-u`, `--user` | `<USER>…` | User ID or alias; repeat or comma-separate for group DM |
    | `-m`, `--message` | `<TEXT>` | Message body |
    | `-f`, `--file` | `<FILE>` | Read message body from file or `-` for stdin |
    | `-N`, `--filename` | `<NAME>` | Filename metadata for stdin sources |
    | `-b`, `--blocks` | | Treat message source as raw Block Kit JSON |
    | `-s`, `--schedule` | `<WHEN>` | Schedule for future send: RFC3339, Go duration, or Unix seconds |
    | `-n`, `--dry-run` | | Preview without sending |

--8<-- "includes/attribution-flags.md"

If `default_channel` is set in config, both `--channel` and `--user` can be
omitted for immediate sends. Scheduled sends require an explicit `--channel` or
`--user` target.

`--user` accepts Slack user IDs, configured aliases, and Slack-profile email
addresses resolved via `users.lookupByEmail`. Repeat the flag or comma-separate
values to open a group DM. If Slack returns `users_not_found` for an email,
that address did not match a Slack user in the active workspace.

!!! note "Accepted `--schedule` formats"
    `--schedule` accepts only RFC3339 timestamps, Go durations such as `90m`
    or `2h30m`, and Unix seconds. It rejects every other format, including
    natural-language strings such as `tomorrow at 9am`.

Scheduled sends accept either `--channel` or `--user`; scheduled DMs
resolve/open the DM before calling the Slack scheduled-message API. Real
scheduled `--user` sends return the raw DM or MPIM conversation ID in
`data.channel`. Scheduled sends return `scheduled_message_id`, not `ts`,
because the Slack API does not assign a message timestamp until the message fires.

### Markdown handling

Markdown converts to Block Kit before posting:

*   **Paragraphs** and **headings** render as `section` blocks with mrkdwn
    text. (Headings do not become Slack `header` blocks — slick treats them
    as section text so Slack renders them inline with the rest of the
    message.)
*   **Tables** render as native Block Kit `table` blocks. Slack rejects
    messages containing more than one table block, so slick errors out
    rather than truncating.
*   **Unsupported block-level constructs** — lists, blockquotes, fenced
    code, raw HTML — are preserved as readable `section` blocks rather
    than dropped.
*   **User content is escaped.** Section-block text passes through
    [`slack-go/slackutilsx.EscapeMessage`](https://pkg.go.dev/github.com/slack-go/slack/slackutilsx#EscapeMessage)
    before posting: `&` → `&amp;`, `<` → `&lt;`, `>` → `&gt;`. Slack
    sentinels like `<!channel>`, `<@U…>`, `<#C…>`, `<!subteam^…>`, and the
    `<url|label>` link pattern in piped content render as literal text
    instead of firing mentions or forging links. Markdown blockquotes
    (`> quoted`) and bare URLs (`https://example.com`) still render
    correctly — Slack decodes the escapes before mrkdwn parsing.

!!! tip "Sending intentional mentions / links"
    The escape applies to *every* `< > &` in Markdown source, including
    sentinels you typed on purpose. To deliberately ping a user, channel,
    or user-group, bypass the Markdown converter with `--blocks` and use
    Slack's wire syntax inside your JSON:

    ```sh
    slick lookup user --filter john.doe -o compact | jq -r '.users[0].id'
    # → U123ABC
    printf '%s' '[{"type":"section","text":{"type":"mrkdwn","text":"Deploy failed — <@U123ABC>"}}]' \
      | slick message send --channel C1234567890 --blocks --file -
    ```

    The `--blocks` path is the deliberate-action escape hatch; `<@U…>`,
    `<#C…>`, `<!subteam^S…>`, and `<url|label>` all work there. Look up
    IDs with [`lookup user`](lookup.md#lookup-user) /
    [`lookup channel`](lookup.md#lookup-channel).

### Output

=== "Human"

    Real send:

    ```text
    Message sent channel=C7N2Q8L4P ts=1746284582.123456 permalink=p1746284582123456
    ```

    When Slack metadata is available in human mode, `channel` may render as a
    friendly clickable label such as `#deployments` or `@johndoe`. Use JSON
    output for the raw conversation ID plus `channel_name`, `channel_hr`, and
    `channel_url` metadata.

    `--dry-run` — `ts` is the literal `"dry-run"`, no `permalink` is emitted,
    and `dry_run=true` joins the event:

    ```text
    Message sent channel=C7N2Q8L4P ts=dry-run dry_run=true
    ```

=== "JSON"

    Real send:

    ```json
    {
      "meta": {"command": "message.send", "workspace": "default", "timestamp": "2026-05-26T03:00:56Z", "request_id": "337f5bd1-a5f2-4bb8-8da5-510cb801f62d"},
      "data": {
        "message": {
          "type": "message",
          "text": "Deploy complete",
          "ts": "1746284582.123456",
          "channel": "C7N2Q8L4P",
          "channel_name": "deployments",
          "channel_hr": "#deployments",
          "channel_url": "https://app.slack.com/client/T8KQ42P9D/C7N2Q8L4P"
        },
        "permalink": "https://example.slack.com/archives/C7N2Q8L4P/p1746284582123456",
        "attribution": true
      },
      "errors": []
    }
    ```

    `--dry-run` — no Slack call is made, so `ts` is the literal string
    `"dry-run"`, `permalink` is absent, and `dry_run: true` joins the data
    record:

    ```json
    {
      "data": {
        "message": {
          "type": "message",
          "text": "Deploy complete",
          "ts": "dry-run",
          "channel": "C7N2Q8L4P",
          "channel_url": "https://app.slack.com/client/T8KQ42P9D/C7N2Q8L4P"
        },
        "dry_run": true,
        "attribution": true
      }
    }
    ```

    `--schedule` — scheduled messages have no `ts` until Slack posts them, so
    `scheduled_message_id` is the reference for list/delete:

    ```json
    {
      "data": {
        "channel": "C1234567890",
        "channel_name": "deployments",
        "channel_hr": "#deployments",
        "channel_url": "https://app.slack.com/client/T8KQ42P9D/C1234567890",
        "scheduled_message_id": "Q123",
        "post_at": 1780335600,
        "post_at_iso": "2026-06-01T19:00:00Z",
        "text": "Deploy tomorrow",
        "attribution": {"enabled": true, "label": "slick"}
      }
    }
    ```

`permalink` is Slack's `chat.getPermalink` HTTP URL in JSON. Human mode renders
the short form (`p1746284582123456`) as an OSC 8 terminal hyperlink on
supporting terminals.

## message edit

Edit your own message. The Slack API permits edits only to messages your token
identity sent.

```sh
slick message edit --channel C1234567890 --timestamp 1746284582.123456 --message "Corrected text"
slick message edit --channel C1234567890 --timestamp 1746284582.123456 --message "Updated" --dry-run
```

### Flags

??? note "Flags"

    | Flag | Value | Description |
    |------|-------|-------------|
    | `-c`, `--channel` | `<CHANNEL>` | Channel ID, name, or alias |
    | `-t`, `--timestamp` | `<TS>` | Message timestamp |
    | `-m`, `--message` | `<TEXT>` | Message body |
    | `-f`, `--file` | `<FILE>` | Read message body from file or `-` for stdin |
    | `-b`, `--blocks` | | Treat message source as raw Block Kit JSON |
    | `-n`, `--dry-run` | | Preview without mutating |

--8<-- "includes/attribution-flags.md"

### Output

=== "Human"

    `--dry-run`:

    ```text
    Message edited channel=C7N2Q8L4P ts=1746284582.123456 dry_run=true
    ```

=== "JSON"

    Real edit. Slack's `chat.update` does not return a permalink, and slick
    suppresses `data.message.blocks` even when the edit re-renders Block Kit —
    verify rendered output via [`history list`](history.md#history-list) or the
    Slack UI when needed. The `attribution` field is still present:

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

| Flag | Value | Description |
|------|-------|-------------|
| `-c`, `--channel` | `<CHANNEL>` | Channel ID, name, or alias |
| `-t`, `--timestamp` | `<TS>` | Message timestamp |
| `-n`, `--dry-run` | | Preview without mutating |
| `-F`, `--force` | | Confirm deletion |

### Output

=== "Human"

    ```text
    Message deleted channel=C7N2Q8L4P ts=1746284582.123456 dry_run=true
    ```

=== "JSON"

    The envelope keeps `ts` (formerly `timestamp` before v0.4.0):

    ```json
    {
      "data": {"channel": "C7N2Q8L4P", "ts": "1746284582.123456", "dry_run": true}
    }
    ```

The `deleted` field was removed in v0.4.0; the action label conveys success
and `errors[]` conveys failure.

## message scheduled list

List pending scheduled messages. Pass `--channel` to narrow the result to one
conversation, or omit it to ask the Slack API for the workspace-visible
scheduled messages for the active token.

```sh
slick message scheduled list --channel C1234567890 --limit 20 --output=json
slick message scheduled list --cursor <next-cursor> --output=json
```

### Flags

| Flag | Value | Description |
|------|-------|-------------|
| `-c`, `--channel` | `<CHANNEL>` | Channel ID, name, or alias |
| `-C`, `--cursor` | `<CURSOR>` | Pagination cursor |
| `-L`, `--limit` | `<N>` | Maximum scheduled messages to return |

### Output

=== "Human"

    Output in a TTY or with `--output=human` renders a table. `CHANNEL` uses a
    readable `#channel` or `@user` name when Slack metadata is readable, and
    `DM` marks direct-message conversations:

    ```text
    ID           CHANNEL       DM     POST_AT               TEXT
    Q123         #deployments  false  2026-06-01T19:00:00Z  Deploy tomorrow
    Q124         @johndoe      true   2026-06-01T20:00:00Z  Hello from slick — sched…
    ```

    When there are no pending messages, human output keeps the same action
    label and adds `count=0`:

    ```text
    Scheduled messages retrieved count=0
    ```

=== "JSON"

    JSON output uses `data.scheduled_messages[]` and cursor metadata:

    ```json
    {
      "meta": {
        "command": "message.scheduled.list",
        "pagination": {"next_cursor": "cur-2", "has_more": true, "max_items": 20, "items_returned": 1}
      },
      "data": {
        "scheduled_messages": [
          {
            "id": "Q123",
            "channel": "C1234567890",
            "channel_name": "deployments",
            "channel_hr": "#deployments",
            "channel_url": "https://app.slack.com/client/T8KQ42P9D/C1234567890",
            "channel_type": "channel",
            "is_dm": false,
            "post_at": 1780335600,
            "post_at_iso": "2026-06-01T19:00:00Z",
            "text_preview": "Deploy tomorrow"
          }
        ]
      }
    }
    ```

For automation, use JSON. The human `CHANNEL` value is display-only; pass the
raw `data.scheduled_messages[].channel` value back to
`message scheduled delete` or to future scheduled sends. JSON also adds
optional `channel_name`, `channel_hr`, `channel_url`, `channel_type`,
`channel_user`, and `is_dm` fields when metadata can be resolved.

`text_preview` is capped at 200 Unicode characters and ends with `…` when
truncated.

## message scheduled delete

Delete a pending scheduled message by exact channel and scheduled-message ID.
No `--force` flag is required.

```sh
slick message scheduled delete --channel C1234567890 --scheduled-id Q123 --dry-run
slick message scheduled delete --channel C1234567890 --scheduled-id Q123 --output=json
```

### Flags

| Flag | Value | Description |
|------|-------|-------------|
| `-c`, `--channel` | `<CHANNEL>` | Channel ID, name, or alias |
| `--scheduled-id` | `<QID>` | Scheduled message ID returned by scheduled send/list |
| `-n`, `--dry-run` | | Preview without mutating |

JSON output:

```json
{
  "data": {"channel": "C1234567890", "scheduled_message_id": "Q123", "deleted": true}
}
```

## Common errors

| Error | Cause | Fix |
|-------|-------|-----|
| `validation_error: --channel or --user is required` | No destination given and `default_channel` isn't set in config. | Provide `--channel`/`--user`, or set `default_channel`. |
| `not_in_channel` (`not_found`, exit 2) | App or user is not a member of the channel. | Add the bot to the channel, or use a user token that is a member. |
| `channel_not_found` | Bad channel ID or alias not configured. | Verify with [`lookup channel`](lookup.md#lookup-channel). |
| `user_not_found` | Email or alias did not resolve. | Use a known Slack user ID or verify via [`lookup user`](lookup.md#lookup-user). |
| `cant_update_message` / `cant_delete_message` (`validation_error`, exit 4) | Slack disallows the edit/delete for this token identity. | Use the user token that originally sent the message. |
| Schedule `validation_error` | `--schedule` was in the past, beyond 120 days, or not RFC3339/duration/Unix seconds. | Use a future RFC3339 timestamp, Go duration, or Unix seconds. |
| Raw Block Kit `validation_error` with no stdout data | Malformed JSON, unsupported block types, missing required fields, or Slack limits. | Inspect the `--blocks` JSON; the error envelope describes the rule that failed. |

## See also

*   [`reply`](reply.md) — thread replies (separate top-level command).
*   [`react`](react.md) — reactions on messages.
*   [`history`](history.md) — verify rendered output after edits.
*   [`status`](status.md), [`auth`](auth.md), [`config`](config.md).
*   Slack API methods:
  [`chat.postMessage`](https://docs.slack.dev/reference/methods/chat.postMessage/),
  [`chat.update`](https://docs.slack.dev/reference/methods/chat.update/),
  [`chat.delete`](https://docs.slack.dev/reference/methods/chat.delete/),
  [`chat.scheduleMessage`](https://docs.slack.dev/reference/methods/chat.scheduleMessage/),
  [`chat.scheduledMessages.list`](https://docs.slack.dev/reference/methods/chat.scheduledMessages.list/),
  [`chat.deleteScheduledMessage`](https://docs.slack.dev/reference/methods/chat.deleteScheduledMessage/).
