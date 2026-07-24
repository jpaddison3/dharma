#!/usr/bin/env bash
# Cut a GitHub release: builds an unsigned universal macOS binary, tars it
# (tar preserves the exec bit; a bare binary attachment wouldn't survive
# download), and publishes it as a GitHub release asset. The asset name is a
# contract with scripts/install-chatgpt.sh, which greps the latest release
# for it — keep them in sync if this ever changes.
#
#   ./scripts/release.sh <version>   (e.g. 0.2.0)
set -euo pipefail

REPO="jpaddison3/dharma"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$REPO_ROOT/scripts/lib.sh"

VERSION="${1:?usage: scripts/release.sh <version>  (e.g. 0.2.0)}"

# Everything below binds the release to *this* commit: colleagues' installers
# fetch whatever the latest release holds, so a tag pointing somewhere other
# than the built code is a provenance bug they can't see.
if [ -n "$(git -C "$REPO_ROOT" status --porcelain)" ]; then
  echo "error: working tree not clean — commit or stash first" >&2
  exit 1
fi
BRANCH="$(git -C "$REPO_ROOT" rev-parse --abbrev-ref HEAD)"
if [ "$BRANCH" != "main" ]; then
  echo "error: on branch '$BRANCH' — releases are cut from main" >&2
  exit 1
fi
COMMIT="$(git -C "$REPO_ROOT" rev-parse HEAD)"
if ! git -C "$REPO_ROOT" merge-base --is-ancestor "$COMMIT" "origin/main" 2>/dev/null; then
  echo "error: HEAD ($(git -C "$REPO_ROOT" rev-parse --short HEAD)) isn't on origin/main — push first" >&2
  echo "       (run 'git fetch origin' if origin/main is stale)" >&2
  exit 1
fi
# --target only decides where a *missing* tag is created; an existing v$VERSION
# keeps pointing wherever it already does, so this build would be published
# under another commit's tag and generated notes. gh creates the tag on the
# remote, so the remote is what has to be checked — and both lookups are peeled
# with ^{commit} because an annotated tag's ref names the tag object, not the
# commit.
check_tag() { # <sha or empty> <where>
  if [ -n "$1" ] && [ "$1" != "$COMMIT" ]; then
    echo "error: tag v$VERSION already exists on $2 at ${1:0:7}, not HEAD (${COMMIT:0:7})" >&2
    echo "       delete or move the tag, or pick a new version" >&2
    exit 1
  fi
}
check_tag "$(git -C "$REPO_ROOT" rev-parse -q --verify "refs/tags/v$VERSION^{commit}" || true)" "this clone"
check_tag "$(git -C "$REPO_ROOT" ls-remote --tags origin "refs/tags/v$VERSION^{}" | cut -f1)" "origin"
gh auth status

# The suite is the only gate on the artifact colleagues install, so run it here
# rather than trusting that someone ran it. ASANA_TOKEN is stripped so the gate
# is the same hermetic suite every time: with a token exported it would also
# run the live smoke test, letting an Asana-side blip block a release.
echo "running tests..."
env -u ASANA_TOKEN go -C "$REPO_ROOT" test ./...

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

# --repo and --target pin the release to this repository and this commit; gh
# would otherwise infer the repo from the caller's directory and tag the
# remote default branch's HEAD, which may not be what was just built.
echo "creating GitHub release v$VERSION on $REPO @ ${COMMIT:0:7}..."
gh release create "v$VERSION" "$ASSET" \
  --repo "$REPO" --target "$COMMIT" --title "dharma v$VERSION" --generate-notes

echo "done: v$VERSION published with $(basename "$ASSET")"
