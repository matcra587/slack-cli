---
slug: cache-metadata
title: "Prime users and channels for repeated lookup and shell completion"
description: "Prime users and channels for repeated lookup and shell completion"
when_to_use: "Before following the cache metadata workflow in slick."
commands: ["slick cache users", "slick cache channels", "slick cache clear"]
aliases: ["cache_metadata"]
---

## Decide

Goal: Prime, refresh, or clear local caches of active users and channels so shell completion and repeat lookups don't hit Slack.

*   Priming for completion / live tests: `cache users` and/or `cache channels`.
*   Forcing a refresh: add `--refresh` to ignore the TTL.
*   Resetting one cache or all: `cache clear <resource>` or `cache clear` (no arg = all).

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
*   Tune pagination on large workspaces: `--page-size <n>` + `--max-pages <n>`
*   Clear one resource: `slick cache clear users` or `slick cache clear channels`
*   Clear all for the active profile: `slick cache clear`

### Behavior

*   Cache files live under XDG cache home, normally `~/.cache/slick/<profile>/`.
*   Caches are metadata only — they never store tokens or message content.
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

*   Then: any → `lookup_user` / → `discover_destination` invocation now hits the cache first.
