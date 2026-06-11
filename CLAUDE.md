# dharma

Personal CLI for the Asana API. Goal: replace the flaky Asana MCP server with a reliable CLI that agents can call.

## Layout

- `cmd/dharma/main.go` — binary entrypoint
- `internal/cli/` — cobra commands, one file per resource (`task.go`, `project.go`, etc.); `api.go` is the `gh api`-style escape hatch
- `internal/client/` — thin HTTP wrapper; unwraps Asana's `{data: ...}` envelope, surfaces `{errors: [...]}` as typed errors
- `internal/config/` — `~/.config/dharma/config.json`, 0600
- `internal/output/` — JSON output (pretty for TTY, compact when piped)
- `plugin/` — Claude Cowork plugin (manifest, skill, arch-dispatching `bin/dharma` wrapper); binaries and config are bundled at install time by `scripts/install-cowork-plugin.sh`, never committed
- `mcpb/` — Claude Desktop extension (MCP shim over the CLI for non-technical colleagues); `scripts/build-mcpb.sh` → `dist/dharma.mcpb`; smoke-test with `ASANA_TOKEN=... node mcpb/smoke.mjs`

## Dev

```sh
go build -o dharma ./cmd/dharma
./dharma --help
```

Install the pre-commit hook once per clone:

```sh
git config core.hooksPath .githooks
```

The hook runs `gofmt -l` on staged `.go` files and fails the commit if anything is unformatted.

## Conventions

- **Committing directly on `main` is fine** — no need for feature branches for this repo.
- `dharma api` is the escape hatch for endpoints without typed wrappers; `-f key=value` becomes query params on GET/DELETE/HEAD and a body field on POST/PUT/PATCH.
- Output is JSON-only for now (`--output table` is on the roadmap).
