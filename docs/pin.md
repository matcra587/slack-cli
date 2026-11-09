# Pinned messages

Pin a message, list a channel's pins, or remove a pin:

```sh
slick pin list --channel C1234567890 --output=json
slick pin add --channel C1234567890 --timestamp 1746284582.123456 --dry-run
slick pin add --channel C1234567890 --timestamp 1746284582.123456 --output=json
slick pin remove --channel C1234567890 --timestamp 1746284582.123456 --output=json
```

`--channel` accepts a channel ID or configured alias. Add and remove require an
exact message timestamp. Removing a pin leaves the message in the channel.
File and file-comment pinning are not supported; pin the file's channel message.

Listing requires `pins:read`. Add and remove require `pins:write`. Slack also
checks whether your identity can access the channel and pin its messages.

JSON uses slick's shared envelope. List results contain `data.channel` and
`data.items`, including an empty array when there are no pins. Mutations contain
the channel and message `ts`; dry runs also contain `dry_run: true` and make no
credential or Slack calls. Successful mutations are acknowledgements; list pins
to inspect the resulting state. Missing results are errors, not empty lists.

After an uncertain mutation or exit 8, list pins before retrying. These commands
do not treat an already-pinned or absent item as a new successful mutation.

See Slack's [pins API](https://docs.slack.dev/reference/methods/pins.list/).
