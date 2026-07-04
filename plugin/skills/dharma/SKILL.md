---
name: dharma
description: Interact with Asana — tasks, projects, sections, tags, My Tasks, comments, attachments, search — via the bundled dharma CLI. Use whenever the user wants to read or modify Asana data (look up or edit a task, list My Tasks, comment, find a project, etc.).
---

# dharma — Asana CLI

This plugin bundles `dharma`, an agent-friendly CLI for the Asana API, with prebuilt Linux binaries and a pre-authenticated config (personal access token + default workspace, baked in when the plugin was installed).

## Setup (once per session)

```sh
DHARMA="${CLAUDE_PLUGIN_ROOT}/bin/dharma"
"$DHARMA" user me
```

If `CLAUDE_PLUGIN_ROOT` is unset, the plugin root is two directories above this SKILL.md file. The `bin/dharma` wrapper picks the right binary for the current OS/architecture and points it at the bundled config — invoke the wrapper, not the `dharma-linux-*` binaries directly.

`user me` prints the authenticated user as JSON. If it works, you're set.

## Troubleshooting

- **`Permission denied` running the wrapper** — exec bit lost in file sync. Run it via `sh "$DHARMA" ...` instead; the wrapper self-heals the binary's exec bit.
- **Network error / timeout** — `app.asana.com` isn't on the Cowork egress allowlist. Ask the user to allow it (consumer Cowork: approve the network permission prompt or add it in settings; managed deployments: `coworkEgressAllowedHosts`).
- **401 / invalid token** — the baked-in token was revoked or expired. Ask the user to re-run `scripts/install-cowork-plugin.sh` from the dharma repo on the host machine.

## Usage

`--help` on any command (and `"$DHARMA" --help` for the command list) is authoritative — the CLI is self-documenting, so check it rather than guessing flags. A few motivating examples:

```sh
"$DHARMA" my-tasks list --incomplete --fields name,due_on   # open tasks assigned to me
"$DHARMA" task search --text "keyword" --completed=false --fields name
"$DHARMA" task get <gid> --fields name,notes,assignee.name
"$DHARMA" task create --name "Do the thing" --project <gid> --assignee me
"$DHARMA" task comment <gid> --text "..."
```

For endpoints without a typed command, `dharma api` works like `gh api` (`"$DHARMA" api --help` documents the `-f`/`--body` semantics):

```sh
"$DHARMA" api /users/me
"$DHARMA" api -X POST /tasks -f name=Foo -f projects=<gid>
```

## Conventions

- **Output** is a JSON envelope on stdout (compact when piped):
  - **Lists** → `{"ok": true, "count": N, "has_more": bool, "hint"?, "data": [...]}`. `has_more: true` means the results were capped — `--paginate` or narrow filters (the `hint` field says how). Pull rows with `jq '.data[]'`.
  - **Single objects** (get, create, mutations) → `{"ok": true, "data": {...}}`.
  - **Failures** → `{"ok": false, "error": {"message", "http_status"?, "help"?}}` on stdout, plus a one-line `Error: …` on stderr.
  - `dharma api` is the exception: it passes Asana's raw response through unchanged (no envelope).
- **Exit codes**: `0` success · `1` API/operational error · `2` auth (missing or rejected token) · `3` usage error (bad flags or arguments). Branch on the exit code rather than scraping text.
- **Not idempotent**: `task create` and `task comment` POST new objects and Asana has no dedupe key — if a call times out, verify with `task search` / `task stories` before retrying, or you may create a duplicate.
- Asana gids are opaque strings — never invent one; get them from list/search output.
- **Fields**: list/get commands send a curated `--fields` set by default (small but useful). Override with `--fields a,b,c`, or `--fields ""` for Asana's raw representation. Caveat: **Asana silently ignores unknown/misspelled opt_fields** — a typo yields a bare `{"gid": ...}` with no error, so if an expected field is missing, check the spelling.
- The default workspace comes from the bundled config; override with `--workspace <gid>` if needed.
