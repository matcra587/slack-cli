# Overview

A headless Slack CLI for agents, scripts, and CI jobs. The binary is `slick`;
the repo and Go module are `slack-cli`.

Runtime commands take flags, stdin, or environment variables. They never
prompt. Command data goes to stdout; diagnostics and errors go to stderr.
Successful events render as human-friendly clog output by default and
auto-switch to JSON when stdout is not a TTY or an agent is detected.

## Install

```sh
brew install matcra587/tap/slick
```

Also available via `go install` and pre-built binaries. See
[Installation](installation.md) for go install, GitHub Releases, source
builds, and upgrade paths.

The examples below assume `slick` is on `PATH`. Every visible flag has a short
form; run `slick <command> --help` for the current mapping.

## First-time setup

```sh
slick manifest template --preset messaging --type user --name slack-cli > manifest.json
# Import manifest.json in Slack, then:
slick auth login
slick auth status
```

See [auth](auth.md) and [manifest](manifest.md) for full setup, including
OAuth redirect URL handling and the token-based auth path.

## Commands

| Command | Summary |
|---------|---------|
| [`auth`](auth.md) | Manage Slack authentication: OAuth or token login, status, switch, logout. |
| [`message`](message.md) | Send, edit, and delete Slack messages (channels and DMs). |
| [`reply`](reply.md) | Reply to a Slack thread by parent timestamp. |
| [`react`](react.md) | Add, remove, or list emoji reactions. |
| [`history`](history.md) | Read channel or thread history. |
| [`lookup`](lookup.md) | Look up channels, users, or search messages. |
| [`cache`](cache.md) | Prime and inspect local Slack metadata caches. |
| [`status`](status.md) | Set or clear the authenticated user's Slack status. |
| [`health`](health.md) | Check Slack service health and Web API reachability. |
| [`file`](file.md) | Upload a file to Slack (probationary; hidden in help). |
| [`config`](config.md) | Manage local preferences (not auth). |
| [`workspace`](workspace.md) | List configured workspace profiles. |
| [`manifest`](manifest.md) | Generate Slack app manifests for import. |
| [`agent`](agent.md) | Emit command schema and workflow runbooks for agents. |
| [`version`](version.md) | Print version information. |

## Output modes

slick has a single output flag, `--output` (short `-o`), with four values:

*   `auto` — the default. TTY without an agent renders human-readable clog
    output; everything else renders the JSON envelope.
*   `human` — human-readable clog output.
*   `json` — full envelope with `meta`, `data`, and `errors[]`.
*   `compact` — JSON `data` only; no envelope.

Under `auto`, JSON is selected when stdout is not a TTY or when slick detects
an agent environment (e.g. `CLAUDE_CODE`, `CURSOR_TERMINAL`, `CODEX`,
`GITHUB_ACTIONS`, `CI`).

Use command-local `--blocks` when the message body is already a Slack Block
Kit JSON array.

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | `auth_failure` (`invalid_auth`, `missing_scope`, `no_permission`, expired) |
| 2 | `not_found` (`channel_not_found`, `user_not_found`, `not_in_channel`, …) |
| 3 | `rate_limit` (with `retry_after_seconds` in the error envelope) |
| 4 | `validation_error` (bad flags, malformed input, Slack rejects the value) |
| 5 | `server_error` (Slack 5xx or filesystem/runtime failure) |
| 6 | `canceled` (SIGINT/SIGTERM during a Slack call) |
| 7 | `timeout` (`--timeout` exceeded) |

JSON-mode failures put `errors[0].type`, `errors[0].message`, and
`errors[0].exit_code` on stderr. The action label (e.g. `Message sent`) goes
to stdout only on success.

## Output styling

slick uses [`gechr/clog`](https://github.com/gechr/clog) for human-mode output
and [`gechr/primer/table`](https://github.com/gechr/primer) for tables. The
visual conventions:

*   **Identity fields** (`channel`, `user`, `team_id`, `workspace`, `file_id`,
  …) are hash-coloured from the theme's entity palette so the same ID renders
  the same colour across commands.
*   **Human labels** (channel name, user name, file name, descriptions) render
  default-styled.
*   **Time fields** (`ts`, `fetched_at`, `expiration`) render in clog's
  `FieldTime` magenta.
*   **Bool fields** follow a three-tier polarity:
    *   Alarming on true (e.g. `is_archived`, `deleted`, `truncated`) — red on
    true, dim on false.
    *   Both states matter (e.g. `authenticated`, `exists`) — green on true, red
    on false.
    *   Routine on true (e.g. `is_member`) — dim on true, default on false.
*   **Action-result bools** (`dry_run`) are kept where they convey whether a
  real Slack call happened. `cache clear` retains `cleared` because the
  operation has no other status field; the other action-result bools
  (`deleted`, `removed`, `written`) were dropped in v0.4.0 in favour of the
  action label plus `errors[]`.

Field order across commands follows a canonical taxonomy: *where → what →
when → state → detail → numbers → diagnostics → pagination*. An AST-walking
test enforces this on every CI run; see
[`internal/cli/output/field_order_test.go`](https://github.com/matcra587/slack-cli/blob/main/internal/cli/output/field_order_test.go)
if you want the full rule.

## Attribution

When slick detects an agent or CI environment, the four mutating commands
(`message send`, `message edit`, `reply`, `file upload`) attach a Block Kit
context block to the Slack message. The trigger set covers most popular AI
assistants (Claude Code, Cursor, Codex, Aider, Cline, Windsurf, GitHub
Copilot, Codeium, Amazon Q, Gemini Code Assist, Cody) and CI systems (GitHub
Actions, Buildkite, Jenkins, GitLab CI, CircleCI, Travis, Bitbucket
Pipelines, TeamCity, Azure Pipelines, and the generic `CI` variable). The
authoritative list lives in
[`internal/agent/detect.go`](https://github.com/matcra587/slack-cli/blob/main/internal/agent/detect.go).
Override per-call with `--attribution-label`, `--attribution-emoji`,
`--attribution-message`. Toggle the block itself with `--attribution` (force
on) or `--no-attribution` (force off); both override config defaults
and env detection, so you can attribute a single command on a workspace with
`attribution.enabled = false` or suppress a single one when running inside a
detected agent.

The attribution context block reflects the detection state in the rendered
Slack message:

*   `:robot_face: _Sent via slick (agent mode)_` — slick auto-detected an
    agent or CI environment.
*   `:robot_face: _Sent via slick_` — slick is running interactively (no
    agent triggers) but `attribution.enabled = true` is set in config.

Override either piece with `--attribution-message` (text body) or
`--attribution-emoji` (leading emoji).

Config can pin attribution defaults per workspace:

```sh
slick config set workspaces.default.attribution.enabled true
slick config set workspaces.default.attribution.emoji :robot_face:
slick config set workspaces.default.attribution.message "Sent via build agent"
```

## Configuration paths

*   Config: `${SLICK_CONFIG:-${XDG_CONFIG_HOME:-~/.config}/slick/config.toml}`.
  `SLACK_CLI_CONFIG` remains as a legacy override.
*   Cache: `${XDG_CACHE_HOME:-~/.cache}/slick/<profile>/`.
*   Path inputs (`SLICK_CONFIG`, `--token-file`, `--file`, …) expand `~` and
  environment variables.

Tokens never appear in argv, TOML, stdout, stderr, or any of these docs.
Auth-owned fields store keychain or secret-manager references; config
commands do not edit them.

## Further reading

*   Per-command pages above for flags, examples, and JSON shapes.
*   [`slick agent guide`](agent.md#agent-guide) for machine-readable runbooks designed for
  agent consumption.
*   [`slick agent schema`](agent.md#agent-schema) for the full command tree and exit-code
  contract in JSON.
*   [Repo source](https://github.com/matcra587/slack-cli)
