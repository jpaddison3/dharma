# dharma

Agent-friendly CLI for the Asana API. Built because the Asana MCP server keeps breaking and CLIs are easier for AI agents to call reliably.

## Status

Early — auth, raw API passthrough, and resource commands (`user`, `workspace`, `project`, `task`, `my-tasks`) are functional.

## Install

Requires Go 1.22+.

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
dharma task stories <gid> --fields type,text,created_at,created_by.name

dharma my-tasks list                                  # all tasks assigned to me
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
