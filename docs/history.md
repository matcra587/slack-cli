# slick history

Read channel or thread history with bounded pagination. The default scope is
parent messages in a channel; `--thread` fetches replies to a parent
timestamp.

```text
slick history list  List channel or thread history
```

## history list

```sh
slick history list --channel C1234567890 --max-items 50
slick history list --channel C1234567890 --thread 1746284582.123456
slick history list --channel C1234567890 --since 1746000000.000000 --until 1746999999.999999
slick history list --channel C1234567890 --user U1234567890
slick history list --channel C1234567890 --include-replies --reply-limit 10
slick history list --channel C1234567890 --max-items 50 --cursor <meta.pagination.next_cursor>
```

### Flags

```text
-c, --channel <CHANNEL>  Channel ID, name, or alias
-M, --max-items <N>      Maximum messages to return
-s, --since <TS>         Oldest Slack timestamp
-u, --until <TS>         Latest Slack timestamp
-U, --user <USER>        Filter by user ID
-t, --thread <TS>        Read replies for parent timestamp
-C, --cursor <CURSOR>    Pagination cursor
-R, --include-replies    Include bounded thread replies
-L, --reply-limit <N>    Maximum replies per parent
```

History returns parent messages by default; `--thread` switches to replies
under a specific parent. `--include-replies` bounds replies per parent via
`--reply-limit`.

### Output

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

`ts` and `age` render in `FieldTime` magenta when colour is enabled. The
USER column hash-colours by user ID.

JSON envelope. Each message carries Slack's full conversation object —
expect `permalink`, `bot_id` (present for messages sent via Slack's API
even when the `user` field is a real user), `reply_count`, an embedded
`reactions` array (same shape as [`react list`](react.md#react-list)),
and the rendered `blocks` (Block Kit payload, including the agent
attribution context block when present):

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

`next_cursor` is an opaque base64-encoded string — treat it as bytes; do
not try to decode or modify it. Pagination continuation passes the value
verbatim back via `--cursor`.

## Pagination

Pass `meta.pagination.next_cursor` back as `--cursor` to fetch the next page.
`has_more` is the explicit "there is more" signal; an empty `next_cursor`
ends the walk.

## Common errors

| Error | Cause | Fix |
|-------|-------|-----|
| `not_in_channel` (`not_found`, exit 2) | Token identity is not a member of the channel. | Add the bot to the channel, or use a user-token profile. |
| `channel_not_found` | Bad channel ID. | Verify with [`lookup channel`](lookup.md#lookup-channel). |

## See also

*   [`message`](message.md) — verify rendered output after edits.
*   [`lookup messages`](lookup.md#lookup-messages) — workspace-wide message search.
*   [`react list`](react.md#react-list) — read reactions on a known message.
*   [README](https://github.com/matcra587/slack-cli#readme) and [index](index.md).
