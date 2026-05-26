# slick manifest

**Print** a Slack app manifest you can import into a Slack workspace. slick
does not create Slack apps; it generates the manifest text.

## manifest template

=== "Default"

    Default messaging manifest for user-token auth:

    ```sh
    slick manifest template --preset messaging --type user --name slack-cli > manifest.json
    ```

=== "YAML output"

    Emit the manifest as YAML:

    ```sh
    slick manifest template --preset messaging --type user --name slack-cli --format yaml > manifest.yml
    ```

=== "Pin callback port"

    Pin a local OAuth callback port (used by `auth login --oauth-callback-port`):

    ```sh
    slick manifest template --preset messaging --type user --name slack-cli --callback-port 53221 > manifest.json
    ```

=== "Override scopes"

    Override scopes individually:

    ```sh
    slick manifest template --preset readonly --type user \
      --user-scope channels:history --user-scope groups:history > manifest.json
    ```

### Flags

??? note "Flags"

    | Flag | Value | Description |
    |------|-------|-------------|
    | `-N`, `--name` | `<NAME>` | App display name |
    | `-d`, `--description` | `<TEXT>` | Short app description |
    | `-L`, `--long-description` | `<TEXT>` | Long app description |
    | `-p`, `--preset` | `<PRESET>` | Scope preset: readonly, messaging, files, search, or full |
    | `-t`, `--type` | `<TYPE>` | Auth shape: user, bot, or both |
    | `-B`, `--background-color` | `<#RRGGBB>` | App background color |
    | `-S`, `--bot-scope` | `<SCOPE>…` | Override bot OAuth scope |
    | `-U`, `--user-scope` | `<SCOPE>…` | Override user OAuth scope |
    | `-r`, `--redirect-url` | `<URL>…` | OAuth redirect URL |
    | `-C`, `--callback-port` | `<PORT>` | Local OAuth callback port for the generated redirect URL |
    | `-f`, `--format` | `<FORMAT>` | Output format: json or yaml |

`--callback-port` and `--redirect-url` are alternatives: pass `--callback-port`
and slick builds `http://localhost:<port>/callback`; pass `--redirect-url`
to set the URL directly.

### Presets

| Preset | Summary |
|--------|---------|
| `readonly` | Read conversations, reactions, and users without mutation scopes. |
| `messaging` | Default. Readonly plus send/edit/delete messages and DMs, manage reactions. |
| `files` | Messaging plus file upload (`files:write`). |
| `search` | Readonly plus workspace message search (`search:read`, user-token only). |
| `full` | Messaging, file upload, search, and `users.profile:write` for `status set`. |

??? note "Scope matrix"

    Authoritative per-preset scope set, derived from
    [`internal/cli/manifest/manifest.go`](https://github.com/matcra587/slack-cli/blob/main/internal/cli/manifest/manifest.go):

    | Scope | `readonly` | `messaging` | `files` | `search` | `full` |
    |---|:-:|:-:|:-:|:-:|:-:|
    | `channels:history` | ✓ | ✓ | ✓ | ✓ | ✓ |
    | `channels:read` | ✓ | ✓ | ✓ | ✓ | ✓ |
    | `chat:write` |   | ✓ | ✓ |   | ✓ |
    | `files:write` |   |   | ✓ |   | ✓ |
    | `groups:history` | ✓ | ✓ | ✓ | ✓ | ✓ |
    | `groups:read` | ✓ | ✓ | ✓ | ✓ | ✓ |
    | `im:history` | ✓ | ✓ | ✓ | ✓ | ✓ |
    | `im:read` | ✓ | ✓ | ✓ | ✓ | ✓ |
    | `im:write` |   | ✓ | ✓ |   | ✓ |
    | `mpim:history` | ✓ | ✓ | ✓ | ✓ | ✓ |
    | `mpim:read` | ✓ | ✓ | ✓ | ✓ | ✓ |
    | `mpim:write` |   | ✓ | ✓ |   | ✓ |
    | `reactions:read` | ✓ | ✓ | ✓ | ✓ | ✓ |
    | `reactions:write` |   | ✓ | ✓ |   | ✓ |
    | `search:read` |   |   |   | ✓ | ✓ |
    | `users:read` | ✓ | ✓ | ✓ | ✓ | ✓ |
    | `users:read.email` |   | ✓ | ✓ |   | ✓ |
    | `users.profile:write` |   |   |   |   | ✓ |

`--type user` is the normal choice — the CLI acts as you. Use `--type bot`
only when messages should originate from the app's bot user. `--type both`
generates a manifest with both shapes; in that case `search:read` is placed
under user scopes only, since bot tokens cannot use the search API.

### Output

The manifest is printed to stdout as JSON (default) or YAML. Pipe it to a
file or your clipboard.

```sh
slick manifest template --preset messaging --type user --name slack-cli | \
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
  --callback-port "$PORT" > manifest.json

# Import manifest.json in Slack, then:
slick auth login --oauth-callback-port "$PORT"
```

!!! warning "Redirect URL must match"
    The OAuth flow only works when the manifest's redirect URL matches the local
    callback slick is listening on.

## See also

*   [`auth`](auth.md) — once the manifest is imported in Slack.
*   [README](https://github.com/matcra587/slack-cli#readme) for the end-to-end setup.
