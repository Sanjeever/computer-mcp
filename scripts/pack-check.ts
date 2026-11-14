import { execFileSync } from "node:child_process";
import { mkdtempSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

const artifacts = resolve("artifacts");
const files = readdirSync(artifacts);
const native = files.find(
  (name) =>
    name.includes(`computer-mcp-${process.platform}-${process.arch}-`) && name.endsWith(".tgz"),
);
const launcher = files.find(
  (name) => /^sanjeever-computer-mcp-\d/.test(name) && name.endsWith(".tgz"),
);
if (!native || !launcher) throw new Error("Pack both the native and launcher packages first");
const temporary = mkdtempSync(join(tmpdir(), "computer-mcp-pack-"));
const npm = process.platform === "win32" ? "npm.cmd" : "npm";
try {
  writeFileSync(join(temporary, "package.json"), JSON.stringify({ private: true }));
  execFileSync(
    npm,
    [
      "install",
      join(artifacts, native),
      join(artifacts, launcher),
      "--offline",
      "--ignore-scripts",
      "--no-audit",
      "--no-fund",
      "--cache",
      join(temporary, "cache"),
    ],
    { cwd: temporary, stdio: "inherit", shell: process.platform === "win32" },
  );
  execFileSync(
    process.execPath,
    [
      "--import=tsx",
      "scripts/smoke.ts",
      join(temporary, "node_modules", "@sanjeever", "computer-mcp", "dist", "cli.js"),
    ],
    { stdio: "inherit" },
  );
} finally {
  rmSync(temporary, { recursive: true, force: true });
}
