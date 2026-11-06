---
slug: cache-metadata
title: "Prime users and channels for repeated lookup and shell completion"
description: "Prime users and channels for repeated lookup and shell completion"
when_to_use: "Before following the cache metadata workflow in slick."
commands: ["slick cache users", "slick cache channels", "slick cache clear"]
aliases: ["cache_metadata"]
---

## Decide

Goal: Prime, refresh, or clear local caches of active users and channels so shell completion and conversation labels can reuse local metadata.

*   Priming for completion / live tests: `cache users` and/or `cache channels`.
*   Forcing a refresh: add `--refresh` to ignore the TTL.
*   Resetting one cache or all: `cache clear <resource>` or `cache clear` (no arg = all).
*   Previewing any cache operation: add `--dry-run` or `-n`. The plan names the profile and resource (`all` for a profile clear). It does not read credentials, call Slack, or change files. It does not verify Slack permissions or filesystem access.

Channel caching needs any one conversation read scope: `channels:read`,
`groups:read`, `im:read`, or `mpim:read`. Slack returns only the conversations
visible to that token; it does not need all four scopes.

### Workflow checkpoints

*   Run cache users and cache channels once per work session
*   Use --refresh when Slack membership changed
*   Use --ttl-minutes to choose daily or weekly refresh windows
*   Clear stale resources with cache clear users or cache clear channels

## Run

*   Prime users: `slick cache users --output=json` (active only — deactivated users stay out)
*   Prime channels: `slick cache channels --output=json` (active public + private + IM + MPIM)
*   Force refresh: append `--refresh`
*   Tune freshness: `--ttl-minutes 10080` for a weekly window (default 1440)
*   Tune pagination on large workspaces: `--page-size <n>` + `--max-pages <n>`; page size is capped at 200. Nonpositive values use 200 and 20 respectively.
*   Page flags apply to fetching, not a fresh cache hit; add `--refresh` to fetch again.
*   TTL must not exceed 153722867 minutes; invalid values fail locally with exit 4. Nonpositive values keep the default 1440-minute window.
*   Clear one resource: `slick cache clear users` or `slick cache clear channels`
*   Clear all `.json` cache files directly under the active profile: `slick cache clear`; other files, nested directories, and other profiles remain.

### Behavior

*   Cache files live under XDG cache home, normally `~/.cache/slick/<profile>/`. Names needing path encoding use a stable folder identifier so profiles cannot share a folder.
*   Affected profiles fetch a new cache instead of moving or clearing their former shared folder from older underscore-based naming.
*   Caches store supported user/channel metadata fields; unknown API fields and tokens are not copied into them.
*   Wrong-profile entries, missing/null lists, items without IDs, and invalid field types trigger a fresh fetch. Failed recovery preserves the prior file. Valid empty lists remain cache hits.
*   Shell completion prefers fresh cached entries before falling back to live Slack API calls.

## Save

> Requires `--output=json`.

Clear one resource:

*   `{profile, resource, cleared}` — `cleared=true` ⇒ removed something; `false` ⇒ cache already empty.

Clear all:

*   `{profile, resources}` — `resources[]` lists removed names. Empty/absent slice ⇒ nothing removed.
*   On partial failure: `errors[0].details.partial` carries resources removed before the failure.

## Preconditions

## Recover

## Next

*   Then: → `lookup_user` / → `discover_destination`; direct lookups still call Slack, while cached metadata can supply conversation labels.
