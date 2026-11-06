---
slug: auth-setup
title: "Generate a manifest and authenticate a profile"
description: "Generate a manifest and authenticate a profile"
when_to_use: "Before following the auth setup workflow in slick."
commands: ["slick manifest template", "slick auth login", "slick auth status"]
aliases: ["auth_setup"]
---

## Decide

Goal: Wire a Slack workspace profile with a valid token before any other workflow can call Slack.

### command shape

*   Bare `slick auth login` opens an interactive form on a TTY — the only place that prompts.
*   Headless: add `--workspace-name <profile>` plus either `--method oauth ...` or a token source flag. Default method is `token`, so passing only `--token-stdin|file|env` is enough.

### app shape

*   `--type user` (default; "act as me") vs `--type bot` (app identity) when generating the manifest.

### scope footprint

*   `--preset messaging` (send + read), `--preset readonly`, `--preset files`, `--preset search`, `--preset full`.

### guard

*   Already authenticated? Run preflight first.

### Workflow checkpoints

*   Generate/import a manifest template when needed
*   Use local OAuth PKCE or safe token auth
*   Store credentials in keychain or 1Password, never TOML plaintext or argv
*   Use profile names case-insensitively
*   Verify with auth status before posting

## Run

*   Preflight: `slick auth status --output=json`
*   Manifest: `slick manifest template --preset messaging --type user --name <app-name>` (swap `--preset` for a different scope footprint)
*   OAuth login (browser, PKCE): `slick auth login --workspace-name <profile> --method oauth --oauth-client-id <id> --oauth-callback-port <port>`
*   Token login (no token in argv): pipe via `--token-stdin`, point at `--token-file <path>`, or pass an env var name (NOT its value) with `--token-env <VAR>`

### Behavior

*   Login accepts exactly one token source in token mode. OAuth-only options require `--method oauth`, which rejects token source flags. Choose a redirect URL or a callback port, not both. Login `--dry-run` validates option shape and returns a profile plan without reading credentials, opening a listener/browser, calling Slack or changing files; it does not authenticate.

*   Login snapshots the previous keychain credential even with `--force`. Failed writes or config saves restore it, or remove a new entry. If restoring the credential fails, the error includes `details.credential_restore_failed: true`; inspect the keychain before retrying. Config is published only after saving, and malformed existing config is preserved.

*   `auth logout <profile>` preserves profile preferences and saves the config before revoking or deleting credentials. A failed save leaves prior state intact. `--dry-run` previews without credential access or changes; `--keep-token` skips revocation and keychain deletion. Cleanup failures warn on stderr and may leave an active token or stored credential.

*   `auth switch <profile>` changes the local default without calling Slack or reading credentials. Names match without regard to case and results use the stored spelling. `--dry-run` previews the target; failed saves preserve the previous default and profile settings.

*   `auth status` checks all profiles in name order. Missing or invalid credentials are per-profile results with exit 0; cancellation and timeouts stop the command with exit 6 or 7 and no partial success result. An empty profile list is `[]`. Provider and transport details are redacted; safe Slack error codes remain visible.

*   Credentials live in OS keychain or a configured secret backend (e.g. `op://...`). Never put plaintext `xox*` tokens in TOML, argv, stdout, stderr, or logs.
*   Runtime override: `SLACK_CLI_TOKEN_<PROFILE>` beats `SLACK_CLI_TOKEN` and the configured secret reference for that profile. Profile-suffix uppercased with non-alphanumeric → `_`.

## Save

> Requires `--output=json`.
*   `data.workspaces[].workspace`, `data.workspaces[].team_id`, `data.workspaces[].token_type` [string, required] — confirmation without credential material.
*   `data.workspaces[].validation_state` [string, required] — `valid`, `missing`, or `invalid`.

## Preconditions

*   Profile names use Unicode case matching (`Default`/`default`, `s`/`ſ`, and `σ`/`ς` collide). A config with matching names is rejected before credentials are read or Slack is called. Rename one conflicting profile; config is not changed automatically.
*   `--token-env` takes an environment-variable name, not the token value itself.

## Recover

| Symptom | Cause | Next |
|---|---|---|
| `bad_client_secret` from OAuth | App configured for confidential client | Import a slick-generated manifest, or enable PKCE in Slack |
| Slack rejects the OAuth redirect URL | Random callback port mismatch | Copy the exact local callback URL printed during login; pass it back as `--oauth-redirect-url` |
| `auth_failure: missing_scope` after login | Manifest lacks the scope the workflow needs | Edit manifest preset → reauth |

## Next

*   Then: → `config_prefs` (set `default_channel`, attribution defaults)
*   Then: → `send_msg` (verify auth with a real send)
