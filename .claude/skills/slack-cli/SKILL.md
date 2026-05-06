---
name: slack-cli
description: Use when the user asks an agent to operate Slack through the matcra587/slack-cli binary, including messages, DMs, threads, reactions, history, search, cleanup, users, channels, auth, config, or app manifests.
allowed-tools: Bash(slack agent:*) Bash(slack auth login:*) Bash(slack auth status:*) Bash(slack auth switch:*) Bash(slack config get:*) Bash(slack config init:*) Bash(slack config list:*) Bash(slack config path:*) Bash(slack config set:*) Bash(slack history list:*) Bash(slack lookup:*) Bash(slack manifest template:*) Bash(slack message edit:*) Bash(slack message send:*) Bash(slack react:*) Bash(slack reply:*) Bash(slack workspace list:*)
---

# slack-cli

Use this skill as a thin router. The Slack CLI binary contains the operational
runbooks. Load the matching runbook before acting. Do not copy CLI runbooks into
this skill; update the embedded `slack agent guide` source when details change.

## First Step

Run the matching guide command:

```sh
slack agent guide <workflow>
```

Use the schema only when you need command inventory or flag details:

```sh
slack agent schema --compact
```

## Workflow Map

| User task | Load this runbook |
| --- | --- |
| Send a channel message or DM | `slack agent guide send_msg` |
| Post a realistic PR review, incident update, release note, reactions, and thread | `slack agent guide developer_review` |
| Reply in a thread | `slack agent guide reply` |
| Add, remove, or list reactions | `slack agent guide react` |
| Read channel history or thread replies | `slack agent guide read_history` |
| Search messages, especially by run ID | `slack agent guide search_msgs` |
| Clean up live-test messages | `slack agent guide cleanup_msgs` |
| Edit a message | `slack agent guide edit_msg` |
| Delete a message | Requires explicit user approval; then load `slack agent guide delete_msg` |
| Find channels, private channels, or DMs | `slack agent guide discover_destination` |
| Find users or presence | `slack agent guide lookup_user` |
| Send direct messages | `slack agent guide send_dm` |
| Auth, manifests, token setup | `slack agent guide auth_setup` |
| Config preferences | `slack agent guide config_prefs` |
| Output modes, exit codes, parsing | `slack agent guide core_contract` |
| High-impact or destructive operations | `slack agent guide safe_mutation` |
| File upload testing | `slack agent guide upload_file` |
| Command inventory | `slack agent guide inspect_schema` |

## Non-Negotiables

- Run `slack agent guide <workflow>` before taking action. The guide is the
  runbook; the schema is only command inventory.
- Keep automation on JSON output. Do not parse `--plain`.
- Treat stdout as command data and stderr as diagnostics or structured errors.
- Parse failures from stderr JSON: `errors[0].type`, `errors[0].message`, and
  `errors[0].exit_code`.
- Keep Slack timestamps as strings. They are scoped to a channel.
- Use exact `channel` + `ts` for replies, reactions, edits, and deletes.
- Never pass Slack tokens in argv. Use stdin, file, env-name, keychain, or a
  configured secret reference.
- Use real multiline stdin for multiline Slack messages. Do not type literal
  `\n` into `--message` when the UI should show a new line.
- Use `--dry-run` before high-visibility or destructive mutations.
- This skill does not preapprove deletes. Treat `slack message delete` as an
  explicit-user-approval operation outside `allowed-tools`.
- Do not duplicate attribution text in the message body. Attribution renders as
  a context block when enabled.
- For live tests, use realistic content and unique run IDs. Clean up with the
  paginated `cleanup_msgs` runbook.
- For search cleanup, follow `meta.pagination.next_cursor` until
  `meta.pagination.has_more` is false, then repeat the search after deletes
  until there are no matches.

## Command Name Guardrails

- There is no `slack dm` command. Use `slack message send --user`.
- There is no `slack thread` command. Use `slack reply`.
- There is no `slack reaction` command. Use `slack react`.
- `--raw` is an output mode. Use `--blocks` only when the input is raw Block Kit
  JSON.
- File upload remains probationary. Use `slack agent guide upload_file` before
  testing it and prefer dry-run first.

## Rate-Limit Guardrail

The CLI returns structured `rate_limit` errors with exit code `3`. Respect
`retry_after_seconds`.

Separate CLI processes do not share proactive throttle state. Keep shell fanout
modest unless the user explicitly asks for a rate-limit test.
