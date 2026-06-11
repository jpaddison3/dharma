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

const tools = await client.listTools();
console.log("tools:", tools.tools.map((t) => t.name).join(", "));

const who = await client.callTool({ name: "whoami", arguments: {} });
console.log("whoami:", who.content[0].text.slice(0, 200), who.isError ? "(ERROR)" : "");

const mine = await client.callTool({ name: "my_tasks", arguments: { fields: "name,due_on" } });
const text = mine.content[0].text;
console.log("my_tasks:", mine.isError ? `ERROR: ${text}` : `${JSON.parse(text).length} tasks`);

const bad = await client.callTool({ name: "get_task", arguments: { task_gid: "1" } });
console.log("bad gid -> isError:", bad.isError === true, "|", bad.content[0].text.slice(0, 80));

await client.close();
