#!/usr/bin/env bash
# Build the dharma desktop extension (.mcpb) for Claude Desktop / Cowork.
# Produces dist/dharma.mcpb — double-click it to install, or send it to a
# colleague. The bundle contains a universal macOS dharma binary, the Node
# MCP shim, and its node_modules; the recipient needs nothing but Claude
# Desktop and an Asana PAT (entered in the extension's settings form).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MCPB_DIR="$REPO_ROOT/mcpb"
mkdir -p "$MCPB_DIR/bin" "$REPO_ROOT/dist"

# Universal (arm64 + x86_64) binary — the shim runs host-side on macOS.
for arch in arm64 amd64; do
  echo "building darwin/$arch..."
  CGO_ENABLED=0 GOOS=darwin GOARCH="$arch" \
    go -C "$REPO_ROOT" build -trimpath -ldflags="-s -w" \
    -o "$MCPB_DIR/bin/dharma-$arch" ./cmd/dharma
done
lipo -create -output "$MCPB_DIR/bin/dharma" \
  "$MCPB_DIR/bin/dharma-arm64" "$MCPB_DIR/bin/dharma-amd64"
rm "$MCPB_DIR/bin/dharma-arm64" "$MCPB_DIR/bin/dharma-amd64"

echo "installing shim dependencies..."
if [ -f "$MCPB_DIR/package-lock.json" ]; then
  (cd "$MCPB_DIR" && npm ci --omit=dev --silent)
else
  (cd "$MCPB_DIR" && npm install --omit=dev --silent)
fi

echo "packing..."
npx --yes @anthropic-ai/mcpb pack "$MCPB_DIR" "$REPO_ROOT/dist/dharma.mcpb"

echo "done: dist/dharma.mcpb"
