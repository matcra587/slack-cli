---
description: "Find Slack channels, users, and messages by ID, name, or search query."
---

# slick lookup

**Look up** channels and users, and **search** messages.

!!! note "lookup messages needs a user token"
    `lookup messages` requires a user token with `search:read`; bot-token
    profiles cannot use it.

## lookup channel

Find channel and conversation metadata. With `--channel`, fetch one
conversation; without, list conversations of the requested types.

Slack filters results to conversations the token can read. A token with only
`channels:read` can list public channels without `groups:read`.
`--types all` requests every type; it does not grant access to more conversations.
Any one of `channels:read`, `groups:read`, `im:read`, or `mpim:read` permits the
list request. Slack still decides which conversations to return.

=== "List"

    List conversations, capped by `--max-items`:

    ```sh
    slick lookup channel --max-items 20
    ```

=== "By type"

    Narrow to DMs and group DMs, or ask for every conversation type:

    ```sh
    slick lookup channel --types im,mpim
    slick lookup channel --types all
    ```

=== "Single channel"

    Fetch metadata for one conversation by ID or configured alias:

    ```sh
    slick lookup channel --channel C1234567890
    ```

=== "Filter"

    Apply a client-side ID or name filter to the list:

    ```sh
    slick lookup channel --filter deploy
    ```

### Flags

??? note "Flags"

    | Flag | Value | Description |
    |------|-------|-------------|
    | `-c`, `--channel` | `<CHANNEL>` | Channel or conversation ID, or configured alias |
    | `-M`, `--max-items` | `<N>` | Maximum conversations to return |
    | `-C`, `--cursor` | `<CURSOR>` | Pagination cursor |
    | `-f`, `--filter` | `<TEXT>` | Filter by ID or name |
    | `-t`, `--types` | `<TYPE>` | Conversation types: public_channel, private_channel, im, mpim, dm, or all |

### Output

=== "Human"

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

=== "JSON"

    JSON data excerpt (list). Slack's conversation type is `channel` for public
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

`--channel` does not resolve Slack display names. To find a channel by name,
list with `--filter`, inspect the returned names, and use the matching ID.
The filter applies to one page, so follow `next_cursor` when more pages remain.
Configured aliases map directly to IDs. Lookup trims whitespace around the
argument and the stored alias value.
An alias that resolves to an empty value fails locally with exit 4, before
credentials are read or Slack is called.

## lookup user

Find user metadata. With `--user`, fetch one by ID or configured alias;
without, list active users. Lookup trims whitespace around the argument and
the stored alias value.

=== "List"

    List active users, capped by `--max-items`:

    ```sh
    slick lookup user --max-items 50
    ```

=== "Include deleted"

    Return the full `users.list`, including deleted and deactivated users:

    ```sh
    slick lookup user --include-deleted
    ```

=== "Single user"

    Fetch metadata for one user by ID:

    ```sh
    slick lookup user --user U1234567890
    ```

=== "Presence"

    Fetch presence alongside metadata for one user. Slack no longer provides
    presence through user lists:

    ```sh
    slick lookup user --user U123ABC --presence
    ```

=== "Filter"

    Apply a client-side filter to the list:

    ```sh
    slick lookup user --filter mcraven
    ```

### Flags

??? note "Flags"

    | Flag | Value | Description |
    |------|-------|-------------|
    | `-u`, `--user` | `<USER>` | Slack user ID |
    | `-M`, `--max-items` | `<N>` | Maximum users to return |
    | `-C`, `--cursor` | `<CURSOR>` | Pagination cursor |
    | `-f`, `--filter` | `<TEXT>` | Filter by ID or name |
    | `-p`, `--presence` | | Fetch presence |
    | `-d`, `--include-deleted` | | Include deleted or deactivated users |

List mode excludes deleted and deactivated users by default. Add
`--include-deleted` when you need the full `users.list` result.

### Output

=== "Human"

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

=== "JSON"

    JSON data excerpt:

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

Search uses Slack’s documented defaults: 20 results per page, sorted by score
in descending order. Its `--cursor` is a page number, while channel and user
lookup cursors are opaque tokens.

Slack limits this search method to 100 pages. If more results remain at page
100, slick keeps that page’s matches and reports `data.truncated=true` and
`pagination.has_more=true`, with no `next_cursor`. Narrow the query before
continuing; slick never suggests an unsupported page 101. Search results can
change between pages.

Search Slack workspace messages with the Web API `search.messages` method.
Requires a user token with `search:read`. Slack labels this method as legacy;
slick retains its existing query, scope, and page-number contract. See the
[method reference](https://docs.slack.dev/reference/methods/search.messages/).

Search result envelopes use `meta.command="lookup.messages"` for both success
and failure. Earlier builds used `search.messages` for successful results only.

=== "Search"

    Search for matching messages, capped by `--max-items`:

    ```sh
    slick lookup messages --query "deploy failed" --max-items 10
    ```

=== "Modifiers"

    Use the Slack search modifiers inside the query:

    ```sh
    slick lookup messages --query "in:#alerts after:2026-05-01" --max-items 50
    ```

=== "Paginate"

    Page through results with a cursor from the previous response:

    ```sh
    slick lookup messages --query "deploy" --cursor <meta.pagination.next_cursor>
    ```

=== "Full text"

    Show full match text in human mode, which otherwise truncates long matches:

    ```sh
    slick lookup messages --query "release" --full
    ```

### Flags

??? note "Flags"

    | Flag | Value | Description |
    |------|-------|-------------|
    | `-q`, `--query` | `<QUERY>` | Search query |
    | `-M`, `--max-items` | `<N>` | Matches per page: 1–100; 0 uses Slack’s default 20 |
    | `-C`, `--cursor` | `<CURSOR>` | Search page number: 1–100 |
    | `-F`, `--full` | | Show full text in human mode |

When Slack returns empty message text, slick extracts readable text from the
message's blocks. This includes file, canvas, citation, message, list, workflow,
and app references. Slick uses the supplied label or URL, then a reference ID
when available. Nonempty text returned by Slack takes precedence.

Query syntax follows Slack's search modifiers (`from:`, `in:`, `before:`,
`after:`, etc.).

=== "Human"

    When there are no matches:

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

    When Slack returns empty text for a [Block Kit](https://docs.slack.dev/block-kit/)
    message, slick extracts readable text from its blocks. An attribution context
    block, when present, follows the body on its own line. Nonempty text returned
    by Slack takes precedence over block text.

=== "JSON"

    JSON data excerpt. When Slack returns empty `text`, slick extracts readable
    text from `blocks[]`. A supplied attribution context line follows the body.
    Machine output always retains the full text; `--full` affects human output:

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
| `auth_failure` (exit 1) | `lookup messages` invoked under a bot-token profile. Slick rejects it before calling Slack. | Use a user-token profile with `search:read`. |
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

Channel, user, and message-search lists show pagination below human-readable
tables, including any next cursor, `has_more`, and `items_returned`. Empty
pages retain their continuation and an explicit zero count in all output modes.

## Incomplete Slack responses

A successful lookup must include the requested channel, user, list, or presence
value. Slick reports malformed or incomplete responses as `server_error`
(exit 5). A valid empty list is still a success; a missing list is an error.
Permission failures retain their normal class and scope details.

If email lookup fails while preparing a message, slick stops before opening a
DM or sending. It does not replay the lookup. Fix the response or permission
problem before starting another attempt. Optional metadata lookups can fall
back to known IDs instead of using malformed data.

## List and single-item flags

`--channel` and `--user` select a single-item lookup. List-only options are
rejected in that mode, including explicitly supplied empty or default values:

*   Channels: `--max-items`, `--cursor`, `--filter`, and `--types`.
*   Users: `--max-items`, `--cursor`, `--filter`, and `--include-deleted`.

Omit the item selector to use those list options. `--presence` remains available
with an explicit `--user`. A negative `--max-items` fails locally with exit 4;
zero uses the default page size. The limit applies to one Slack page before local
filtering, so a filtered page can contain fewer results while still having a
next cursor.

Channel lists accept `--max-items` from 0 through 999. Slack requires a page
limit below 1,000; zero omits the limit and uses Slack's default. See
[`conversations.list`](https://docs.slack.dev/reference/methods/conversations.list/). User lists
have a separate API contract and do not share this upper bound.

`--types` accepts the four Slack types (`public_channel`, `private_channel`,
`im`, and `mpim`) and slick's aliases (`channel`, `channels`, `public`,
`private`, `group`, `groups`, `dm`, `dms`, `group_dm`, and `group-dm`). Values
are case-insensitive and duplicates are removed. `all` expands to the four
types, but every other supplied value must still be valid. An unknown type
fails locally with exit 4, even alongside `all`. Empty input uses the default
public/private selection.
