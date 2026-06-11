#!/usr/bin/env bash
# Build the dharma Cowork plugin and install it into Claude Desktop's
# org-plugins directory (macOS). Bundles linux binaries for the Cowork VM
# plus your local dharma config (token + default workspace).
#
# Re-run after changing dharma or rotating your Asana PAT. Needs sudo to
# write under /Library/Application Support.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEST="/Library/Application Support/Claude/org-plugins/dharma"
CONFIG_SRC="${XDG_CONFIG_HOME:-$HOME/.config}/dharma/config.json"

if [ ! -f "$CONFIG_SRC" ]; then
  echo "error: $CONFIG_SRC not found — run 'dharma auth login' first" >&2
  exit 1
fi

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

# Plugin skeleton (manifest, skill, wrapper).
cp -R "$REPO_ROOT/plugin/." "$STAGE/"

# Cross-compile for the Cowork VM (Ubuntu arm64 on Apple Silicon; amd64 as
# cheap insurance for other hosts).
for arch in arm64 amd64; do
  echo "building linux/$arch..."
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" \
    go -C "$REPO_ROOT" build -trimpath -ldflags="-s -w" \
    -o "$STAGE/bin/dharma-linux-$arch" ./cmd/dharma
done
chmod 755 "$STAGE/bin/dharma"

# Bundle auth config. The wrapper sets XDG_CONFIG_HOME to <plugin>/config, so
# dharma's normal config loading finds it.
mkdir -p "$STAGE/config/dharma"
install -m 600 "$CONFIG_SRC" "$STAGE/config/dharma/config.json"

# Any change to version.json triggers a plugin resync.
printf '{"version":"%s"}\n' \
  "$(git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null || echo dev)-$(date +%Y%m%d%H%M%S)" \
  > "$STAGE/version.json"

echo "installing to $DEST (sudo)..."
sudo mkdir -p "$DEST"
sudo rsync -a --delete "$STAGE/" "$DEST/"
# The Claude app (running as you) must be able to read the plugin; keep the
# token file 0600.
sudo chown -R "$(id -u):$(id -g)" "$DEST"
sudo chmod 600 "$DEST/config/dharma/config.json"

echo "done. Next steps:"
echo "  1. Quit and relaunch Claude Desktop so it picks up the plugin."
echo "  2. Check Settings → Plugins for 'dharma'."
echo "  3. In a Cowork session, allow network access to app.asana.com when prompted."
