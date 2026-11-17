---
description: "Inspect, refresh, and clear slick metadata caches for channels and users."
---

# slick cache

**Prime** and **inspect** local Slack metadata caches. Cached users and channels
power shell-completion suggestions and reduce Slack API pressure on repeated
commands.

`cache channels` stores the conversations the token can read. It accepts any one
of `channels:read`, `groups:read`, `im:read`, or `mpim:read`; the token does not
need all four. Slack filters out conversations outside its access.

`cache users`, `cache channels`, and `cache clear` support `--dry-run` or `-n`.
They return a plan with `profile`, `resource`, and `dry_run: true`, without
credential lookups, HTTP requests, or file changes. Clearing a whole profile
uses `resource: "all"`. A preview does not fetch data or check Slack permissions,
cache contents, or filesystem access. Invalid options, including an excessive
TTL, still fail validation. Remove the flag or use `--dry-run=false` to perform
the operation.

Shell suggestions use the selected profile's fresh cache before reading
credentials. Empty lists, including cached users that are all deleted, return
no suggestions without fetching again. Local workspace, config, guide, and
cache-name suggestions, shell script generation, and completion installation
need no token.

If a completion update fails before the new script is published, the previous
script stays intact. Existing symlinks and file permission bits are preserved.
Filesystem failures use exit 5. Error details include `may_have_changed` when
publication may already have happened. A failure to write the result uses
exit 8; the requested change may already be complete. Do not treat a lost
result as proof that the operation failed.

When Slack is needed, credential lookup and the API call share a five-second
budget and honor a shorter `--timeout`. Failures return no suggestions.
Completion reads do not update cache files. If a suggestion or completion script cannot be written
to stdout, slick stops writing and returns exit 8 with `output_write_failed`
on stderr. Ignore any partial suggestion or script output on exit 8. Slick does not
append a JSON envelope to the shell protocol. `--workspace` selects the profile
for channel and user suggestions; a `workspaces.<profile>.default_channel`
config key uses the profile named in that key, including names with dots.

Cache files live under `${XDG_CACHE_HOME:-~/.cache}/slick/<profile>/`. The
default freshness window is 24 hours.

Short, portable profile names such as `default` keep their named cache folders.
Names with path separators, spaces, Unicode, a trailing dot, or Windows device
names use a stable folder identifier instead. Long names use that identifier
too. Distinct profiles keep separate cache data, including names such as `a/b`
and `a_b`.

Older versions replaced some characters with underscores, which could put
different profiles in one folder. Slick now fetches a new cache for affected
names. It does not move or clear a former shared folder when serving an
affected profile.

A cache hit must match the selected profile and resource and contain a valid
user or channel list. An explicit empty list is valid. Missing or null lists,
items without IDs, invalid field types, mismatched metadata, and malformed JSON
trigger a fresh
fetch. If that fetch fails, slick reports the failure and keeps the prior file
unchanged. A successful fetch replaces it with a private file containing only
the supported metadata fields.

## cache users / cache channels

In human terminal sessions, cache fetches show page and save activity through
[terminal status](index.md#terminal-status).
Fresh cache hits and dry runs omit these reports.

Prime the cache (or read it back if fresh). Both commands accept the same
flags.

=== "Prime"

    Prime the cache, or read it back when it is still fresh:

    ```sh
    slick cache users
    ```

=== "Refresh"

    Force a fetch even when the cache is fresh:

    ```sh
    slick cache channels --refresh
    ```

=== "Custom TTL"

    Set a one-week freshness window:

    ```sh
    slick cache users --ttl-minutes 10080
    ```

=== "Page sizing"

    Tune the Slack page size and page cap while priming:

    ```sh
    slick cache channels --page-size 200 --max-pages 50
    ```

### Flags

??? note "Flags"

    | Flag | Value | Description |
    |------|-------|-------------|
    | `-r`, `--refresh` | | Force a fetch even when the cache is fresh |
    | `-T`, `--ttl-minutes` | `<MINUTES>` | Freshness window; maximum 153722867, nonpositive uses 1440 |
    | `-s`, `--page-size` | `<N>` | Slack page size, capped at 200; nonpositive uses 200 |
    | `-N`, `--max-pages` | `<N>` | Maximum Slack pages; nonpositive uses 20 |

Page size and page count apply when fetching; changing them does not refresh
a fresh cache. Add `--refresh` to fetch again. A TTL above 153722867 minutes
fails locally with exit 4 before reading credentials or using cached results.
Smaller positive TTL values are used as supplied; zero and negative values
keep the default 24-hour window.

### Output

Human output is a single summary event — the command does not echo the full
cached list. JSON envelope carries the data.

=== "Human"

    Human (fresh fetch — `from_cache` is omitted entirely):

    ```text
    User cache primed profile=default resource=users count=128
    Channel cache primed profile=default resource=channels count=42
    ```

    Human (served from cache):

    ```text
    User cache primed profile=default resource=users fetched_at=2026-05-10T14:38:47Z from_cache=true count=128
    ```

    `fetched_at` only appears when serving from cache; a fresh fetch is
    implicitly "just now" and the field is omitted from the event.

=== "JSON"

    Data excerpt from the JSON envelope (`cache users`; user list shortened):

    ```json
    {
      "data": {
        "profile": "default",
        "users": [{"id": "U…", "name": "…", "tz": "…"}],
        "count": 128,
        "from_cache": false,
        "fetched_at": "2026-05-10T14:38:47Z"
      }
    }
    ```

!!! note "Truncated results"
    `truncated=true` means the prime hit `--max-pages` before exhausting the
    Slack pagination cursor — increase `--max-pages` and add `--refresh` if
    you need the full listing. The field is omitted when the listing is complete.

## cache clear

Remove cached metadata. With a `<resource>` argument, removes one resource;
without, removes `.json` cache files directly under the active profile.
Other files, nested directories, and other profiles are preserved.

=== "Clear users"

    Remove the cached users for the active profile:

    ```sh
    slick cache clear users
    ```

=== "Clear channels"

    Remove the cached channels for the active profile:

    ```sh
    slick cache clear channels
    ```

=== "Sweep all"

    Sweep every cache file under the active profile:

    ```sh
    slick cache clear
    ```

### Output

=== "Human"

    Single resource — success path. Human:

    ```text
    Cache cleared profile=default resource=users cleared=true
    ```

    Already empty renders a different human-mode label and `cleared=false` in
    the JSON envelope:

    ```text
    Cache already empty profile=default resource=users
    ```

    Sweep with content — human mode keeps `removed_count` for human reading
    even though the field is no longer in the JSON envelope:

    ```text
    Cache cleared profile=default resources=channels,users removed_count=2
    ```

    Human mode for an empty sweep:

    ```text
    Cache already empty profile=default
    ```

=== "JSON"

    Data excerpts from the JSON envelope:

    ```json
    {"data": {"profile": "default", "resource": "users", "cleared": true}}
    ```

    ```json
    {"data": {"profile": "default", "resource": "users", "cleared": false}}
    ```

    Sweep (resources alphabetized):

    ```json
    {"data": {"profile": "default", "resources": ["channels", "users"]}}
    ```

    Empty sweep (the `resources` array is omitted entirely when the sweep had
    nothing to remove):

    ```json
    {"data": {"profile": "default"}}
    ```

    Partial failure mid-sweep surfaces the resources removed before the failure
    in `errors[0].details`:

    ```json
    {
      "data": null,
      "errors": [{
        "type": "server_error",
        "exit_code": 5,
        "message": "remove …/users.json: permission denied",
        "details": {"partial": ["channels"], "removed_count": 1}
      }]
    }
    ```

The pre-v0.4.0 `removed` field is now `cleared`; `removed_count` was dropped
from the JSON payload (callers compute `len(resources)` from the sweep
response).

## See also

*   [`lookup`](lookup.md) — direct Slack lookups; cached metadata can supply
    conversation labels, but the requested lookup still calls Slack.
*   [`config`](config.md) — inspect and manage workspace profiles.
*   Slack API methods: [`users.list`](https://docs.slack.dev/reference/methods/users.list/),
  [`conversations.list`](https://docs.slack.dev/reference/methods/conversations.list/).

## Install or remove shell completion

These root flags manage the generated completion entry for the selected shell:

```sh
slick --install-completion --@shell=fish --dry-run --output=json
slick --install-completion --@shell=fish --output=json
slick --uninstall-completion --@shell=fish --output=json
```

Supported shells are `bash`, `zsh`, `fish`, `nu`, `pwsh`, and `elvish`.
`--dry-run` reports the plan without creating directories, changing scripts,
or removing entries. Removing a missing entry succeeds with `changed=false`.
Removal unlinks a symlink without deleting its target. A directory or another
non-file object at the completion path is left alone and returns exit 5.
Other scripts and shell settings remain untouched.

Install and removal successes use stdout in every output mode: JSON has the
standard envelope, compact has the data object, and human mode has a text
status. Failures follow the usual error contract. Operation data has these fields:

| Field | Meaning |
|-------|---------|
| `action` | `install` or `uninstall` |
| `shell` | Selected shell |
| `path` | Completion entry being managed |
| `changed` | This invocation wrote or removed the entry |
| `dry_run` | The operation was a plan |
| `would_change` | The planned operation would change the entry; used with `dry_run=true` |

`slick completion fish` and `--print-completion` print shell source.
That output remains plain text, including with `--output=json`.

Completion actions (`--print-completion`, `--install-completion`,
`--uninstall-completion`, and `--@complete`) must be invoked on `slick` without a
subcommand. Active actions on a subcommand return validation exit 4 before
credentials or Slack calls. Explicit false flags and an empty `--@complete`
value leave normal command execution unchanged. Generated root callbacks may
still pass positional context after `--`.
