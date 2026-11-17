# slack-cli

`slick` is a headless Slack CLI for agents, scripts, and CI jobs.

I built slick to use Slack from my terminal and from agents.

The name is a short play on `slack-cli`: the project is `slack-cli`, the
installed binary is `slick`.

> [!NOTE]
> Not the same project as [`slackapi/slack-cli`](https://github.com/slackapi/slack-cli),
> the official Slack platform CLI for building and deploying Slack apps.
> That tool is for app development; this one is for talking to Slack
> from scripts and agents.

## Install

See [`docs/installation.md`](docs/installation.md) for install methods:
Homebrew, `go install`, pre-built binaries, and source builds.

The examples below assume `slick` is on `PATH`. Run `slick <command> --help`
for the full flag list and any short forms.

## Agent plugin

The `slack-cli` plugin includes the [Slack CLI skill](skills/slack-how-to/SKILL.md).
The skill uses the guides and command schema from the installed `slick` CLI.

In Claude Code, install the plugin from the marketplace:

```text
/plugin install slack-cli --marketplace matcra587/mattc-marketplace
```

In Codex, select `slack-cli` from the same marketplace.

## Quick start

```sh
# 1. Generate a Slack app manifest and import it in Slack
slick manifest template --preset messaging --type user --name slick --output=human > manifest.json

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

slick emits readable output in a terminal and switches to JSON
automatically when piped or run by a detected agent. Pick a
mode explicitly with `--output` (short `-o`); valid values are `auto`,
`human`, `json`, and `compact`. Machine successes and failure envelopes go to stdout; human diagnostics
go to stderr. Existing exit codes remain unchanged; output-write failures use exit 8.

```sh
slick auth status --output=json
slick lookup channel --output=human
slick agent schema -o compact
```

Tokens never appear in argv, TOML, stdout, stderr, docs, or examples.
They are stored via your OS keychain or a configured secret manager;
`slick config` commands can't edit them.

## Commands

| Command | Docs |
|---------|------|
| `auth` | [auth.md](docs/auth.md) |
| `message`, `reply` | [message.md](docs/message.md), [reply.md](docs/reply.md) |
| `history`, `lookup` | [history.md](docs/history.md), [lookup.md](docs/lookup.md) |
| `react`, `status`, `health` | [react.md](docs/react.md), [status.md](docs/status.md), [health.md](docs/health.md) |
| `file` | [file.md](docs/file.md) |
| `dnd`, `pin`, `bookmark` | [dnd.md](docs/dnd.md), [pin.md](docs/pin.md), [bookmark.md](docs/bookmark.md) |
| `cache` | [cache.md](docs/cache.md) |
| `config`, `workspace` | [config.md](docs/config.md), [workspace.md](docs/workspace.md) |
| `manifest` | [manifest.md](docs/manifest.md) |
| `agent`, `version` | [agent.md](docs/agent.md), [version.md](docs/version.md) |
| `api` | [api-cli.md](docs/api-cli.md) |

For authenticated Go clients, see [Use slick from Go](docs/api.md).

## Wait for a reaction

A script can wait for approval before continuing:

```sh
slick react watch --channel C1234567890 --timestamp 1746284582.123456 \
  --emoji :+1: --timeout 10m --output=json && ./next-step.sh
```

See [reaction watches](docs/react.md#react-watch) for user filters and polling limits.

## Attribution

When slick detects an agent or CI environment, the four mutating commands
(`message send`, `message edit`, `reply`, `file upload`) attach a Block Kit
context block to the message identifying it as agent-generated. The trigger
set covers the common AI assistants and CI systems; see
[`docs/index.md`](docs/index.md#attribution) for the full list and
[`internal/agent/detect.go`](internal/agent/detect.go) for the authoritative
source.

Override per-call with `--attribution-label`, `--attribution-emoji`,
`--attribution-message`. Toggle the block per call with `--attribution` (force
on) or `--no-attribution` (force off); both override config defaults
and env detection. Pin defaults per workspace in config:

```sh
slick config set workspaces.default.attribution.enabled true
slick config set workspaces.default.attribution.emoji :robot_face:
slick config set workspaces.default.attribution.message "Sent via build agent"
```

## Development

See [`CONTRIBUTING.md`](CONTRIBUTING.md) for setup, the local check
matrix, hooks, repository layout, conventions, and the release flow.

## License

See [`LICENSE`](LICENSE).
