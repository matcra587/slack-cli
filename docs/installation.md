# Installation

`slick` ships as a single static Go binary. There are three supported install
paths plus a local-development workflow.

## Homebrew (recommended)

```sh
brew install matcra587/tap/slick
```

The formula lives in [`matcra587/homebrew-tap`](https://github.com/matcra587/homebrew-tap)
and tracks the latest GitHub release. Upgrades come through `brew upgrade
slick`. Older install commands using the previous formula name
(`matcra587/tap/slack-cli`) continue to resolve via the tap's
`formula_renames.json`.

## go install

```sh
go install github.com/matcra587/slack-cli/cmd/slick@latest
```

Installs the latest tagged release into `$(go env GOBIN)` (or
`$(go env GOPATH)/bin`). This path does **not** embed version metadata —
`slick version` will report `dev` / `unknown` because the version package
reads compile-time `-X` ldflag overrides and `go install` doesn't supply
them. If you need accurate `slick version` output, use Homebrew or the
pre-built binaries instead, or build from a checkout via `mise run
install` (which does set the ldflags).

## Pre-built binaries

GitHub Releases ship checksummed tarballs for Linux on amd64/arm64 and
macOS on arm64 (Apple Silicon). Intel macOS (`darwin_amd64`) is not built —
the Homebrew formula and pre-built archives both omit it; use `go install`
on those machines.

```sh
VERSION=$(curl -fsSL https://api.github.com/repos/matcra587/slack-cli/releases/latest | jq -r .tag_name)
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')

curl -fsSLO "https://github.com/matcra587/slack-cli/releases/download/${VERSION}/slack-cli_${VERSION#v}_${OS}_${ARCH}.tar.gz"
curl -fsSLO "https://github.com/matcra587/slack-cli/releases/download/${VERSION}/checksums.txt"
grep "_${OS}_${ARCH}.tar.gz$" checksums.txt | sha256sum -c

tar xzf "slack-cli_${VERSION#v}_${OS}_${ARCH}.tar.gz"
sudo install slick /usr/local/bin/
```

The `checksums.txt` file itself is signed with
[cosign](https://github.com/sigstore/cosign) keyless signing — a
`checksums.txt.sigstore.json` bundle is uploaded next to the release
artifacts. To verify the chain end to end:

```sh
curl -fsSLO "https://github.com/matcra587/slack-cli/releases/download/${VERSION}/checksums.txt.sigstore.json"
cosign verify-blob \
    --bundle checksums.txt.sigstore.json \
    --certificate-identity-regexp "https://github.com/matcra587/slack-cli/" \
    --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
    checksums.txt
```

The archives themselves are not individually signed; the checksum bundle
covers them transitively.

## From source (development)

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

slick does not currently ship a `slick update` self-updater; upgrade
through whichever install path you chose.

## See also

*   [README](https://github.com/matcra587/slack-cli#readme) — quick start.
*   [`version`](version.md) — what the installed binary reports.
*   [`auth`](auth.md#auth-login) — once installed, authenticate a workspace.
