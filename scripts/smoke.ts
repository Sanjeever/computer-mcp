import { spawn } from "node:child_process";
import { createInterface } from "node:readline";
import { strict as assert } from "node:assert";
import { resolve } from "node:path";

const binary = process.argv[2] ?? `build/computer-mcp${process.platform === "win32" ? ".exe" : ""}`;
const child = binary.endsWith(".js")
  ? spawn(process.execPath, [resolve(binary)], { stdio: ["pipe", "pipe", "pipe"] })
  : spawn(resolve(binary), [], { stdio: ["pipe", "pipe", "pipe"] });
const pending = new Map<
  number,
  { resolve: (v: Record<string, unknown>) => void; reject: (error: Error) => void }
>();
let sequence = 0;
let stderr = "";
child.stderr.on("data", (data: Buffer) => {
  stderr += data.toString();
});
const timer = setTimeout(() => {
  child.kill();
  throw new Error(`stdio smoke timed out: ${stderr}`);
}, 15000);
const lines = createInterface({ input: child.stdout });
lines.on("line", (line) => {
  const message = JSON.parse(line) as {
    id?: number;
    error?: unknown;
    result?: Record<string, unknown>;
  };
  if (message.id === undefined) return;
  const waiter = pending.get(message.id);
  pending.delete(message.id);
  if (message.error) waiter?.reject(new Error(JSON.stringify(message.error)));
  else waiter?.resolve(message.result ?? {});
});
child.on("error", (error) => {
  for (const waiter of pending.values()) waiter.reject(error);
});
child.on("exit", (code) => {
  for (const waiter of pending.values()) waiter.reject(new Error(`exited ${code}: ${stderr}`));
});
function request(method: string, params: object): Promise<Record<string, unknown>> {
  const id = ++sequence;
  return new Promise((resolve, reject) => {
    pending.set(id, { resolve, reject });
    child.stdin.write(JSON.stringify({ jsonrpc: "2.0", id, method, params }) + "\n");
  });
}
try {
  const initialized = await request("initialize", {
    protocolVersion: "2025-06-18",
    capabilities: {},
    clientInfo: { name: "smoke", version: "1" },
  });
  assert.equal((initialized.serverInfo as { name: string }).name, "computer-mcp");
  child.stdin.write(JSON.stringify({ jsonrpc: "2.0", method: "notifications/initialized" }) + "\n");
  const tools = await request("tools/list", {});
  assert.equal((tools.tools as unknown[]).length, 9);
  const status = await request("tools/call", { name: "computer_status", arguments: {} });
  assert.notEqual(status.isError, true, JSON.stringify(status));
  const content = status.content as { type: string; text: string }[];
  const parsed = JSON.parse(content[0].text) as { platform: string; displays: unknown[] };
  assert.ok(["darwin", "windows"].includes(parsed.platform));
  assert.ok(Array.isArray(parsed.displays));
  console.log(`stdio smoke passed (${parsed.platform})`);
} finally {
  child.stdin.end();
  await new Promise<void>((resolve) => {
    if (child.exitCode !== null) resolve();
    else child.once("exit", () => resolve());
  });
  clearTimeout(timer);
  lines.close();
}
