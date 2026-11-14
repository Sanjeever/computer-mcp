import { describe, expect, it } from "vitest";
import { join } from "node:path";
import { binaryPath, platformPackage } from "./launcher.js";

describe("platform selection", () => {
  it.each([
    ["darwin", "arm64"],
    ["win32", "x64"],
  ])("selects %s/%s", (platform, arch) => {
    expect(platformPackage(platform, arch)).toBe(`@sanjeever/computer-mcp-${platform}-${arch}`);
    const path = binaryPath(platform, arch, (id) => {
      expect(id).toBe(`@sanjeever/computer-mcp-${platform}-${arch}/package.json`);
      return join("installed", "package.json");
    });
    expect(path).toBe(
      join("installed", "bin", platform === "win32" ? "computer-mcp.exe" : "computer-mcp"),
    );
  });
  it.each([
    ["darwin", "x64"],
    ["win32", "arm64"],
    ["linux", "x64"],
  ])("rejects %s/%s", (platform, arch) => {
    expect(() => platformPackage(platform, arch)).toThrow("Unsupported platform");
  });
  it("explains missing optional packages", () => {
    expect(() =>
      binaryPath("darwin", "arm64", () => {
        throw new Error("MODULE_NOT_FOUND");
      }),
    ).toThrow("optional dependencies");
  });
});
