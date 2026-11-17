# Slack API catalog

This package records request metadata from Slack's official method pages and the public client methods in the pinned Go SDK. It supports API discovery and conservative execution decisions. It is not a mock Slack service or proof that an endpoint works with a particular account.

The snapshot contains 357 official method pages and 10 additional endpoint names found in slack-go/slack v0.36.0. The SDK inventory lists all 514 exported `*Client` methods, including aliases, configuration helpers, pagination constructors, and non-Web-API operations. Those counts describe different surfaces and should not be compared as a coverage percentage.

Tests compare the inventory with the SDK version in `go.mod` and the compiled SDK method set. They also check that the public `api.Client` preserves every native method's arguments and results. An SDK upgrade therefore requires reviewing and refreshing the inventory.

## Sources and limits

*   The [official index](https://docs.slack.dev/llms.txt) links to the [Markdown sitemap](https://docs.slack.dev/llms-sitemap.md).
*   Each method links to its canonical source and records a SHA-256 hash of the downloaded Markdown. The snapshot also hashes the source index and SDK sources.
*   The parser reads method names, HTTP verbs, descriptions, rate tiers, token-type scope lists, argument types, required arguments, and explicit defaults. It reads the documented request-content types and pagination parameters from the page body.
*   A scope list can contain alternatives that depend on the target. It does not mean every scope must be granted.
*   `ReadOnly` comes from a reviewed allowlist in `policy.go`. GET requests are not automatically safe reads. Unreviewed and SDK-only operations remain mutations.
*   OAuth, multipart-only operations, binary downloads, and known retired endpoints carry explicit unsupported reasons for the generic JSON caller. The typed SDK can expose additional operations with different requirements.
*   `SensitiveResponse` identifies credential-bearing or credential-management operations. Callers must protect these results and must not include them in general logs or caches.
*   The SDK call graph follows static function calls, endpoint literals, and constants. Explicit mappings cover deferred pagination and legacy workspace-admin URL builders. An empty endpoint list does not prove that a method performs no network requests.
*   The snapshot records metadata, not a claim that every paragraph, conditional permission rule, limit, or response variant was reviewed. Tests use selected documented contracts and isolated fixtures; live behavior requires separate verification.

## Refresh the snapshot

Run from the repository root with the pinned toolchain:

```sh
mise exec go -- go run ./internal/cmd/slackcatalog \
  -sdk-dir "$(mise exec go -- go list -m -f '{{.Dir}}' github.com/slack-go/slack)" \
  -sdk-version v0.36.0
```

Downloads use six workers, a 20-second request timeout, a five-minute overall timeout, an HTTPS origin restriction, and a source-size limit. A failed download or parse stops generation before replacing output files.

Both output files are staged before publication. If a later file replacement fails, the generator restores earlier files and their permissions. If restoration also fails, it keeps the prior bytes in a temporary backup and reports its path. Two filesystem paths cannot change atomically: process termination or power loss between replacements can still leave a mixed generation. Run one generator at a time for a given output pair.

For repeatable offline generation, pass `-index` with a saved sitemap, `-docs-dir` with the corresponding method Markdown files, and `-date` with their retrieval date. `-sdk-dir` and `-sdk-version` must refer to the same SDK version. Review the resulting metadata diff and execution policy before committing it.

```sh
mise exec go -- go test -race -shuffle=on \
  ./internal/slackcatalog ./internal/cmd/slackcatalog
```
