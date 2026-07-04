// MCP shim exposing the bundled dharma CLI as desktop-extension tools.
// Auth arrives as ASANA_TOKEN in the environment (substituted from the
// extension's user_config by Claude Desktop).
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { z } from "zod";
import { execFile } from "node:child_process";
import { fileURLToPath } from "node:url";
import path from "node:path";
import os from "node:os";
import fs from "node:fs";

const here = path.dirname(fileURLToPath(import.meta.url));
const pkg = JSON.parse(fs.readFileSync(path.join(here, "..", "package.json"), "utf8"));

// Claude Desktop substitutes ${user_config.*} into env; depending on the host
// version an unset optional field can arrive as an empty string or as the
// literal placeholder. Treat both as absent rather than letting them reach
// the CLI as real values.
for (const key of ["ASANA_TOKEN", "ASANA_WORKSPACE"]) {
  if (process.env[key]) process.env[key] = process.env[key].trim();
  if (!process.env[key] || process.env[key].startsWith("${")) delete process.env[key];
}
// A pasted workspace setting that isn't a gid would surface as opaque 400s on
// every call; drop it so auto-detection (or its instructive multi-workspace
// error) takes over instead — but remember, so that error can explain why the
// setting the user thinks they configured isn't in effect.
let discardedWorkspaceSetting = false;
if (process.env.ASANA_WORKSPACE && !/^\d+$/.test(process.env.ASANA_WORKSPACE)) {
  delete process.env.ASANA_WORKSPACE;
  discardedWorkspaceSetting = true;
  console.error("dharma-asana: ignoring configured Workspace GID — not a numeric gid");
}

let dharmaBin = path.join(here, "..", "bin", "dharma");
if (!fs.existsSync(dharmaBin)) {
  throw new Error(`bundled dharma binary missing at ${dharmaBin} — run scripts/build-mcpb.sh`);
}

// Self-heal a lost exec bit (zip extraction doesn't always preserve it).
try {
  fs.accessSync(dharmaBin, fs.constants.X_OK);
} catch {
  try {
    // Repairing in place fixes the install permanently.
    fs.chmodSync(dharmaBin, 0o755);
  } catch {
    // Read-only install dir: heal to a stable tmp name via unique copy +
    // atomic rename, so a concurrent or restarting server never sees a
    // partial copy. (In-flight execs are safe; the stable name itself is
    // shared across server instances and versions.)
    const healed = path.join(os.tmpdir(), "dharma-mcpb-bin");
    const tmp = `${healed}.${process.pid}`;
    fs.copyFileSync(dharmaBin, tmp);
    fs.chmodSync(tmp, 0o755);
    fs.renameSync(tmp, healed);
    dharmaBin = healed;
  }
}

// A path that never exists: dharma treats a missing config file as empty, so
// a ~/.config/dharma/config.json on the host can never mask broken token
// plumbing — the extension must work from its own settings alone, exactly as
// it would on a colleague's machine.
const isolatedConfig = path.join(here, "..", "no-user-config");

let workspaceGid = process.env.ASANA_WORKSPACE ?? null;
let workspacePromise = null;

function runDharma(args) {
  return new Promise((resolve) => {
    const env = { ...process.env, XDG_CONFIG_HOME: isolatedConfig };
    // Once resolved, the workspace gid is offered to every call; commands
    // that don't take a workspace ignore the env var.
    if (workspaceGid) env.ASANA_WORKSPACE = workspaceGid;
    execFile(dharmaBin, args, { env, maxBuffer: 32 * 1024 * 1024 }, (err, stdout, stderr) => {
      resolve({ ok: !err, stdout: stdout ?? "", stderr: stderr ?? "", error: err?.message ?? "" });
    });
  });
}

async function fetchSingleWorkspace() {
  const res = await runDharma(["workspace", "list"]);
  if (!res.ok) {
    throw new Error(`could not list workspaces: ${(res.stderr.trim() || res.error)}`);
  }
  // dharma wraps lists as {"ok":true,"count":N,"data":[...]}. Tolerate a bare
  // array too, so an older/newer CLI can't silently break workspace resolution.
  const parsed = JSON.parse(res.stdout);
  const workspaces = Array.isArray(parsed) ? parsed : parsed.data;
  if (!Array.isArray(workspaces) || workspaces.length === 0) {
    throw new Error("no Asana workspaces visible to this token");
  }
  if (workspaces.length > 1) {
    const names = workspaces.map((w) => `${w.name} (${w.gid})`).join(", ");
    throw new Error(
      `your Asana token can see multiple workspaces: ${names}. ` +
        `Ask the user to set "Workspace GID" in this extension's settings ` +
        `(Claude Desktop → Settings → Extensions → Asana (dharma)) to the gid of the one to use.` +
        (discardedWorkspaceSetting
          ? ` Note: the currently configured "Workspace GID" was ignored because it isn't a numeric gid.`
          : "")
    );
  }
  return workspaces[0].gid;
}

// Share one in-flight lookup across concurrent first calls, but clear it on
// failure so a transient error isn't sticky for the life of the process.
async function ensureWorkspace() {
  if (workspaceGid) return;
  workspacePromise ??= fetchSingleWorkspace();
  try {
    workspaceGid = await workspacePromise;
  } catch (e) {
    workspacePromise = null;
    throw e;
  }
}

function asResult(res) {
  if (!res.ok) {
    // On failure dharma writes a structured {"ok":false,"error":{...}} envelope
    // to stdout and a one-line summary to stderr. Prefer the structured stdout
    // so the model gets http_status / help; fall back to stderr if it's absent.
    return {
      content: [
        {
          type: "text",
          text: res.stdout.trim() || res.stderr.trim() || res.error || "dharma failed with no output",
        },
      ],
      isError: true,
    };
  }
  let text = res.stdout.trim() || "(empty response)";
  // dharma signals caveats — notably "results truncated, more pages exist" —
  // on stderr even when the call succeeds; the model needs to see them.
  const warning = res.stderr.trim();
  if (warning) text += `\n\n[dharma warning] ${warning}`;
  return { content: [{ type: "text", text }] };
}

const server = new McpServer({ name: "dharma-asana", version: pkg.version });

// Registers a tool whose handler returns a dharma argv (or throws).
// needsWorkspace may be a boolean or a predicate of the tool's args. Argv
// builders place "--" before model-supplied positionals so a value starting
// with "-" can't be parsed as a flag.
function cliTool(name, description, shape, { needsWorkspace }, buildArgs) {
  server.registerTool(name, { description, inputSchema: shape }, async (args) => {
    try {
      const needs = typeof needsWorkspace === "function" ? needsWorkspace(args) : needsWorkspace;
      if (needs) await ensureWorkspace();
      return asResult(await runDharma(buildArgs(args)));
    } catch (e) {
      return { content: [{ type: "text", text: String(e.message ?? e) }], isError: true };
    }
  });
}

const fieldsSchema = z.string().optional().describe("Comma-separated opt_fields, e.g. name,assignee.name,due_on");

cliTool(
  "whoami",
  "Get the authenticated Asana user (gid, name, email). Useful as a connectivity check.",
  {},
  { needsWorkspace: false },
  () => ["user", "me"]
);

cliTool(
  "my_tasks",
  "List open tasks in the user's My Tasks. Returns {ok,count,has_more,data:[...]}; has_more=true means more than the first page exist (set paginate). Optionally filter to a named section (e.g. \"Main Work\").",
  {
    section: z.string().optional().describe("My Tasks section name to filter to"),
    paginate: z.boolean().optional().describe("Fetch all pages instead of the first 100 (can be large)"),
    include_completed: z.boolean().optional().describe("Include completed tasks (first 100 in My Tasks order; default false)"),
    fields: fieldsSchema,
  },
  { needsWorkspace: true },
  ({ section, paginate, include_completed, fields }) => {
    const argv = ["my-tasks", "list"];
    if (!include_completed) argv.push("--incomplete");
    if (paginate) argv.push("--paginate");
    else argv.push("--limit", "100");
    if (section) argv.push("--section", section);
    if (fields) argv.push("--fields", fields);
    return argv;
  }
);

cliTool(
  "search_tasks",
  "Search tasks across the workspace by text and filters. Returns {ok,count,has_more,data:[...]} with at most 100 results; has_more=true means the cap was hit — narrow filters (the result's hint field suggests how).",
  {
    text: z.string().optional().describe("Match against task name/description"),
    assignee: z.string().optional().describe("Assignee user gid, or 'me'"),
    project: z.string().optional().describe("Project gid"),
    completed: z.boolean().optional().describe("Filter by completion; omit for both"),
    fields: fieldsSchema,
  },
  { needsWorkspace: true },
  ({ text, assignee, project, completed, fields }) => {
    const argv = ["task", "search"];
    if (text) argv.push("--text", text);
    if (assignee) argv.push("--assignee", assignee);
    if (project) argv.push("--project", project);
    if (completed !== undefined) argv.push(`--completed=${completed}`);
    if (fields) argv.push("--fields", fields);
    return argv;
  }
);

cliTool(
  "get_task",
  "Get a single task by gid. Returns {ok,data,context}, where context summarizes the comment count, attachment names, subtask count, and project names — read it to decide what to follow up on (e.g. task_stories when comments > 0) without extra calls.",
  {
    task_gid: z.string().describe("Task gid"),
    fields: fieldsSchema,
    full: z.boolean().optional().describe("Return full notes without truncation"),
  },
  { needsWorkspace: false },
  ({ task_gid, fields, full }) => {
    const argv = ["task", "get"];
    if (fields) argv.push("--fields", fields);
    if (full) argv.push("--full");
    argv.push("--", task_gid);
    return argv;
  }
);

cliTool(
  "task_stories",
  "Get a task's stories (comments and activity history). Fields default to type,text,created_at,created_by.name. Long comment text is truncated (see truncated_fields); pass full for complete text.",
  {
    task_gid: z.string().describe("Task gid"),
    fields: fieldsSchema,
    full: z.boolean().optional().describe("Return full comment text without truncation"),
  },
  { needsWorkspace: false },
  ({ task_gid, fields, full }) => {
    const argv = ["task", "stories"];
    if (fields) argv.push("--fields", fields);
    if (full) argv.push("--full");
    argv.push("--", task_gid);
    return argv;
  }
);

cliTool(
  "list_projects",
  "List projects in the workspace. Returns {ok,count,has_more,data:[...]}; has_more=true means more than the first page exist (set paginate).",
  {
    paginate: z.boolean().optional().describe("Fetch all pages instead of the first 100"),
  },
  { needsWorkspace: true },
  ({ paginate }) => {
    const argv = ["project", "list"];
    if (paginate) argv.push("--paginate");
    return argv;
  }
);

cliTool(
  "list_project_tasks",
  "List open tasks in a project. Returns {ok,count,has_more,data:[...]}; has_more=true means more than the first page exist (set paginate).",
  {
    project_gid: z.string().describe("Project gid"),
    include_completed: z.boolean().optional().describe("Include completed tasks (default false)"),
    paginate: z.boolean().optional().describe("Fetch all pages instead of the first 100"),
    fields: fieldsSchema,
  },
  { needsWorkspace: false },
  ({ project_gid, include_completed, paginate, fields }) => {
    const argv = ["task", "list", "--project", project_gid];
    if (!include_completed) argv.push("--incomplete");
    if (paginate) argv.push("--paginate");
    if (fields) argv.push("--fields", fields);
    return argv;
  }
);

cliTool(
  "create_task",
  "Create a task. With no project it goes to the user's My Tasks.",
  {
    name: z.string().describe("Task name"),
    notes: z.string().optional().describe("Task description"),
    project_gid: z.string().optional().describe("Project to add the task to; omit to create in My Tasks"),
    assignee: z.string().optional().describe("Assignee user gid, or 'me'"),
  },
  // A project-backed create infers its workspace from the project; only
  // workspace-level creates need resolution.
  { needsWorkspace: (args) => !args.project_gid },
  ({ name, notes, project_gid, assignee }) => {
    const argv = ["task", "create", "--name", name];
    if (notes) argv.push("--notes", notes);
    if (project_gid) argv.push("--project", project_gid);
    if (assignee) argv.push("--assignee", assignee);
    // A task with neither project nor assignee would land in nobody's My
    // Tasks (and be findable only via search); default to the user.
    else if (!project_gid) argv.push("--assignee", "me");
    return argv;
  }
);

cliTool(
  "comment_task",
  "Add a comment to a task.",
  {
    task_gid: z.string().describe("Task gid"),
    text: z.string().describe("Comment text"),
  },
  { needsWorkspace: false },
  ({ task_gid, text }) => ["task", "comment", "--text", text, "--", task_gid]
);

cliTool(
  "complete_task",
  "Mark a task complete.",
  {
    task_gid: z.string().describe("Task gid"),
  },
  { needsWorkspace: false },
  ({ task_gid }) => ["task", "complete", "--", task_gid]
);

cliTool(
  "set_due_date",
  "Set or clear a task's due date.",
  {
    task_gid: z.string().describe("Task gid"),
    due: z.string().optional().describe("Due date: YYYY-MM-DD, 'today', 'tomorrow', or ISO datetime"),
    clear: z.boolean().optional().describe("Clear the due date instead of setting one"),
  },
  { needsWorkspace: false },
  ({ task_gid, due, clear }) => {
    if (due && clear) throw new Error("provide either due or clear, not both");
    const argv = ["task", "set-due"];
    if (clear) argv.push("--clear");
    else if (due) argv.push("--due", due);
    else throw new Error("provide either due or clear");
    argv.push("--", task_gid);
    return argv;
  }
);

cliTool(
  "asana_api",
  "Raw Asana API passthrough for anything the other tools don't cover (modeled on `gh api`). " +
    "field entries like key=value become query parameters on GET/DELETE and JSON body fields " +
    "(wrapped in Asana's {data: ...} envelope) on POST/PUT/PATCH.",
  {
    method: z.enum(["GET", "POST", "PUT", "PATCH", "DELETE"]).default("GET").describe("HTTP method"),
    path: z.string().describe("API path, e.g. /users/me or /tasks/123"),
    field: z.array(z.string()).optional().describe("key=value pairs (query params on GET/DELETE, body fields otherwise)"),
    body: z.string().optional().describe("Raw JSON body, passed through unchanged"),
    paginate: z.boolean().optional().describe("Follow all pages (GET only)"),
  },
  { needsWorkspace: false },
  ({ method, path: apiPath, field, body, paginate }) => {
    const argv = ["api", "-X", method];
    for (const f of field ?? []) argv.push("-f", f);
    if (body) argv.push("--body", body);
    if (paginate) argv.push("--paginate");
    argv.push("--", apiPath);
    return argv;
  }
);

const transport = new StdioServerTransport();
await server.connect(transport);
