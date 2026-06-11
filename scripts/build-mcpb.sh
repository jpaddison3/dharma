#!/usr/bin/env bash
# Build the dharma desktop extension (.mcpb) for Claude Desktop / Cowork.
# Produces dist/dharma.mcpb — double-click it to install, or send it to a
# colleague. The bundle contains a universal macOS dharma binary, the Node
# MCP shim, and its node_modules; the recipient needs nothing but Claude
# Desktop and an Asana PAT (entered in the extension's settings form).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$REPO_ROOT/scripts/lib.sh"
MCPB_DIR="$REPO_ROOT/mcpb"
mkdir -p "$MCPB_DIR/bin" "$REPO_ROOT/dist"

# Universal (arm64 + x86_64) binary — the shim runs host-side on macOS.
build_dharma darwin arm64 "$MCPB_DIR/bin/dharma-arm64"
build_dharma darwin amd64 "$MCPB_DIR/bin/dharma-amd64"
lipo -create -output "$MCPB_DIR/bin/dharma" \
  "$MCPB_DIR/bin/dharma-arm64" "$MCPB_DIR/bin/dharma-amd64"
rm "$MCPB_DIR/bin/dharma-arm64" "$MCPB_DIR/bin/dharma-amd64"

echo "installing shim dependencies..."
(cd "$MCPB_DIR" && npm ci --omit=dev --silent)

echo "packing..."
# Packer pinned: this is a distribution pipeline, not a dev convenience —
# bump deliberately.
npx --yes @anthropic-ai/mcpb@2.1.2 pack "$MCPB_DIR" "$REPO_ROOT/dist/dharma.mcpb"

echo "done: dist/dharma.mcpb"
