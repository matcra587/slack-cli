# slack-cli

`slick` is a headless Slack CLI for agents, scripts, and CI jobs. The command
name is a short play on `slack-cli`: the project is `slack-cli`, the installed
binary is `slick`. Runtime commands take flags, stdin, or environment
variables. They do not prompt.

## Install

```sh
mise install
mise run install
slick version
```

From a checkout, `mise run install` runs `go install ./cmd/slick` with version
metadata. Once the module is published, you can also install it directly:

```sh
go install github.com/matcra587/slack-cli/cmd/slick@latest
```

The examples below assume `slick` is on `PATH`.
Every visible flag has a short form; run `slick <command> --help` for the
current mapping.

To build a local dist binary instead:

```sh
mise run build
```

`mise run build` writes the binary to `./dist/slick-<goos>-<goarch>`.

## Create a Slack App Manifest

The CLI does not create Slack apps. It prints a manifest you can import in
Slack.

```sh
slick manifest template \
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

slick manifest template \
  --preset messaging \
  --type user \
  --name slack-cli \
  --callback-port "$PORT" > manifest.json

slick auth login --oauth-callback-port "$PORT"
```

The default manifest enables PKCE and token rotation. `--type user` is the
normal choice because the CLI acts as you. Use `--type bot` only when messages
should come from the app's bot user. Presets are `readonly`, `messaging`,
`files`, `search`, and `full`.

## Authenticate

Use OAuth for normal setup:

```sh
slick auth login
```

Choose Slack OAuth, paste the app's client ID, and use the redirect URL that is
configured in Slack. If you set `SLACK_CLI_CALLBACK_PORT`, both
`manifest template` and `auth login` use that port. OAuth derives the workspace
ID and workspace name after authorization. It does not need a client secret.

Token setup is also supported:

```sh
printf '%s\n' "$SLACK_TOKEN" |
  slick auth login \
    --workspace default \
    --method token \
    --token-stdin
```

You can also use `--token-file ./slack-token.txt` or `--token-env
SLACK_TOKEN`. `--token-env` takes an environment variable name, not the token
value, so token auth never requires a raw token in argv.

`auth login` validates the token with Slack, stores a structured credential in
keychain, and writes only a credential reference to TOML.

Check the result:

```sh
slick auth status
slick workspace list
```

## Configure Preferences

Config uses XDG config home unless `SLACK_CLI_CONFIG` is set. By default that is
`~/.config/slack-cli/config.toml`. Path inputs such as
`SLACK_CLI_CONFIG`, `--token-file`, and `--file` expand `~` and environment
variables.
`config` manages preferences, not auth. Use `auth login`, `auth status`,
`auth switch`, and `auth logout` for credentials.

```sh
slick config init
slick config set workspaces.default.default_channel C1234567890
slick config set workspaces.default.attribution.enabled true
slick config set workspaces.default.attribution.emoji :robot_face:
slick config set workspaces.default.attribution.message "Sent via build agent"
slick config list
```

Never store plaintext `xox*` tokens in TOML. Auth-owned fields may appear in the
file as keychain or secret references, but config commands do not edit them.

## Send Messages

```sh
slick message send \
  --channel C1234567890 \
  --message "Deploy **complete**"

echo "Deploy complete" |
  slick message send --channel C1234567890 --file -

slick message send \
  --user U1234567890 \
  --message "Build artifact is ready"

slick message send \
  --user dev@example.com,ops@example.com \
  --user U1234567890 \
  --message "PR is ready for review"
```

`--user` accepts Slack user IDs, configured aliases, and email addresses. Repeat
it or comma-separate values to open a group DM. If `default_channel` is set in
config, `slick message send` can omit both `--channel` and `--user`.

Use `--dry-run` before high-visibility sends.

```sh
slick message send \
  --channel C1234567890 \
  --message "Deploy complete" \
  --dry-run
```

Use `--blocks` only when the message body is already a Slack Block Kit JSON
array:

```sh
slick message send --channel C1234567890 --blocks --file blocks.json
```

Raw Block Kit input is validated before any Slack mutation. Malformed JSON,
unsupported block types, missing required fields, and Slack limits return a
`validation_error` with no command data on stdout.

Markdown input is converted to Block Kit by default. Paragraphs, headings, and
tables use semantic blocks where possible. Unsupported block-level constructs,
such as lists, blockquotes, fenced code, and HTML blocks, preserve the original
Markdown source text in readable section blocks instead of being dropped.

## Agent Attribution

When agent mode is detected, sent messages include a Block Kit context block.
Triggers include `CLAUDE_CODE`, `CLAUDECODE`, `CURSOR_TERMINAL`, `CODEX`,
`GITHUB_ACTIONS`, and `CI`. You can also pass `--agent`.

```sh
CLAUDE_CODE=1 slick message send \
  --channel C1234567890 \
  --message "Deploy complete"
```

Disable attribution only when you mean it:

```sh
slick message send \
  --channel C1234567890 \
  --message "Manual relay" \
  --no-agent-attribution
```

Customize the context block per command or config profile:

```sh
slick message send \
  --channel C1234567890 \
  --message "Build passed" \
  --agent \
  --agent-emoji :gear: \
  --agent-message "Sent by release automation"
```

## Read and Search

`lookup messages` searches Slack messages with the Web API `search.messages`
method. It requires a user token with `search:read`; bot-token profiles cannot
use this command. In generated manifests, `--type both` places `search:read`
under user scopes only.

```sh
slick history list --channel C1234567890 --max-items 50
slick history list --channel C1234567890 --thread 1746284582.123456
slick lookup messages --query "deploy failed" --max-items 10
slick lookup channel --max-items 20
slick lookup channel --types im
slick lookup user --presence
```

History returns parent messages by default. Fetch thread replies with `--thread`.

Slack scope checks are best-effort when token scope metadata is available. If
Slack rejects a request during the target API call, common permission outcomes
map to the fixed error contract: `missing_scope` and `no_permission` are
`auth_failure`; `not_in_channel`, `channel_not_found`, and `user_not_found` are
`not_found`.

## Set Status

`status` mutates your Slack profile, so it requires a user token with
`users.profile:write`.

```sh
slick status set --text "Heads down" --emoji :headphones: --expires-in 2h
slick status set "In a meeting" :calendar:
slick status clear
```

## Mutate Safely

```sh
slick reply \
  --channel C1234567890 \
  --parent 1746284582.123456 \
  --message "Investigating"

slick react add \
  --channel C1234567890 \
  --timestamp 1746284582.123456 \
  --emoji :thumbsup:

slick react list \
  --channel C1234567890 \
  --timestamp 1746284582.123456

slick message edit \
  --channel C1234567890 \
  --timestamp 1746284582.123456 \
  --message "Corrected text"

slick message delete \
  --channel C1234567890 \
  --timestamp 1746284582.123456 \
  --force
```

Deletes require `--force` unless you use `--dry-run`.

## Upload Files

`file upload` is probationary. It is implemented and covered by mock tests, but
the command remains hidden from help and shell completion. Agent schema and
workflow guidance may mention it only as probationary and not promoted until the
remaining promotion evidence, including live Slack smoke, is complete.

```sh
slick file upload --channel C1234567890 --file ./report.txt

tar czf - ./dist |
  slick file upload \
    --channel C1234567890 \
    --file - \
    --filename dist.tgz
```

Progress and diagnostics go to stderr. stdout stays reserved for command data.

## Output

TTY output uses clog's human-readable mode. Non-TTY and agent contexts output
JSON. Use flags when you need a specific shape:

```sh
slick auth status --json
slick lookup channel --plain
slick agent schema --compact
```

`--compact` removes the envelope. `--plain` is for humans. `--raw` is
output-only for Slack-native payloads; it does not make message input raw Block
Kit. Use command-local `--blocks` for raw Block Kit input.

## Agent Help

Agents can inspect the command tree and workflow notes:

```sh
slick agent schema
slick agent schema --compact
slick agent guide --help
slick agent guide send_msg
```

`agent guide --help` lists workflows such as `auth_setup`, `send_msg`,
`reply`, `react`, `read_history`, `discover_destination`, and `safe_mutation`.
