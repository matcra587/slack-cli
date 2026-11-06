---
description: "Install, build, and upgrade slick on macOS, Linux, and Windows."
---

# Installation

`slick` ships as a single static Go binary. Pick the tab for your platform in
[Quick start](#quick-start), or jump straight to a method:

*   [One-line install](#one-line-install) — `curl | sh` on Linux & Apple Silicon macOS.
*   [Homebrew](#homebrew) — recommended on macOS (Apple Silicon) and Linux.
*   [Pre-built binaries](#pre-built-binaries) — checksummed, signed tarballs.
*   [`go install`](#go-install) — any Go platform supported by slick's dependencies.
*   [From source](#from-source) — build with the pinned toolchain.

## Platform support

Pre-built archives and the Homebrew formula cover Linux (amd64/arm64) and
Apple Silicon macOS. Intel macOS installs through `go install` or a source
build.

| Platform | Homebrew | Pre-built tarball | `go install` | From source |
|----------|:--------:|:-----------------:|:------------:|:-----------:|
| Linux · amd64 | ✓ | ✓ | ✓ | ✓ |
| Linux · arm64 | ✓ | ✓ | ✓ | ✓ |
| macOS · Apple Silicon (arm64) | ✓ | ✓ | ✓ | ✓ |
| macOS · Intel (amd64) | — | — | ✓ | ✓ |
| Windows · amd64/arm64 | — | — | See note | Build verified |

!!! note "Windows source builds"
    The current source cross-compiles for Windows on amd64 and arm64. The pinned
    `gechr/clog` dependency has a non-Unix terminal-resize fallback, so the former
    `syscall.SIGWINCH` compilation blocker no longer applies.

    Native Windows runtime behavior has not yet been verified. The release
    pipeline does not publish Windows archives, and the POSIX installer and
    Homebrew instructions below target Linux and macOS.

    The latest tagged release, `v0.5.9`, still uses the older dependency.
    `go install …@latest` selects that release; it does not include this fix.
    Build an updated source checkout instead. WSL remains an alternative for
    using the published Linux binaries.

!!! note "No Intel-macOS binary"
    The release pipeline builds `linux/amd64`, `linux/arm64`, and
    `darwin/arm64` only. On Intel macOS, use [`go install`](#go-install)
    (which produces a working binary from source) or [build from
    source](#from-source). `brew install --HEAD` also compiles from source
    on any Homebrew platform.

## Quick start

=== "Linux"

    **Recommended** — Homebrew on amd64/arm64. You get `brew upgrade slick`
    for managed updates:

    ```sh
    brew install matcra587/tap/slick
    ```

    No Homebrew? The one-line installer drops the binary into
    `$HOME/.local/bin`. Re-run it to upgrade in place:

    ```sh
    curl -fsSL https://matcra587.github.io/slack-cli/install.sh | sh
    ```

=== "macOS"

    **Recommended** — Homebrew on Apple Silicon. You get `brew upgrade slick`
    for managed updates:

    ```sh
    brew install matcra587/tap/slick
    ```

    No Homebrew? The one-line installer drops the binary into
    `$HOME/.local/bin`. Re-run it to upgrade in place:

    ```sh
    curl -fsSL https://matcra587.github.io/slack-cli/install.sh | sh
    ```

    !!! note "Intel macs"
        No `darwin/amd64` bottle or tarball is published. Use [`go
        install`](#go-install), a [source build](#from-source), or `brew
        install --HEAD matcra587/tap/slick`.

=== "Windows"

    Build an updated source checkout with the toolchain selected by `go.mod`:

    ```powershell
    go build -o slick.exe ./cmd/slick
    .\slick.exe version
    ```

    Cross-compilation has been verified for Windows amd64 and arm64; native
    Windows runtime testing is still outstanding. No Windows release archive
    is published, and `go install …@latest` still selects the older tagged
    release. See [Platform support](#platform-support) for details.

    To use the published Linux binaries on Windows, install
    [WSL](https://learn.microsoft.com/windows/wsl/) and follow the Linux
    instructions.

## Homebrew

Recommended on macOS (Apple Silicon) and Linux (amd64/arm64).

```sh
brew install matcra587/tap/slick
```

The formula lives in [`matcra587/homebrew-tap`](https://github.com/matcra587/homebrew-tap)
and tracks the latest GitHub release. Upgrades come through `brew upgrade
slick`.

!!! tip "Source build via Homebrew"
    `brew install --HEAD matcra587/tap/slick` compiles the latest `main`
    with the Go toolchain and embeds version metadata. This is the one
    Homebrew path that works on Intel macOS.

## go install

```sh
go install github.com/matcra587/slack-cli/cmd/slick@latest
```

Installs the latest tagged release into `$(go env GOBIN)` (or
`$(go env GOPATH)/bin`).

!!! warning "No version metadata"
    This path does **not** embed version metadata — `slick version` will
    report `dev` / `unknown` because the version package reads compile-time
    `-X` ldflag overrides and `go install` doesn't supply them. If you need
    accurate `slick version` output, use Homebrew or the pre-built binaries
    instead, or build from a checkout via `mise run install` (which does set
    the ldflags).

## One-line install

For Linux (amd64/arm64) and Apple Silicon macOS, a POSIX-sh installer hosted
on this site does the download, verify, and install in one step:

```sh
curl -fsSL https://matcra587.github.io/slack-cli/install.sh | sh
```

It auto-detects OS and architecture, resolves the latest release, verifies the
SHA-256 against the published `checksums.txt`, optionally verifies the cosign
signature when `cosign` is on `PATH`, and installs the binary to
`$HOME/.local/bin`. The script prints a PATH-fix hint if that directory isn't
already on your shell's `PATH`.

!!! tip "Prefer Homebrew for managed upgrades"
    The one-line installer has no upgrade command of its own — re-run it to
    pick up the latest release. If you want `brew upgrade slick` style
    managed updates, install with [Homebrew](#homebrew) instead.

??? info "Environment overrides"

    | Variable | Effect |
    |---|---|
    | `SLICK_VERSION` | Pin a specific release tag (e.g. `v0.5.9`). Default: latest. |
    | `SLICK_INSTALL_DIR` | Install directory. Default: `$HOME/.local/bin`. Use `/usr/local/bin` with `curl … \| sudo sh` for a system-wide install. |
    | `SLICK_NO_VERIFY` | Set to `1` to skip cosign verification when cosign is unavailable. SHA-256 still runs. |

    Examples:

    ```sh
    # Pin a version
    SLICK_VERSION=v0.5.9 curl -fsSL https://matcra587.github.io/slack-cli/install.sh | sh

    # System-wide install
    curl -fsSL https://matcra587.github.io/slack-cli/install.sh | SLICK_INSTALL_DIR=/usr/local/bin sudo -E sh
    ```

!!! warning "What `curl | sh` actually runs"
    The installer's source lives at [`docs/install.sh`](https://github.com/matcra587/slack-cli/blob/main/docs/install.sh) — about 130 lines of POSIX sh.
    Inspect it before piping into `sh` if you don't already trust the source:

    ```sh
    curl -fsSL https://matcra587.github.io/slack-cli/install.sh | less
    ```

## Pre-built binaries

GitHub Releases ship checksummed tarballs for Linux on amd64/arm64 and macOS
on arm64 (Apple Silicon). The [one-line installer](#one-line-install) wraps
the download/verify/install dance, but if you'd rather do it by hand —
for a scripted pipeline, a stricter audit trail, or because you don't want
to pipe `curl` into `sh` — the manual recipe is below.

??? note "Manual download + SHA-256 verify"

    === "bash · zsh"

        ```bash
        VERSION=$(curl -fsSLI -o /dev/null -w '%{url_effective}' \
            https://github.com/matcra587/slack-cli/releases/latest | sed 's#.*/tag/##')
        OS=$(uname -s | tr '[:upper:]' '[:lower:]')
        ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')

        curl -fsSLO "https://github.com/matcra587/slack-cli/releases/download/${VERSION}/slick_${VERSION#v}_${OS}_${ARCH}.tar.gz"
        curl -fsSLO "https://github.com/matcra587/slack-cli/releases/download/${VERSION}/checksums.txt"
        grep "_${OS}_${ARCH}.tar.gz$" checksums.txt | sha256sum -c

        tar xzf "slick_${VERSION#v}_${OS}_${ARCH}.tar.gz"
        sudo install slick /usr/local/bin/
        ```

    === "fish"

        ```fish
        set VERSION (curl -fsSLI -o /dev/null -w '%{url_effective}' \
            https://github.com/matcra587/slack-cli/releases/latest | string replace -r '.*/tag/' '')
        set OS (uname -s | string lower)
        set ARCH (uname -m | string replace x86_64 amd64 | string replace aarch64 arm64)
        set NUM (string replace -r '^v' '' $VERSION)
        set TARBALL (printf 'slick_%s_%s_%s.tar.gz' $NUM $OS $ARCH)

        curl -fsSLO "https://github.com/matcra587/slack-cli/releases/download/$VERSION/$TARBALL"
        curl -fsSLO "https://github.com/matcra587/slack-cli/releases/download/$VERSION/checksums.txt"
        grep (printf '_%s_%s.tar.gz$' $OS $ARCH) checksums.txt | sha256sum -c

        tar xzf $TARBALL
        sudo install slick /usr/local/bin/
        ```

??? note "Cosign signature verification"

    `checksums.txt` is signed with [cosign](https://github.com/sigstore/cosign)
    keyless signing — a `checksums.txt.sigstore.json` bundle ships next to
    the release artifacts. The [one-line installer](#one-line-install)
    verifies it automatically when `cosign` is on `PATH`. To verify by hand:

    === "bash · zsh"

        ```bash
        curl -fsSLO "https://github.com/matcra587/slack-cli/releases/download/${VERSION}/checksums.txt.sigstore.json"
        cosign verify-blob \
            --bundle checksums.txt.sigstore.json \
            --certificate-identity-regexp "https://github.com/matcra587/slack-cli/" \
            --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
            checksums.txt
        ```

    === "fish"

        ```fish
        curl -fsSLO "https://github.com/matcra587/slack-cli/releases/download/$VERSION/checksums.txt.sigstore.json"
        cosign verify-blob \
            --bundle checksums.txt.sigstore.json \
            --certificate-identity-regexp "https://github.com/matcra587/slack-cli/" \
            --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
            checksums.txt
        ```

    The archives themselves are not individually signed; the checksum bundle
    covers them transitively.

## From source

```sh
git clone https://github.com/matcra587/slack-cli
cd slack-cli
mise install                  # provision pinned toolchain
mise run install              # go install ./cmd/slick with version metadata
slick version
```

`mise run build` writes a release-style binary to
`./dist/slick-<goos>-<goarch>` if you need to test the artifact shape used
in release.

## Verifying the install

```sh
slick version
```

Should print the installed version, commit, branch, build time, and
`built by` (one of `goreleaser`, `homebrew`, or the local user for a dev
build).

## Upgrading

| Install path | Upgrade |
|---|---|
| Homebrew | `brew upgrade slick` |
| go install | `go install github.com/matcra587/slack-cli/cmd/slick@latest` |
| Pre-built | Re-download from [releases](https://github.com/matcra587/slack-cli/releases) |
| From source | `git pull && mise run install` |

!!! info "No self-updater"
    slick does not currently ship a `slick update` self-updater; upgrade
    through whichever install path you chose.

## See also

*   [README](https://github.com/matcra587/slack-cli#readme) — quick start.
*   [`version`](version.md) — what the installed binary reports.
*   [`auth`](auth.md#auth-login) — once installed, authenticate a workspace.
