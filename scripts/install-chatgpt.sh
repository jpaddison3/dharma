#!/usr/bin/env bash
# Install dharma as a ChatGPT desktop / Codex MCP server: downloads the
# latest release binary, authenticates if needed, and registers the server
# in Codex's shared config. Safe to re-run (updates the binary, skips auth if
# already configured, and re-registers the MCP server idempotently).
#
#   curl -fsSL https://raw.githubusercontent.com/jpaddison3/dharma/main/scripts/install-chatgpt.sh | bash
#
# No jq or node required — GitHub's release JSON and Codex's TOML config are
# both parsed with grep/sed/awk.
set -euo pipefail

REPO="jpaddison3/dharma"
BIN="$HOME/.local/bin/dharma"

# --- 1. Preflight -----------------------------------------------------------

if [ "$(uname -s)" != "Darwin" ]; then
  echo "error: this installer only supports macOS. Detected: $(uname -s)" >&2
  exit 1
fi

# --- 2. Download --------------------------------------------------------

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "fetching latest release info..."
release_json="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest")"
asset_url="$(printf '%s\n' "$release_json" \
  | grep -o '"browser_download_url"[[:space:]]*:[[:space:]]*"[^"]*dharma-macos-universal\.tar\.gz"' \
  | head -n1 \
  | sed -E 's/^.*"(https[^"]+)"$/\1/')"
if [ -z "$asset_url" ]; then
  echo "error: couldn't find a dharma-macos-universal.tar.gz asset on the latest release" >&2
  exit 1
fi

echo "downloading $asset_url..."
curl -fsSL "$asset_url" -o "$tmp/dharma-macos-universal.tar.gz"
tar -xzf "$tmp/dharma-macos-universal.tar.gz" -C "$tmp"
chmod 755 "$tmp/dharma"

mkdir -p "$(dirname "$BIN")"
mv "$tmp/dharma" "$BIN"
echo "installed $BIN"

# --- 3. Auth (only if needed) ------------------------------------------

if "$BIN" user me >/dev/null 2>&1; then
  echo "already authenticated."
else
  echo "no valid Asana token found."
  if exec 3</dev/tty 2>/dev/null; then
    exec 3<&-
    "$BIN" auth login </dev/tty
  else
    echo "no interactive terminal available here — run this manually to authenticate:"
    echo "  $BIN auth login"
  fi
fi

# --- 4. Register the MCP server (idempotent) ----------------------------

print_manual_mcp_instructions() {
  echo ""
  echo "Couldn't register the MCP server automatically. In ChatGPT desktop, go to"
  echo "Settings → MCP servers → Add server, and enter:"
  echo "  name:    dharma"
  echo "  command: $BIN"
  echo "  args:    mcp"
}

# Strips any existing [mcp_servers.dharma] block (that header up to, but not
# including, the next [section] header) and appends a fresh one, so re-running
# never accumulates duplicate blocks.
register_via_toml() {
  local config="$1"
  mkdir -p "$(dirname "$config")" || return 1
  touch "$config" || return 1
  local stripped
  stripped="$(mktemp)" || return 1
  # Buffer surviving lines and trim trailing blanks at END, so re-running
  # this doesn't grow an ever-longer gap before the appended block.
  awk '
    /^\[mcp_servers\.dharma\]/ { skip=1; next }
    /^\[/ { skip=0 }
    !skip { lines[++n] = $0 }
    END {
      while (n > 0 && lines[n] == "") n--
      for (i = 1; i <= n; i++) print lines[i]
    }
  ' "$config" > "$stripped" || { rm -f "$stripped"; return 1; }
  {
    cat "$stripped"
    echo ""
    echo "[mcp_servers.dharma]"
    printf 'command = "%s"\n' "$BIN"
    echo 'args = ["mcp"]'
  } > "$config.new" || { rm -f "$stripped" "$config.new"; return 1; }
  rm -f "$stripped"
  mv "$config.new" "$config"
}

if command -v codex >/dev/null 2>&1; then
  codex mcp remove dharma >/dev/null 2>&1 || true
  if codex mcp add dharma -- "$BIN" mcp; then
    echo "registered dharma with codex mcp."
  else
    print_manual_mcp_instructions
  fi
else
  codex_config="$HOME/.codex/config.toml"
  if register_via_toml "$codex_config"; then
    echo "registered dharma in $codex_config."
  else
    print_manual_mcp_instructions
  fi
fi

# --- 5. Finish -----------------------------------------------------------

echo ""
"$BIN" --version
echo ""
echo "Open ChatGPT desktop → Settings → MCP servers and confirm \`dharma\` is listed and enabled (restart ChatGPT if it was running)."
