# Development and release

[简体中文](development.zh-CN.md)

This guide covers local builds, verification, architecture decisions, and release operations. Supported systems and desktop permissions are defined in the [README](../README.md).

Use the Go version in [go.mod](../go.mod) and the pnpm version in [package.json](../package.json). Use the latest patch of the Node.js major selected by [CI](../.github/workflows/ci.yml) for development. The launcher's `engines` field describes the published package. Build on the target OS and architecture. macOS requires Xcode Command Line Tools. Windows uses the Go toolchain without cgo.

```sh
pnpm install --frozen-lockfile
pnpm build
pnpm build:native
pnpm smoke
```

The launcher build writes `dist/cli.js`. The native build writes `build/computer-mcp` on macOS or `build/computer-mcp.exe` on Windows. The [pnpm workspace configuration](../pnpm-workspace.yaml) excludes optional platform packages from source installs so development works before those packages are published.

To check installed packages locally after building:

```sh
pnpm pack:platform
npm pack --pack-destination artifacts --ignore-scripts
pnpm pack:check
```

Use freshly built launcher and host platform tarballs in `artifacts/`. `pack:check` installs them offline in a temporary directory and runs the same stdio smoke check through the installed launcher. `smoke` negotiates MCP, checks the tool count and read-only status, then closes stdin and waits for the process within the smoke deadline. It sends no desktop input.

## Architecture decisions

The backend uses one locked OS thread to preserve UIA COM lifetimes. Provider calls can outlive the request deadline, so the worker retains the per-user lock until the native call returns to prevent an overlapping operation. See the [request limits](../README.md#limits-and-behavior).

`computer_batch` uses one job and one desktop lock. It plans image-coordinate transforms before the first stroke, consumes the public snapshot at dispatch, and keeps the transform local to the job. Each stroke still has its own target and path checks and mouse button lifetime. The checks can identify changes to window geometry and occlusion. Changes to the brush or canvas inside the window are application state that the caller should confirm between batches.

The batch publishes completed strokes and the current dispatch state before it enters a potentially blocking provider. A timeout response can include those receipts while the worker still holds the desktop lock. Publishing the next stroke as uncertain before the last cancellation check prevents a canceled caller from seeing `not_sent` while dispatch can still proceed.

Window and element IDs follow native identity to keep title edits and traversal order changes from retargeting input. macOS preserves window mappings across temporary enumeration gaps and retains AX identity when a screen-window match is ambiguous.

Window capture and screen capture are explicit choices because an unobscured window image and the visible input target can differ. OCR text from screen capture may belong to an overlapping window, so it stays separate from native controls. The Windows npm distribution has no MSIX package identity, so OCR is currently available through macOS Vision. See the [tool contract](../README.md#tools).

Windows element clicks use UIA bounds in physical desktop pixels. For a hit in another root window, the native backend verifies its same-process owner chain and checks that it contains the observed element's bounds. UIA hit testing matches the requested element in the control view and can follow an icon or text child to its control. The owned-popup fixture exercises a control beyond the main window, an unowned covering window, and a covering control inside the popup.

Windows follow-up checks the foreground GUI thread's menu state for native menus and the owner chain for titleless foreground popups. A screen capture stays within the verified menu bounds. The server checks menu identity and geometry again after observation and before using the menu snapshot for input.

## Verification

[CI](../.github/workflows/ci.yml) defines the full check sequence and uploads the tarballs after the package smoke checks. Use its macOS deployment environment when running Go tests or vet directly. [`build:native`](../scripts/native.ts) sets the deployment target for native builds.

The [server tests](../internal/server/) use a fake backend for desktop-dependent contract checks. The [catalog tests](../internal/i18n/i18n_test.go) check key parity, and the [launcher tests](../src/launcher.test.ts) cover package selection and missing dependencies. Native interaction coverage is maintained with the [macOS desktop](../internal/desktop/integration_darwin_test.go), [macOS server](../internal/server/coordinates_darwin_test.go), [Windows desktop](../internal/desktop/integration_windows_test.go), and [Windows capture](../internal/desktop/capture_windows_test.go) fixtures.

Native UI acceptance should use a dedicated test window in an interactive session. Verify Unicode text, click, keyboard selection, scrolling, dragging, screenshots, focus rejection, and stale references. Repeat at multiple scaling factors and with secondary monitors before claiming those configurations are validated. Do not automate input into personal applications during tests.

On macOS, use the CI deployment environment and run the fixture packages serially.

```sh
COMPUTER_MCP_NATIVE_TEST=1 go test -p 1 -v ./internal/desktop ./internal/server
```

On Windows, run the desktop fixture package in an interactive session.

```powershell
$env:COMPUTER_MCP_NATIVE_TEST = "1"
go test -p 1 -v ./internal/desktop
Remove-Item Env:COMPUTER_MCP_NATIVE_TEST
```

Keep the desktop idle so the fixture windows retain focus. Windows fixture setup can click its own verified edit control to acquire foreground access. macOS fixtures report missing Accessibility or Screen Recording permissions as skips. Hosted CI leaves interactive fixtures disabled and does not verify permission attribution or signed upgrades.

The macOS `stdio drawing batch` case sends the same eight strokes and 256 points through sequential `computer_drag` calls with `observe_after` and through one `computer_batch` call. It verifies order and stroke boundaries from fixture events and logs calls, JPEGs, and elapsed time after the initial observation. `TestNativeWindowsDrawingBatch` checks independent press, drag, and release sequences and the follow-up JPEG through the Windows stdio server.

On a Windows test machine without the Go toolchain, copy the desktop test executable from `go test -c ./internal/desktop` and a native server built from the same checkout. Set `COMPUTER_MCP_TEST_BINARY` to the server's absolute path before running the fixture executable. The follow-up observation fixtures exercise an owned dialog, a Win32 context menu, and a titleless owned popup through the stdio server.

## Release

The root [package.json](../package.json) version is authoritative. Native builds embed it, and platform packaging checks the `optionalDependencies` versions against it. Update these entries together, then run `pnpm install --lockfile-only`. Commit the manifest and lockfile, along with any affected bilingual documentation.

[Release](../.github/workflows/release.yml) supports two entry points.

- Push a `vX.Y.Z` tag to build, verify, publish the three npm packages, and create a GitHub Release.
- Run the Release workflow manually with `publish=false` to build and upload artifacts without publishing. With `publish=true`, select a matching version tag.

The tag must match `package.json`. The workflow publishes native packages before the launcher using the CI tarballs. npm versions are immutable. Check which packages were published before rerunning a failed publishing job.

Before the first public release, establish package ownership for `name` and `optionalDependencies` in [package.json](../package.json). Initial package creation may require an authenticated bootstrap publication. Configure npm trusted publishing for the repository and [release workflow](../.github/workflows/release.yml), using the environment declared by the publish job. Create that GitHub environment and set its protection rules. The workflow uses OIDC and npm provenance.

The [platform packaging script](../scripts/platform.ts) includes third-party license notices with the precompiled binaries. The release workflow currently omits Apple Developer ID signing and notarization and Windows Authenticode signing. Verify permission persistence across signed upgrades before documenting it as supported.
