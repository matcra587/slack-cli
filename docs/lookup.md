# slick lookup

Look up channels and users, and search messages. `lookup messages` requires a
user token with `search:read`; bot-token profiles cannot use it.

```text
slick lookup channel   Look up Slack channels and conversations
slick lookup user      Look up Slack users
slick lookup messages  Search Slack messages
```

## lookup channel

Find channel and conversation metadata. With `--channel`, fetch one
conversation; without, list conversations of the requested types.

```sh
slick lookup channel --max-items 20
slick lookup channel --types im,mpim                  # only DMs / group DMs
slick lookup channel --types all                      # all types
slick lookup channel --channel C1234567890            # single channel info
slick lookup channel --filter deploy                  # client-side ID/name filter
```

### Flags

```text
-c, --channel <CHANNEL>  Channel or conversation ID, name, or alias
-M, --max-items <N>      Maximum conversations to return
-C, --cursor <CURSOR>    Pagination cursor
-f, --filter <TEXT>      Filter by ID or name
-t, --types <TYPE>       Conversation types: public_channel, private_channel, im, mpim, dm, or all
```

### Output

Human (list) renders a primer table:

```text
CHANNEL      NAME            TYPE             USER  MEMBER  ARCHIVED  MEMBERS  TOPIC
C1234567890  deploy-bots     channel                true    false     42       Deploy automation
C7N2Q8L4P    incidents       private_channel        true    false     8        Incident response
C0228L1B726  direct          im               U123…  true    false     1
```

Columns: `CHANNEL`, `NAME`, `TYPE`, `USER`, `MEMBER`, `ARCHIVED`, `MEMBERS`,
`TOPIC`. `TYPE` is Slack's conversation kind — `channel` for public,
`private_channel` for private, `im`/`mpim` for DMs. `USER` is populated only
for `im` rows (the DM counterparty). `CHANNEL` stays the raw conversation ID
for copy/paste and may be an OSC 8 terminal hyperlink in human mode. Channel IDs
hash-colour; `TYPE` colours by hash. `MEMBER` uses dim-on-true (being a member
is the routine state); `ARCHIVED` uses red-on-true.

Human (single channel via `--channel`):

```text
Channel resolved channel=C1234567890 name=deploy-bots type=channel is_member=true num_members=42
```

A `topic` field appears at the end when the channel has one set.

JSON envelope (list). Slack's conversation type is `channel` for public
channels and `private_channel` for private; `is_im` distinguishes DM
conversations even when other types coexist in the list. `hr` and `url` are
best-effort display metadata; `id` remains the raw Slack conversation ID:

```json
{
  "data": {
    "channels": [
      {
        "id": "C7N2Q8L4P",
        "name": "deploy-bots",
        "hr": "#deploy-bots",
        "url": "https://app.slack.com/client/T8KQ42P9D/C7N2Q8L4P",
        "type": "channel",
        "is_member": true,
        "is_im": false,
        "num_members": 42,
        "is_archived": false
      }
    ]
  }
}
```

## lookup user

Find user metadata. With `--user`, fetch one; without, list active users.

```sh
slick lookup user --max-items 50
slick lookup user --include-deleted          # full users.list
slick lookup user --user U1234567890         # single user info
slick lookup user --presence                 # fetch presence flag too
slick lookup user --filter mcraven           # client-side filter
```

### Flags

```text
-u, --user <USER>      Slack user ID
-M, --max-items <N>    Maximum users to return
-C, --cursor <CURSOR>  Pagination cursor
-f, --filter <TEXT>    Filter by ID or name
-p, --presence         Fetch presence
-d, --include-deleted  Include deleted or deactivated users
```

List mode excludes deleted and deactivated users by default. Add
`--include-deleted` when you need the full `users.list` result.

### Output

Human (list) renders a primer table:

```text
USER       NAME      TZ                   STATUS
USLACKBOT  slackbot  America/Los_Angeles
U123ABC    mcraven   America/Toronto      Working remotely
U2A8B0DCA  ansible   America/Los_Angeles
```

Human (single user):

```text
User resolved user=U123ABC name=mcraven timezone=America/Toronto status_text="Working remotely"
```

With `--presence`, a `presence=active|away` field joins the event:

```text
User resolved user=U123ABC name=mcraven timezone=America/Toronto presence=active status_text="Working remotely"
```

`timezone` renders Region/City with the region dim and the city bold.

JSON:

```json
{
  "data": {
    "user": {"id": "U123ABC", "name": "mcraven", "deleted": false, "tz": "America/Toronto", "status_text": "Working remotely", "presence": "active"}
  }
}
```

`status_text` is present when the user has a status set. `presence` is
populated only when `--presence` is passed (otherwise the field is absent).

## lookup messages

Search Slack workspace messages with the Web API `search.messages` method.
Requires a user token with `search:read`.

```sh
slick lookup messages --query "deploy failed" --max-items 10
slick lookup messages --query "in:#alerts after:2026-05-01" --max-items 50
slick lookup messages --query "deploy" --cursor <meta.pagination.next_cursor>

# Full text in human mode (default truncates long matches)
slick lookup messages --query "release" --full
```

### Flags

```text
-q, --query <QUERY>    Search query
-M, --max-items <N>    Maximum matches to return
-C, --cursor <CURSOR>  Pagination cursor
-F, --full             Show full text in human mode
```

Query syntax follows Slack's search modifiers (`from:`, `in:`, `before:`,
`after:`, etc.). When there are no matches:

```text
Messages searched query="deploy failed" count=0
```

`count` is forced to render even at zero (it would otherwise be stripped by
clog's `OmitZero`).

With matches, human mode renders a `TS / CHANNEL / USER / TEXT` table:

```text
TS                 CHANNEL      USER       TEXT
1746284582.123456  #deploy-bot  U123ABC    Deploy v0.4.0 complete — rollback window closes 18:00 UTC.
:robot_face: _Sent via slick (agent mode)_
```

The attribution context block (when present) renders on its own line below
the body, joined by a newline. That's how Slack's `search.messages`
returns Block Kit messages — the body lives in `blocks[]`, not in the
top-level `text` field, and slick reconstructs the readable text by
flattening section / header / context / rich_text blocks.

JSON envelope. For Block Kit messages (which is every message slick
sends — attribution adds a context block), Slack returns `text=""` in
the raw response and the body lives inside `blocks[]`. slick flattens
section / context / rich_text blocks back into the `text` field so
consumers get something readable; the attribution context line appears
on its own line after the body:

```json
{
  "data": {
    "matches": [
      {
        "channel": {
          "id": "C1234567890",
          "name": "deploy",
          "hr": "#deploy",
          "url": "https://app.slack.com/client/T8KQ42P9D/C1234567890"
        },
        "user": "U123ABC",
        "text": "Deploy v0.4.0 complete — rollback window closes 18:00 UTC.\n:robot_face: _Sent via slick (agent mode)_",
        "ts": "1746284582.123456",
        "permalink": "https://example.slack.com/archives/C1234567890/p1746284582123456"
      }
    ],
    "query": "deploy"
  }
}
```

## Common errors

| Error | Cause | Fix |
|-------|-------|-----|
| `not_allowed_token_type` (validation, exit 4) | `lookup messages` invoked under a bot-token profile. | Use a user-token profile; `search:read` is user-scope only. |
| `missing_scope` (`auth_failure`, exit 1) | Token lacks `users:read`, `channels:read`, or `search:read`. | Update the manifest, regenerate, re-auth. |
| Empty list with `--filter` | Filter is client-side; Slack list API returned nothing first. | Drop the filter or widen `--max-items`. |

## See also

*   [`cache`](cache.md) — prime users/channels for faster repeated lookups.
*   [`history`](history.md) — read messages by channel/thread rather than
  search.
*   Slack API methods:
  [`conversations.list`](https://docs.slack.dev/reference/methods/conversations.list/),
  [`conversations.info`](https://docs.slack.dev/reference/methods/conversations.info/),
  [`users.list`](https://docs.slack.dev/reference/methods/users.list/),
  [`users.info`](https://docs.slack.dev/reference/methods/users.info/),
  [`search.messages`](https://docs.slack.dev/reference/methods/search.messages/).
