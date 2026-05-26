# slick cache

**Prime** and **inspect** local Slack metadata caches. Cached users and channels
power shell-completion suggestions and reduce Slack API pressure on repeated
lookups.

Cache files live under `${XDG_CACHE_HOME:-~/.cache}/slick/<profile>/`. The
default freshness window is 24 hours.

## cache users / cache channels

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
    | `-T`, `--ttl-minutes` | `<MINUTES>` | Freshness window before automatic refresh |
    | `-s`, `--page-size` | `<N>` | Slack page size while priming |
    | `-N`, `--max-pages` | `<N>` | Maximum Slack pages to fetch |

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

    JSON envelope (`cache users`):

    ```json
    {
      "data": {
        "profile": "default",
        "users": [{"id": "U…", "name": "…", "tz": "…"}],
        "count": 128,
        "from_cache": false,
        "fetched_at": "2026-05-10T14:38:47Z",
        "truncated": false
      }
    }
    ```

!!! note "Truncated results"
    `truncated=true` means the prime hit `--max-pages` before exhausting the
    Slack pagination cursor — increase `--max-pages` if you need the full
    listing.

## cache clear

Remove cached metadata. With a `<resource>` argument, removes one resource;
without, sweeps every cache file under the active profile.

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

    JSON envelope:

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

*   [`lookup`](lookup.md) — direct Slack lookups (uses cache when fresh).
*   [`config`](config.md) — `slick config path` shows where the cache root is
  derived from.
*   Slack API methods: [`users.list`](https://docs.slack.dev/reference/methods/users.list/),
  [`conversations.list`](https://docs.slack.dev/reference/methods/conversations.list/).
