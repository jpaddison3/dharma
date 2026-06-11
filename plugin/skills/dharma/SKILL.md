---
name: dharma
description: Interact with Asana — tasks, projects, sections, tags, My Tasks, comments, attachments, search — via the bundled dharma CLI. Use whenever the user wants to read or modify Asana data (look up or edit a task, list My Tasks, comment, find a project, etc.).
---

# dharma — Asana CLI

This plugin bundles `dharma`, an agent-friendly CLI for the Asana API, with prebuilt Linux binaries and a pre-authenticated config (personal access token + default workspace, baked in when the plugin was installed).

## Setup (once per session)

```sh
DHARMA="${CLAUDE_PLUGIN_ROOT}/bin/dharma"
"$DHARMA" auth status
```

If `CLAUDE_PLUGIN_ROOT` is unset, the plugin root is two directories above this SKILL.md file. The `bin/dharma` wrapper picks the right binary for the current OS/architecture and points it at the bundled config — invoke the wrapper, not the `dharma-linux-*` binaries directly.

`auth status` prints the authenticated user as JSON. If it works, you're set.

## Troubleshooting

- **`Permission denied` running the wrapper** — exec bit lost in file sync. Run it via `sh "$DHARMA" ...` instead; the wrapper self-heals the binary's exec bit.
- **Network error / timeout** — `app.asana.com` isn't on the Cowork egress allowlist. Ask the user to allow it (consumer Cowork: approve the network permission prompt or add it in settings; managed deployments: `coworkEgressAllowedHosts`).
- **401 / invalid token** — the baked-in token was revoked or expired. Ask the user to re-run `scripts/install-cowork-plugin.sh` from the dharma repo on the host machine.

## Usage

`--help` on any command is authoritative; the CLI is self-documenting. Common operations:

```sh
"$DHARMA" user me
"$DHARMA" workspace list
"$DHARMA" project list

"$DHARMA" task list --project <gid> --fields name,assignee.name,due_on
"$DHARMA" task get <gid> --fields name,notes,assignee.name
"$DHARMA" task create --name "Do the thing" --project <gid> --assignee me
"$DHARMA" task comment <gid> --text "..."
"$DHARMA" task move <gid> --section <section-gid>
"$DHARMA" task complete <gid>
"$DHARMA" task set-due <gid> --due 2026-06-15        # or: today, tomorrow, ISO datetime; --clear
"$DHARMA" task assign <gid> --to me                  # or a user gid; --clear to unassign
"$DHARMA" task set-notes <gid> --notes "..."
"$DHARMA" task search --text "keyword" --completed=false --fields name
"$DHARMA" task stories <gid> --fields type,text,created_at,created_by.name
"$DHARMA" task download-attachments <task-gid> --output-dir ./out

"$DHARMA" my-tasks list --incomplete                 # open tasks assigned to me
"$DHARMA" my-tasks list --section "Main Work"
"$DHARMA" my-tasks list --paginate --limit 100
```

### Raw API passthrough

For endpoints without a typed command, `dharma api` works like `gh api`:

```sh
"$DHARMA" api /users/me
"$DHARMA" api -X POST /tasks -f name=Foo -f projects=<gid>
"$DHARMA" api /workspaces/<gid>/tasks --paginate
"$DHARMA" api -X PUT /tasks/<gid> --body '{"data": {"completed": true}}'
```

`-f key=value` becomes a query parameter on GET/DELETE/HEAD and a JSON body field (wrapped in Asana's `{"data": ...}` envelope) on POST/PUT/PATCH.

## Conventions

- Output is JSON on stdout (compact when piped); errors go to stderr with a non-zero exit code.
- Asana gids are opaque strings — never invent one; get them from list/search output.
- Use `--fields` to keep payloads small; default field sets are minimal.
- The default workspace comes from the bundled config; override with `--workspace <gid>` if needed.
