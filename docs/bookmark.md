# Channel bookmarks

Manage link bookmarks attached to a channel:

```sh
slick bookmark list --channel C1234567890 --output=json
slick bookmark add --channel C1234567890 --title Runbook \
  --link https://example.com/runbook --output=json
slick bookmark edit --channel C1234567890 --id Bk1234567890 --title 'On-call runbook'
slick bookmark edit --channel C1234567890 --id Bk1234567890 --emoji=
slick bookmark remove --channel C1234567890 --id Bk1234567890 --dry-run
```

`--channel` accepts an ID or configured alias. Listing requires `bookmarks:read`;
changes require `bookmarks:write`, plus access to the channel. These commands
manage channel link bookmarks, not personal saved items in Slack's Later tab.

Add requires a title and an HTTP or HTTPS link. URLs containing embedded
credentials are rejected. The bookmark type is always `link`. Use `--emoji` to
set an optional emoji. Edit requires `--id` and at least one changed field.
Omitted fields stay unchanged; `--emoji=` clears the emoji. Empty titles and
links are rejected. Remove deletes the bookmark, not the linked resource.

JSON uses slick's shared envelope. Listing returns `data.bookmarks`, including
an empty array when none exist. Add and edit return `data.bookmark` from Slack.
Remove returns the requested `bookmark_id`. Dry runs return the requested
`changes` and `dry_run: true` without fetching credentials or calling Slack.
Missing or mismatched bookmark responses are errors.

After an uncertain mutation or exit 8, list bookmarks before retrying. A created
bookmark may exist even when its result could not be delivered.

See Slack's [bookmark methods](https://docs.slack.dev/reference/methods/bookmarks.list/).
