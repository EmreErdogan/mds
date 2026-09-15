#!/bin/sh
# Cross-compile release binaries into dist/ and write checksums.txt.
# Usage: scripts/build.sh [version]
set -eu
cd "$(dirname "$0")/.."

VERSION="${1:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
rm -rf dist && mkdir -p dist

for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
  os=${target%/*}; arch=${target#*/}
  out="dist/mds_${os}_${arch}"
  [ "$os" = windows ] && out="$out.exe"
  echo "building $out"
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath \
    -ldflags "-s -w -X main.version=$VERSION" -o "$out" .
done

cd dist
if command -v sha256sum >/dev/null 2>&1; then
  sha256sum mds_* > checksums.txt
else
  shasum -a 256 mds_* > checksums.txt
fi
echo "done: $(ls | tr '\n' ' ')"
