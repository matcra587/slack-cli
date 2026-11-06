---
slug: config-prefs
title: "Set profile preferences without touching auth"
description: "Set profile preferences without touching auth"
when_to_use: "Before following the config prefs workflow in slick."
commands: ["slick config init", "slick config set", "slick config get"]
aliases: ["config_prefs"]
---

## Decide

Goal: Change profile preferences (default channel, attribution defaults) without touching credentials.

*   File missing? → `slick config init` first (it does NOT ask for tokens).
*   Default destination: `workspaces.<profile>.default_channel` (fallback for `message send` when neither `--channel` nor `--user` is passed).
*   Attribution defaults: `workspaces.<profile>.attribution.{enabled,message,emoji}`.

### Workflow checkpoints

*   Use config init for preferences only
*   Use auth commands for credentials
*   Set default channel and attribution text with config set
*   Do not write plaintext tokens to TOML
*   Remember env overrides beat config values

## Run

*   Init: `slick config init`
*   Set default channel: `slick config set workspaces.<profile>.default_channel <channel-id>`
*   Set attribution copy: `slick config set workspaces.<profile>.attribution.message <text>`
*   Set attribution emoji: `slick config set workspaces.<profile>.attribution.emoji <emoji>`
*   Inspect path: `slick config path`
*   Inspect effective: `slick config list --output=json`

### Behavior

*   Credentials are owned by `slick auth login|status|switch|logout` — not by `config`.
*   Config path: `~/.config/slick/config.toml` (XDG, including macOS). `SLICK_CONFIG` overrides; legacy `SLACK_CLI_CONFIG` still works. Path inputs expand `~` and env vars.
*   Precedence: environment → config file → defaults.
*   Runtime token override is profile-scoped: `SLACK_CLI_TOKEN_<PROFILE>`, name uppercased and non-alphanumeric replaced with `_`.

## Save

> Requires `--output=json`.
*   `data.settings[]` [array, required] — `{key, value, description}` rows; never contains credential material.

## Preconditions

## Recover

| Symptom | Cause | Next |
|---|---|---|
| `config file not found` | Profile never initialized | Run `slick config init` |
| `unknown key` from `config set` | Typo or wrong scope path | Use `slick config list` to see effective keys |

## Next
