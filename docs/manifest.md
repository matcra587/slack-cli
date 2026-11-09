---
description: "Generate Slack app manifests for slick’s required OAuth permissions."
---

# slick manifest

**Print** a Slack app manifest you can import into a Slack workspace. slick
does not create Slack apps; it generates the manifest text.

## manifest template

=== "Default"

    Default messaging manifest for user-token auth:

    ```sh
    slick manifest template --preset messaging --type user --name slack-cli --output=human > manifest.json
    ```

=== "YAML output"

    Emit the manifest as YAML:

    ```sh
    slick manifest template --preset messaging --type user --name slack-cli --format yaml --output=human > manifest.yml
    ```

=== "Pin callback port"

    Pin a local OAuth callback port (used by `auth login --oauth-callback-port`):

    ```sh
    slick manifest template --preset messaging --type user --name slack-cli --callback-port 53221 --output=human > manifest.json
    ```

=== "Override scopes"

    Override scopes individually:

    ```sh
    slick manifest template --preset readonly --type user \
      --user-scope channels:history --user-scope groups:history --output=human > manifest.json
    ```

### Flags

??? note "Flags"

    | Flag | Value | Description |
    |------|-------|-------------|
    | `-N`, `--name` | `<NAME>` | App display name, 1–35 characters |
    | `-d`, `--description` | `<TEXT>` | Short app description, up to 140 characters |
    | `-L`, `--long-description` | `<TEXT>` | Long app description, 175–4000 characters when supplied |
    | `-p`, `--preset` | `<PRESET>` | Scope preset: readonly, messaging, files, search, organize, or full |
    | `-t`, `--type` | `<TYPE>` | Auth shape: user, bot, or both |
    | `-B`, `--background-color` | `<#RGB>` or `<#RRGGBB>` | App background color in hexadecimal |
    | `-S`, `--bot-scope` | `<SCOPE>…` | Override bot OAuth scope |
    | `-U`, `--user-scope` | `<SCOPE>…` | Override user OAuth scope |
    | `-r`, `--redirect-url` | `<URL>…` | OAuth redirect URL |
    | `-C`, `--callback-port` | `<PORT>` | Local OAuth callback port, 1–65535; 0 allocates a free port |
    | `-f`, `--format` | `<FORMAT>` | Output format: json or yaml |

Pass `--callback-port` and slick builds `http://localhost:<port>/callback`.
Without that flag, `SLACK_CLI_CALLBACK_PORT` supplies the port; otherwise slick
allocates a free local port. Port 0 also requests automatic allocation.
An explicit `--redirect-url` takes precedence over both callback-port settings.

Display text is trimmed before validation. Empty descriptions and background
colours are omitted. Invalid display lengths, colours, or selected callback
ports return a validation error (exit 4). These checks run locally without
Slack credentials. Slack still validates the complete manifest when you import
it; local checks do not prove that Slack will accept every supplied setting.

### Presets

| Preset | Summary |
|--------|---------|
| `readonly` | Read conversations, reactions, and users without mutation scopes. |
| `messaging` | Default. Readonly plus send/edit/delete messages and DMs, manage reactions. |
| `files` | Messaging plus file upload (`files:write`). |
| `search` | Readonly plus workspace message search (`search:read`, user-token only). |
| `organize` | DND, pins and channel link bookmarks. |
| `full` | Messaging, files, search, profile status, DND, pins and bookmarks. |

??? note "Scope matrix"

    Authoritative per-preset scope set, derived from
    [`internal/cli/manifest/manifest.go`](https://github.com/matcra587/slack-cli/blob/main/internal/cli/manifest/manifest.go):

    | Scope | `readonly` | `messaging` | `files` | `search` | `organize` | `full` |
    |---|:-:|:-:|:-:|:-:|:-:|:-:|
    | `bookmarks:read` |   |   |   |   | ✓ | ✓ |
    | `bookmarks:write` |   |   |   |   | ✓ | ✓ |
    | `channels:history` | ✓ | ✓ | ✓ | ✓ |   | ✓ |
    | `channels:read` | ✓ | ✓ | ✓ | ✓ |   | ✓ |
    | `chat:write` |   | ✓ | ✓ |   |   | ✓ |
    | `dnd:read` |   |   |   |   | ✓ | ✓ |
    | `dnd:write` |   |   |   |   | ✓ | ✓ |
    | `files:write` |   |   | ✓ |   |   | ✓ |
    | `groups:history` | ✓ | ✓ | ✓ | ✓ |   | ✓ |
    | `groups:read` | ✓ | ✓ | ✓ | ✓ |   | ✓ |
    | `im:history` | ✓ | ✓ | ✓ | ✓ |   | ✓ |
    | `im:read` | ✓ | ✓ | ✓ | ✓ |   | ✓ |
    | `im:write` |   | ✓ | ✓ |   |   | ✓ |
    | `mpim:history` | ✓ | ✓ | ✓ | ✓ |   | ✓ |
    | `mpim:read` | ✓ | ✓ | ✓ | ✓ |   | ✓ |
    | `mpim:write` |   | ✓ | ✓ |   |   | ✓ |
    | `pins:read` |   |   |   |   | ✓ | ✓ |
    | `pins:write` |   |   |   |   | ✓ | ✓ |
    | `reactions:read` | ✓ | ✓ | ✓ | ✓ |   | ✓ |
    | `reactions:write` |   | ✓ | ✓ |   |   | ✓ |
    | `search:read` |   |   |   | ✓ |   | ✓ |
    | `users.profile:write` |   |   |   |   |   | ✓ |
    | `users:read` | ✓ | ✓ | ✓ | ✓ |   | ✓ |
    | `users:read.email` |   | ✓ | ✓ |   |   | ✓ |

`--type user` is the normal choice — the CLI acts as you. Use `--type bot`
only when messages should originate from the app's bot user. `--type both`
generates a manifest with both shapes; in that case `search:read` is placed
under user scopes only, since bot tokens cannot use the search API.
`users.profile:write` and `dnd:write` are also excluded from bot scopes.

`--user-scope` replaces the user-token preset scopes for `user` and `both`;
`--bot-scope` replaces the bot-token preset scopes for `bot` and `both`.
Overrides for a token shape outside the selected `--type` are ignored. Repeat
the matching flag for multiple scopes. slick trims, deduplicates, and sorts
custom scope lists; a list containing only blank values uses the preset.
Bot scope lists omit `search:read` and `users.profile:write`. If filtering leaves
both scope lists empty, JSON and YAML preserve an empty `scopes` object.

### Output

Human output prints the importable manifest directly as JSON (default) or YAML.
`--format yml` is also accepted as an alias for `yaml`.
Use `--output=human` when saving an importable file. Machine output uses the
common result envelope: extract `data.manifest` for JSON or `data.document` for
YAML; compact output has the same payload without the envelope. Pipe it to a
file or your clipboard.

```sh
slick manifest template --preset messaging --type user --name slack-cli --output=human | \
  jq -e '.oauth_config.scopes.user'      # confirm scope shape
```

### Recommended port-pinning workflow

```sh
PORT=$(python3 - <<'PY'
import socket
s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()
PY
)

slick manifest template --preset messaging --type user --name slack-cli \
  --callback-port "$PORT" --output=human > manifest.json

# Import manifest.json in Slack, then:
slick auth login --oauth-callback-port "$PORT"
```

!!! warning "Redirect URL must match"
    The OAuth flow only works when the manifest's redirect URL matches the local
    callback slick is listening on.

## See also

*   [`auth`](auth.md) — once the manifest is imported in Slack.
*   [README](https://github.com/matcra587/slack-cli#readme) for the end-to-end setup.
