# slick version

**Print** version, commit, branch, build time, and builder identity for the
installed slick binary.

## version

```sh
slick version
slick version --output=json
```

### Output

=== "Human"

    Human mode (values depend on the installed build; `built` renders as a
    relative phrase derived from the RFC3339 build timestamp, e.g. `now`,
    `2 minutes ago`, `3 days ago`):

    ```text
    slick v0.5.5
       commit=87e81b8
       branch=main
       built="2 minutes ago"
       built by=goreleaser
    ```

=== "JSON"

    JSON envelope (`--output=json`):

    ```json
    {
      "meta": {"command": "version", "workspace": "version", "timestamp": "2026-05-26T03:00:56Z", "request_id": "337f5bd1-a5f2-4bb8-8da5-510cb801f62d"},
      "data": {
        "version": "v0.5.5",
        "commit": "87e81b8",
        "branch": "main",
        "build_time": "2026-05-10T19:55:00Z",
        "build_by": "goreleaser"
      },
      "errors": []
    }
    ```

The version string follows the latest released tag. Local builds via
`mise run build` embed `-dirty` when the working tree has uncommitted
changes; `built by` reflects the build environment (`goreleaser` for
release artifacts, `homebrew` for the brew bottle, the local user for
dev builds).

### Flags

??? note "Flags"

    | Flag | Value | Description |
    |------|-------|-------------|
    | `-h`, `--help` | | help for version |

## See also

*   [README](https://github.com/matcra587/slack-cli#readme) for install paths.
*   [Release tags on GitHub](https://github.com/matcra587/slack-cli/releases)
    for the canonical version history.
