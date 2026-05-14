# slick file

Upload a file to Slack. **Probationary**: the command is implemented and
covered by mock tests, but it's hidden from `slick --help` and shell
completion. Agent schema and workflow guidance mention it as probationary
and not promoted until live-Slack smoke evidence completes.

```text
slick file upload  Upload a file to Slack
```

## file upload

```sh
slick file upload --channel C1234567890 --file ./report.txt

# Stream from stdin with explicit filename + title
tar czf - ./dist |
  slick file upload \
    --channel C1234567890 \
    --file - \
    --filename dist.tgz \
    --title "Build artifact"

# Reply in thread with the upload
slick file upload --channel C1234567890 --file ./report.txt --thread 1746284582.123456

# Preview without uploading
slick file upload --channel C1234567890 --file ./report.txt --dry-run
```

### Flags

```text
-c, --channel <CHANNEL>  Channel ID, name, or alias
-f, --file <FILE>        Path to upload or - for stdin
-N, --filename <NAME>    Filename for stdin or override
-T, --title <TITLE>      Slack file title
-m, --message <TEXT>     Initial comment
-b, --blocks             Treat upload message as raw Block Kit JSON
-t, --thread <TS>        Thread timestamp
-n, --dry-run            Preview without uploading
    --attribution                Force attribution on for this command
    --no-attribution             Disable attribution for this command
    --attribution-label <LABEL>      Override attribution label
    --attribution-emoji <EMOJI>      Override attribution emoji
    --attribution-message <TEXT>     Override attribution message
```

Progress and diagnostics go to stderr; stdout stays reserved for command
data.

### Output

Human (real upload — post-v0.4.0 the JSON fields now match Slack's own file
object shape: `file_id` → `id`, `file_name` → `name`, `size_human` dropped
in favour of human-mode-only formatting of the numeric
`size`):

```text
File uploaded channel=C7N2Q8L4P id=F0B2RSKLSJH name=slick-test.txt attribution=false size="48 B"
```

Human (`--dry-run`). `id` is the literal `"dry-run"` and the event carries
`dry_run=true`; no Slack call is made:

```text
File uploaded channel=C7N2Q8L4P id=dry-run name=report.txt attribution=false dry_run=true size="13 B"
```

JSON envelope (real upload):

```json
{
  "data": {
    "file": {"id": "F0B2RSKLSJH", "name": "slick-test.txt", "size": 48},
    "channel": "C7N2Q8L4P",
    "attribution": false
  }
}
```

JSON envelope (`--dry-run`). `file.id` is the literal `"dry-run"` and the
data record carries `dry_run: true`; no Slack call is made:

```json
{
  "data": {
    "file": {"id": "dry-run", "name": "report.txt", "size": 13},
    "channel": "C7N2Q8L4P",
    "attribution": false,
    "dry_run": true
  }
}
```

Slack's `files.completeUploadExternal` does not populate `permalink` in the
upload response; slick attempts to resolve it with `files.info` after upload.
If that lookup fails, `permalink` stays absent.

## Common errors

| Error | Cause | Fix |
|-------|-------|-----|
| `not_in_channel` (`not_found`, exit 2) | App/user is not a member of the channel. | Add the bot, or use a user-token profile. |
| `validation_error: --file is required` | Missing `--file`. | Provide the path or `-` for stdin. |
| `validation_error: --filename required for stdin` | Stdin upload with no filename. | Pass `--filename`. |
| `missing_scope` (`auth_failure`, exit 1) | Token lacks `files:write`. | Use the `files` or `full` manifest preset. |

## See also

*   [`message`](message.md) — send a message without uploading a file.
*   [`manifest`](manifest.md) — `files` preset enables `files:write`.
*   Slack API methods:
  [`files.getUploadURLExternal`](https://docs.slack.dev/reference/methods/files.getUploadURLExternal/),
  [`files.completeUploadExternal`](https://docs.slack.dev/reference/methods/files.completeUploadExternal/),
  [`files.info`](https://docs.slack.dev/reference/methods/files.info/).
