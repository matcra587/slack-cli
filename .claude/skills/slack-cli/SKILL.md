---
name: slack-cli
description: Use when the user asks an agent to operate Slack through the matcra587/slack-cli binary, including messages, scheduled messages, DMs, threads, reactions, history, search, cleanup, users, channels, status, cache, auth, config, or app manifests.
allowed-tools: Bash(slick agent:*) Bash(slick auth login:*) Bash(slick auth status:*) Bash(slick auth switch:*) Bash(slick cache:*) Bash(slick config get:*) Bash(slick config init:*) Bash(slick config list:*) Bash(slick config path:*) Bash(slick config set:*) Bash(slick file upload:*) Bash(slick history list:*) Bash(slick lookup:*) Bash(slick manifest template:*) Bash(slick message edit:*) Bash(slick message scheduled list:*) Bash(slick message scheduled delete:*) Bash(slick message send:*) Bash(slick react:*) Bash(slick reply:*) Bash(slick status:*) Bash(slick workspace list:*)
---

# slack-cli

Use this skill as a thin router. The Slack CLI binary contains the operational
runbooks. Load the matching runbook before acting. Do not copy CLI runbooks into
this skill; update the embedded `slick agent guide` source when details change.

The repository is `matcra587/slack-cli` but the installed binary is `slick`.

## First Step

Run the matching guide command:

```sh
slick agent guide <workflow>
```

Use the schema only when you need command inventory or flag details:

```sh
slick agent schema --compact
```

## Workflow Map

| User task | Load this runbook |
| --- | --- |
| Send a channel message or DM | `slick agent guide send_msg` |
| Schedule, list, or cancel future messages | `slick agent guide schedule_msg` |
| Post a realistic PR review, incident update, release note, reactions, and thread | `slick agent guide developer_review` |
| Reply in a thread | `slick agent guide reply` |
| Add, remove, or list reactions (single or ordered multi-emoji) | `slick agent guide react` |
| Read channel history or thread replies | `slick agent guide read_history` |
| Search messages, especially by run ID | `slick agent guide search_msgs` |
| Clean up live-test messages | `slick agent guide cleanup_msgs` |
| Edit a message | `slick agent guide edit_msg` |
| Delete a message | Requires explicit user approval; then load `slick agent guide delete_msg` |
| Find channels, private channels, or DMs | `slick agent guide discover_destination` |
| Find users or presence | `slick agent guide lookup_user` |
| Send direct messages | `slick agent guide send_dm` |
| Set or clear the authenticated user's Slack status | `slick agent guide set_status` |
| Prime, refresh, or clear local Slack metadata caches | `slick agent guide cache_metadata` |
| Auth, manifests, token setup | `slick agent guide auth_setup` |
| Config preferences | `slick agent guide config_prefs` |
| Output modes, exit codes, parsing | `slick agent guide core_contract` |
| High-impact or destructive operations | `slick agent guide safe_mutation` |
| File upload testing | `slick agent guide upload_file` |
| Command inventory | `slick agent guide inspect_schema` |

## Non-Negotiables

*   Run `slick agent guide <workflow>` before taking action. The guide is the
  runbook; the schema is only command inventory.
*   Keep automation on JSON output. Do not parse `--plain`.
*   Treat stdout as command data and stderr as diagnostics or structured errors.
*   Parse failures from stderr JSON: `errors[0].type`, `errors[0].message`, and
  `errors[0].exit_code`.
*   Keep Slack timestamps as strings. They are scoped to a channel.
*   Use exact `channel` + `ts` for replies, reactions, edits, and deletes.
*   Never pass Slack tokens in argv. Use stdin, file, env-name, keychain, or a
  configured secret reference.
*   Use real multiline stdin for multiline Slack messages. Do not type literal
  `\n` into `--message` when the UI should show a new line.
*   Use `--dry-run` before high-visibility or destructive mutations.
*   `--schedule` accepts `--channel` or `--user`. For scheduled DMs, pass the
  user ID, alias, or Slack-profile email with `--user`; real sends return the
  raw DM/MPIM channel ID in `data.channel`.
*   This skill does not preapprove deletes. Treat `slick message delete` as an
  explicit-user-approval operation outside `allowed-tools`.
*   Status set/clear mutates the authenticated user's Slack profile and requires
  a user token with `users.profile:write`. Bot-token profiles cannot use it.
*   Do not duplicate attribution text in the message body. Attribution renders as
  a context block when enabled.
*   For live tests, use realistic content and unique run IDs. Clean up with the
  paginated `cleanup_msgs` runbook.
*   For search cleanup, follow `meta.pagination.next_cursor` until
  `meta.pagination.has_more` is false, then repeat the search after deletes
  until there are no matches.

## Command Name Guardrails

*   There is no `slick dm` command. Use `slick message send --user`.
*   Immediate DM email targeting uses the user's Slack profile email. If Slack
  returns `users_not_found`, resolve the Slack-side address or use the user ID.
*   There is no `slick thread` command. Use `slick reply`.
*   There is no `slick reaction` command. Use `slick react`.
*   `--raw` is an output mode. Use `--blocks` only when the input is raw Block Kit
  JSON.
*   `slick react add` accepts one or more emoji: comma-separate (`--emoji a,b,c`)
  or repeat the flag to apply multiple in input order. Halts on first failure.
*   File upload remains probationary. Use `slick agent guide upload_file` before
  testing it and prefer dry-run first.

## Rate-Limit Guardrail

The CLI returns structured `rate_limit` errors with exit code `3`. Respect
`retry_after_seconds`.

Separate CLI processes do not share proactive throttle state. Keep shell fanout
modest unless the user explicitly asks for a rate-limit test.
