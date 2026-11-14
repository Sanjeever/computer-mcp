import { execFileSync } from "node:child_process";
import { mkdirSync, readFileSync } from "node:fs";
import { platformPackage } from "../src/launcher.js";

platformPackage(process.platform, process.arch);
const { version } = JSON.parse(readFileSync("package.json", "utf8")) as { version: string };
if (!/^\d+\.\d+\.\d+(?:-[a-zA-Z0-9.-]+)?$/.test(version))
  throw new Error("Invalid package version");
mkdirSync("build", { recursive: true });
execFileSync(
  "go",
  [
    "build",
    "-trimpath",
    "-ldflags",
    `-s -w -X main.version=${version}`,
    "-o",
    `build/computer-mcp${process.platform === "win32" ? ".exe" : ""}`,
    "./cmd/computer-mcp",
  ],
  {
    stdio: "inherit",
    env: {
      ...process.env,
      ...(process.platform === "darwin"
        ? {
            MACOSX_DEPLOYMENT_TARGET: "14.0",
            CGO_CFLAGS: `${process.env.CGO_CFLAGS ?? ""} -mmacosx-version-min=14.0`,
            CGO_LDFLAGS: `${process.env.CGO_LDFLAGS ?? ""} -mmacosx-version-min=14.0`,
          }
        : {}),
    },
  },
);
