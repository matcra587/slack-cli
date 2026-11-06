---
description: "Set and clear your Slack profile status with slick."
---

# slick status

**Set** or **clear** the authenticated user's Slack status.

!!! note "Requires a user token"
    Requires a user token with `users.profile:write`; bot-token profiles
    cannot set a user's status.

This command is for your Slack profile status. Use [`health`](health.md) for
Slack service health and Slack Web API reachability.

`meta.command` (`status.set` vs `status.clear`) distinguishes which path
ran. The pre-v0.4.0 `cleared` action-result bool was removed; the action
label conveys the outcome.

## status set

Set the user's Slack status. Text and emoji may be passed as flags or as
positional arguments. The first positional argument supplies text and the second
supplies emoji. A nonempty `--text` or `--emoji` value takes priority over its
matching positional argument.

Status text is plain text; Slack does not render Markdown or mentions in it.

=== "Flags"

    Set status with flags and a relative expiry:

    ```sh
    slick status set --text "Heads down" --emoji :headphones: --expires-in 2h
    ```

=== "Positional"

    Pass text and emoji as positional arguments:

    ```sh
    slick status set "In a meeting" :calendar:
    ```

=== "Until"

    Expire at an absolute RFC3339 time:

    ```sh
    slick status set --text "Vacation" --until 2027-06-01T00:00:00Z
    ```

=== "Dry run"

    Preview without mutating. Dry runs do not resolve credentials or check scopes:

    ```sh
    slick status set "PR review" :eyes: --dry-run
    ```

### Flags

??? note "Flags"

    | Flag | Value | Description |
    |------|-------|-------------|
    | `-t`, `--text` | `<TEXT>` | Status text |
    | `-e`, `--emoji` | `<EMOJI>` | Status emoji |
    | `-x`, `--expires-in` | `<DURATION>` | Status expiration duration |
    | `-U`, `--until` | `<TIME>` | Status expiration time |
    | `-n`, `--dry-run` | | Preview without mutating |

`--expires-in` accepts Go duration strings (`30m`, `2h`, `8h30m`). A positive
value expires that duration from now; `0` leaves the status without an expiry.
Negative values are rejected.

`--until` accepts a Unix timestamp in seconds, an RFC3339 timestamp, a relative
value such as `+2h`, or a local clock time (`HH:MM` or `HH:MM:SS`). A clock time
uses the process's local timezone (`TZ` when supported): today when it is
still in the future, otherwise tomorrow. Clock times use calendar dates, including days with daylight-saving changes. Relative durations
measure elapsed time, so `+24h` can end at a different local clock time tomorrow.
A positive `--expires-in` value cannot be combined with a nonempty `--until`.
Zero duration and an empty `--until` are treated as unset when choosing a format.
With neither option, the status does not expire.

### Output

Results contain the requested status values, rather than a fresh read of your
profile. Slack can choose a default emoji when text is supplied without one.
Only the three custom-status fields are sent; other profile settings are left
alone.

If Slack returns a missing or invalid acknowledgement, slick exits with
`server_error` (exit 5). The update may already have happened. Slick does not
replay it automatically; check your status before deciding whether to try again.

=== "Human"

    Human (no expiration set):

    ```text
    Status set dry_run=true text="In a meeting" emoji=:calendar:
    ```

    Human (with `--expires-in 2h`):

    ```text
    Status set expiration=1747152000 dry_run=true text="Heads down" emoji=:headphones:
    ```

=== "JSON"

    JSON data excerpt. `expiration` is absent when unset:

    ```json
    {
      "data": {"text": "In a meeting", "emoji": ":calendar:", "dry_run": true}
    }
    ```

    Result data with expiration. The value is a Unix timestamp (seconds since
    epoch) at which the Slack API will clear the status:

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

??? note "Flags"

    | Flag | Value | Description |
    |------|-------|-------------|
    | `-n`, `--dry-run` | | Preview without mutating |

=== "Human"

    Human:

    ```text
    Status cleared dry_run=true
    ```

=== "JSON"

    JSON data excerpt. Empty fields are omitted, so a preview carries only `dry_run`.
    A real clear returns an empty `data` object:

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
*   [`health`](health.md) — check Slack service and Web API health.
*   [`manifest`](manifest.md) — generate a manifest that includes
  `users.profile:write`.
*   Slack API method: [`users.profile.set`](https://docs.slack.dev/reference/methods/users.profile.set/).
