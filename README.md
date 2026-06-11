# dharma

Agent-friendly CLI for the Asana API. The Asana MCP server is underpowered for real workflows; dharma offers native commands for the common operations (tasks, sections, tags, My Tasks) and a `gh api`-style passthrough for everything else.

## Status

Early but usable. Auth, raw API passthrough, and resource commands (`user`, `workspace`, `project`, `section`, `tag`, `task`, `my-tasks`, `attachment`) are functional.

## Install

Requires Go 1.25+.

```sh
go install github.com/jpaddison3/dharma/cmd/dharma@latest
```

## Auth

Create a [Personal Access Token](https://app.asana.com/0/my-apps), then:

```sh
dharma auth login    # prompts for token, validates, stores in ~/.config/dharma/config.json (0600)
dharma auth status   # prints the authenticated user
dharma auth logout   # clears the stored token
```

Or skip the config file: `ASANA_TOKEN=... dharma user me`.

## Usage

```sh
dharma user me
dharma workspace list
dharma project list --workspace 1234567890

dharma task list --project 1234567890 --fields name,assignee.name,due_on
dharma task get <gid> --fields name,assignee.name
dharma task create --name "Do the thing" --project 1234567890 --assignee me
dharma task comment <gid> --text "See https://app.asana.com/0/0/<other-gid>"
dharma task move <gid> --section <section-gid>
dharma task rename <gid> --name "New name"
dharma task complete <gid>
dharma task set-due <gid> --due 2026-06-15        # or: today, tomorrow, or ISO datetime
dharma task set-due <gid> --clear
dharma task assign <gid> --to me                  # or a user gid; --clear to unassign
dharma task set-notes <gid> --notes "..."         # pass "" to clear
dharma task search --text "MINERVA" --completed=false --fields name
dharma task stories <gid> --fields type,text,created_at,created_by.name

# attachments
dharma attachment download <gid> --output ./screenshot.png
dharma attachment download <gid> --output-dir ./downloads        # uses attachment name
dharma task download-attachments <task-gid> --output-dir ./out   # all attachments on a task

dharma my-tasks list                                  # all tasks assigned to me
dharma my-tasks list --incomplete                     # only open tasks
dharma my-tasks list --section "Main Work"            # filtered to a named section
dharma my-tasks list --paginate --limit 100           # follow all pages

# raw API passthrough (modeled on `gh api`)
dharma api /users/me
dharma api -X POST /tasks -f name=Foo -f projects=1234567890
dharma api /workspaces/123/tasks --paginate
dharma api -X PUT /tasks/123 --body '{"data": {"completed": true}}'
```

### Output

JSON to stdout: pretty when stdout is a TTY, compact when piped. Errors go to stderr with non-zero exit code.

### `-f` semantics for `dharma api`

`-f key=value` becomes a **query parameter** on GET/DELETE/HEAD and a **JSON body field** (wrapped in Asana's `{"data": ...}` envelope) on POST/PUT/PATCH. `--body` passes raw JSON through unchanged.

### Workspace default

Many endpoints need a workspace gid. Resolution order:
1. `--workspace` flag
2. `ASANA_WORKSPACE` env var
3. `default_workspace` in the config file

## Cowork plugin

> **Status: experimental — never tested against a live Cowork session.** The `.mcpb` desktop extension below is the verified route; prefer it. A further difference: this route places the raw PAT inside the VM-readable filesystem (and SKILL.md tells the agent where the config lives), so a prompt-injected session could read the token itself — the `.mcpb` keeps the token host-side where the model only ever sees tool results.

To use dharma from inside [Claude Cowork](https://claude.com/docs/cowork)'s VM, install it as a plugin:

```sh
./scripts/install-cowork-plugin.sh
```

This cross-compiles static linux binaries (the Cowork VM is Ubuntu arm64), bundles your local dharma config (PAT + default workspace), and installs everything to `/Library/Application Support/Claude/org-plugins/dharma/` (sudo). Then relaunch Claude Desktop and allow network access to `app.asana.com` when the Cowork session asks.

Notes:

- Your PAT is copied into the plugin directory (0600, owned by you). Re-run the script after rotating the token or updating dharma — any run bumps `version.json`, which triggers a plugin resync.
- The plugin's `skills/dharma/SKILL.md` teaches the agent to call the `bin/dharma` wrapper, which picks the right binary for the VM's architecture and points it at the bundled config.

## Desktop extension (.mcpb)

For sharing with colleagues who don't use a terminal: a Claude Desktop extension that wraps dharma in an MCP shim. Recipients double-click the file, paste an Asana PAT into the settings form (stored in macOS Keychain), and get Asana tools in chat and Cowork — no Node, npm, or terminal needed. Recipients in more than one Asana workspace get an instructive error pointing at the optional "Workspace GID" setting (no silent guessing).

```sh
./scripts/build-mcpb.sh      # → dist/dharma.mcpb
```

The bundle contains a universal (arm64 + x86_64) macOS dharma binary, a Node MCP server (`mcpb/server/index.js`) exposing typed tools plus an `asana_api` passthrough, and its node_modules; it runs on the Node runtime bundled inside Claude Desktop. The shim isolates `XDG_CONFIG_HOME`, so it only ever authenticates via the extension's configured token — a local `~/.config/dharma/config.json` can't leak in.

Smoke-test the shim without installing:

```sh
ASANA_TOKEN=... node mcpb/smoke.mjs
```

Caveat for distribution: the bundled binary is unsigned. A bundle downloaded via Slack/browser gets macOS quarantine, and Gatekeeper may block the binary on first run on the recipient's machine — test that path before sending it widely.

## License

MIT — see [LICENSE](LICENSE).
