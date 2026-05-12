# slack-cli

`slick` is a headless Slack CLI for agents, scripts, and CI jobs. The command
name is a short play on `slack-cli`: the project is `slack-cli`, the installed
binary is `slick`. Runtime commands take flags, stdin, or environment
variables. They do not prompt.

> [!NOTE]
> Not to be confused with [`slackapi/slack-cli`](https://github.com/slackapi/slack-cli),
> the official Slack platform CLI for building and deploying Slack apps. This
> project is an independent, headless client for sending messages, reading
> history, and managing Slack identity from agents and CI.

> **Full documentation:** [matcra587.github.io/slack-cli](https://matcra587.github.io/slack-cli)
> (sources in [`docs/`](docs/)).

## Install

```sh
brew install matcra587/tap/slick
```

Also available via `go install` and pre-built binaries from
[releases](https://github.com/matcra587/slack-cli/releases). See
[`docs/installation.md`](docs/installation.md) for details.

The examples below assume `slick` is on `PATH`. Every visible flag has a short
form; run `slick <command> --help` for the current mapping.

## Quick start

```sh
# 1. Generate a Slack app manifest and import it in Slack
slick manifest template --preset messaging --type user --name slack-cli > manifest.json

# 2. Authenticate (OAuth recommended; token modes also supported)
slick auth login
slick auth status

# 3. Send your first message
slick message send --channel C1234567890 --message "Hello from slack-cli"
```

See [`docs/auth.md`](docs/auth.md) for token-mode auth, OAuth port pinning,
and the `--force` rules. See [`docs/manifest.md`](docs/manifest.md) for
scope presets and bot-vs-user manifest shapes.

## Output

slick emits human-friendly clog output on TTYs and switches to JSON
automatically for non-TTY callers and detected agent environments. Pick a
mode explicitly with `--output` (short `-o`); valid values are `auto`,
`human`, `json`, and `compact`. Command data goes to stdout; diagnostics
and structured errors go to stderr.

```sh
slick auth status --output=json
slick lookup channel --output=human
slick agent schema -o compact
```

Tokens never appear in argv, TOML, stdout, stderr, docs, or examples.
Auth-owned fields are stored as keychain or secret-manager references; config
commands do not edit them.

## Commands

| Command | Docs |
|---------|------|
| `auth` | [auth.md](docs/auth.md) |
| `message`, `reply` | [message.md](docs/message.md), [reply.md](docs/reply.md) |
| `history`, `lookup` | [history.md](docs/history.md), [lookup.md](docs/lookup.md) |
| `react`, `status` | [react.md](docs/react.md), [status.md](docs/status.md) |
| `file` (probationary) | [file.md](docs/file.md) |
| `cache` | [cache.md](docs/cache.md) |
| `config`, `workspace` | [config.md](docs/config.md), [workspace.md](docs/workspace.md) |
| `manifest` | [manifest.md](docs/manifest.md) |
| `agent`, `version` | [agent.md](docs/agent.md), [version.md](docs/version.md) |

## Agent attribution

When slick detects an agent environment, mutating commands attach a Block
Kit context block to the Slack message. The trigger set covers most popular
AI assistants (Claude Code, Cursor, Codex, Aider, Cline, Windsurf, GitHub
Copilot, Codeium, Amazon Q, Gemini Code Assist, Cody) and CI systems
(GitHub Actions, Buildkite, Jenkins, GitLab CI, CircleCI, Travis, Bitbucket
Pipelines, TeamCity, Azure Pipelines, and the generic `CI` variable). The
authoritative list lives in
[`internal/agent/detect.go`](internal/agent/detect.go). Override per-call
with `--agent-label`, `--agent-emoji`, `--agent-message`; disable with
`--no-agent-attribution`; force with `--agent`. Pin defaults per workspace
in config:

```sh
slick config set workspaces.default.attribution.enabled true
slick config set workspaces.default.attribution.emoji :robot_face:
slick config set workspaces.default.attribution.message "Sent via build agent"
```

## Development

```sh
mise run check        # vet, lint, fmt, test
mise run test:all     # all uncached tests
mise run release:check
mise tasks            # list everything
```

Project conventions live in [`CLAUDE.md`](CLAUDE.md). Output styling rules
and field-order policy live at the top of
[`internal/cli/output/output.go`](internal/cli/output/output.go); an
AST-walking test enforces field order on every CI run
([`internal/cli/output/field_order_test.go`](internal/cli/output/field_order_test.go)).

## License

See [`LICENSE`](LICENSE).
