---
description: "Discover slick’s command schema, read authoritative runbooks, and export agent skills."
---

# slick agent

The agent surface uses [docent](https://github.com/matcra587/docent) to serve
embedded runbooks, command discovery, and exported skills without credentials.
The embedded guides are the workflow authority.

## Documentation for agents

The website's [llms.txt index](https://matcra587.github.io/slack-cli/llms.txt)
links to Markdown copies of the public reference pages. The
[full-text export](https://matcra587.github.io/slack-cli/llms-full.txt) combines
those pages, and **Copy as Markdown** copies the current page's rendered content.

These exports describe the website revision. Use the installed binary's schema
and embedded guides for its exact contract and authoritative workflows; neither
requires Slack credentials:

```sh
slick agent schema --output=json
slick agent guide --output=human
slick agent guide core-contract --output=human
slick agent guide safe-mutation --output=human
slick agent guide --all --output=human
```

The website export does not include the complete embedded runbook collection.
Retrieve runbooks from the binary rather than assuming the website matches an
older installed release. For machine-readable guides, use `--output=json` and
read `data.document`.

## Schema

```sh
slick agent schema --output=json
slick agent schema --path 'message send' --output=json
slick agent schema --shapes --output=compact
slick agent schema --compact --output=json
```

JSON uses the common `{ok,meta,data,errors}` envelope. The schema artifact lives
at `data.schema`, or `schema` in compact mode. Local `$ref` values resolve
against that extracted artifact. Human mode prints the JSON artifact directly.

The docent tree uses `name`, `path`, `children`, `flags`, and `flag_groups`.
Persistent flags are declared on their owning ancestor. Host contract metadata
remains under `extensions`: auth, environment names, exit codes, output modes,
examples, input/output descriptions, guide summaries, and operational guidance.
`extensions.contract_version` is `2`.

| Flag | Meaning |
| --- | --- |
| `--path`, `-p` | Select one canonical command subtree, including schemas |
| `--shapes`, `-s` | Embed schemas throughout the tree; repeated bodies pool under `$defs` |
| `--compact`, `-c` | Retain routing facts while omitting verbose descriptions and metadata |

These flags are mutually exclusive. The local `--compact` changes schema
content; global `--output=compact` selects the result format.

## Guides

```sh
slick agent guide --output=human
slick agent guide send-msg --output=human
slick agent guide send_msg --section Run --output=human
slick agent guide --all --output=json
```

No slug returns an index. A slug returns its runbook; old underscore names
resolve as aliases. `--section`, `-s` selects one of Decide, Run, Save,
Preconditions, Recover, or Next. `--all`, `-a` returns every guide in canonical
order. JSON carries Markdown at `data.document`; compact at `document`.
Human mode prints Markdown directly. All-guide documents use form-feed
separators in their canonical machine representation.

## Export

```sh
slick agent export --format agent-skill --dir ./slick-skills --output=json
slick agent export --scope project --harness codex --output=json
```

`--dir`, `-d` requires an explicit `--format`, `-f`. Alternatively,
`--scope`, `-s` selects `project` or `user`; `--harness`, `-H` selects the
consumer, or docent detects it. Explicit directory and scope are exclusive.
Built-in formats are `agent-skill` and `claude-skill`; exported skill names
are qualified with `slick-`. JSON reports relative written paths in
`data.paths`. Export writes files to the selected destination.

## Compatibility

Contract version 2 moves raw schema output into the common result envelope,
replaces the bespoke schema walker with docent, and changes no-argument guide
output from a full dump to an index. Use `agent guide --all` for the full set,
`--output=human` for direct Markdown, and extract `data.schema` when a tool
needs the schema artifact alone. Former top-level contract metadata is now
under the artifact's `extensions` object.
