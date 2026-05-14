# slick config

Manage local preferences. `config` does not handle auth — use
[`auth`](auth.md) for credentials. Config holds the keychain reference and
non-secret workspace preferences (default channel, attribution).

```text
slick config init   First-run config setup
slick config path   Print the config file path
slick config list   Show current settings (aliases: ls)
slick config get    Show a configuration value
slick config set    Add or update a setting
slick config unset  Remove a setting (aliases: rm, remove)
```

Config lives at `${SLICK_CONFIG:-${XDG_CONFIG_HOME:-~/.config}/slick/config.toml}`.
`SLACK_CLI_CONFIG` remains as a legacy override. Path inputs expand `~` and
environment variables.

## config init

Bootstrap a config file. The default profile is named `default`.

```sh
slick config init
slick config init --profile staging
slick config init --default-channel C1234567890 \
                  --attribution-enabled \
                  --attribution-emoji :robot_face: \
                  --attribution-message "Sent via slack-cli"
slick config init --force          # overwrite existing config
```

### Flags

```text
-p, --profile <NAME>                Local workspace profile name
-c, --default-channel <CHANNEL>     Default message channel ID or alias
-A, --attribution-enabled           Enable visible attribution by default
-l, --attribution-label <LABEL>     Attribution label
-e, --attribution-emoji <EMOJI>     Attribution emoji
-m, --attribution-message <TEXT>    Attribution message
-F, --force                         Overwrite an existing config
```

Output (post-v0.4.0; `written` field was removed):

```text
Config initialized path=~/.config/slick/config.toml profile=default workspace=default
```

## config path

Print the resolved config path and whether the file exists.

```sh
slick config path
```

Human:

```text
Config path resolved path=~/.config/slick/config.toml exists=true
```

`exists` is green on true, red on false.

## config list

Render every configurable key as a table. KEY column hash-colours the
dotted leaf segment; VALUE shows bool values in green/red, `(unset)` dim for
empty.

```sh
slick config list
slick config ls           # alias
```

Human:

```text
KEY                                       VALUE     DESCRIPTION
default_workspace                         default   Default workspace profile name
workspaces.default.default_channel        (unset)   Fallback message channel ID or alias
workspaces.default.attribution.enabled    true      Add visible attribution by default
workspaces.default.attribution.label      (unset)   Attribution label override
workspaces.default.attribution.emoji      (unset)   Attribution emoji override
workspaces.default.attribution.message    (unset)   Attribution message override
```

The summary header (`Config listed path=… default_workspace=… settings=N`)
only renders under `--debug` to keep the default view focused on the table.

## config get

Print one setting. Empty values display as `(unset)` dim.

```sh
slick config get default_workspace
slick config get workspaces.default.attribution.emoji
```

```text
Config value retrieved key=workspaces.default.attribution.enabled value=true
```

## config set / config unset

Update or remove a setting. Workspace-scoped keys use the
`workspaces.<profile>.<field>` form.

```sh
slick config set default_workspace staging
slick config set workspaces.default.default_channel C1234567890
slick config set workspaces.default.attribution.enabled true
slick config unset workspaces.default.attribution.message
slick config rm workspaces.default.attribution.emoji     # alias
```

### Available keys

```text
default_workspace
workspaces.<profile>.default_channel
workspaces.<profile>.attribution.enabled
workspaces.<profile>.attribution.label
workspaces.<profile>.attribution.emoji
workspaces.<profile>.attribution.message
```

Auth-owned fields (`team_id`, `team_name`, `token_type`, `token_ref`) are
managed by [`auth login`](auth.md#auth-login); `config set` does not edit them.

## Common errors

| Error | Cause | Fix |
|-------|-------|-----|
| `config already exists; rerun with --force to overwrite` | `config init` ran with an existing config file. | `--force` to replace; `config set` for targeted edits. |
| `workspace "<name>" not found` | `set default_workspace <name>` for a workspace not in `Workspaces`. | Run `slick workspace list` to confirm the profile name. |
| `unknown config key <key>` | Key is not in the supported set above. | Run `slick config list` to see the available keys. |

## See also

*   [`auth`](auth.md) — credentials, not preferences.
*   [`workspace`](workspace.md) — list configured profiles.
