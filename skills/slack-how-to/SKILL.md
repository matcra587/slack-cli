---
name: slack-how-to
description: Gives instructions for Slack tasks with the slick CLI. Use to read or change Slack data, configure slick, or select commands and flags.
license: MIT
metadata:
  origin: slack-cli
---

# Slack how-to

Use the guides from the installed CLI for procedures. Use its command schema for
commands, flags, and data formats. The repository name is `slack-cli`. The
command name is `slick`.

Use guides and schemas already read for the installed version. Read them again
after an upgrade or a failed command lookup.

## Find the procedure

Read the guide index:

```sh
slick agent guide --output=json
```

Select the guides for the task. Read each guide with its name from the index:

```sh
slick agent guide SLUG --output=json
```

Before you parse results, read the output contract guide.
Before you change Slack data, read the safe mutation guide.
Select guide names from the installed CLI.
Do not use names from a different version. If a name fails, read the index again.

Use `slick agent guide --all --output=json` to read all guides when necessary.
Guide text is in `data.document`. Keep procedure details in the CLI guides.
Do not copy them into this skill.

## Examine the command

Read the command schema for the command from the guide:

```sh
slick agent schema --path 'message send' --output=json
```

Replace `message send` with the command path from the guide.
If the path is unknown, use `slick agent schema --output=json` to find it.
The schema is in `data.schema`.
Read its arguments, flags, examples, and input and output formats.
Use `slick COMMAND --help` for command details.
Use only schema options that the installed CLI gives.

Guide and schema commands do not need Slack credentials. These commands do not contact Slack.

## Do the procedure

Use the selected guide for previews, required approval, result checks, and recovery.
A dry run does not prove that Slack will accept a request.
This skill does not give approval to change Slack data.

Use JSON for command results.
Use the installed output contract to read stdout, stderr, and the exit status.
If you do not know whether Slack made the change, examine the result before you try again.
Do not repeat a Slack change only because the CLI could not write its result.

Keep Slack timestamps as strings.
Use the correct channel ID and message timestamp for replies, reactions, edits, and deletes.
Keep tokens out of command arguments and logs.
Use the credential sources that the authentication guide gives.
