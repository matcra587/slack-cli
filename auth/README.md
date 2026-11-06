# Saved slick authentication for Go programs

The `auth` package resolves the workspace credentials saved by `slick auth login`.
It does not start a login flow or read credentials when you import it.

Set `Options.Workspace` to select a saved profile explicitly. Otherwise,
`auth.Resolve(ctx, auth.Options{})` uses `SLACK_CLI_DEFAULT_WORKSPACE` when set,
then the saved default workspace. Each call reads the current
configuration and resolves the current credential, so switching profiles or
replacing a credential takes effect on the next call.

```go
session, err := auth.Resolve(ctx, auth.Options{Workspace: "work"})
if err != nil {
    return err
}
client := slack.New(session.Token())
```

Here, `slack` is `github.com/slack-go/slack`. The returned client has the token's
existing permissions; resolving credentials does not grant additional scopes.

`Options.ConfigPath` overrides configuration discovery. Otherwise, resolution
uses `SLICK_CONFIG`, then `SLACK_CLI_CONFIG`, then `$XDG_CONFIG_HOME/slick/config.toml`
or `~/.config/slick/config.toml`. Workspace token environment overrides, keychain
references, and explicit `op://` references follow the same rules as the CLI.
An `op://` reference can invoke 1Password; the package does not request it for
keychain or environment credentials.

Expiring keychain OAuth credentials refresh through Slack before being returned.
A failed refresh or credential-store update returns an error and preserves the
prior stored value. Other profiles and workspace settings are unchanged.

Session formatting and JSON include workspace identity only. `Session.Token()`
is the explicit secret accessor: pass its result to a client, and do not log or
persist it. Normal error formatting hides provider diagnostics that may contain
credentials. Use `errors.Is(err, auth.ErrNotAuthenticated)` to classify credential
resolution failures. Underlying causes remain available through `errors.Is` and
`errors.As`; treat provider causes as sensitive diagnostic data.

Use a context with a deadline for secret-manager reads and OAuth refresh. A
synchronous OS keychain call cannot be interrupted through that context. Tests
and applications can inject `CredentialStore`, `SecretReader`, and `HTTPClient`
without opening the real keychain or contacting Slack.
