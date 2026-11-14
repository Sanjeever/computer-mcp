import { execFileSync } from "node:child_process";
import {
  chmodSync,
  copyFileSync,
  mkdirSync,
  readFileSync,
  readdirSync,
  writeFileSync,
} from "node:fs";
import { join } from "node:path";
import { platformPackage } from "../src/launcher.js";

const name = platformPackage(process.platform, process.arch);
const root = JSON.parse(readFileSync("package.json", "utf8")) as {
  version: string;
  repository: object;
  optionalDependencies: Record<string, string>;
};
if (Object.values(root.optionalDependencies).some((version) => version !== root.version))
  throw new Error("Platform versions must match root version");
if (process.env.GITHUB_REF_TYPE === "tag" && process.env.GITHUB_REF_NAME !== `v${root.version}`)
  throw new Error("Tag and package version differ");
const folder = `build/package-${process.platform}-${process.arch}`;
mkdirSync(join(folder, "bin"), { recursive: true });
const filename = `computer-mcp${process.platform === "win32" ? ".exe" : ""}`;
copyFileSync(join("build", filename), join(folder, "bin", filename));
chmodSync(join(folder, "bin", filename), 0o755);
copyFileSync("LICENSE", join(folder, "LICENSE"));
writeFileSync(
  join(folder, "package.json"),
  JSON.stringify(
    {
      name,
      version: root.version,
      description: `Native ${process.platform}/${process.arch} backend for Computer MCP`,
      license: "MIT",
      repository: root.repository,
      os: [process.platform],
      cpu: [process.arch],
      files: ["bin/", "LICENSE", "THIRD_PARTY_NOTICES.txt"],
      publishConfig: { access: "public" },
    },
    null,
    2,
  ) + "\n",
);
const modules = execFileSync(
  "go",
  [
    "list",
    "-deps",
    "-f",
    "{{with .Module}}{{if not .Main}}{{.Path}}\t{{.Dir}}{{end}}{{end}}",
    "./cmd/computer-mcp",
  ],
  { encoding: "utf8" },
);
const notices: string[] = [];
for (const line of new Set(modules.trim().split(/\r?\n/))) {
  if (!line.trim()) continue;
  const [module, dir] = line.split("\t");
  if (!dir) throw new Error(`Missing module directory: ${module}`);
  const licenses = readdirSync(dir).filter((file) =>
    /^(license|copying|notice)([.-]|$)/i.test(file),
  );
  if (!licenses.length) throw new Error(`Missing license: ${module}`);
  for (const file of licenses)
    notices.push(`${module} — ${file}\n\n${readFileSync(join(dir, file), "utf8")}`);
}
writeFileSync(join(folder, "THIRD_PARTY_NOTICES.txt"), notices.join("\n\n-----\n\n"));
mkdirSync("artifacts", { recursive: true });
execFileSync(
  process.platform === "win32" ? "npm.cmd" : "npm",
  ["pack", `./${folder}`, "--pack-destination", "artifacts", "--ignore-scripts"],
  { stdio: "inherit", shell: process.platform === "win32" },
);
