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
let dharmaBin = path.join(here, "..", "bin", "dharma");

// Self-heal a lost exec bit (zip extraction doesn't always preserve it).
try {
  fs.accessSync(dharmaBin, fs.constants.X_OK);
} catch {
  const healed = path.join(os.tmpdir(), "dharma-mcpb-bin");
  fs.copyFileSync(dharmaBin, healed);
  fs.chmodSync(healed, 0o755);
  dharmaBin = healed;
}

// Point dharma at an empty config dir so a ~/.config/dharma/config.json on
// the host can never mask broken token plumbing — the extension must work
// from ASANA_TOKEN alone, exactly as it would on a colleague's machine.
const isolatedConfig = fs.mkdtempSync(path.join(os.tmpdir(), "dharma-mcpb-cfg-"));

let workspaceGid = null;

function runDharma(args) {
  return new Promise((resolve) => {
    const env = { ...process.env, XDG_CONFIG_HOME: isolatedConfig };
    if (workspaceGid) env.ASANA_WORKSPACE = workspaceGid;
    execFile(dharmaBin, args, { env, maxBuffer: 32 * 1024 * 1024 }, (err, stdout, stderr) => {
      resolve({ ok: !err, stdout: stdout ?? "", stderr: stderr ?? "" });
    });
  });
}

// Most endpoints need a workspace gid; colleagues won't know theirs, so
// resolve it once from the API (nearly everyone at 80k is in one workspace).
async function ensureWorkspace() {
  if (workspaceGid) return;
  const res = await runDharma(["workspace", "list"]);
  if (!res.ok) throw new Error(`could not list workspaces: ${res.stderr.trim()}`);
  const workspaces = JSON.parse(res.stdout);
  if (!Array.isArray(workspaces) || workspaces.length === 0) {
    throw new Error("no Asana workspaces visible to this token");
  }
  workspaceGid = workspaces[0].gid;
}

function asResult(res) {
  if (!res.ok) {
    return { content: [{ type: "text", text: res.stderr.trim() || "dharma exited non-zero" }], isError: true };
  }
  const text = res.stdout.trim() || res.stderr.trim() || "(empty response)";
  return { content: [{ type: "text", text }] };
}

// Registers a tool whose handler returns a dharma argv (or throws).
function cliTool(name, description, shape, needsWorkspace, buildArgs) {
  server.registerTool(name, { description, inputSchema: shape }, async (args) => {
    try {
      if (needsWorkspace) await ensureWorkspace();
      return asResult(await runDharma(buildArgs(args)));
    } catch (e) {
      return { content: [{ type: "text", text: String(e.message ?? e) }], isError: true };
    }
  });
}

const server = new McpServer({ name: "dharma-asana", version: "0.1.0" });

const fields = z.string().optional().describe("Comma-separated opt_fields, e.g. name,assignee.name,due_on");

cliTool(
  "whoami",
  "Get the authenticated Asana user (gid, name, email). Useful as a connectivity check.",
  {},
  false,
  () => ["user", "me"]
);

cliTool(
  "my_tasks",
  "List open tasks in the user's My Tasks. Optionally filter to a named section (e.g. \"Main Work\").",
  {
    section: z.string().optional().describe("My Tasks section name to filter to"),
    include_completed: z.boolean().optional().describe("Include completed tasks (capped at 100 most recent; default false)"),
    fields,
  },
  true,
  ({ section, include_completed, fields }) => {
    const argv = ["my-tasks", "list"];
    // Incomplete-only paginates safely; the full history is thousands of tasks.
    if (include_completed) argv.push("--limit", "100");
    else argv.push("--incomplete", "--paginate");
    if (section) argv.push("--section", section);
    if (fields) argv.push("--fields", fields);
    return argv;
  }
);

cliTool(
  "search_tasks",
  "Search tasks across the workspace by text and filters. Returns at most 100 results; narrow with filters if truncated.",
  {
    text: z.string().optional().describe("Match against task name/description"),
    assignee: z.string().optional().describe("Assignee user gid, or 'me'"),
    project: z.string().optional().describe("Project gid"),
    completed: z.boolean().optional().describe("Filter by completion; omit for both"),
    fields,
  },
  true,
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
  "Get a single task by gid.",
  {
    task_gid: z.string().describe("Task gid"),
    fields,
  },
  false,
  ({ task_gid, fields }) => {
    const argv = ["task", "get", task_gid];
    if (fields) argv.push("--fields", fields);
    return argv;
  }
);

cliTool(
  "task_stories",
  "Get a task's stories (comments and activity history).",
  {
    task_gid: z.string().describe("Task gid"),
    fields,
  },
  false,
  ({ task_gid, fields }) => {
    const argv = ["task", "stories", task_gid];
    argv.push("--fields", fields || "type,text,created_at,created_by.name");
    return argv;
  }
);

cliTool(
  "list_projects",
  "List projects in the workspace.",
  {},
  true,
  () => ["project", "list"]
);

cliTool(
  "list_project_tasks",
  "List tasks in a project.",
  {
    project_gid: z.string().describe("Project gid"),
    fields,
  },
  false,
  ({ project_gid, fields }) => {
    const argv = ["task", "list", "--project", project_gid];
    if (fields) argv.push("--fields", fields);
    return argv;
  }
);

cliTool(
  "create_task",
  "Create a task.",
  {
    name: z.string().describe("Task name"),
    notes: z.string().optional().describe("Task description"),
    project_gid: z.string().optional().describe("Project to add the task to; omit to create in My Tasks"),
    assignee: z.string().optional().describe("Assignee user gid, or 'me'"),
  },
  true,
  ({ name, notes, project_gid, assignee }) => {
    const argv = ["task", "create", "--name", name];
    if (notes) argv.push("--notes", notes);
    if (project_gid) argv.push("--project", project_gid);
    if (assignee) argv.push("--assignee", assignee);
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
  false,
  ({ task_gid, text }) => ["task", "comment", task_gid, "--text", text]
);

cliTool(
  "complete_task",
  "Mark a task complete.",
  {
    task_gid: z.string().describe("Task gid"),
  },
  false,
  ({ task_gid }) => ["task", "complete", task_gid]
);

cliTool(
  "set_due_date",
  "Set or clear a task's due date.",
  {
    task_gid: z.string().describe("Task gid"),
    due: z.string().optional().describe("Due date: YYYY-MM-DD, 'today', 'tomorrow', or ISO datetime"),
    clear: z.boolean().optional().describe("Clear the due date instead of setting one"),
  },
  false,
  ({ task_gid, due, clear }) => {
    const argv = ["task", "set-due", task_gid];
    if (clear) argv.push("--clear");
    else if (due) argv.push("--due", due);
    else throw new Error("provide either due or clear");
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
  true,
  ({ method, path: apiPath, field, body, paginate }) => {
    const argv = ["api", "-X", method, apiPath];
    for (const f of field ?? []) argv.push("-f", f);
    if (body) argv.push("--body", body);
    if (paginate) argv.push("--paginate");
    return argv;
  }
);

const transport = new StdioServerTransport();
await server.connect(transport);
