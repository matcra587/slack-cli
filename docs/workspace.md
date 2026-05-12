# slick workspace

List configured workspace profiles. Workspace identity is managed by
[`auth`](auth.md); `config` manages preferences. `workspace` is a read-only
view.

```text
slick workspace list  List configured workspaces
```

## workspace list

```sh
slick workspace list
slick workspace list --output=json
```

### Output

Human (a primer table):

```text
PROFILE   WORKSPACE     NAME              TOKEN
default   T123ABC456    Example Inc       user
staging   T987XYZ123    Example Staging   bot
```

`PROFILE` and `WORKSPACE` (`team_id`) hash-colour as identity fields.

JSON envelope:

```json
{
  "data": {
    "workspaces": [
      {"name": "default", "team_id": "T123ABC456", "team_name": "Example Inc", "token_type": "user"},
      {"name": "staging", "team_id": "T987XYZ123", "team_name": "Example Staging", "token_type": "bot"}
    ]
  }
}
```

The `default` workspace is the one selected when no `--workspace` flag is
passed; change it with [`auth switch`](auth.md#auth-switch) or
`slick config set default_workspace <name>`.

## See also

*   [`auth status`](auth.md#auth-status) — see auth state of each workspace.
*   [`auth switch`](auth.md#auth-switch) — change the default workspace.
*   [`config`](config.md) — workspace-scoped preferences.
*   [README](https://github.com/matcra587/slack-cli#readme) and [index](index.md).
