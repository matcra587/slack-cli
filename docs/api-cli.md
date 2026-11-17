---
description: "Discover Slack Web API methods and call them with saved workspace authentication."
---

# Slack API commands

Use `slick api` for published Slack methods without a dedicated task command.
Method discovery works offline without authentication:

```sh
slick api methods conversations. --output=json
slick api describe conversations.history --output=json
```

The catalog describes arguments, scopes, pagination, and execution restrictions.
It does not prove that your workspace or token can use a method.

## Call a method

Calls use the selected workspace's saved authentication. Repeat `--field` for
string parameters, or use `--input` for a JSON object with typed values.

```sh
slick api call users.info --field user=U1234567890 --output=json
slick api call conversations.history --input request.json --output=json
```

Use `--input=-` to read JSON from stdin. Input is limited to 4 MiB. Credential
parameters are rejected; keep credentials out of arguments and input files.
Responses retain Slack's native JSON under `data.response` in envelope mode.

Methods that can change Slack state require `--force`. Use `--dry-run` to
validate a request and inspect its parameters without contacting Slack or
resolving credentials:

```sh
slick api call chat.postMessage --field channel=C1234567890 \
  --field 'text=Release checks passed.' --dry-run --output=json
```

Generic calls never retry automatically, including mutations and rate limits.
Methods with unsupported authentication, transport, or response requirements
are rejected before sending a request.

## Pagination and response files

`--paginate` fetches pages only for reviewed read methods with documented
pagination. `--max-pages` defaults to 10 and accepts 1 through 1000.

```sh
slick api call conversations.list --paginate --max-pages=5 --output=json
```

Paginated results include `pages`, `completed_pages`, and `complete`. A bounded
result preserves `next_cursor` or `next_page` when more results remain. If a
later request fails, the error includes completed-page and resume metadata;
the invocation fails instead of reporting partial success.

Use `--output-file` to save a response in a new private file. Existing paths
are refused. Methods classified as returning sensitive data require this flag.
The normal result contains a file receipt instead of the response body.
Failed requests remove their newly created output file. Output-write failures
use exit 8; inspect the remote outcome before retrying a mutation.
