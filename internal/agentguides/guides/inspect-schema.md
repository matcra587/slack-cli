---
slug: inspect-schema
title: "Read the machine schema and workflow guide"
description: "Read the machine schema and workflow guide"
when_to_use: "Before following the inspect schema workflow in slick."
commands: ["slick agent schema", "slick agent guide"]
aliases: ["inspect_schema"]
---

## Decide

Goal: Self-discover commands, flags, output contracts, and other workflows from inside the CLI itself.

*   Need the full inventory: `slick agent schema --output=json`.
*   Want a smaller command tree: `slick agent schema --compact` (shape change). NOTE: `--compact` (no value) is different from `--output=compact` (envelope strip only).
*   Just want workflow names: `slick agent guide` (no args).
*   Want one workflow's runbook: `slick agent guide <workflow>`.

### Workflow checkpoints

*   Use agent schema, not the removed root schema alias
*   Use --compact (the local agent-schema flag) for a smaller command tree; --output=compact only strips the JSON envelope
*   Use agent guide <workflow> to load the task runbook

## Run

*   Full schema: `slick agent schema --output=json`
*   Compact: `slick agent schema --compact --output=json`
*   Workflow list: `slick agent guide`
*   Workflow runbook: `slick agent guide <workflow>`

### Behavior

*   The old root `slick schema` alias was removed — discovery lives under `agent schema`.
*   Examples are examples, NOT guaranteed-safe invocations. Apply `--dry-run` to mutations first.
*   Conflict resolution: if a guide section disagrees with `agent schema` on flags or command existence, trust the schema (it's generated from the cobra tree) and file a bug against the guide.

## Save

*   JSON wraps discovery in `data.schema`; compact output keeps it at `schema`.
*   The docent artifact uses `name`, `path`, `children`, `flags`, and
    `flag_groups`. Persistent flags are declared on their owning ancestor.
*   `extensions` retains auth, output modes, input/output descriptions,
    environment names, exit codes, examples, best practices, and anti-patterns.
*   `extensions.guides` lists embedded runbooks and their legacy aliases.
*   `extensions.contract_version` identifies slick's agent contract.
*   `--path <command>` selects a subtree with schemas; `--shapes` includes
    schemas throughout the tree and pools common bodies under `$defs`.
*   Guide JSON carries canonical Markdown at `data.document`; compact keeps it
    at `document`. Use `--output=human` for direct Markdown.
*   `agent guide` returns the index; `--all` returns all guides. `--section`
    selects one of Decide, Run, Save, Preconditions, Recover, or Next.

## Preconditions

## Recover

## Next

*   Then: the workflow you discovered — every workflow name in this guide doubles as a `slick agent guide <name>` argument.
