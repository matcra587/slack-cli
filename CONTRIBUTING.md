# Contributing

## Setup

```bash
mise install
mise run deps
mise run build
```

`mise run build` writes `./dist/slick-<goos>-<goarch>`.
The repository is `slack-cli`; the installed command is `slick`, a short play
on `slack-cli`.

## Checks

```bash
mise run check
mise run test:all
mise run test:integration
mise run release:check
actionlint -color .github/workflows/*.yml
zizmor --persona pedantic --offline --no-progress --color never .github/
```

## Hooks

```bash
mise exec -- hk install --mise
mise run pre-commit
mise exec -- hk check --all --check
mise exec -- hk fix --all
```

## Common Tasks

```bash
mise tasks
mise run fmt
mise run lint
mise run lint:fix
mise run vet
mise run security
mise run deps:update
```

## Repository Layout

| Path | Purpose |
| --- | --- |
| `cmd/slick/` | Cobra root, persistent flags, runtime wiring |
| `internal/agent/` | Agent/CI environment detection and attribution labels |
| `internal/agenthelp/` | Runbooks served by `slick agent guide` and schema generation |
| `internal/blockkit/` | Block Kit types, Markdown conversion, validation |
| `internal/cli/` | Per-command packages (`auth`, `message`, `reply`, `history`, `lookup`, `react`, `status`, `file`, `cache`, `config`, `workspace`, `manifest`, `agent`) plus shared `output`, `runtime`, `completion`, `slackclient` |
| `internal/config/` | TOML profile loading, migrations, credential references, workspace resolution |
| `internal/ratelimit/` | Slack Web API method tiers, throttling, 429 retry |
| `internal/version/` | Build-time `-X` ldflag receivers for `slick version` |
| `docs/` | Published docs site (Zensical, sources under `docs/`, built to `site/`) |
| `tests/integration/` | Built-binary pipe contracts and fake-Slack HTTP tests |
| `tests/live/` | Optional tests against a real Slack workspace (`mise run test:live`) |
| `testdata/` | Fixtures consumed by unit and integration tests |
| `.mise/tasks/` and `tasks.toml` | Local task definitions |

## Conventions

*   The binary is `slick`. The repository and Go module are `slack-cli`.
*   Runtime Slack commands are non-interactive. Stdout is structured command
  data; stderr is diagnostics. `slick auth login` and `slick config init`
  are the only interactive commands.
*   Output mode is one knob: `--output=auto|human|json|compact`. Agent-mode
  rendering is triggered by env detection or `FORCE_AGENT_MODE=1`.
*   Token values must not appear in argv, TOML, stdout, stderr, docs, or
  examples. Config stores keychain or secret-manager references.
*   Output styling rules and field-order policy live at the top of
  [`internal/cli/output/output.go`](internal/cli/output/output.go). An
  AST-walking test enforces field order on every CI run
  ([`internal/cli/output/field_order_test.go`](internal/cli/output/field_order_test.go)).
*   [`CLAUDE.md`](CLAUDE.md) documents the broader project guidelines used
  when working with AI assistants on this repo.

## Commits

Use Conventional Commits:

```text
<type>[(scope)][!]: <subject>
```

Common types: `feat`, `fix`, `docs`, `test`, `refactor`, `build`, `ci`,
`chore`. The pre-commit hook enforces this format and runs the standard
local checks; do not bypass it without a reason.

## Releasing

```bash
mise run release -- v0.8.1
```

The release task verifies the working tree is clean and up to date with
`origin/main`, runs `mise run ci` plus `mise run release:check`, then
creates an annotated tag and pushes it. The
[`release`](.github/workflows/release.yml) workflow builds binaries with
GoReleaser, signs `checksums.txt` with cosign, publishes the GitHub
release, and triggers the [Homebrew
tap](https://github.com/matcra587/homebrew-tap) to update the formula.

## Pull Requests

Open pull requests against `main`. Describe what changed and why.
