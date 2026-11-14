import { spawn } from "node:child_process";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { existsSync } from "node:fs";

export function platformPackage(platform: string, arch: string): string {
  if (!((platform === "darwin" && arch === "arm64") || (platform === "win32" && arch === "x64"))) {
    throw new Error(
      `Unsupported platform: ${platform}/${arch}. Supported: macOS arm64, Windows x64. / 不支持此平台。`,
    );
  }
  return `@sanjeever/computer-mcp-${platform}-${arch}`;
}

export function binaryPath(
  platform: string,
  arch: string,
  resolve: (id: string) => string,
): string {
  const name = platformPackage(platform, arch);
  let manifest: string;
  try {
    manifest = resolve(`${name}/package.json`);
  } catch (cause) {
    throw new Error(
      `Missing ${name}. Reinstall with optional dependencies enabled. / 缺少平台包，请启用可选依赖后重新安装。`,
      { cause },
    );
  }
  return join(dirname(manifest), "bin", platform === "win32" ? "computer-mcp.exe" : "computer-mcp");
}

export function launch(): void {
  const require = createRequire(import.meta.url);
  const binary = binaryPath(process.platform, process.arch, require.resolve);
  if (!existsSync(binary)) throw new Error(`Native executable missing / 原生程序不存在: ${binary}`);
  const child = spawn(binary, process.argv.slice(2), { stdio: "inherit", windowsHide: true });
  const forward = (signal: NodeJS.Signals) => {
    child.kill(signal);
  };
  const onInt = () => forward("SIGINT");
  const onTerm = () => forward("SIGTERM");
  const onExit = () => {
    child.kill();
  };
  process.on("SIGINT", onInt);
  process.on("SIGTERM", onTerm);
  process.on("exit", onExit);
  child.once("error", (error) => {
    console.error(error.message);
    process.exitCode = 1;
  });
  child.once("close", (code, signal) => {
    process.off("SIGINT", onInt);
    process.off("SIGTERM", onTerm);
    process.off("exit", onExit);
    process.exitCode = code ?? (signal === "SIGINT" ? 130 : 143);
  });
}
