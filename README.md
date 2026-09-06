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
dharma project get 1234567890
dharma project get 1234567890 --fields 'name,notes'
dharma project get 1234567890 --fields 'name,owner.name,team.name'
dharma project get 1234567890 --fields 'name,current_status_update.title,current_status_update.resource_subtype'
dharma project get 1234567890 --fields ""

dharma section list --project 1234567890
dharma section get <gid>
dharma section get <gid> --fields name,project.name

dharma task list --project 1234567890 --fields name,assignee.name,due_on
dharma task list --assignee me --modified-since '2026-07-06T00:00:00Z' --completed-since '2026-07-06T00:00:00Z' --paginate
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
dharma task search --created-after '2026-07-06T00:00:00Z' --created-before '2026-09-06T00:00:00Z' --sort-by created_at --sort-ascending
dharma task stories <gid> --fields type,text,created_at,created_by.name

# attachments
dharma attachment download <gid> --output-file ./screenshot.png
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

`task list` supports normal Asana offset pagination, so `--paginate` follows all
pages while retaining its selector and time cutoffs. `task search` makes one
request and cannot offset-paginate; narrow its filters or use creation-time
bounds to inspect another window, but do not treat date windows as a guaranteed
exhaustive cursor. These time and sort options are CLI-only and are not exposed
by the MCP tools.

`project get` looks up a project directly by GID, so it does not need a workspace. It requests the compact `name,archived,permalink_url` projection by default; pass `--fields` to replace it, or `--fields ""` to use Asana's raw default representation.

### Output

A JSON envelope to stdout: pretty when stdout is a TTY, compact when piped.

- **Lists** — `{"ok": true, "count": N, "has_more": bool, "hint"?: "...", "data": [...]}`. `has_more: true` means the results were capped (`--paginate` or narrow filters; `hint` says how). Pull rows with `jq '.data[]'`.
- **Single objects** (get, create, mutations) — `{"ok": true, "data": {...}}`.
- **Failures** — a structured error to stdout plus a one-line summary to stderr:

  ```json
  {"ok": false, "error": {"message": "Not Authorized", "http_status": 401, "help": "..."}}
  ```

Long free text (`task get` notes, `task stories` text) over ~2,000 chars is truncated with an inline `… (truncated, N chars total — rerun with --full)` marker and named in a top-level `truncated_fields`; pass `--full` for the complete text.

`dharma api` is the exception — on **success** it passes Asana's raw response through unchanged, no envelope, and always as JSON (it ignores `--output toon` so the escape hatch stays jq-parseable). On **failure** it still emits dharma's `{"ok": false, "error": {...}}` envelope and the exit codes below (the structured error and exit code are more useful to a caller than Asana's raw error body), so the raw-passthrough promise covers the success path only.

Exit codes: `0` success · `1` API/operational error · `2` auth (missing or rejected token) · `3` usage error (bad flags or arguments).

### Output format (experimental)

`--output toon` emits [TOON](https://github.com/toon-format/toon) instead of JSON — a line-oriented format that drops repeated object keys. On real Asana payloads (byte proxy for tokens, via `scripts/measure-toon.sh`):

| payload | savings |
| --- | --- |
| `my-tasks list` (flat) | ~38% |
| `project list` (flat) | ~33% |
| `task get` (nested object) | ~0% |
| `task list --fields …,assignee.name` (nested rows) | ~0% |

The win is real but only for **flat** list rows; nested rows fall back to inline JSON and single objects break even. It stays opt-in (default `json`): TOON isn't a `jq` target (`.data[]` won't work), and the mcpb shim parses JSON, so don't pipe TOON into either.

`internal/output/toon.go` is a hand-rolled encoder covering the subset dharma emits (tabular flat arrays, inline-JSON fallback, custom quoting) — it is **TOON-ish, not guaranteed byte-compatible** with a spec-compliant parser. Treat the linked spec as the shape it aims at, not a round-trip contract.

### Fields

List and `get` commands send a curated `--fields` (opt_fields) set by default — small but useful, and it also strips Asana's `resource_type` noise. Override with `--fields a,b,c`, or `--fields ""` for Asana's raw representation. Note that Asana **silently ignores** unknown or misspelled opt_fields (a typo yields a bare `{"gid": ...}` with no error), so check spelling if a field you expect is missing.

### `-f` semantics for `dharma api`

`-f key=value` becomes a **query parameter** on GET/DELETE/HEAD and a **JSON body field** (wrapped in Asana's `{"data": ...}` envelope) on POST/PUT/PATCH. `--body` passes raw JSON through unchanged.

For rich text via `html_notes` / `html_text`, wrap the value in `<body>`, escape only `& < >`, and use literal UTF-8—not numeric character references. Do not use `<p>` or `<br>`.
The typed `--notes`, `set-notes`, and `comment` paths are plain text. See `dharma api --help` for the authoritative rules, allowed tags, and examples.

### Workspace default

Many endpoints need a workspace gid. Resolution order:
1. `--workspace` flag
2. `ASANA_WORKSPACE` env var
3. `default_workspace` in the config file
4. otherwise resolved via the API: exactly one visible workspace is used automatically (one extra round-trip per command — set `default_workspace` to skip it); several produce an error naming them

## Cowork plugin

> **Status: experimental — never tested against a live Cowork session.** The `.mcpb` desktop extension below is the verified route; prefer it. A further difference: this route places the raw PAT inside the VM-readable filesystem (and SKILL.md tells the agent where the config lives), so a prompt-injected session could read the token itself — the `.mcpb` keeps the token host-side where the model only ever sees tool results.

To use dharma from inside [Claude Cowork](https://claude.com/docs/cowork)'s VM, install it as a plugin:

```sh
./scripts/install-cowork-plugin.sh
```

This cross-compiles a static linux/arm64 binary (the Cowork VM is Ubuntu arm64), bundles your local dharma config (PAT + default workspace), and installs everything to `/Library/Application Support/Claude/org-plugins/dharma/` (sudo). Then relaunch Claude Desktop and allow network access to `app.asana.com` when the Cowork session asks.

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

## ChatGPT desktop (MCP)

For colleagues on a paid ChatGPT plan: a native `dharma mcp` command that ChatGPT desktop runs as a local stdio MCP server — no Node, npm, or `.mcpb` install needed.

Prerequisites: macOS, the ChatGPT desktop app, and a paid ChatGPT plan (required for custom MCP servers).

```sh
curl -fsSL https://raw.githubusercontent.com/jpaddison3/dharma/main/scripts/install-chatgpt.sh | bash
```

This downloads the latest release binary to `~/.local/bin/dharma`, prompts for an Asana PAT if one isn't already configured, and registers `dharma mcp` in Codex's shared MCP config (`~/.codex/config.toml`), which ChatGPT desktop also reads. Re-run the same one-liner to update — it replaces the binary and re-registers the MCP server idempotently.

Your PAT is stored in plaintext at `~/.config/dharma/config.json` (0600, readable only by you) — the same config `dharma auth login` always uses. If this machine is ever compromised, revoke it at your [Asana Apps settings](https://app.asana.com/0/my-apps).

After installing, open ChatGPT desktop → Settings → MCP servers, confirm `dharma` is listed and enabled, and ask ChatGPT to run `whoami` to verify it can reach your Asana account.

Codex CLI and the Codex IDE extension read the same `~/.codex/config.toml`, so they should pick up `dharma` for free — an untested byproduct of targeting that config file.

## License

MIT — see [LICENSE](LICENSE).
