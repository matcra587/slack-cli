---
slug: organize
title: "Manage notification pauses, pins and channel bookmarks"
description: "Inspect DND settings and manage pinned messages and channel links."
when_to_use: "When changing notification availability or organizing channel resources."
commands: ["slick dnd info", "slick dnd team", "slick dnd snooze", "slick dnd end-snooze", "slick dnd end", "slick pin list", "slick pin add", "slick pin remove", "slick bookmark list", "slick bookmark add", "slick bookmark edit", "slick bookmark remove"]
---

## Decide

Use DND for the authenticated user's notification pause, pins for exact channel
messages, and bookmarks for channel links. These commands do not manage personal
Later items or the retired reminders integration.

## Run

```sh
slick dnd info --output=json
slick dnd snooze --minutes 60 --dry-run --output=json
slick pin list --channel C1234567890 --output=json
slick pin add --channel C1234567890 --timestamp 1746284582.123456 --dry-run --output=json
slick bookmark list --channel C1234567890 --output=json
slick bookmark add --channel C1234567890 --title Runbook --link https://example.com/runbook --dry-run --output=json
```

Remove `--dry-run` only when the mutation is authorized. DND changes affect the
authenticated user; pins and bookmarks require an explicit channel ID or alias.
Bookmark edits change only flags supplied; `--emoji=` clears the emoji.

## Save

For `--output=json`, inspect `ok` and `meta.command`. Read `data.status` or
`data.users` for DND reads, `data.snooze` for snooze changes, `data.items` for pins, and `data.bookmarks` for channel
bookmarks. Add and edit return `data.bookmark`; save its `id` for later edits or
removal. Keep channel IDs and message timestamp strings intact.

## Preconditions

Use an authenticated workspace with the scopes for the selected operation.
DND changes require a user token. Confirm the exact channel, message timestamp
or bookmark ID before changing it.

## Recover

Missing scopes require the corresponding `dnd:read`, `dnd:write`, `pins:read`,
`pins:write`, `bookmarks:read`, or `bookmarks:write` permission. DND mutations
require a user token. Malformed responses are errors. After an uncertain
mutation or result-write failure (exit 8), read the current state before retrying.

## Next

Use → `read-history` to find message timestamps or → `discover-destination` to
resolve a channel. Use → `auth-setup` when the selected token lacks permissions.
