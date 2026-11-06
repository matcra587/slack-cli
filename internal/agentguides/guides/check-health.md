---
slug: check-health
title: "Check Slack service health and Web API reachability"
description: "Check Slack service health and Web API reachability"
when_to_use: "Before following the check health workflow in slick."
commands: ["slick health check", "slick health current", "slick health history", "slick health api-test"]
aliases: ["check_health"]
---

## Decide

Goal: Decide whether a failing Slack call is the Slack service, the Web API, your network, or your auth — before blaming auth or scopes.

*   Combined probe (status + api.test): `slick health check`.
*   Status only: `slick health current` (or `history` for past incidents).
*   API only: `slick health api-test`.
*   Narrow to one service: `--service <Messaging|Search|Files|Apps/Integrations/APIs>`.

### Workflow checkpoints

*   Use health check for a combined Slack Status plus api.test probe
*   Use health current for active incidents
*   Use health history for recent Slack incidents
*   No Slack token or scopes are required
*   Parse data.healthy, data.api_ok, data.status, and data.active_incidents[] from JSON

## Run

*   Combined: `slick health check --output=json`
*   Service-filtered: `slick health check --service Messaging --output=json`
*   API probe: `slick health api-test --output=json`
*   History: `slick health history --limit 20 --output=json`

### Behavior

*   These commands do NOT use the configured workspace, token, or scopes. A failing health command points at Slack-side service, network, timeout, or malformed-response issues — not at profile auth.
*   Slack Status `current` can report `status=active` for services unrelated to your workflow. Filter with `--service` when only one matters.

## Save

> Requires `--output=json`.
*   `data.healthy` [bool, required] — combined verdict.
*   `data.api_ok` [bool, required] — Web API reachability.
*   `data.status` [string, required] — Slack Status overall.
*   `data.active_incidents[]` [array, optional] — each row has `id`, `title`, `type`, `status`, `url`, `date_created`, `date_updated`, `services[]`, `note_count`.

## Preconditions

## Recover

## Next

*   Alternative: → `auth_setup` (when health is fine but real Slack calls still fail — likely a scope/profile issue)
