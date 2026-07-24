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
# `|| true`: with pipefail a non-matching grep would kill the script here,
# before the explicit check below can print something a colleague can act on.
asset_url="$(printf '%s\n' "$release_json" \
  | grep -o '"browser_download_url"[[:space:]]*:[[:space:]]*"[^"]*dharma-macos-universal\.tar\.gz"' \
  | head -n1 \
  | sed -E 's/^.*"(https[^"]+)"$/\1/' || true)"
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

# The probe runs with ASANA_TOKEN and XDG_CONFIG_HOME stripped: ChatGPT
# desktop launches from Finder and inherits no shell environment, so a token
# that only exists in this terminal — or in a config directory only this shell
# knows about — would make the install look complete while every tool call
# fails. What counts is a token dharma can find the way ChatGPT will run it.
if env -u ASANA_TOKEN -u XDG_CONFIG_HOME "$BIN" user me >/dev/null 2>&1; then
  echo "already authenticated."
else
  echo "no valid Asana token found."
  # The probe is wrapped in a brace group so its 2>/dev/null applies only to
  # the probe: a bare `exec 2>/dev/null` would silence stderr for the rest of
  # the script — including `auth login`'s own PAT prompt, which is written to
  # stderr, leaving the installer looking hung on an invisible question.
  if { : </dev/tty; } 2>/dev/null; then
    # XDG_CONFIG_HOME is stripped here too, so the token is written to the
    # config the probe reads and ChatGPT desktop will find. Failing to
    # authenticate must not abort the install: registration below is
    # independent, and an unauthenticated server returns an instructive "run
    # dharma auth login" tool error rather than breaking.
    env -u XDG_CONFIG_HOME "$BIN" auth login </dev/tty || {
      echo "authentication didn't complete. Finish it later with:" >&2
      echo "  env -u XDG_CONFIG_HOME $BIN auth login" >&2
    }
  else
    echo "no interactive terminal available here — run this manually to authenticate:"
    echo "  env -u XDG_CONFIG_HOME $BIN auth login"
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
register_via_toml() (
  local config="$1"
  # Everything this function creates carries other servers' env secrets copied
  # out of the existing config, so nothing it writes may exist as 0644 even
  # briefly: a umask, not a chmod after the fact, is what makes that true if
  # the script is interrupted mid-write.
  umask 077
  mkdir -p "$(dirname "$config")" || exit 1
  touch "$config" || exit 1
  chmod 600 "$config" || exit 1
  local stripped
  stripped="$(mktemp)" || exit 1
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
  ' "$config" > "$stripped" || { rm -f "$stripped"; exit 1; }
  # A surviving `dharma` definition means the strip missed a spelling, and
  # appending now would declare the table twice — which makes every Codex app
  # reject the whole file. The pattern covers the three shapes the awk above
  # doesn't strip: a header with internal whitespace, quotes, or double
  # brackets; a top-level dotted key (`mcp_servers.dharma.command = ...`,
  # which needs no [mcp_servers] header at all); and a dotted or quoted key
  # under [mcp_servers] (`dharma.command =`, `dharma = {...}`). Matching too
  # much only costs a bail-out; matching too little corrupts the file.
  #
  # A [mcp_servers.dharma.env] sub-table is deliberately not matched: it
  # survives and re-attaches to the fresh block, which is where a hand-set
  # ASANA_WORKSPACE lives. Note that a stale env.ASANA_TOKEN there outranks the
  # config file's token, so re-running this installer cannot fix "still the old
  # account" — that sub-table has to be removed by hand.
  if grep -Eq "^[[:space:]]*(\[+[[:space:]]*[\"']?mcp_servers[\"']?[[:space:]]*\.[[:space:]]*[\"']?dharma[\"']?[[:space:]]*\]|[\"']?mcp_servers[\"']?[[:space:]]*\.[[:space:]]*[\"']?dharma[\"']?[[:space:]]*[.=]|[\"']?dharma[\"']?[[:space:]]*[.=])" "$stripped"; then
    rm -f "$stripped"
    exit 2
  fi
  # A fresh temp file in the same directory, not a fixed "$config.new": a stale
  # one left 0644 by an earlier interrupted run would be truncated in place,
  # keeping its mode, and then moved over the real config with every other
  # server's secrets inside it. Same directory so the mv is a rename.
  local merged
  merged="$(mktemp "$(dirname "$config")/config.toml.XXXXXX")" || { rm -f "$stripped"; exit 1; }
  {
    cat "$stripped"
    echo ""
    echo "[mcp_servers.dharma]"
    printf 'command = "%s"\n' "$BIN"
    echo 'args = ["mcp"]'
  } > "$merged" || { rm -f "$stripped" "$merged"; exit 1; }
  rm -f "$stripped"
  mv "$merged" "$config"
)

# The guard above returns 2 (rather than 1) when it found a dharma entry it
# can't safely rewrite: that colleague usually has a *working* registration,
# so telling them to add the server again would be wrong.
print_existing_entry_notice() {
  echo ""
  echo "Found an existing dharma entry in $codex_config in a format this installer"
  echo "won't rewrite, so it was left untouched. If dharma already works in ChatGPT"
  echo "desktop, nothing more is needed — it now points at the updated binary only if"
  echo "that entry's command is $BIN. Otherwise, edit that entry by hand to:"
  echo "  command = \"$BIN\""
  echo "  args    = [\"mcp\"]"
}

# ChatGPT desktop reads ~/.codex/config.toml; it is not known to honor
# CODEX_HOME the way the codex CLI does, so this path stays literal — writing
# elsewhere could register with codex but not with the app this installs for.
codex_config="$HOME/.codex/config.toml"
# `codex mcp add` replaces the whole named entry rather than merging into it
# (verified against codex 0.145.0: a re-add drops env, timeout, and
# enabled/disabled), so an update run first asks whether the existing entry
# already points at this binary and leaves it alone if so — that is what keeps
# a hand-set [mcp_servers.dharma.env] ASANA_WORKSPACE across updates. The add
# is never preceded by a remove: that would turn one upsert into two steps and
# leave a colleague with nothing if the add then failed. If codex can't do it
# (an older codex has no `mcp` subcommand), fall through to editing the config
# it shares with ChatGPT desktop; only if that fails too does a non-technical
# colleague get manual steps.
register_via_codex() {
  command -v codex >/dev/null 2>&1 || return 1
  # Match on the whole transport, not just the path: an entry pointing at this
  # binary with different args (or a disabled one) is not a working
  # registration, and skipping the add would report success for something that
  # never starts. tr squeezes the pretty-printed JSON onto one line so the
  # command, its args, and enabled can be matched together.
  local entry
  entry="$(codex mcp get dharma --json 2>/dev/null | tr -d ' \n' || true)"
  case "$entry" in
    *"\"enabled\":true"*"\"command\":\"$BIN\",\"args\":[\"mcp\"]"*)
      echo "dharma is already registered and enabled with codex mcp at $BIN — leaving its settings alone."
      return 0
      ;;
  esac
  if [ -n "$entry" ]; then
    echo "note: replacing an existing codex mcp entry for dharma — any custom env," >&2
    echo "      timeout, or approval settings on it are reset." >&2
  fi
  codex mcp add dharma -- "$BIN" mcp || return 1
  echo "registered dharma with codex mcp."
}

if ! register_via_codex; then
  register_status=0
  register_via_toml "$codex_config" || register_status=$?
  case "$register_status" in
    0) echo "registered dharma in $codex_config." ;;
    2) print_existing_entry_notice ;;
    *) print_manual_mcp_instructions ;;
  esac
fi

# --- 5. Finish -----------------------------------------------------------

echo ""
"$BIN" --version
echo ""
echo "Open ChatGPT desktop → Settings → MCP servers and confirm \`dharma\` is listed and enabled (restart ChatGPT if it was running)."
