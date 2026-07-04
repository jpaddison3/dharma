// Smoke test for the MCP shim, simulating Claude Desktop: spawns the server
// over stdio with ASANA_TOKEN in env, lists tools, exercises a few calls.
//   ASANA_TOKEN=... node smoke.mjs
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";
import { fileURLToPath } from "node:url";
import path from "node:path";

const here = path.dirname(fileURLToPath(import.meta.url));
const transport = new StdioClientTransport({
  command: "node",
  args: [path.join(here, "server", "index.js")],
  env: { ...process.env },
});
const client = new Client({ name: "smoke", version: "0.0.1" });
await client.connect(transport);

// Lists come back as an envelope {ok,count,has_more,hint?,data:[...]}. A
// success may still carry a "[dharma warning] ..." suffix (appended from
// stderr); split it off before parsing.
function parseEnvelope(result) {
  const [json, warning] = result.content[0].text.split("\n\n[dharma warning] ");
  return { env: JSON.parse(json), warning };
}

const tools = await client.listTools();
console.log("tools:", tools.tools.map((t) => t.name).join(", "));

const who = await client.callTool({ name: "whoami", arguments: {} });
console.log("whoami:", who.content[0].text.slice(0, 200), who.isError ? "(ERROR)" : "");

const mine = await client.callTool({ name: "my_tasks", arguments: { fields: "name,due_on" } });
if (mine.isError) {
  console.log("my_tasks: ERROR:", mine.content[0].text);
} else {
  const { env } = parseEnvelope(mine);
  console.log(`my_tasks: ${env.count} tasks${env.has_more ? " (has_more)" : ""}`);
}

const projects = await client.callTool({ name: "list_projects", arguments: {} });
if (projects.isError) {
  console.log("list_projects: ERROR:", projects.content[0].text);
} else {
  const { env } = parseEnvelope(projects);
  console.log(`list_projects: ${env.count} projects${env.has_more ? " (has_more)" : ""}`);
}

const bad = await client.callTool({ name: "get_task", arguments: { task_gid: "1" } });
console.log("bad gid -> isError:", bad.isError === true, "|", bad.content[0].text.slice(0, 80));

const both = await client.callTool({
  name: "set_due_date",
  arguments: { task_gid: "1", due: "today", clear: true },
});
console.log("due+clear -> isError:", both.isError === true, "|", both.content[0].text.slice(0, 60));

await client.close();
