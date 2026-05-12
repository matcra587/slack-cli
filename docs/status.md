# slick status

Set or clear the authenticated user's Slack status. Requires a user token
with `users.profile:write`; bot-token profiles cannot set a user's status.

```text
slick status set    Set Slack status
slick status clear  Clear Slack status
```

`meta.command` (`status.set` vs `status.clear`) distinguishes which path
ran. The pre-v0.4.0 `cleared` action-result bool was removed; the action
label conveys the outcome.

## status set

Set the user's Slack status. Text and emoji may be passed as flags or as
positional arguments.

```sh
slick status set --text "Heads down" --emoji :headphones: --expires-in 2h
slick status set "In a meeting" :calendar:
slick status set --text "Vacation" --until 2026-06-01T00:00:00Z
slick status set "PR review" :eyes: --dry-run
```

### Flags

```text
-t, --text <TEXT>            Status text
-e, --emoji <EMOJI>          Status emoji
-x, --expires-in <DURATION>  Status expiration duration
-U, --until <TIME>           Status expiration time
-n, --dry-run                Preview without mutating
```

`--expires-in` accepts Go duration strings (`30m`, `2h`, `8h30m`).
`--until` accepts an RFC3339 timestamp.

### Output

Human (no expiration set):

```text
Status set dry_run=true text="In a meeting" emoji=:calendar:
```

Human (with `--expires-in 2h`):

```text
Status set expiration=1747152000 dry_run=true text="Heads down" emoji=:headphones:
```

JSON envelope (no expiration). `expiration` uses `omitempty`, so it is
absent in the envelope when unset:

```json
{
  "data": {"text": "In a meeting", "emoji": ":calendar:", "dry_run": true}
}
```

JSON envelope with expiration. The value is a Unix timestamp (seconds since
epoch) at which Slack will clear the status:

```json
{
  "data": {"text": "Heads down", "emoji": ":headphones:", "expiration": 1747152000}
}
```

## status clear

Clear the user's Slack status.

```sh
slick status clear
slick status clear --dry-run
```

### Flags

```text
-n, --dry-run  Preview without mutating
```

Human:

```text
Status cleared dry_run=true
```

JSON envelope. All fields use `omitempty`, so `status clear` carries only
`dry_run` in the dry-run case and an empty `data` object on a real clear:

```json
{"data": {"dry_run": true}}
```

## Common errors

| Error | Cause | Fix |
|-------|-------|-----|
| `auth_failure: status requires a user token with users.profile:write` | Bot-token profile is active. | Switch to a user-token profile (`auth switch`). |
| `missing_scope` (`auth_failure`, exit 1) | Token lacks `users.profile:write`. | Update the manifest to include the scope, regenerate, re-auth. |
| `validation_error: status text or emoji is required` | Both fields empty on `status set`. | Provide at least one of `--text` / `--emoji`, or use positional args. |

## See also

*   [`auth`](auth.md) — switch between user-token and bot-token profiles.
*   [`manifest`](manifest.md) — generate a manifest that includes
  `users.profile:write`.
*   [README](https://github.com/matcra587/slack-cli#readme) and [index](index.md).
