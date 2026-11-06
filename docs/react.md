---
description: "Add, remove, inspect, or wait for emoji reactions on Slack messages."
---

# slick react

Add, remove, list, or watch emoji reactions on a known message. Mutations need
`reactions:write`; list and watch need `reactions:read`.

Use a Slack channel ID or an alias configured in the selected workspace profile.
Aliases resolve locally before the Slack request and in dry-run previews.
Surrounding whitespace is removed from channel inputs, stored alias values,
and timestamps. Timestamp digits, including fractional seconds, are preserved
in API requests, results, and previews.

## react add / react remove

=== "Single emoji"

    Add or remove one reaction:

    ```sh
    slick react add    --channel C1234567890 --timestamp 1746284582.123456 --emoji :thumbsup:
    slick react remove --channel C1234567890 --timestamp 1746284582.123456 --emoji :thumbsup:
    ```

=== "Multiple emoji"

    Apply or remove several emoji in order, left-to-right:

    ```sh
    slick react add --channel C1234567890 --timestamp 1746284582.123456 \
      --emoji thumbsup,white_check_mark,rocket
    ```

=== "Dry run"

    Preview without mutating:

    ```sh
    slick react add --channel C1234567890 --timestamp 1746284582.123456 \
      --emoji :thumbsup: --dry-run
    ```

### Flags

??? note "Flags"

    | Flag | Value | Description |
    |------|-------|-------------|
    | `-c`, `--channel` | `<CHANNEL>` | Channel ID or configured alias |
    | `-t`, `--timestamp` | `<TS>` | Message timestamp |
    | `-e`, `--emoji` | `<NAME>…` | Emoji name; repeat or comma-separate to apply multiple in order |
    | `-n`, `--dry-run` | | Preview without mutating |

Emoji names accept both `thumbsup` and `:thumbsup:` forms.

!!! warning "Ordered multi-emoji"
    Ordered multi-emoji halts on the first failure; `data.mutations[]` is
    absent on the error envelope. `errors[].details.completed_emojis` lists
    requests Slack acknowledged. `attempted_emoji` identifies the request that
    returned an error; `unattempted_emojis` lists work that never started.
    Earlier successes remain in Slack. If an HTTP or server failure leaves the
    attempted emoji's outcome uncertain, inspect `react list` before retrying.
    Error details also retain the exact channel and timestamp.

### Output

=== "Human"

    Human (real, single emoji — no `dry_run` field):

    ```text
    Reaction added channel=C7N2Q8L4P ts=1746284582.123456 emoji=thumbsup
    ```

    Human (`--dry-run`, single emoji):

    ```text
    Reaction added channel=C7N2Q8L4P ts=1746284582.123456 dry_run=true emoji=thumbsup
    ```

    Human (real, multiple emoji — one event line per emoji, in argument order):

    ```text
    Reaction added channel=C7N2Q8L4P ts=1746284582.123456 emoji=rocket
    Reaction added channel=C7N2Q8L4P ts=1746284582.123456 emoji=white_check_mark
    ```

    Human (`react remove`):

    ```text
    Reaction removed channel=C7N2Q8L4P ts=1746284582.123456 emoji=thumbsup
    ```

=== "JSON"

    JSON data excerpt:

    ```json
    {
      "data": {
        "mutations": [
          {"channel": "C7N2Q8L4P", "ts": "1746284582.123456", "emoji": "thumbsup", "dry_run": true}
        ],
        "target": {"channel": "C7N2Q8L4P", "ts": "1746284582.123456"}
      }
    }
    ```

`meta.command` (`react.add` vs `react.remove`) distinguishes the two paths.
The `removed` action-result bool was dropped in v0.4.0; the action label and
`errors[]` carry the same signal.

## react list

```sh
slick react list --channel C1234567890 --timestamp 1746284582.123456
```

List requests Slack's complete reaction list. An empty result includes
`reactions: []` in JSON and compact output. Slack can still omit some reacting
users: use each reaction's `count` for its total, rather than the length of
`users`. All reaction commands reject positional arguments.

Missing acknowledgements, conflicting success/error fields, or missing reaction
items return a server error (exit 5). Watch does not accept a match from a
conflicting response. The failed request is not automatically replayed. A mutation
may have reached Slack before an invalid response arrived; inspect the message
before retrying it.

### Flags

??? note "Flags"

    | Flag | Value | Description |
    |------|-------|-------------|
    | `-c`, `--channel` | `<CHANNEL>` | Channel ID or configured alias |
    | `-t`, `--timestamp` | `<TS>` | Message timestamp |

### Output

=== "Human"

    Human mode renders a primer table:

    ```text
    EMOJI             COUNT  USERS
    thumbsup          3      U123,U456,U789
    white_check_mark  1      U123
    ```

=== "JSON"

    JSON data excerpt. The Slack API normalises some emoji aliases to their
    canonical names in the response (e.g. `thumbsup` → `+1`, `thumbsdown` → `-1`);
    the `name` field reflects the canonical Slack form, not necessarily the alias
    you passed to `react add`:

    ```json
    {
      "data": {
        "reactions": [
          {"name": "+1", "count": 3, "users": ["U123", "U456", "U789"]},
          {"name": "white_check_mark", "count": 1, "users": ["U123"]}
        ],
        "target": {"channel": "C1234567890", "ts": "1746284582.123456"}
      }
    }
    ```

## react watch

Wait for an emoji reaction, then continue a script:

```sh
slick react watch --channel C1234567890 --timestamp 1746284582.123456 \
  --emoji :+1: --timeout 10m --output=json && ./next-step.sh
```

The command only reads reactions. A matching reaction that already exists
satisfies the watch immediately. `:+1:` and `:thumbsup:` match the same reaction.
Use `--user U1234567890` to wait until Slack explicitly reports that user.

| Flag | Meaning |
| --- | --- |
| `--channel`, `-c` | Channel containing the message |
| `--timestamp`, `-t` | Message timestamp, kept as a string |
| `--emoji`, `-e` | One emoji name, with or without colons |
| `--user`, `-U` | Optional reacting user ID |
| `--interval`, `-i` | Polling interval; default 5 seconds, minimum 1 second |
| `--timeout`, `-I` | Total command deadline; default 30 seconds |

Successful watches write one result containing `data.target` and `data.reactions`.
Compact output contains those fields directly. A deadline returns exit 7; a
canceled command returns exit 6. Both use the common machine failure envelope.
Progress and diagnostics belong on stderr, so stdout can feed another command.

Slack can omit reacting users even when it returns the correct reaction count.
A user-filtered watch may therefore time out without proving that user has not
reacted. Polling can also miss a reaction added and removed between requests.
The token needs `reactions:read` and access to the message.

## Common errors

| Error | Cause | Fix |
|-------|-------|-----|
| `invalid_name: Slack does not recognize that emoji name` (validation, exit 4) | The emoji name is not a custom workspace emoji and not a recognised Slack default. | Check spelling; try with/without colons; verify in the Slack composer. |
| `already_reacted` (validation, exit 4) | This emoji is already on the message from the same user. | Skip; treat as idempotent success at the caller. |
| `no_reaction` (`not_found`, exit 2) | `react remove` cannot find that emoji on the message. | Confirm with `react list` before removing. |
| `too_many_emoji` (validation, exit 4) | The message has reached its limit for distinct emoji. | Inspect existing reactions before choosing another action. |
| `too_many_reactions` (validation, exit 4) | The authenticated person has reached their reaction limit on this message. | Inspect that person's existing reactions. |
| `thread_locked` / `is_archived` (validation, exit 4) | Reactions are disabled for the target thread or channel. | Choose an available target. |
| `bad_timestamp` (validation, exit 4) | Slack rejected the timestamp. | Copy the exact string from history. |
| `no_access` / `access_denied` (auth, exit 1) | The authenticated identity cannot access the target. | Check the workspace profile and target access. |
| `not_in_channel` (`not_found`, exit 2) | App/user is not a member of the target channel. | Add the bot, or use a user-token profile. |

## See also

*   [`message`](message.md) — send the message before reacting to it.
*   [`history`](history.md) — look up the timestamp.
*   Slack API methods: [`reactions.add`](https://docs.slack.dev/reference/methods/reactions.add/),
  [`reactions.remove`](https://docs.slack.dev/reference/methods/reactions.remove/),
  [`reactions.get`](https://docs.slack.dev/reference/methods/reactions.get/).
