---
description: "Reuse slick’s saved authentication and Slack client from Go programs."
---

# Use slick from Go

Authenticate with `slick auth login`, then construct a client in your program:

```go
ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
defer cancel()

client, err := api.NewClientWithContext(ctx, api.ClientOptions{})
if err != nil {
    return err
}

var result struct {
    OK       bool `json:"ok"`
    Messages []struct {
        Text string `json:"text"`
        TS   string `json:"ts"`
    } `json:"messages"`
}
err = client.CallWithContext(ctx, "conversations.history", map[string]any{
    "channel": "C1234567890",
    "limit":   20,
}, &result)
```

Import `context`, `time`, and `github.com/matcra587/slack-cli/api`. Set
`ClientOptions.Workspace` to select a profile. The default config path follows
`SLICK_CONFIG`, the legacy `SLACK_CLI_CONFIG`, then XDG configuration settings,
just as the CLI does. Construct a client for each operation to pick up changed
workspace settings or replaced credentials.

The client embeds `slack-go/slack.Client`, so its typed context methods are
available directly. Use `client.SDK()` when you need a `*slack.Client` for a
helper such as `api.WatchReaction`. Native SDK methods retain their SDK retry
behavior; generic `CallWithContext` does not retry. A failed generic call leaves
the destination value unchanged. Numbers in untyped responses use `json.Number`.

The default SDK transport validates acknowledgements and item objects for
`reactions.add`, `reactions.remove`, and `reactions.get`. Malformed responses
return an error without replaying the request. Supplying a native SDK
`OptionHTTPClient` overrides the default transport policy.

Use `auth.Resolve` from `github.com/matcra587/slack-cli/auth` when you only need
saved authentication. Session formatting omits credentials; `Session.Token()`
explicitly exposes the value for a caller that needs it. Keep it out of logs.
Applications own their ingestion, scheduling, and storage logic.

For isolated tests, supply an explicit synthetic `ClientOptions.AuthToken` and
a loopback `APIURL`. An explicit token bypasses all saved credential discovery.
Other endpoints require HTTPS. Generic calls reject redirects and bound the
response size to 8 MiB by default.

For `auth.test` and `auth.revoke`, the default
SDK transport requires an explicit, consistent success or error acknowledgement.
Successful revocation replies must include a boolean `revoked` field. An explicit
`false` is preserved for the caller to handle. Auth replies are bounded to 8 MiB;
missing, malformed, contradictory, truncated or oversized replies return safe
errors. Valid identity fields, scope headers and native Slack errors are retained.

Response checks follow the original API operation through custom URL prefixes
and explicitly allowed redirects. Downloads and multipart transfers keep their
raw-byte contracts, even when a filename looks like an API method.

Use `SDKOptions.APIURL` for a custom SDK endpoint so slick can identify native
multipart API calls as well as form, JSON, and GET calls. An opaque native
`slack.OptionAPIURL` override retains form, JSON, and GET pacing; multipart
pacing is retained only on canonical `/api/<method>` routes. Multipart transfers outside
those canonical routes are excluded from pacing. Redirect hops share one pacing
reservation, and the supplied HTTP client's timeout includes the pacing wait.

The default SDK logger writes safe summaries when `SDKOptions.Debug` is enabled.
HTTP failures include the numeric status and its standard description; request
payloads, response headers and bodies, retry details, and unknown SDK diagnostic
text are omitted. Message content and signed upload URLs are not debug output.
A native `slack.OptionLog` override replaces this protection, so its caller owns
the logging policy.

For file upload and file-info methods, the default SDK
transport requires an explicit success or error acknowledgement. Success
responses must include the upload URL or file objects needed for that method.
Malformed responses return a safe error before the SDK continues. Error replies
retain the SDK's structured scope fields. Native batch completion and binary
file downloads keep their existing contracts.

The default native SDK client stops redirects for POST requests, including Slack
RPC calls and multipart uploads. This prevents a redirect from replaying a
mutation or forwarding its credentials. File downloads can follow up to ten
requests in a redirect chain. Redirects to another origin (scheme, host, or
port) remove copied authentication and cookie headers; redirects with credentials
in the URL are rejected.

An explicit `SDKOptions.HTTPClient.CheckRedirect` replaces that default redirect
policy. A supplied cookie jar retains its own cookie rules. Native
`OptionHTTPClient` also replaces the default HTTP client. Callers choosing these
options own their redirect and credential policies.

For lookup methods (`conversations.info`,
`conversations.list`, `users.info`, `users.lookupByEmail`, `users.list`, and
`users.getPresence`), the default SDK transport validates acknowledgements and
required result data. Missing objects, IDs, list fields, or presence values
return a safe error. Empty lists remain valid. Native multi-user `users.info`
responses remain supported. Permission responses retain native structured scope
fields, and file downloads keep their binary contract.

For `users.profile.set`, the default SDK transport requires an explicit success
or error acknowledgement. Missing, malformed, or contradictory acknowledgements
return a safe error without replaying the update. Valid permission errors retain
the SDK's structured scope fields. This validates the acknowledgement; it does
not compare the resulting profile with the requested values.

The default SDK transport handles HTTP `429` before SDK retry parsing, including
upload and download requests. One valid, nonnegative integer `Retry-After`
value within the supported wait duration keeps the default bounded retries.
Unknown, ambiguous, invalid, or excessively long timing returns an error
without an automatic retry. These errors unwrap to
`slack.SlackErrorResponse{Err: "ratelimited"}`. Use `errors.As` with an
`interface { RetryAfterSeconds() *int }` to read optional exact integer timing;
`nil` means no trustworthy timing was available. Usable delays retain the native
`*slack.RateLimitedError` after retries are exhausted. Cancellation and deadlines
still stop retry waits.

Native SDK retry options wrap the current HTTP client. Setting `MaxRetries: 0`
does not remove slick's existing default retry wrapper. A native
`OptionHTTPClient` replaces the client and its default retry, validation, and
redirect policies; callers choosing it own those policies.

For `chat.postMessage`, `chat.update`, `chat.delete`, `chat.scheduleMessage`,
`chat.scheduledMessages.list`, and `chat.deleteScheduledMessage`, the default
SDK transport requires an explicit success or error acknowledgement. Sends
require returned channel and timestamp identifiers. Scheduling requires its
assigned ID, and scheduled lists require an explicit list with usable row IDs,
channels, and posting times. An empty list remains valid. The original response
is passed to the SDK so returned content, timestamp strings, scope diagnostics,
and pagination are preserved. Invalid responses fail without replaying a write.

For `conversations.history` and `conversations.replies`, the default SDK
transport requires an explicit success or error acknowledgement. Successful
pages must contain a messages array; an empty array remains valid. Missing,
malformed, or contradictory acknowledgements return a safe error. The original
message data and pagination continue through the SDK decoder.

OAuth exchange and saved-token refresh use a guarded HTTP client. Its default
policy refuses redirects so codes, verifiers and refresh tokens are not
forwarded. It validates acknowledgements, credential shape and expiry bounds
before storing a refresh result, and limits replies to 8 MiB. It adds no
automatic retries. A supplied client keeps its timeout, cookie jar, transport
and explicit redirect policy; its fields are not changed.
