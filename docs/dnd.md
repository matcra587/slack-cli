# Do Not Disturb

Inspect notification settings or change your own Do Not Disturb session:

```sh
slick dnd info --output=json
slick dnd info --user U1234567890 --output=json
slick dnd team --users U1234567890,U2345678901 --output=json
slick dnd snooze --minutes 60 --dry-run --output=json
slick dnd snooze --minutes 60 --output=json
slick dnd end-snooze --output=json
slick dnd end --output=json
```

Reads require `dnd:read`. Changes require a user token with `dnd:write` and
always affect that token's user. `snooze` starts or extends a notification pause;
`end-snooze` ends that pause. `end` ends the current scheduled Do Not Disturb
session; it does not delete your recurring notification schedule.

For app setup, generate a manifest with `slick manifest template --preset organize`
or `--preset full`. After configuring those scopes in your app, choose the same
`--scope-preset` during `slick auth login --method oauth`. Login still defaults
to the narrower `messaging` preset; existing tokens gain no new permissions.

`--minutes` must be a positive whole number. Slack checks its maximum duration.
`team` accepts between 1 and 50 explicit user IDs; repeat `--users` or separate IDs with commas.

JSON uses slick's shared envelope. `data.status` contains Slack's DND fields for
`info`; `data.users` maps user IDs to settings for `team`. Snooze changes return
`data.snooze`, including `snooze_enabled`. `snooze_remaining` is measured
in **seconds**, even though the requested duration is in minutes. Dry runs
contain the requested minutes and `dry_run: true`, without fetching credentials
or calling Slack. A successful `end` acknowledges completion without a fresh
settings read. Read `dnd info` to check the resulting state.

Malformed or missing responses are errors. After an uncertain mutation or exit
8, inspect the current settings before retrying.

See Slack's [Do Not Disturb methods](https://docs.slack.dev/reference/methods/dnd.info/).
