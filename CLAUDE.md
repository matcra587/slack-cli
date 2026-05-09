# slack-cli

## Product

`slick` is a headless Slack CLI for agents, scripts, CI jobs, and human users.
The name is a short play on `slack-cli`: the repository and module stay
`slack-cli`, while the installed binary is `slick`. Runtime Slack commands are
non-interactive. Command data goes to stdout; diagnostics and errors go to
stderr.

## Local Workflow

- Tasks are defined in `tasks.toml` and `.mise/tasks/`.
- List tasks with `mise tasks`.
- Run the main check with `mise run check`.
- Run all uncached tests with `mise run test:all`.
- Validate release config with `mise run release:check`.
- Validate workflows with `actionlint -color .github/workflows/*.yml`.
- Validate workflow security with
  `zizmor --persona pedantic --offline --no-progress --color never .github/`.
- Project tools are pinned through `.mise.toml` and `mise.lock`.
- Markdown lint configuration is in `.rumdl.toml`; fixture Markdown under
  `testdata/**` is excluded there.

## Repository Map

- `cmd/slick/`: Cobra commands and CLI behavior.
- `internal/agent/`: agent detection and attribution helpers.
- `internal/agenthelp/`: runbooks for `slick agent guide`.
- `internal/config/`: TOML profiles, migrations, credential references, and
  workspace resolution.
- `internal/ratelimit/`: Slack Web API method tiers, throttling, and 429 retry.
- `internal/blockkit/`: Block Kit types, Markdown conversion, rendering, and
  validation.
- `tests/integration/`: built-binary pipe and Slack HTTP tests.
- `testdata/`: fixtures.

## Implementation Notes

- Slack Web API calls use `slack-go/slack` directly.
- Block Kit JSON is the canonical rich-content contract. Markdown input is
  converted through `internal/blockkit`; unsupported block-level Markdown preserves
  readable source text as fallback.
- `--blocks` is command-local input for raw Slack Block Kit JSON arrays.
  `--raw` remains an output mode.
- Token values must not appear in argv, TOML, stdout, stderr, docs, or examples.
- Config stores keychain or secret-manager references.
- Workspace selection resolves exactly one active profile through `--workspace`
  or `default_workspace`.
- Config paths come from XDG config home unless `SLICK_CONFIG` is set;
  default is `~/.config/slick/config.toml`. `SLACK_CLI_CONFIG` remains as a
  legacy override.
- Metadata cache paths come from XDG cache home; default is
  `~/.cache/slick/<profile>/`.
- Path inputs use `gechr/x` expansion for `~` and environment variables.
- Mutations support `--dry-run`; `message delete` also requires `--force`.
- `message send --user` accepts repeated, comma-separated, and email-address
  recipients; email lookup uses `users.lookupByEmail`.
- `status set|clear` mutates the authenticated user's Slack profile and
  requires a user token with `users.profile:write`.
- Slack permission failures such as `missing_scope`, `not_in_channel`, and
  `no_permission` map to structured CLI errors.

## Testing Notes

- Use `tests/integration/` for built-binary behavior, pipe contracts, stdout and
  stderr separation, retry behavior, and fake Slack HTTP requests.
- Use `internal/testutil.NewSlackServer` for command tests that need Slack API
  request recording.
- Release workflow changes are covered by `mise run release:check`,
  `actionlint`, and `zizmor`.

## Live Slack Runbooks

- Live tests should cover parent messages, replies, edits, reactions, deletes,
  search, pagination, and cleanup.
- Live message content should look like real user content, such as a PR review,
  incident update, or release note.
- Use a distinctive run ID in live-test messages.
- Cleanup targets require exact Slack `channel.id` and `ts` values.
- Search cleanup must paginate until no matches remain.
- Separate CLI processes do not share proactive throttle state.
- Structured rate-limit errors include `retry_after_seconds`.
- Slack history text can flatten visual formatting. Inspect `blocks` or the
  Slack UI when validating rendered formatting.
