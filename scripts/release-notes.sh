#!/bin/sh
# Print the CHANGELOG.md section for the given version (without the "v" prefix).
# Usage: scripts/release-notes.sh 0.1.0
set -eu
cd "$(dirname "$0")/.."
ver="${1#v}"
awk -v ver="$ver" '
  /^## / { if (found) exit; if (index($0, "[" ver "]") || index($0, " " ver " ") || $0 ~ ("\\[" ver "\\]|## " ver "$")) { found=1; next } }
  found { print }
' CHANGELOG.md
