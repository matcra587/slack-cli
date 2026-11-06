---
description: "Read Slack channel and thread history with pagination and structured output."
---

# slick history

**Read** channel or thread history with bounded pagination. The default scope is
parent messages in a channel; `--thread` fetches replies to a parent
timestamp.

Use a Slack channel ID or an alias configured in the selected workspace profile.
Aliases resolve locally before the Slack request. History is read-only;
`--dry-run` does not suppress these reads.

## history list

=== "By channel"

    Read parent messages in a channel, capped by `--max-items`:

    ```sh
    slick history list --channel C1234567890 --max-items 50
    ```

=== "Thread"

    Read replies under a specific parent timestamp:

    ```sh
    slick history list --channel C1234567890 --thread 1746284582.123456
    ```

=== "Time range"

    Bound the window with oldest and latest Slack timestamps:

    ```sh
    slick history list --channel C1234567890 --since 1746000000.000000 --until 1746999999.999999
    ```

=== "By user"

    Filter messages on the fetched page to a single author:

    ```sh
    slick history list --channel C1234567890 --user U1234567890
    ```

=== "With replies"

    Include bounded thread replies per parent:

    ```sh
    slick history list --channel C1234567890 --include-replies --reply-limit 10
    ```

=== "Paginate"

    Continue from a prior page using the opaque cursor:

    ```sh
    slick history list --channel C1234567890 --max-items 50 --cursor <meta.pagination.next_cursor>
    ```

### Flags

??? note "Flags"

    | Flag | Value | Description |
    |------|-------|-------------|
    | `-c`, `--channel` | `<CHANNEL>` | Channel ID or configured alias |
    | `-M`, `--max-items` | `<N>` | Page size: 1–999 for channels, 1–1000 for threads; 0 uses Slack's default |
    | `-s`, `--since` | `<TS>` | Oldest Slack timestamp |
    | `-u`, `--until` | `<TS>` | Latest Slack timestamp |
    | `-U`, `--user` | `<USER>` | Filter by user ID |
    | `-t`, `--thread` | `<TS>` | Read replies for parent timestamp |
    | `-C`, `--cursor` | `<CURSOR>` | Pagination cursor |
    | `-R`, `--include-replies` | | Include bounded thread replies |
    | `-L`, `--reply-limit` | `<N>` | With `--include-replies`: 1–999 replies per parent; 0 uses Slack's page default |

History returns parent messages by default; `--thread` switches to replies
under a specific parent. `--since`, `--until`, and `--user` also apply with
`--thread`. Time bounds exclude messages exactly at either boundary. The user
filter runs locally on each fetched page, so a page can be empty while another
cursor remains. `--include-replies` bounds replies per parent via `--reply-limit`;
its nested reply fetches do not inherit the parent selection filters.
Each invocation fetches one page. Omitted or zero count flags leave the page size
to Slack. `--reply-limit` requires `--include-replies`, and `--include-replies`
cannot be combined with `--thread`. Negative counts and counts above the limits
return a validation error before authentication or Slack requests.

Surrounding whitespace is trimmed from channel inputs, stored alias values,
`--thread`, `--since`, `--until`, and `--user`. Empty optional values remove
that filter; an empty `--thread` selects channel history. Timestamp digits are
preserved as strings. Opaque `--cursor` bytes are passed unchanged.

### Output

=== "Human"

    Human (populated):

    ```text
    TS                 USER       TEXT                                 REPLIES
    1746284600.000001  U123ABC    Deploy complete                      3
    1746284582.123456  U987XYZ    Starting deploy                      0
    ```

    Human (empty result — table is suppressed, only the summary event renders):

    ```text
    History retrieved max_items=5
    ```

    `ts` renders in `FieldTime` magenta when colour is enabled. The USER column
    hash-colours by user ID.

=== "JSON"

    JSON data excerpt. Each message includes supported Slack fields when Slack
    supplies them, such as `bot_id`, `reply_count`, an embedded
    `reactions` array (same shape as [`react list`](react.md#react-list)),
    and `blocks` ([Block Kit](https://docs.slack.dev/block-kit/) JSON, including
    an attribution context block when present). Channel reads also attempt an
    optional permalink lookup; failure leaves that field absent:

    ```json
    {
      "meta": {"pagination": {"next_cursor": "bmV4dF90czoxNzc4NDQxNjE4OTg5NDg5", "has_more": true, "max_items": 50, "items_returned": 50}, "…": "…"},
      "data": {
        "messages": [
          {
            "type": "message",
            "user": "U123ABC",
            "bot_id": "B0B1HM17BHS",
            "text": "Deploy complete",
            "ts": "1746284600.000001",
            "thread_ts": "1746284600.000001",
            "channel": "C1234567890",
            "permalink": "https://example.slack.com/archives/C1234567890/p1746284600000001",
            "reply_count": 3,
            "reactions": [
              {"name": "+1", "count": 2, "users": ["U123ABC", "U987XYZ"]}
            ],
            "blocks": [
              {"type": "section", "text": {"type": "mrkdwn", "text": "Deploy complete"}, "block_id": "…"},
              {"type": "context", "block_id": "…", "elements": [{"type": "mrkdwn", "text": ":robot_face: _Sent via slick (agent mode)_"}]}
            ]
          },
          {"type": "message", "user": "U987XYZ", "text": "Starting deploy", "ts": "1746284582.123456", "channel": "C1234567890"}
        ]
      }
    }
    ```

JSON and compact output preserve `client_msg_id` and `edited` metadata when
present, including the exact edit timestamp and editing user ID.
They also preserve SDK-decoded `attachments` and `files`
when Slack returns them, including files inside forwarded attachments. Empty
or omitted arrays are omitted from each message. Attachment text, fields,
blocks, and timestamps remain available alongside the primary message text.
File objects can contain names, titles, private URLs, and access-limited
metadata such as `file_access`. slick does not download these files or follow
their URLs. See Slack's [file object reference](https://docs.slack.dev/reference/objects/file-object/).

The human table shows the primary message text. Use JSON or compact output to
inspect attachment and file data. The message result contains selected fields
decoded by the pinned SDK.

!!! warning "Opaque cursor"
    `next_cursor` is an opaque base64-encoded string — treat it as bytes; do
    not try to decode or modify it. Pagination continuation passes the value
    verbatim back via `--cursor`.

## Pagination

Pass `data.pagination.next_cursor` back as `--cursor` to fetch the next page.
JSON also repeats pagination in `meta.pagination`. Human output shows the
same continuation details beneath populated tables and on empty pages,
including `has_more=false` when the page is complete.

Slack can return more history without a cursor. In that case, channel history
returns `next_until`; pass it as `--until`. Thread history returns `next_since`;
pass it as `--since`. Keep the other filters and omit the old cursor. These
boundaries come from the Slack page before local author filtering, so even an
empty filtered page can have a continuation. Stop when `has_more=false`.

With `--include-replies`, each parent with fetched replies has a separate
`replies_pagination` object. Continue that thread with `--thread <parent-ts>`
and its next cursor or time boundary. The outer pagination still describes the
channel page. Inline replies can be incomplete even when the channel page is
complete.

History requires Slack's explicit `ok: true` acknowledgement. A missing or false
acknowledgement without a usable Slack error is a server error, even when a
messages array is present. Contradictory success and error fields also fail.
An explicit empty `messages` array is a valid result. Missing or null messages,
or a page that promises more results without a usable continuation, return a
server error instead of reporting a complete empty history.

## Common errors

| Error | Cause | Fix |
|-------|-------|-----|
| `not_in_channel` (`not_found`, exit 2) | Token identity is not a member of the channel. | Add the bot to the channel, or use a user-token profile. |
| `channel_not_found` | Bad channel ID. | Verify with [`lookup channel`](lookup.md#lookup-channel). |
| `thread_not_found` (`not_found`, exit 2) | The thread does not exist in this channel. | Verify the channel and parent timestamp. |
| `invalid_cursor` (validation, exit 4) | The cursor is invalid or has expired. | Restart the walk without the cursor. |
| `invalid_ts_oldest` / `invalid_ts_latest` (validation, exit 4) | Slack rejected a time boundary. | Use the exact timestamp string from a message. |
| `token_expired` / `access_denied` (auth, exit 1) | The credential expired or cannot access the history. | Check authentication and channel access. |

## See also

*   [`message`](message.md) — verify rendered output after edits.
*   [`lookup messages`](lookup.md#lookup-messages) — workspace-wide message search.
*   [`react list`](react.md#react-list) — read reactions on a known message.
*   Slack API methods:
  [`conversations.history`](https://docs.slack.dev/reference/methods/conversations.history/),
  [`conversations.replies`](https://docs.slack.dev/reference/methods/conversations.replies/).
