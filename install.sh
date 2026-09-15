#!/bin/sh
# mds installer
#
#   curl -fsSL https://raw.githubusercontent.com/EmreErdogan/mds/main/install.sh | sh
#
# Options (environment variables):
#   MDS_VERSION      release tag to install, e.g. v0.1.0 (default: latest)
#   MDS_INSTALL_DIR  directory to install into (default: ~/.local/bin)
set -eu

REPO="EmreErdogan/mds"
INSTALL_DIR="${MDS_INSTALL_DIR:-$HOME/.local/bin}"
VERSION="${MDS_VERSION:-}"

say() { printf '%s\n' "$*" >&2; }
die() { say "error: $*"; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || die "$1 is required"; }
need curl
need uname

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$os" in
  linux|darwin) ;;
  mingw*|msys*|cygwin*) os=windows ;;
  *) die "unsupported OS: $os" ;;
esac
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) die "unsupported architecture: $arch" ;;
esac

asset="mds_${os}_${arch}"
[ "$os" = windows ] && asset="${asset}.exe"

if [ -z "$VERSION" ]; then
  VERSION=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
    | grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
  [ -n "$VERSION" ] || die "could not determine latest version"
fi

base="https://github.com/$REPO/releases/download/$VERSION"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

say "downloading mds $VERSION ($asset)..."
curl -fsSL "$base/$asset" -o "$tmp/mds" || die "download failed: $base/$asset"

if curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt" 2>/dev/null; then
  want=$(grep " \*\{0,1\}$asset\$" "$tmp/checksums.txt" | awk '{print $1}')
  if [ -n "$want" ]; then
    if command -v sha256sum >/dev/null 2>&1; then
      got=$(sha256sum "$tmp/mds" | awk '{print $1}')
    else
      got=$(shasum -a 256 "$tmp/mds" | awk '{print $1}')
    fi
    [ "$want" = "$got" ] || die "checksum mismatch"
  fi
fi

mkdir -p "$INSTALL_DIR"
chmod 0755 "$tmp/mds"
target="$INSTALL_DIR/mds"
[ "$os" = windows ] && target="$target.exe"
mv "$tmp/mds" "$target"
say "installed $target"

# Add the install directory to PATH if it is not already there.
case ":$PATH:" in
  *":$INSTALL_DIR:"*) exit 0 ;;
esac

shell_name=$(basename "${SHELL:-sh}")
line="export PATH=\"$INSTALL_DIR:\$PATH\""
case "$shell_name" in
  zsh)  rc="$HOME/.zshrc" ;;
  bash) rc="$HOME/.bashrc"; [ "$os" = darwin ] && rc="$HOME/.bash_profile" ;;
  fish) rc="$HOME/.config/fish/config.fish"; line="fish_add_path $INSTALL_DIR" ;;
  *)    rc="$HOME/.profile" ;;
esac

mkdir -p "$(dirname "$rc")"
if ! grep -qsF "$INSTALL_DIR" "$rc"; then
  printf '\n# added by mds installer\n%s\n' "$line" >> "$rc"
  say "added $INSTALL_DIR to PATH in $rc"
fi
say "restart your shell or run: $line"
