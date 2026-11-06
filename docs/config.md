---
description: "Manage slick configuration, defaults, aliases, and credential references."
---

# slick config

**Manage** local preferences. `config` does not handle auth — use
[`auth`](auth.md) for credentials. Config holds the keychain reference and
non-secret workspace preferences (default channel, attribution).

Config lives at `${SLICK_CONFIG:-${XDG_CONFIG_HOME:-~/.config}/slick/config.toml}`.
`SLACK_CLI_CONFIG` remains as a legacy override. Path inputs expand `~` and
environment variables. `SLICK_CONFIG` takes precedence over the legacy override.
Relative override paths are interpreted from the current directory.

`SLACK_CLI_DEFAULT_WORKSPACE` selects a workspace for the current process when
`--workspace` is omitted. It does not change the saved default. `config get` and
`config list` show saved preferences; `config set` and `config unset` preserve
the saved default unless you explicitly set `default_workspace`. An unknown
environment selection fails commands that need that workspace; you can still
edit the config or select a known workspace with `--workspace`.

Saving writes a complete replacement file with mode `0600` on Unix. It requires
write access to the existing file and its parent directory. Existing symlinks
are kept, and their targets are updated. Temporary files are removed on failure.
Errors before replacement leave the previous file intact. If directory sync
fails after replacement, the new file and the runtime settings stay applied.
The command exits 5, and machine errors include `config_published: true` and
`durability_confirmed: false` in `errors[].details`. The new file is in place,
but its survival across a crash is uncertain. Inspect the config before retrying.

Configuration text must be valid UTF-8. Quotes, backslashes, and control
characters are escaped as TOML so the saved values can be read back unchanged.
Invalid text is rejected before the config is replaced.

`config init`, `config set`, and `config unset` support `--dry-run` or `-n`.
A preview validates the requested change and returns `dry_run: true` without
prompts, credential lookups, file or directory creation, or changes to the
in-memory config. Previewing replacement with `config init` still requires
`--force` when a file exists. A preview does not check whether a later write
will succeed. Remove the flag or use `--dry-run=false` to apply the change.

```sh
slick config set workspaces.default.default_channel C123 --dry-run
```

## config init

Bootstrap a config file. The default profile is named `default`.

Profile selectors use Unicode case matching. Names that match this way cannot
coexist in one config: for example, `s` and `ſ`, or `σ` and `ς`. Rename one
conflicting profile before using that config. Matching does not remove accents
or normalize different Unicode spellings.

This command accepts flags only. Use `--profile NAME` to name the profile.
Without `--force`, an existing file is kept unless you confirm replacement
in an interactive terminal. With `--force`, the entire file is replaced with
one profile and the supplied preferences. Other profiles, aliases, and credential
references are removed from the config. Use `config set` for targeted changes.

=== "Default"

    Initialize the default profile:

    ```sh
    slick config init
    ```

=== "Named profile"

    Initialize a named workspace profile:

    ```sh
    slick config init --profile staging
    ```

=== "With attribution"

    Set a default channel and attribution defaults at init time:

    ```sh
    slick config init --default-channel C1234567890 \
                      --attribution-enabled \
                      --attribution-emoji :robot_face: \
                      --attribution-message "Sent via slack-cli"
    ```

=== "Overwrite"

    Overwrite an existing config:

    ```sh
    slick config init --force
    ```

### Flags

??? note "Flags"

    | Flag | Value | Description |
    |------|-------|-------------|
    | `-p`, `--profile` | `<NAME>` | Local workspace profile name |
    | `-c`, `--default-channel` | `<CHANNEL>` | Default message channel ID or alias |
    | `-A`, `--attribution-enabled` | | Enable visible attribution by default |
    | `-l`, `--attribution-label` | `<LABEL>` | Attribution label |
    | `-e`, `--attribution-emoji` | `<EMOJI>` | Attribution emoji |
    | `-m`, `--attribution-message` | `<TEXT>` | Attribution message |
    | `-F`, `--force` | | Overwrite an existing config |

Output (post-v0.4.0; `written` field was removed):

=== "Human"

    ```text
    Config initialized path=~/.config/slick/config.toml profile=default workspace=default
    ```

=== "JSON"

    !!! warning "TODO"
        TODO: Gather output for `config init`

## config path

Print the expanded config path and whether that path exists, following symlinks.
Missing paths and symlinks with missing targets return `exists=false`. A
directory returns `exists=true`; this command checks presence, not config
contents. Permission errors and symlink cycles fail with exit code 5 instead
of reporting a missing path. The command does not change the config.

```sh
slick config path
```

=== "Human"

    ```text
    Config path resolved path=~/.config/slick/config.toml exists=true
    ```

=== "JSON"

    The `data` object is shown below. JSON wraps it in the common result
    envelope; compact output prints this object directly.

    ```json
    {
      "path": "/home/example/.config/slick/config.toml",
      "exists": true
    }
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

=== "Human"

    ```text
    KEY                                       VALUE     DESCRIPTION
    default_workspace                         default   Default workspace profile name
    workspaces.default.default_channel        (unset)   Fallback message channel ID or alias
    workspaces.default.attribution.enabled    true      Add visible attribution by default
    workspaces.default.attribution.label      (unset)   Attribution label override
    workspaces.default.attribution.emoji      (unset)   Attribution emoji override
    workspaces.default.attribution.message    (unset)   Attribution message override
    ```

=== "JSON"

    !!! warning "TODO"
        TODO: Gather output for `config list`

The summary header (`Config listed path=… default_workspace=… settings=N`)
only renders under `--debug` to keep the default view focused on the table.

## config get

Print one setting. Empty values display as `(unset)` dim.

```sh
slick config get default_workspace
slick config get workspaces.default.attribution.emoji
```

=== "Human"

    ```text
    Config value retrieved key=workspaces.default.attribution.enabled value=true
    ```

=== "JSON"

    !!! warning "TODO"
        TODO: Gather output for `config get`

## config set / config unset

Update or remove a setting. Workspace-scoped keys use the
`workspaces.<profile>.<field>` form.

Use the exact profile name shown by `config list`. A dot in the profile name
stays part of that name: for profile `team.prod`, the default-channel key is
`workspaces.team.prod.default_channel`.

The value of `default_workspace` is a profile selector, like `--workspace`.
It ignores surrounding whitespace and uses the same Unicode case matching.
The saved value and command result use the profile's original spelling.
Blank or unknown names fail without changing the config.

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

Older configs may store attribution under `agent_attribution`, `agent_label`,
`agent_emoji`, and `agent_message`. `get` and `list` include these configured
values. A non-empty nested text value takes precedence over its legacy value;
an explicit nested boolean takes precedence over the legacy boolean.

Updating or unsetting an attribution setting clears its legacy copy too.
For text settings, `set KEY ""` clears the value. Other whitespace is preserved.
Saving writes the nested `attribution` fields, keeping unrelated preferences,
aliases, and credential references.

## Common errors

| Error | Cause | Fix |
|-------|-------|-----|
| `config already exists; rerun with --force to overwrite` | `config init` ran with an existing config file. | `--force` to replace; `config set` for targeted edits. |
| `workspace "<name>" not found` | `set default_workspace <name>` for a workspace not in `Workspaces`. | Run `slick workspace list` to confirm the profile name. |
| `unknown config key <key>` | Key is not in the supported set above. | Run `slick config list` to see the available keys. |

## See also

*   [`auth`](auth.md) — credentials, not preferences.
*   [`workspace`](workspace.md) — list configured profiles.
