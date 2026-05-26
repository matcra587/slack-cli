#!/bin/sh
# slick installer — POSIX sh
#
# Usage:
#   curl -fsSL https://matcra587.github.io/slack-cli/install.sh | sh
#
# Environment overrides:
#   SLICK_VERSION       Pin a release tag (e.g. v0.5.9). Default: latest.
#   SLICK_INSTALL_DIR   Install directory.                Default: $HOME/.local/bin.
#   SLICK_NO_VERIFY     Set to 1 to skip cosign verification (sha256 still runs).
#
# Source: https://github.com/matcra587/slack-cli

set -eu

REPO="matcra587/slack-cli"
BIN="slick"
INSTALL_DIR="${SLICK_INSTALL_DIR:-$HOME/.local/bin}"

# --- output helpers -------------------------------------------------------

if [ -t 1 ]; then
    BLD="$(printf '\033[1m')"; RED="$(printf '\033[31m')"
    YLW="$(printf '\033[33m')"; GRN="$(printf '\033[32m')"
    DIM="$(printf '\033[2m')"; RST="$(printf '\033[0m')"
else
    BLD=""; RED=""; YLW=""; GRN=""; DIM=""; RST=""
fi

say()  { printf '%s\n' "$*"; }
info() { printf '%s::%s %s\n'   "$DIM" "$RST" "$*"; }
ok()   { printf '%s ok %s %s\n' "$GRN" "$RST" "$*"; }
warn() { printf '%swarn%s %s\n' "$YLW" "$RST" "$*" >&2; }
die()  { printf '%serr %s %s\n' "$RED" "$RST" "$*" >&2; exit 1; }

# --- pre-flight -----------------------------------------------------------

need() { command -v "$1" >/dev/null 2>&1 || die "missing required tool: $1"; }
need curl
need tar
need uname
need grep

# Pick the right sha256 implementation; GNU on Linux, BSD on macOS.
if command -v sha256sum >/dev/null 2>&1; then
    SHA_CMD="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
    SHA_CMD="shasum -a 256"
else
    die "missing required tool: sha256sum or shasum"
fi

# --- OS / arch detection --------------------------------------------------

case "$(uname -s)" in
    Linux)  OS=linux ;;
    Darwin) OS=darwin ;;
    *) die "unsupported OS: $(uname -s). See https://matcra587.github.io/slack-cli/installation/ for alternatives." ;;
esac

case "$(uname -m)" in
    x86_64|amd64)   ARCH=amd64 ;;
    aarch64|arm64)  ARCH=arm64 ;;
    *) die "unsupported architecture: $(uname -m). See https://matcra587.github.io/slack-cli/installation/ for alternatives." ;;
esac

# Reject combinations the release pipeline doesn't build.
if [ "$OS" = "darwin" ] && [ "$ARCH" = "amd64" ]; then
    die "Intel macOS (darwin/amd64) has no pre-built tarball. Options:
    brew install --HEAD matcra587/tap/slick
    go install github.com/matcra587/slack-cli/cmd/slick@latest
  See https://matcra587.github.io/slack-cli/installation/ for details."
fi

# --- version resolution ---------------------------------------------------

if [ -n "${SLICK_VERSION:-}" ]; then
    VERSION="$SLICK_VERSION"
    case "$VERSION" in v*) ;; *) VERSION="v$VERSION" ;; esac
else
    info "Resolving latest version from GitHub"
    # GitHub redirects /releases/latest -> /releases/tag/vX.Y.Z; capture the
    # effective URL and strip everything up to the tag.
    VERSION="$(curl -fsSLI -o /dev/null -w '%{url_effective}' \
        "https://github.com/$REPO/releases/latest" | sed 's#.*/tag/##')"
    [ -n "$VERSION" ] || die "could not resolve latest version"
fi
NUM="${VERSION#v}"

TARBALL="${BIN}_${NUM}_${OS}_${ARCH}.tar.gz"
BASE_URL="https://github.com/$REPO/releases/download/$VERSION"

info "Installing ${BLD}${BIN} ${VERSION}${RST} for ${OS}/${ARCH}"

# --- download + verify ----------------------------------------------------

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
cd "$TMP"

info "Downloading $TARBALL"
curl -fsSLO "$BASE_URL/$TARBALL" || die "failed to download $TARBALL"

info "Downloading checksums.txt"
curl -fsSLO "$BASE_URL/checksums.txt" || die "failed to download checksums.txt"

info "Verifying SHA-256"
grep " $TARBALL\$" checksums.txt | $SHA_CMD -c >/dev/null \
    || die "checksum verification failed for $TARBALL"
ok "sha256 verified"

# Optional cosign verification — best-effort. Skipped silently when
# SLICK_NO_VERIFY=1; warned about when cosign is missing.
if [ "${SLICK_NO_VERIFY:-0}" != "1" ]; then
    if command -v cosign >/dev/null 2>&1; then
        info "Verifying cosign signature"
        if curl -fsSLO "$BASE_URL/checksums.txt.sigstore.json" 2>/dev/null; then
            cosign verify-blob \
                --bundle checksums.txt.sigstore.json \
                --certificate-identity-regexp "https://github.com/$REPO/" \
                --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
                checksums.txt >/dev/null 2>&1 \
                || die "cosign verification failed"
            ok "cosign verified"
        else
            warn "no sigstore bundle published for $VERSION; skipping cosign"
        fi
    else
        warn "cosign not installed; skipping signature verification (set SLICK_NO_VERIFY=1 to silence)"
    fi
fi

# --- extract + install ----------------------------------------------------

tar xzf "$TARBALL"

mkdir -p "$INSTALL_DIR" || die "cannot create $INSTALL_DIR"
if ! install -m 0755 "$BIN" "$INSTALL_DIR/$BIN" 2>/dev/null; then
    cp "$BIN" "$INSTALL_DIR/$BIN" \
        || die "cannot write to $INSTALL_DIR — set SLICK_INSTALL_DIR or rerun with sudo"
    chmod 0755 "$INSTALL_DIR/$BIN"
fi
ok "Installed $BIN to $INSTALL_DIR/$BIN"

# --- PATH hint ------------------------------------------------------------

case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;
    *)
        warn "$INSTALL_DIR is not on your PATH"
        say "  bash / zsh:  ${DIM}export PATH=\"$INSTALL_DIR:\$PATH\"${RST}"
        say "  fish:        ${DIM}fish_add_path $INSTALL_DIR${RST}"
        ;;
esac

say
"$INSTALL_DIR/$BIN" version 2>/dev/null || true
