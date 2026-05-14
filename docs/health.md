# slick health

Check Slack service health and Slack Web API reachability. These commands do
not use the configured workspace, token, or scopes.

```text
slick health check     Run Slack Status and api.test checks
slick health current   Show current Slack service status
slick health history   List recent Slack service incidents
slick health api-test  Check Slack Web API reachability with api.test
```

`health current` and `health history` call Slack Status API v2:

*   `https://slack-status.com/api/v2.0.0/current`
*   `https://slack-status.com/api/v2.0.0/history`

`health api-test` calls Slack Web API `api.test` without a token.

## health check

Run both the Web API probe and current service-status check.

```sh
slick health check
slick health check --service Messaging
slick health check --output=json
```

### Flags

```text
-s, --service <SERVICE>  Filter active incidents by Slack service
```

Human output:

```text
Slack health ok updated=2026-05-11T14:48:12-07:00 healthy=true status=ok api_ok=true active_incidents=0 total_active_incidents=0
```

JSON envelope:

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

```text
-s, --service <SERVICE>  Filter active incidents by Slack service
```

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

## health history

List recent Slack service incidents. The default limit is 20.

```sh
slick health history
slick health history --limit 10
slick health history --service Search --limit 5
```

### Flags

```text
-L, --limit <N>          Maximum incidents to return; 0 returns all
-s, --service <SERVICE>  Filter incidents by Slack service
```

Human output renders the same incident table as `health current`.

JSON output includes incident fields that may be hidden in a narrow human table:

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
        "note_count": 15
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

Human output:

```text
Slack API tested ok=true
```

JSON envelope:

```json
{
  "data": {"ok": true}
}
```

## Incident fields

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
  "note_count": 1
}
```

## Common errors

| Error | Cause | Fix |
|-------|-------|-----|
| `server_error: http status 503` | Slack Status or Slack Web API returned a non-2xx response. | Retry later or inspect Slack Status in a browser. |
| `server_error: decode response` | The endpoint returned malformed JSON. | Retry; if persistent, capture the response for debugging. |
| `timeout` (exit 7) | `--timeout` expired before the endpoint responded. | Increase `--timeout` or retry after checking network connectivity. |
| Slack Web API errors such as `invalid_auth`, `access_denied`, or `ratelimited` | `api.test` returned a documented Slack error response. | Read stderr JSON; `type`, `message`, and `exit_code` follow the normal slick error contract. |

## See also

*   [`status`](status.md) - set or clear your Slack profile status.
*   [`auth`](auth.md) - inspect local workspace authentication.
*   [Slack Status API](https://docs.slack.dev/reference/slack-status-api/)
*   [`api.test`](https://docs.slack.dev/reference/methods/api.test/)
