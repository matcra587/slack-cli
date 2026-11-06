# Live verification

Live Slack evidence complements the unit and fake-server integration suites.
It does not replace fault injection, native terminal checks, or release checks.
Record each case as passed, failed, blocked, or skipped, and identify the tested
commit. A skipped case is unverified.

## Current run

The initial public-service preflight tested the binary built from `ffcaa51`
against Slack's public Status and Web API endpoints. `health current`,
`health history`, `health api-test`, and `health check` each passed in JSON,
compact, and human modes: 12 cases. Machine records parsed, command identities
matched, stderr was empty, and the Web API probes returned success.

Authenticated workspace cases have not run. The designated setup entry has
app and target details, but its bot/user token fields still contain placeholders.
OAuth scope selection is unchanged; this run will use pre-issued tokens.

## Setup and isolation

Use explicit test profiles and channels, a disposable user identity for status,
and consenting DM recipients. Use a bot profile and a user profile; search and
status need the latter. Provision a separate restricted token for real scope
failures and a disposable credential for revocation. Obtain these through the
designated secret-manager entry without logging values or putting them in argv.

Use isolated config/cache paths. Config contains references, never token values.
The current `op://` resolver expects a structured credential payload, rather
than a raw token field. Raw setup tokens can instead be injected into the
explicit test profile's supported runtime environment variable by the runner.
Do not overwrite the everyday config, cached metadata, or credentials.

Snapshot the designated user's original status, including expiration, before
running status tests. Restore and independently verify it in runner cleanup;
the existing status test itself sets and clears status. Uploaded files also
need explicit deletion through Slack's API or UI because slick has no file
delete command. Track file IDs as soon as they are returned.

The existing suite runs with `mise run test:live`. It mutates Slack. Supply
explicit `SLICK_LIVE_WORKSPACE`, `SLICK_LIVE_CHANNEL`, and `SLICK_LIVE_USER`;
do not fall back to everyday defaults during a comprehensive run. Run once
per applicable identity/channel combination and report skips separately.

## Coverage matrix

For commands with result data, verify JSON and compact independently, including
their actual payload fields. Exercise human output on representative data and
use a real terminal for styling and interactive behavior. Verify auto selection
both with and without agent detection. Local validation failures do not count
as live Slack permission/error evidence.

| Surface | Required cases | Verification |
| --- | --- | --- |
| Root, help, version, agent schema/guide, completions | Offline discovery; root JSON/compact; raw schema/Markdown exceptions; explicit help; command and flag inventory | Parse machine records, preserve guide text, verify completion results and documented format exceptions |
| Auth login/status/switch/logout | Bot and user token import; references; profile switching; invalid and revoked tokens; disposable revocation; OAuth browser success/denial/cancel/timeout separately | Check identity with Slack, preserve unrelated profiles, verify local state and remote revocation |
| Config init/path/list/get/set/unset; workspace list | Fresh isolated config, preferences, aliases, defaults, overrides, unknown keys, missing config | Inspect supported non-secret settings and ensure credential values never appear |
| Lookup channel | Public, private, IM, MPIM; ID/name/alias; filters; membership; archived/inaccessible targets; multiple pages | Compare stable IDs and access facts with independent Slack reads |
| Lookup user | List/info, aliases, filters, deleted-user fixtures, email, presence | Check expected identity/profile fields; explicitly record retired list-presence limitation |
| History list | Parents/threads, empty results, max-items/time filters, next-page traversal | Recover exact fixture timestamps across pages without dropping cursors in compact output |
| Lookup messages | User-token search, text from blocks, empty search, filters, multi-page results, bot-token rejection | Poll boundedly for indexing; unindexed fixtures remain unverified, not passed |
| Message send/edit/delete | Public/private/DM/group DM; IDs/aliases/email; repeated/comma recipients; argv/stdin/file input; Unicode, links, code, Markdown, raw blocks; attribution; dry runs; ownership and force guards | Read back exact receipts/content and verify edits/deletions; inspect blocks separately from flattened text |
| Reply | Parent timestamp, stdin/file/blocks, attribution, dry run, invalid parent | Read thread and verify parent/reply relationship |
| React add/remove/list | One/multiple emoji, repeated/comma inputs, dry run, duplicate add/missing remove | Read actual reaction state before and after each transition |
| Message scheduled list/delete; send --schedule | Channel/DM/group DM; RFC3339/duration/Unix inputs; dry run; invalid times; pagination; cancel; actual delivery | Record scheduled IDs, verify pending state, cancel exact IDs, verify delivered receipt and cleanup |
| File upload (hidden/probationary) | File/stdin, filename/title, channel/thread, Markdown/raw-block comment, dry run, permalink | Independently verify file metadata/content/sharing; delete uploaded file and verify absence |
| Status set/clear | Flags/positional input, emoji, relative/absolute/no expiration, dry run, bot rejection | Independently read back status, then restore original status and expiration |
| Cache users/channels/clear | Cold/warm/refresh, TTL, page bounds, truncation, clearing one/all, stale/missing data | Verify isolated cache contents, hit/refresh facts, and complete/truncated indicators |
| Health | Current/history/API/combined; service filters; bounded history; all output modes | Public-service preflight passed; filter/limit cases remain pending |
| Machine failures | Real missing scope, inaccessible/not-found resource, validation/auth failures; JSON/compact; early parse errors | Full stdout failure envelope, clean stderr, preserved exit code, command/workspace metadata |
| Controlled failures | HTTP 429/retry timing, 5xx, timeout, SIGINT/SIGTERM, serialization/short writes, closed pipe, ambiguous mutation receipt | Use fake-server/process tests; do not manufacture workspace load to induce rate limits |
| Native environment | Linux and macOS 13+; keychain; OAuth spinner cleanup; TTY/non-TTY; forced color; themes; hyperlinks; tables; shell completion | Execute on the actual OS/terminal; cross-compilation is build evidence only |
| Release | Config validation, three-target builds, hosted CI, signing/update/publication boundaries | Local checks/builds already passed; hosted/OIDC/publishing require separate evidence |

## Cleanup and evidence

Assign a unique run ID and use realistic review, release, or incident content.
Register cleanup immediately after each successful receipt. Store exact channel,
timestamp, scheduled-message ID, and file ID in a private run ledger. Never
delete arbitrary older runs by a shared text prefix.

Verify cleanup through paginated history/search/scheduled listings and exact
resource reads. Search indexing can lag, so search alone is insufficient proof
of deletion. Exhaust pagination; reaching an arbitrary page cap is incomplete
verification. Record residual objects and restoration failures as failures.

Keep raw workspace payloads and target identifiers in private artifacts. The
repository report should contain only case names, tested versions, outcomes,
failure summaries, cleanup status, and remaining verification gaps.
