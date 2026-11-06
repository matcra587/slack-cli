# Slack API client

The `api` package lets Go applications reuse the workspace saved by
`slick auth login`. Applications own source selection, scheduling, and storage.

```go
client, err := api.NewClientWithContext(ctx, api.ClientOptions{})
if err != nil {
    return err
}
var result struct {
    Messages []json.RawMessage `json:"messages"`
    HasMore  bool              `json:"has_more"`
}
err = client.CallWithContext(ctx, "conversations.history", map[string]any{
    "channel": channelID,
    "limit":   100,
}, &result)
```

Import `github.com/matcra587/slack-cli/api` and `encoding/json`. The example
expects a context, a channel ID, and an enclosing function that returns errors.
Handle pagination through Slack's `response_metadata.next_cursor` when needed.

Set `Workspace` to select another saved profile, or `ConfigPath` to use another
config file. Each constructor resolves credentials afresh and refreshes a saved
OAuth token when required. Existing scopes still apply; constructing a client
never grants additional permissions. Use a context deadline for credential
resolution. Operating-system keychain calls cannot always be canceled.

An explicit `AuthToken` bypasses config, environment, keychain, and secret-manager
lookup. This is useful for injected credentials and isolated fixtures. Keep the
token out of command arguments, source code, and logs. Client, options, and
session formatting expose reviewed metadata only.

The embedded `slack.Client`, also available through `client.SDK()`, provides the
typed methods supported by the pinned Slack SDK. `Methods` and `LookupMethod`
expose the method catalog without authentication or network access.

## Generic requests

`CallWithContext` selects the documented HTTP method and request encoding. It
sends the token in the Authorization header and rejects a `token` parameter.
Omitted arguments stay omitted; explicit null, false, and zero values are kept.
JSON response maps use `json.Number`, and Slack timestamp strings stay strings.
Pass a nonnil pointer to a struct, map, or `json.RawMessage`, or pass nil to
validate success without retaining the response. Decode and API failures leave
the previous destination value intact. Successful decoding replaces it.

Generic calls never retry automatically, including failed mutations and HTTP
429 responses. Native SDK methods keep their configured SDK retry policy.
Use `errors.As` with `*api.Error` for a safe error summary, with a
`slack.SlackErrorResponse` value for Slack errors and scope diagnostics, or with
`*slack.RateLimitedError` for rate limits. Unwrapped causes can contain remote
or transport diagnostics; review them before logging.

Responses are limited to 8 MiB by default. Set `MaxResponseBytes` to adjust this
limit. `HTTPClient` is copied without changing the caller's transport, cookie
jar, or timeout; a positive `Timeout` overrides the copied timeout. Redirects
are rejected to prevent credential forwarding. Custom API URLs require HTTPS,
except explicit loopback HTTP URLs for fixtures.

Generic calls reject unknown or deprecated methods, binary responses, and
methods requiring OAuth, configuration, or app-token authentication. Methods
with unsupported transport requirements also fail before sending a request.
Use a native SDK method where it supports the required operation. Neither
client construction nor the method catalog proves that the logged-in workspace
has access to a particular API method.
