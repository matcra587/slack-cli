# slick react

Add, remove, or list emoji reactions on a known message. Requires
`reactions:write` for mutating commands and `reactions:read` for `list`.

```text
slick react add     add a Slack reaction
slick react remove  remove a Slack reaction
slick react list    List Slack reactions
```

## react add / react remove

```sh
slick react add    --channel C1234567890 --timestamp 1746284582.123456 --emoji :thumbsup:
slick react remove --channel C1234567890 --timestamp 1746284582.123456 --emoji :thumbsup:

# Multiple emoji in order (applied or removed left-to-right)
slick react add --channel C1234567890 --timestamp 1746284582.123456 \
  --emoji thumbsup,white_check_mark,rocket

# Preview without mutating
slick react add --channel C1234567890 --timestamp 1746284582.123456 \
  --emoji :thumbsup: --dry-run
```

### Flags

```text
-c, --channel <CHANNEL>      Channel ID, name, or alias
-t, --timestamp <TS>         Message timestamp
-e, --emoji <NAME>…          Emoji name; repeat or comma-separate to apply multiple in order
-n, --dry-run                Preview without mutating
```

Emoji names accept both `thumbsup` and `:thumbsup:` forms. Ordered
multi-emoji halts on the first failure; `data.mutations[]` is absent on the
error envelope, so retry the remaining emojis from a known-good state rather
than assuming partial success.

### Output

Human (real, single emoji — no `dry_run` field):

```text
Reaction added channel=C7N2Q8L4P ts=1746284582.123456 age=now emoji=thumbsup
```

Human (`--dry-run`, single emoji):

```text
Reaction added channel=C7N2Q8L4P ts=1746284582.123456 age=3m dry_run=true emoji=thumbsup
```

Human (real, multiple emoji — one event line per emoji, in argument order):

```text
Reaction added channel=C7N2Q8L4P ts=1746284582.123456 age=now emoji=rocket
Reaction added channel=C7N2Q8L4P ts=1746284582.123456 age=now emoji=white_check_mark
```

Human (`react remove`):

```text
Reaction removed channel=C7N2Q8L4P ts=1746284582.123456 age=now emoji=thumbsup
```

JSON envelope:

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

### Flags

```text
-c, --channel <CHANNEL>  Channel ID, name, or alias
-t, --timestamp <TS>     Message timestamp
```

### Output

Human mode renders a primer table:

```text
EMOJI             COUNT  USERS
thumbsup          3      U123,U456,U789
white_check_mark  1      U123
```

JSON envelope. Slack normalises some emoji aliases to their canonical
names in the response (e.g. `thumbsup` → `+1`, `thumbsdown` → `-1`); the
`name` field reflects Slack's canonical form, not necessarily the alias
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

## Common errors

| Error | Cause | Fix |
|-------|-------|-----|
| `invalid_name: Slack does not recognize that emoji name` (validation, exit 4) | The emoji name is not a custom workspace emoji and not a recognised Slack default. | Check spelling; try with/without colons; verify in the Slack composer. |
| `already_reacted` (validation, exit 4) | This emoji is already on the message from the same user. | Skip; treat as idempotent success at the caller. |
| `no_reaction` (`not_found`, exit 2) | `react remove` cannot find that emoji on the message. | Confirm with `react list` before removing. |
| `too_many_reactions` (validation, exit 4) | Slack message reaction limit reached. | Cannot work around; surface to user. |
| `not_in_channel` (`not_found`, exit 2) | App/user is not a member of the target channel. | Add the bot, or use a user-token profile. |

## See also

*   [`message`](message.md) — send the message before reacting to it.
*   [`history`](history.md) — look up the timestamp.
*   [README](https://github.com/matcra587/slack-cli#readme) and [index](index.md).
