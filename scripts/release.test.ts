import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { basename, join } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { artifactManifest, publish, releasePlan } from "./release.js";

const name = "@sanjeever/computer-mcp";
const native = `${name}-darwin-arm64`;
const windows = `${name}-win32-x64`;
const version = "1.0.0";
const tag = `v${version}`;
const success = (data?: unknown) => ({ status: 0, stdout: JSON.stringify(data) ?? "", stderr: "" });
const failure = (code: string) => ({
  status: 1,
  stdout: JSON.stringify({ error: { code } }),
  stderr: code,
});

function prepare(root: string) {
  mkdirSync(join(root, "docs/releases"), { recursive: true });
  writeFileSync(
    join(root, "package.json"),
    JSON.stringify({
      name,
      version,
      optionalDependencies: { [native]: version, [windows]: version },
    }),
  );
  writeFileSync(join(root, `docs/releases/${version}.md`), "Release notes");
  writeFileSync(join(root, `docs/releases/${version}.zh-CN.md`), "版本日志");
  const plan = releasePlan(root, tag);
  for (const pkg of plan.names) {
    writeFileSync(join(root, `${pkg.slice(1).replace("/", "-")}-${version}.tgz`), pkg);
  }
  const manifest = artifactManifest(plan, root);
  writeFileSync(join(root, "release-manifest.json"), JSON.stringify(manifest));
  return { plan, packages: manifest.packages };
}

describe("tag release", () => {
  let root: string;
  beforeEach(() => {
    root = mkdtempSync(join(tmpdir(), "computer-mcp-release-"));
  });
  afterEach(() => rmSync(root, { recursive: true, force: true }));

  it("rejects mismatched tags and requires both notes", () => {
    prepare(root);
    expect(() => releasePlan(root, "v2.0.0")).toThrow("Tag must match");
    writeFileSync(join(root, `docs/releases/${version}.zh-CN.md`), "\n");
    expect(() => releasePlan(root, tag)).toThrow("Release notes are empty");
  });

  it("checks the assembled bytes before consulting remote services", () => {
    const { plan, packages } = prepare(root);
    writeFileSync(join(root, packages[0].filename), "changed");
    const execute = vi.fn(() => success());
    expect(() => publish(plan, root, execute)).toThrow("differ from the assembled manifest");
    expect(execute).not.toHaveBeenCalled();
  });

  it.each(["E503", "different integrity"])("stops before writes on %s", (state) => {
    const { plan } = prepare(root);
    const result = state === "E503" ? failure(state) : success(state);
    const execute = vi.fn(() => result);
    expect(() => publish(plan, root, execute)).toThrow(/lookup failed|integrity mismatch/);
    expect(execute).toHaveBeenCalledTimes(1);
  });

  it("resumes a partial npm publish from the original draft and packages", () => {
    const { plan, packages } = prepare(root);
    const registry = new Map<string, string>();
    const draft = {
      id: 42,
      tag_name: tag,
      draft: true,
      assets: [] as { name: string; digest: string; state: string }[],
    };
    const writes: string[] = [];
    let exists = false;
    let blocked = true;
    const execute = (command: string, args: string[]) => {
      const [action, target] = args;
      if (command === "gh" && action === "api") return success([exists ? [draft] : []]);
      if (command === "gh" && action === "release") {
        writes.push(target);
        if (target === "create") exists = true;
        if (target === "upload") {
          expect(args.slice(3).map((path) => basename(path))).toEqual(
            packages.map(({ filename }) => filename),
          );
          draft.assets = packages.map(({ filename, digest }) => ({
            name: filename,
            digest,
            state: "uploaded",
          }));
        }
        if (target === "edit") draft.draft = false;
        return success();
      }
      if (command === "npm" && action === "view") {
        const integrity = registry.get(target);
        return integrity ? success(integrity) : failure("E404");
      }
      if (command === "npm" && action === "publish") {
        const pkg = packages.find(({ filename }) => filename === basename(target));
        if (!pkg) throw new Error(`Unexpected package: ${target}`);
        writes.push(pkg.name);
        if (blocked && pkg.name === windows) return failure("E403");
        registry.set(`${pkg.name}@${version}`, pkg.integrity);
        return success();
      }
      throw new Error(`Unexpected call: ${command} ${args.join(" ")}`);
    };

    expect(() => publish(plan, root, execute)).toThrow("npm publish failed");
    expect(writes).toEqual(["create", "upload", native, windows]);
    expect(draft.draft).toBe(true);

    blocked = false;
    publish(plan, root, execute);
    expect(writes).toEqual(["create", "upload", native, windows, windows, name, "edit"]);
    expect(draft.draft).toBe(false);

    writes.length = 0;
    publish(plan, root, execute);
    expect(writes).toEqual([]);
  });
});
