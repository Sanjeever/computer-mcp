import { build } from "esbuild";
import { chmod } from "node:fs/promises";

await build({
  entryPoints: ["src/cli.ts"],
  outfile: "dist/cli.js",
  platform: "node",
  target: "node22",
  format: "esm",
  bundle: true,
  banner: { js: "#!/usr/bin/env node" },
});
await chmod("dist/cli.js", 0o755);
