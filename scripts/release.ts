import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { readFileSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repository = "Sanjeever/computer-mcp";
const registry = "--registry=https://registry.npmjs.org";
const manifestFilename = "release-manifest.json";

export function releasePlan(root: string, tag: string) {
  const { name, version, optionalDependencies } = JSON.parse(
    readFileSync(join(root, "package.json"), "utf8"),
  ) as { name: string; version: string; optionalDependencies: Record<string, string> };
  assert.match(version, /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/, "Expected a stable version");
  assert.equal(tag, `v${version}`, "Tag must match the root version");
  const native = [`${name}-darwin-arm64`, `${name}-win32-x64`];
  assert.deepEqual(optionalDependencies, Object.fromEntries(native.map((pkg) => [pkg, version])));
  const notes = join(root, `docs/releases/${version}.md`);
  for (const path of [notes, join(root, `docs/releases/${version}.zh-CN.md`)]) {
    assert.ok(readFileSync(path, "utf8").trim(), `Release notes are empty: ${path}`);
  }
  return { tag, version, notes, names: [...native, name] };
}

type ReleasePlan = ReturnType<typeof releasePlan>;

export function artifactManifest(plan: ReleasePlan, directory: string) {
  const packages = plan.names.map((name) => {
    const filename = `${name.slice(1).replace("/", "-")}-${plan.version}.tgz`;
    const bytes = readFileSync(join(directory, filename));
    return {
      name,
      filename,
      integrity: `sha512-${createHash("sha512").update(bytes).digest("base64")}`,
      digest: `sha256:${createHash("sha256").update(bytes).digest("hex")}`,
    };
  });
  return { version: plan.version, packages };
}

export function verifyArtifacts(plan: ReleasePlan, directory: string) {
  const manifest = artifactManifest(plan, directory);
  assert.deepEqual(
    JSON.parse(readFileSync(join(directory, manifestFilename), "utf8")),
    manifest,
    "Release artifacts differ from the assembled manifest",
  );
  return manifest;
}

type CommandResult = { status: number | null; stdout: string; stderr: string };
type Run = (command: string, args: string[]) => CommandResult;

const run: Run = (command, args) => {
  const result = spawnSync(command, args, { encoding: "utf8" });
  if (result.error) throw result.error;
  return result;
};

function checked(command: string, args: string[], execute: Run) {
  const result = execute(command, args);
  if (result.status !== 0) {
    throw new Error(`${command} ${args[0]} failed (${result.status}): ${result.stderr.trim()}`);
  }
  return result.stdout;
}

function published(name: string, version: string, integrity: string, execute: Run) {
  const spec = `${name}@${version}`;
  const result = execute("npm", [
    "view",
    spec,
    "dist.integrity",
    "--json",
    "--prefer-online",
    registry,
  ]);
  if (result.status === 1 && result.stdout) {
    const response = JSON.parse(result.stdout) as { error?: { code?: string } } | null;
    if (response?.error?.code === "E404") return false;
  }
  if (result.status !== 0)
    throw new Error(`npm lookup failed for ${spec}: ${result.stderr.trim()}`);
  assert.equal(JSON.parse(result.stdout), integrity, `npm integrity mismatch for ${spec}`);
  return true;
}

type GitHubRelease = {
  id: number;
  tag_name: string;
  draft: boolean;
  assets: { name: string; digest: string | null; state: string }[];
};

export function publish(plan: ReleasePlan, directory: string, execute: Run = run) {
  const { packages } = verifyArtifacts(plan, directory);
  const present = packages.map((pkg) => published(pkg.name, plan.version, pkg.integrity, execute));

  // The list endpoint includes drafts for the workflow's write token.
  const releases = JSON.parse(
    checked(
      "gh",
      ["api", "--paginate", "--slurp", `repos/${repository}/releases?per_page=100`],
      execute,
    ),
  ) as GitHubRelease[][];
  const matches = releases.flat().filter((release) => release.tag_name === plan.tag);
  assert.ok(matches.length <= 1, `Multiple GitHub Releases match ${plan.tag}`);
  const existing = matches[0];
  const missing = packages.filter((pkg) => {
    const asset = existing?.assets.find(({ name }) => name === pkg.filename);
    if (!asset) return true;
    assert.equal(asset.state, "uploaded", `Incomplete GitHub asset: ${pkg.filename}`);
    assert.equal(asset.digest, pkg.digest, `GitHub asset digest mismatch: ${pkg.filename}`);
    return false;
  });
  if (existing && !existing.draft) {
    assert.ok(
      missing.length === 0 && present.every(Boolean),
      `Incomplete public release ${plan.tag}`,
    );
    console.log(`${plan.tag} is already public and all artifacts match`);
    return;
  }
  if (!existing) {
    checked(
      "gh",
      [
        "release",
        "create",
        plan.tag,
        "--verify-tag",
        "--draft",
        "--title",
        plan.tag,
        "--notes-file",
        plan.notes,
      ],
      execute,
    );
  }
  if (missing.length) {
    checked(
      "gh",
      ["release", "upload", plan.tag, ...missing.map((pkg) => join(directory, pkg.filename))],
      execute,
    );
  }

  for (const [index, pkg] of packages.entries()) {
    if (present[index]) {
      console.log(`Already published with matching integrity: ${pkg.name}@${plan.version}`);
      continue;
    }
    checked(
      "npm",
      [
        "publish",
        join(directory, pkg.filename),
        "--access",
        "public",
        "--tag",
        "latest",
        "--provenance",
        "--ignore-scripts",
        registry,
      ],
      execute,
    );
    console.log(`Published ${pkg.name}@${plan.version}`);
  }
  checked("gh", ["release", "edit", plan.tag, "--draft=false", "--latest"], execute);
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const [command, tag, directory = "artifacts", ...extra] = process.argv.slice(2);
  assert.ok(
    tag && extra.length === 0,
    "Usage: release.ts check|assemble|verify|publish <vX.Y.Z> [directory]",
  );
  const plan = releasePlan(".", tag);
  if (command === "assemble") {
    const manifest = artifactManifest(plan, directory);
    writeFileSync(join(directory, manifestFilename), JSON.stringify(manifest, null, 2) + "\n");
  } else if (command === "verify") {
    verifyArtifacts(plan, directory);
  } else if (command === "publish") {
    assert.equal(process.env.GITHUB_ACTIONS, "true", "Publish must run in GitHub Actions");
    assert.equal(process.env.GITHUB_REF_TYPE, "tag");
    assert.equal(process.env.GITHUB_REF_NAME, tag);
    assert.equal(process.env.GH_REPO, repository);
    assert.ok(process.env.GH_TOKEN, "The workflow GitHub token is required");
    publish(plan, resolve(directory));
  } else {
    assert.equal(command, "check", "Unknown release command");
  }
}
