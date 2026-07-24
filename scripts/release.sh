#!/usr/bin/env bash
# Cut a GitHub release: builds an unsigned universal macOS binary, tars it
# (tar preserves the exec bit; a bare binary attachment wouldn't survive
# download), and publishes it as a GitHub release asset. The asset name is a
# contract with scripts/install-chatgpt.sh, which greps the latest release
# for it — keep them in sync if this ever changes.
#
#   ./scripts/release.sh <version>   (e.g. 0.2.0)
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$REPO_ROOT/scripts/lib.sh"

VERSION="${1:?usage: scripts/release.sh <version>  (e.g. 0.2.0)}"

if [ -n "$(git -C "$REPO_ROOT" status --porcelain)" ]; then
  echo "error: working tree not clean — commit or stash first" >&2
  exit 1
fi
gh auth status

RELEASE_DIR="$REPO_ROOT/dist/release"
mkdir -p "$RELEASE_DIR"

# Universal (arm64 + x86_64) binary, version-stamped for `dharma --version`.
build_dharma darwin arm64 "$RELEASE_DIR/dharma-arm64" "$VERSION"
build_dharma darwin amd64 "$RELEASE_DIR/dharma-amd64" "$VERSION"
lipo -create -output "$RELEASE_DIR/dharma" \
  "$RELEASE_DIR/dharma-arm64" "$RELEASE_DIR/dharma-amd64"
rm "$RELEASE_DIR/dharma-arm64" "$RELEASE_DIR/dharma-amd64"

ASSET="$REPO_ROOT/dist/dharma-macos-universal.tar.gz"
tar -czf "$ASSET" -C "$RELEASE_DIR" dharma

echo "creating GitHub release v$VERSION..."
gh release create "v$VERSION" "$ASSET" --title "dharma v$VERSION" --generate-notes

echo "done: v$VERSION published with $(basename "$ASSET")"
