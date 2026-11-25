import { execFileSync } from "node:child_process";
import { accessSync, constants, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { platformPackage } from "../src/launcher.js";

const artifacts = resolve("artifacts");
const { name, version } = JSON.parse(readFileSync("package.json", "utf8")) as {
  name: string;
  version: string;
};
const native = platformPackage(process.platform, process.arch);
const tarball = (packageName: string) =>
  join(artifacts, `${packageName.slice(1).replace("/", "-")}-${version}.tgz`);
const temporary = mkdtempSync(join(tmpdir(), "computer-mcp-pack-"));
const windows = process.platform === "win32";
const npm = windows ? "npm.cmd" : "npm";
try {
  writeFileSync(join(temporary, "package.json"), JSON.stringify({ private: true }));
  execFileSync(
    npm,
    [
      "install",
      tarball(native),
      tarball(name),
      "--offline",
      "--ignore-scripts",
      "--no-audit",
      "--no-fund",
      "--cache",
      join(temporary, "cache"),
    ],
    { cwd: temporary, stdio: "inherit", shell: windows },
  );
  const installed = join(temporary, "node_modules");
  const executable = join(installed, ".bin", windows ? "computer-mcp.cmd" : "computer-mcp");
  accessSync(executable, windows ? constants.F_OK : constants.X_OK);
  execFileSync(
    process.execPath,
    [
      "--import=tsx",
      "scripts/smoke.ts",
      windows ? join(installed, name, "dist", "cli.js") : executable,
      version,
    ],
    { stdio: "inherit" },
  );
} finally {
  rmSync(temporary, { recursive: true, force: true });
}
