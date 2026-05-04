# slack-cli

`slack` is a headless Slack CLI for agents, scripts, and CI jobs. Runtime
commands take flags, stdin, or environment variables. They do not prompt.

## Install

```sh
mise install
task install
slack version
```

From a checkout, `task install` runs `go install ./cmd/slack` with version
metadata. Once the module is published, you can also install it directly:

```sh
go install github.com/matcra587/slack-cli/cmd/slack@latest
```

The examples below assume `slack` is on `PATH`.

To build a local dist binary instead:

```sh
task build
```

`task build` writes the binary to `./dist/slack-<goos>-<goarch>`.

## Create a Slack App Manifest

The CLI does not create Slack apps. It prints a manifest you can import in
Slack.

```sh
slack manifest template \
  --preset messaging \
  --type user \
  --name slack-cli > manifest.json
```

Import the manifest in Slack, then check the app's OAuth settings. Local OAuth
uses a loopback redirect URL. Pick a local port once and use it for both the
manifest and login:

```sh
PORT=$(python3 - <<'PY'
import socket

sock = socket.socket()
sock.bind(("127.0.0.1", 0))
print(sock.getsockname()[1])
sock.close()
PY
)

slack manifest template \
  --preset messaging \
  --type user \
  --name slack-cli \
  --callback-port "$PORT" > manifest.json

slack auth login --oauth-callback-port "$PORT"
```

The default manifest enables PKCE and token rotation. `--type user` is the
normal choice because the CLI acts as you. Use `--type bot` only when messages
should come from the app's bot user. Presets are `readonly`, `messaging`,
`files`, `search`, and `full`.

## Authenticate

Use OAuth for normal setup:

```sh
slack auth login
```

Choose Slack OAuth, paste the app's client ID, and use the redirect URL that is
configured in Slack. If you set `SLACK_CLI_CALLBACK_PORT`, both
`manifest template` and `auth login` use that port. OAuth derives the workspace
ID and workspace name after authorization. It does not need a client secret.

Token setup is also supported:

```sh
slack auth login \
  --workspace-name default \
  --auth-method token \
  --token "$SLACK_TOKEN"
```

`auth login` validates the token with Slack, stores a structured credential in
keychain, and writes only a credential reference to TOML.

Check the result:

```sh
slack auth status
slack workspace list
```

## Configure Preferences

Config lives at `~/.config/slack-cli/config.toml` or `SLACK_CLI_CONFIG`.
`config` manages preferences, not auth. Use `auth login`, `auth status`,
`auth switch`, and `auth logout` for credentials.

```sh
slack config init
slack config set workspaces.default.default_channel C1234567890
slack config set workspaces.default.attribution.message "Sent via build agent"
slack config list
```

Never store plaintext `xox*` tokens in TOML. Auth-owned fields may appear in the
file as keychain or secret references, but config commands do not edit them.

## Send Messages

```sh
slack message send \
  --channel C1234567890 \
  --message "Deploy **complete**"

echo "Deploy complete" |
  slack message send --channel C1234567890 --file -
```

Use `--dry-run` before high-visibility sends.

```sh
slack message send \
  --channel C1234567890 \
  --message "Deploy complete" \
  --dry-run
```

## Agent Attribution

When agent mode is detected, sent messages include a Block Kit context block.
Triggers include `CLAUDE_CODE`, `CLAUDECODE`, `CURSOR_TERMINAL`, `CODEX`,
`GITHUB_ACTIONS`, and `CI`. You can also pass `--agent`.

```sh
CLAUDE_CODE=1 slack message send \
  --channel C1234567890 \
  --message "Deploy complete"
```

Disable attribution only when you mean it:

```sh
slack message send \
  --channel C1234567890 \
  --message "Manual relay" \
  --no-agent-attribution
```

Customize the context block per command or config profile:

```sh
slack message send \
  --channel C1234567890 \
  --message "Build passed" \
  --agent \
  --agent-emoji :gear: \
  --agent-message "Sent by release automation"
```

## Read and Search

```sh
slack history list --channel C1234567890 --max-items 50
slack history list --channel C1234567890 --thread 1746284582.123456
slack search messages --query "deploy failed" --max-items 10
slack channel list --max-items 20
slack dm list
slack user list --presence
```

History returns parent messages by default. Fetch thread replies with `--thread`.

## Mutate Safely

```sh
slack thread reply \
  --channel C1234567890 \
  --parent 1746284582.123456 \
  --message "Investigating"

slack reaction add \
  --channel C1234567890 \
  --timestamp 1746284582.123456 \
  --emoji :thumbsup:

slack message edit \
  --channel C1234567890 \
  --timestamp 1746284582.123456 \
  --message "Corrected text"

slack message delete \
  --channel C1234567890 \
  --timestamp 1746284582.123456 \
  --force
```

Deletes require `--force` unless you use `--dry-run`.

## Upload Files

```sh
slack file upload --channel C1234567890 --file ./report.txt

tar czf - ./dist |
  slack file upload \
    --channel C1234567890 \
    --file - \
    --filename dist.tgz
```

Progress and diagnostics go to stderr. stdout stays reserved for command data.

## Output

TTY output uses clog's human-readable mode. Non-TTY and agent contexts output
JSON. Use flags when you need a specific shape:

```sh
slack auth status --json
slack channel list --plain
slack agent schema --compact
```

`--compact` removes the envelope. `--plain` is for humans. `--raw` is for
Slack-native payloads.

## Agent Help

Agents can inspect the command tree and workflow notes:

```sh
slack agent schema
slack agent schema --compact
slack agent guide --help
slack agent guide send_msg
```

`agent guide --help` lists workflows such as `auth_setup`, `send_msg`,
`react_emoji`, `read_history`, `discover_destination`, and `safe_mutation`.
