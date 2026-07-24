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
release_json="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest")" || {
  echo "error: couldn't fetch release info from https://api.github.com/repos/$REPO/releases/latest" >&2
  echo "       (no release published yet, no network, or GitHub's unauthenticated rate limit — try again in an hour)" >&2
  exit 1
}
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

# The probe runs with ASANA_TOKEN stripped: ChatGPT desktop launches from
# Finder and inherits no shell environment, so a token that only exists in
# this terminal would make the install look complete while every tool call
# fails. What counts is a token dharma can find on its own, in its config.
if env -u ASANA_TOKEN "$BIN" user me >/dev/null 2>&1; then
  echo "already authenticated."
else
  echo "no valid Asana token found."
  # The probe is wrapped in a brace group so its 2>/dev/null applies only to
  # the probe: a bare `exec 2>/dev/null` would silence stderr for the rest of
  # the script — including `auth login`'s own PAT prompt, which is written to
  # stderr, leaving the installer looking hung on an invisible question.
  if { : </dev/tty; } 2>/dev/null; then
    # Failing to authenticate must not abort the install: registration below
    # is independent, and an unauthenticated server returns an instructive
    # "run dharma auth login" tool error rather than breaking.
    "$BIN" auth login </dev/tty || {
      echo "authentication didn't complete. Finish it later with:" >&2
      echo "  $BIN auth login" >&2
    }
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
#
# This is text surgery on a file Codex CLI, the Codex IDE extension, and
# ChatGPT desktop all parse, so it errs toward doing nothing: if the config
# declares dharma in any spelling this can't strip (an inline
# `dharma = { ... }` under [mcp_servers], a quoted or indented header), it
# bails rather than append a second declaration — a duplicate table makes
# every one of those apps refuse to load the whole file, breaking MCP servers
# that have nothing to do with dharma.
register_via_toml() {
  local config="$1"
  mkdir -p "$(dirname "$config")" || return 1
  touch "$config" || return 1
  local stripped
  stripped="$(mktemp)" || return 1
  # The section-boundary test allows leading whitespace, which TOML permits:
  # anchoring it at column 0 would treat an indented sibling table as part of
  # the dharma block and silently delete someone else's server.
  # Buffer surviving lines and trim trailing blanks at END, so re-running
  # this doesn't grow an ever-longer gap before the appended block.
  awk '
    /^[[:space:]]*\[mcp_servers\.dharma\][[:space:]]*$/ { skip=1; next }
    /^[[:space:]]*\[/ { skip=0 }
    !skip { lines[++n] = $0 }
    END {
      while (n > 0 && lines[n] == "") n--
      for (i = 1; i <= n; i++) print lines[i]
    }
  ' "$config" > "$stripped" || { rm -f "$stripped"; return 1; }
  # A surviving `dharma` definition means the strip missed a spelling;
  # appending now would declare the table twice. A [mcp_servers.dharma.env]
  # sub-table deliberately survives and re-attaches to the fresh block (that
  # is where a hand-set ASANA_WORKSPACE lives) — but note a stale
  # env.ASANA_TOKEN there outranks the config file's token, so re-running this
  # installer cannot fix "still the old account"; the sub-table must be
  # removed by hand.
  if grep -Eq '^[[:space:]]*(\[mcp_servers\.("dharma"|dharma)\]|dharma[[:space:]]*=)' "$stripped"; then
    rm -f "$stripped"
    return 1
  fi
  {
    cat "$stripped"
    echo ""
    echo "[mcp_servers.dharma]"
    printf 'command = "%s"\n' "$BIN"
    echo 'args = ["mcp"]'
  } > "$config.new" || { rm -f "$stripped" "$config.new"; return 1; }
  rm -f "$stripped"
  # config.toml holds other servers' env secrets and codex creates it 0600;
  # a fresh file from this shell would land 0644 under the default umask.
  chmod 600 "$config.new" || { rm -f "$config.new"; return 1; }
  mv "$config.new" "$config"
}

# ChatGPT desktop reads ~/.codex/config.toml; it is not known to honor
# CODEX_HOME the way the codex CLI does, so this path stays literal — writing
# elsewhere could register with codex but not with the app this installs for.
codex_config="$HOME/.codex/config.toml"
# `codex mcp add` is an idempotent upsert that uses a real TOML parser, so it
# is tried first and never preceded by a remove — removing first would turn
# one atomic step into two, and a failed add would leave a colleague who had a
# working registration with none. If it fails (an older codex has no `mcp`
# subcommand), fall through to editing the config it shares with ChatGPT
# desktop; only if that fails too does a non-technical colleague get manual
# steps.
if command -v codex >/dev/null 2>&1 && codex mcp add dharma -- "$BIN" mcp; then
  echo "registered dharma with codex mcp."
elif register_via_toml "$codex_config"; then
  echo "registered dharma in $codex_config."
else
  print_manual_mcp_instructions
fi

# --- 5. Finish -----------------------------------------------------------

echo ""
"$BIN" --version
echo ""
echo "Open ChatGPT desktop → Settings → MCP servers and confirm \`dharma\` is listed and enabled (restart ChatGPT if it was running)."
