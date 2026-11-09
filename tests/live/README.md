# Live verification

Live tests require an explicitly approved Slack workspace and designated test
channels. They never discover an everyday account, run OAuth login, or resolve
1Password/keychain credentials. Status changes, OAuth replacement, and token
revocation are explicitly skipped.

## Isolated setup

The caller supplies these values without logging credentials:

*   `SLICK_LIVE_CONFIRM=designated-fixtures`
*   `SLICK_LIVE_PLAN`: absolute path to a private JSON plan
*   `SLICK_LIVE_BINARY`: absolute path to a fixed compiled slick binary
*   `SLICK_LIVE_ACCESS_TOKEN`: the approved access token, in memory/environment only

The plan has exactly these non-secret fields:

```json
{
  "run_root": "/private/temporary/run",
  "config_path": "/private/temporary/run/config.toml",
  "workspace": "live-oauth-isolated",
  "team_id": "TEXAMPLE",
  "actor_id": "UEXAMPLE",
  "public_channel_id": "CEXAMPLE",
  "private_channel_id": "",
  "recipient_ids": [],
  "run_id": "unique-run-identifier"
}
```

Use actual explicitly approved IDs in the private plan. Do not commit that plan
or any token. The root must already exist with private permissions. Config and
binary must be regular files inside it, without symlinks. Config permissions
must exclude group/other access. Use this sole-profile config:

```toml
schema_version = "1"
default_workspace = "live-oauth-isolated"

[workspaces.live-oauth-isolated]
name = "live-oauth-isolated"
team_id = "TEXAMPLE"
token_type = "user"
token_ref = "env:SLICK_LIVE_ACCESS_TOKEN"
```

The live filesystem gate uses Unix permission semantics. Native Windows ACL
validation is not implemented, so Windows live use remains unverified.

All nine plan keys are required, with no duplicate, escaped-duplicate or unknown
keys. An unused private channel must be an explicit empty string; unused
recipients must be an explicit empty array. Null is rejected. Unknown config
fields, other profiles and other credential references are rejected before
token lookup. The test starts only after independent Slack
identity and channel checks match the plan. Child processes receive a fresh
HOME/config/cache/temp environment and only the approved credential variable.
They do not inherit account overrides, providers, proxies or debug settings.
Watch-before-add uses a loopback relay restricted to its exact actor, target,
and auth/reaction/conversation methods. The fault controls below use a separate
relay with a fixed method and parameter allowlist. Both forward only to canonical
Slack, bound request/response bodies and timeouts, and never log credentials.
Neither relay provides a general proxy.

Build the fixed binary into the private root, then run `mise run test:live`.
The task retains its normal build dependency, but the test uses only the
explicit `SLICK_LIVE_BINARY` artifact; the caller must build it from the intended
source and record its identity. A missing confirmation skips live cases. An
invalid plan with confirmation fails. A skipped case remains unverified.

## Coverage and limits

The compact lifecycle uses owned fixtures rather than a flag cross-product:

*   Create a parent with a small Markdown table. Independent SDK readback must
    contain native table and rich-text cell types before the parent is edited.
*   Read the edit and separate reply independently. A dedicated pagination test
    creates its own public parent and five numbered replies, independently
    checks each, and requests one item per page. It requires at least two pages,
    forwards each cursor exactly, and requires every owned reply exactly once.
    Missing/repeated cursors, duplicate replies and wrong parent links fail.
    Returning a single page fails this pagination test; it is not a pass or skip.
*   On the public parent, start watch before adding a reaction. A restricted
    loopback relay confirms an actual empty Slack poll, then another CLI process
    adds the reaction. Both processes use the same approved actor. Independently
    read the match, check CLI reaction list and immediate watch, then remove and
    verify removal. The private-channel case checks the immediate-match path.
    An actor omitted from an incomplete user list is unknown, never absent.
*   Create a schedule 90 minutes ahead, find its exact ID through CLI and SDK
    listings, cancel it, and exhaust listings when verifying absence.
*   Check CLI channel/user lookup, unique-run search, isolated cold/warm/refresh
    metadata caches and resource clearing. Search polls within 45 seconds;
    indexing/budget limits are reported as unverified, not a pass.
*   Run public health current/history/API/check reads and JSON/compact/human
    history. Delete exact messages through CLI `message delete --force`, replies
    first, after all dependent reads. Independently verify each deletion.
*   Run read-only auth status and independent identity checks last.

Run only the two follow-up scenarios, without auth commands or other workflows:

```sh
go test -tags=live ./tests/live -run '^TestLive(ThreadPagination|FileUpload)$' \
  -count=1 -timeout 12m -v
```

These tests use the same explicit isolated plan, credential and cleanup gates.
The thread pagination fixture owns six messages. File uploads are tracked by
exact ID; both scenarios clean their own resources even when an assertion fails.

For the separately approved user/channel/search/schedule/cache pagination run:

```sh
go test -tags=live ./tests/live -run '^TestLiveAdditionalPagination$' \
  -count=1 -timeout 12m -v
```

User and conversation listings use one-item pages and must match independent
complete SDK inventories. Search creates three owned messages and waits up to
45 seconds for indexing before following result pages. Schedule pagination owns
three schedules. Cache priming first verifies a one-page truncation boundary,
then drains one-item pages completely and checks active-user/non-archived-channel
inventories, warm hits and unchanged cache bytes. Each subtest can run separately;
cache-only retests do not repeat message or schedule creation.

Uploads run only if the authenticated scope header includes both `files:read`
and `files:write`. The runner tracks exact file IDs, reads their metadata, and
uses the native SDK to delete and verify them; slick has no file-delete command.
Without both scopes, no upload is created. File downloads/content verification
and share-message behavior remain separate evidence.

With two explicitly approved peer IDs, the suite opens/resumes one DM and one
group DM through the independent SDK, verifies exact membership, then exercises
CLI single, comma-separated and repeated `--user` inputs. Only those verified
conversation IDs enter the allowlist. Conversation type comes from `is_im` or
`is_mpim`, with exact membership checked; an ID prefix does not prove its type.
Every message has its own receipt and
cleanup. Each admitted DM/group DM also gets one scheduled-message
list/cancel round trip. Existing conversations are never closed or archived.
Missing peer IDs skip these workflows. Actual scheduled delivery, permission
failures, OAuth browser flows, keychains, native terminals, and other OS runtimes
are not established by this run. Local validation and fault injection belong
in the synthetic suites, not repeated live mutations.

## Cleanup and evidence

Exact receipts enter memory and a private `receipts-<run_id>.jsonl` ledger
immediately after each command, including nonzero exits, before content
assertions. Replies are separate resources. Cleanup verifies run/actor ownership,
attempts all known receipts in reverse order, and independently verifies absence.
It does not rely on parent deletion removing replies. Independently verified
absent receipts are retained in the private ledger and skipped by fallback
cleanup, including replies whose parent was subsequently deleted. A lost
creation receipt triggers bounded independent reconciliation by exact
approved channel, actor, unique operation marker and run window. Exactly one
owned match enters the ledger and cleanup; the original functional failure is
still reported. Multiple matches, permission/budget errors or unknown upload
IDs remain unresolved, fail cleanup and retain the private ledger.
Do not replay a creation after an ambiguous result or delete by a broad prefix.

Normal work is bounded to eight minutes, with an independent two-minute cleanup
budget. Ordinary reads have a 10-second CLI request budget and a 20-second outer
deadline. Deliberate one-item cache priming allows 75 seconds plus a 10-second
outer margin to include normal rate-limit pacing. Mutations retain their
20-second outer deadline. Children have a two-second pipe-drain limit. Private stdout capture is capped at 8 MiB.
Independent SDK requests also have timeouts
and no retry wrapper. The package task allows twelve minutes. These deadlines
cannot guarantee cleanup after process crashes or forced package termination.

The runner never retries commands itself. The CLI retains its own documented
SDK rate-limit policy. A failed/uncertain mutation requires inspection before
another attempt. Raw authenticated stdout/stderr and remote error bodies are not
printed in test failures. Each CLI execution logs only a fixed command name,
numeric exit and allowlisted error type/code, without arguments or content.
Keep plan, ledger, raw workspace data and target IDs
private. Report tested revision/binary, named case outcomes, explicit skips,
cleanup status, and remaining uncertainty. Fixture tests prove local behavior;
live evidence proves only the exercised Slack paths.

## Safety tests

`go test ./tests/live` runs ordinary synthetic plan/runner tests without a live
build tag. They check confirmation/config/identity/target gates, environment
isolation, exclusion rules, receipt ordering, ambiguity, cleanup independence
and continuation, incomplete reaction users, strict plan keys, durable reply
absence, paginated CLI scheduled inspection, diagnostic redaction, and bounded
child cancellation. No credentials or live Slack
are needed. The live-tag suite can also be compiled while selecting only these
named synthetic tests.

## Live-backed failure controls

With explicit approval for a temporary owned message and cleanup, run:

```sh
go test -tags=live ./tests/live -run '^TestLiveFaultControls$' \
  -count=1 -timeout 12m -v
```

The test distinguishes Slack-generated errors from locally injected faults:

*   If the approved token lacks `users.profile:read`, request the authenticated
    user's profile through the native SDK and require Slack's `missing_scope`.
    Relay the same real method response into a CLI lookup to check its structured
    error mapping. This endpoint substitution is reported explicitly; it does
    not prove that the dedicated lookup method itself lacks permission.
*   Inject one HTTP 429 before forwarding a real read, then verify recovery.
    Separately exhaust the bounded retry policy and verify `retry_after_seconds`.
    Do not flood Slack to produce a rate limit.
*   Inject malformed JSON, drop a real read response, and delay a request until
    the CLI deadline. Check exact attempts and recovery on separate reads.
    A synthetic diagnostic canary and the credential must remain absent from
    captured stdout/stderr, including debug output.
*   Seed a complete metadata cache, forward one real refresh page, then inject
    HTTP 503. The old cache must remain byte-identical and usable as a warm hit;
    saved configuration must remain unchanged.
*   Forward one owned message creation, retain its exact commit receipt, and
    discard the acknowledgement. Require one mutation attempt, an uncertain
    receipt and exactly one independently recovered message. The relay blocks
    any second mutation forward. Normal independent cleanup deletes the exact
    message; a final direct CLI read must receive Slack's missing-message error.

These controls do not establish Slack-origin 429/5xx/malformed outages, hard
process termination or power-loss recovery. They exercise the real CLI against
real Slack with controlled failures at the local boundary. Expected nonzero
exits are passing assertions when the envelope, classification and counts match.

The relay guards and diagnostic capture also run against loopback-only fixtures
with synthetic credentials in `go test ./tests/live`; no live account is used.
