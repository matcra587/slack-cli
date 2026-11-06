---
description: "Check Slack service health and test access to the Slack API."
---

# slick health

**Check** Slack service health and Slack Web API reachability. These commands do
not use the configured workspace, token, or scopes. JSON examples below show
the `data` portion of the standard result envelope.

`health current` and `health history` call Slack Status API v2:

*   `https://slack-status.com/api/v2.0.0/current`
*   `https://slack-status.com/api/v2.0.0/history`

`health api-test` calls Slack Web API `api.test` without a token.

Malformed, incomplete, or oversized responses (over 5 MiB) fail with a structured error.
An API probe succeeds only when Slack returns `ok: true`. Empty status lists
are valid; missing or `null` responses are errors. A valid status response that
reports an incident remains a successful read; inspect `healthy` from
`health check` to decide whether the selected service is healthy.

## health check

Run both the Web API probe and current service-status check.

```sh
slick health check
slick health check --service Messaging
slick health check --output=json
```

### Flags

??? note "Flags"

    | Flag | Value | Description |
    |------|-------|-------------|
    | `-s`, `--service` | `<SERVICE>` | Filter active incidents by Slack service |

`--service` trims surrounding whitespace and matches service names without
regard to case. An empty value removes the filter. Filtering happens locally;
unknown names return no matching incidents. `--service` supports shell
completion for common Slack Status services:

```text
Apps/Integrations/APIs
Canvases
Connectivity
Files
Huddles
Login/SSO
Messaging
Notifications
Search
Workflows
Workspace/Org Administration
```

=== "Human"

    Human output:

    ```text
    Slack health ok updated=2026-05-11T14:48:12-07:00 healthy=true status=ok api_ok=true active_incidents=0 total_active_incidents=0
    ```

=== "JSON"

    JSON data excerpt:

    ```json
    {
      "data": {
        "healthy": true,
        "status": "ok",
        "api_ok": true,
        "date_updated": "2026-05-11T14:48:12-07:00",
        "active_incidents": [],
        "active_incident_count": 0,
        "total_active_incident_count": 0
      }
    }
    ```

Without `--service`, `healthy` is true only when `api_ok` is true, Slack
Status reports `ok`, and there are no active incidents. With `--service`,
`healthy` is true when `api_ok` is true and there are no active incidents for
that service.

## health current

Show active incidents from Slack Status.

```sh
slick health current
slick health current --service "Apps/Integrations/APIs"
```

### Flags

??? note "Flags"

    | Flag | Value | Description |
    |------|-------|-------------|
    | `-s`, `--service` | `<SERVICE>` | Filter active incidents by Slack service |

=== "Human"

    When active incidents exist, human output renders a table:

    ```text
    ID   STATUS  TYPE      UPDATED                    SERVICES         NOTES
    546  active  incident  2026-05-11T14:48:12-07:00  Messaging,Files  1
    ```

    The trailing `TITLE` column is flexible and may appear on wider terminals. JSON
    output always includes `title` and `url`.

    With no active incidents, human output is a compact event:

    ```text
    Slack status retrieved updated=2026-05-11T14:48:12-07:00 status=ok active_incidents=0 total_active_incidents=0
    ```

=== "JSON"

    JSON data excerpt with no active incidents:

    ```json
    {
      "data": {
        "status": "ok",
        "active_incidents": [],
        "active_incident_count": 0,
        "total_active_incident_count": 0
      }
    }
    ```

    Dates are included when Slack supplies them.

## health history

List recent Slack service incidents. The default limit is 20. Filtering happens
before the limit is applied. Use `--limit 0` for all matching incidents; negative
limits are rejected before any request. Slack returns the history in one list,
so the limit changes the output size rather than the API request.

```sh
slick health history
slick health history --limit 10
slick health history --service Search --limit 5
```

### Flags

??? note "Flags"

    | Flag | Value | Description |
    |------|-------|-------------|
    | `-L`, `--limit` | `<N>` | Maximum incidents to return; 0 returns all |
    | `-s`, `--service` | `<SERVICE>` | Filter incidents by Slack service |

=== "Human"

    Human output renders the same incident table as `health current`.

=== "JSON"

    JSON data excerpt with incident fields that may be hidden in a narrow human table:

    ```json
    {
      "data": {
        "limit": 1,
        "incidents": [
          {
            "id": "1551",
            "title": "EKM customers were previously experiencing issues with channel loading and message delivery",
            "type": "incident",
            "status": "resolved",
            "url": "https://slack-status.com/2026-05/d284d808ed66c511",
            "date_created": "2026-05-08T11:31:22-07:00",
            "date_updated": "2026-05-11T14:48:12-07:00",
            "services": ["Messaging", "Files", "Notifications", "Apps/Integrations/APIs"],
            "notes": [{"body": "Messaging has recovered. We are monitoring.", "date_created": "2026-05-11T14:48:12-07:00"}],
            "note_count": 1
          }
        ],
        "incident_count": 1
      }
    }
    ```

## health api-test

Call Slack Web API `api.test`.

```sh
slick health api-test
slick health api-test --output=json
```

=== "Human"

    Human output:

    ```text
    Slack API tested ok=true
    ```

=== "JSON"

    JSON data excerpt:

    ```json
    {
      "data": {"ok": true}
    }
    ```

## Incident fields

JSON and compact results preserve each incident note's `body` and `date_created`
from the [Slack Status API](https://docs.slack.dev/reference/slack-status-api/).
Notes keep their original text, timestamps, and order. Incidents without notes
include `notes: []` and `note_count: 0`. The human table shows the count.

Incident rows use this shape in JSON:

```json
{
  "id": "1551",
  "title": "Messaging is delayed",
  "type": "incident",
  "status": "active",
  "url": "https://slack-status.com/...",
  "date_created": "2026-05-11T13:00:00-07:00",
  "date_updated": "2026-05-11T14:48:12-07:00",
  "services": ["Messaging", "Apps/Integrations/APIs"],
  "notes": [{"body": "We are investigating delivery delays.", "date_created": "2026-05-11T13:00:00-07:00"}],
  "note_count": 1
}
```

Health probes send each request once. HTTP `429` returns `rate_limit` (exit 3).
When one `Retry-After` header contains valid whole seconds, the error includes
`retry_after_seconds`, including zero. Missing, malformed, or conflicting
headers leave that field out. Wait for the reported delay before starting a
new probe; slick does not sleep or retry a health request automatically. See
[Slack's rate-limit guidance](https://docs.slack.dev/apis/web-api/rate-limits/).

## Common errors

| Error | Cause | Fix |
|-------|-------|-----|
| `rate_limit` (exit 3) | The endpoint returned HTTP 429. | Wait for `retry_after_seconds` when supplied, then start a new probe. |
| `server_error` (exit 5) | A request failed, or the endpoint returned another non-2xx, malformed, incomplete, or oversized response. HTTP failures include the numeric status and its standard name. | Check Slack Status and retry later. |
| `timeout` (exit 7) | `--timeout` expired before the endpoint responded. | Increase `--timeout` or retry after checking network connectivity. |
| Slack Web API errors such as `invalid_auth`, `access_denied`, or `ratelimited` | `api.test` returned a documented Slack error response. | Read the stdout failure envelope; `type`, `message`, and `exit_code` follow the normal slick error contract. |

## See also

*   [`status`](status.md) - set or clear your Slack profile status.
*   [`auth`](auth.md) - inspect local workspace authentication.
*   [Slack Status API](https://docs.slack.dev/reference/slack-status-api/)
*   [`api.test`](https://docs.slack.dev/reference/methods/api.test/)
