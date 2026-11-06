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

Use `auth.Resolve` from `github.com/matcra587/slack-cli/auth` when you only need
saved authentication. Session formatting omits credentials; `Session.Token()`
explicitly exposes the value for a caller that needs it. Keep it out of logs.
Applications own their ingestion, scheduling, and storage logic.

For isolated tests, supply an explicit synthetic `ClientOptions.AuthToken` and
a loopback `APIURL`. An explicit token bypasses all saved credential discovery.
Other endpoints require HTTPS. Generic calls reject redirects and bound the
response size to 8 MiB by default.
